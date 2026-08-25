package league

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const avatarTestEmail = "manager@example.com"

var avatarManagerActor = seatActor{email: avatarTestEmail}

var errInjectedLoad = errors.New("injected load failure")

type persistCallerID struct {
	file     string
	receiver string
	function string
}

type persistCallSite struct {
	id   persistCallerID
	fn   *ast.FuncDecl
	call *ast.CallExpr
}

func TestStorePersistMutatorsHaveEarlyWriteGate(t *testing.T) {
	_, testFile, _, _ := runtime.Caller(0)
	sourceDir := filepath.Dir(testFile)
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	var sourcePaths []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		sourcePaths = append(sourcePaths, filepath.Join(sourceDir, entry.Name()))
	}
	if len(sourcePaths) == 0 {
		t.Fatalf("no non-test Go sources found in %s", sourceDir)
	}
	fileSet := token.NewFileSet()

	expected := expectedStorePersistMutators()
	lockedExpected := expectedStoreLockedPersistMutators()
	special := persistCallerID{file: "store.go", receiver: "(*Store)", function: "mutateAvatarIdentity"}
	var sites []persistCallSite

	for _, sourcePath := range sourcePaths {
		file, err := parser.ParseFile(fileSet, sourcePath, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", sourcePath, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "persistLocked" {
					sites = append(sites, persistCallSite{
						id:   persistCallerID{file: filepath.Base(sourcePath), receiver: funcReceiverID(fn), function: fn.Name.Name},
						fn:   fn,
						call: call,
					})
				}
				return true
			})
		}
	}

	byCaller := map[persistCallerID][]persistCallSite{}
	for _, site := range sites {
		byCaller[site.id] = append(byCaller[site.id], site)
	}
	for id, callerSites := range byCaller {
		switch {
		case id == special:
			validateMutateAvatarIdentityPersistCaller(t, callerSites[0].fn, callerSites)
		case hasPersistCaller(expected, id):
			validateOrdinaryPersistCaller(t, callerSites[0].fn, callerSites)
		case hasPersistCaller(lockedExpected, id):
			validateLockedPersistCaller(t, callerSites[0].fn, callerSites)
		default:
			t.Errorf("unexpected persistLocked caller %s", formatPersistCaller(id))
		}
	}
	for id := range expected {
		if _, ok := byCaller[id]; !ok {
			t.Errorf("expected persistLocked caller %s was not discovered", formatPersistCaller(id))
		}
	}
	for id := range lockedExpected {
		if _, ok := byCaller[id]; !ok {
			t.Errorf("expected locked persistLocked caller %s was not discovered", formatPersistCaller(id))
		}
	}
	if got := len(byCaller[special]); got != 1 {
		t.Errorf("special persistLocked caller %s discovered %d times, want exactly once", formatPersistCaller(special), got)
	}
}

func expectedStorePersistMutators() map[persistCallerID]struct{} {
	names := []string{
		"ToggleReady", "SetReady", "MakePick",
		"ArmClock", "StartDraft", "PauseClock", "ResumeClock", "extendClock",
		"SetClockDuration", "ClearClock", "SetAutopick", "SetAutopickIfClaimed",
		"AssignMember", "EnsureMember", "AddInvite", "RemoveInvite",
		"releaseSeat", "InviteCoManager", "BindCoManager", "DetachCoManager",
		"ResetDraft", "ResetLeague", "SetDraftAtOverride", "SetTeamName", "SetDraftOrder", "DrawDraftOrder",
		"TrimUnclaimedSeatsConfirmed", "SetScoringValue", "ResetScoring",
		"BoardAdd", "BoardMove", "BoardMoveTo", "BoardRemove", "BoardClear",
		"SetPickem", "BackfillPickemEnteredAt", "BlitzSetEntry", "FirstSend", "FirstSendBatch",
		"PruneSentLog", "SetNotifyPref", "SetSchedule", "SetScheduleWeek",
		"SetScheduleWeekWithLineups", "SetPhase", "SetPlayoffs",
		"SetRosterOverride", "ClearRosterOverride", "SetLineupSlot",
		"SetLineupWeek", "recordTransactionWithAuthority", "BaselineWaiversProcessedThrough",
		"fileClaimWithAuthority", "CancelClaim", "MoveClaim", "ProcessWaivers", "ProposeTradeOffer",
		"CounterTradeOffer", "DeclineTradeOffer", "WithdrawTradeOffer",
		"AcceptTradeOffer", "ExecuteTradeOffer", "CommissionerVetoTradeOffer",
		"FileTradeVetoOffer", "ExpireTradeOffer", "PostAnnouncement",
		"DeleteAnnouncement",
	}
	expected := make(map[persistCallerID]struct{}, len(names)+4)
	for _, name := range names {
		expected[persistCallerID{file: "store.go", receiver: "(*Store)", function: name}] = struct{}{}
	}
	expected[persistCallerID{file: "pickem_market.go", receiver: "(*Store)", function: "ReconcilePickemMarkets"}] = struct{}{}
	for _, name := range []string{"PlaceInZone", "ClearZone", "activateFromIRWithDropAuthority", "AutoCutHealedIR"} {
		expected[persistCallerID{file: "zones.go", receiver: "(*Store)", function: name}] = struct{}{}
	}
	return expected
}

func expectedStoreLockedPersistMutators() map[persistCallerID]struct{} {
	locked := make(map[persistCallerID]struct{}, 2)
	for _, name := range []string{"undoLastPickLocked", "autoPickLocked"} {
		locked[persistCallerID{file: "store.go", receiver: "(*Store)", function: name}] = struct{}{}
	}
	return locked
}

func hasPersistCaller(callers map[persistCallerID]struct{}, id persistCallerID) bool {
	_, ok := callers[id]
	return ok
}

func formatPersistCaller(id persistCallerID) string {
	return id.file + ":" + id.receiver + "." + id.function
}

func funcReceiverID(fn *ast.FuncDecl) string {
	if fn.Recv == nil {
		return "<free>"
	}
	if len(fn.Recv.List) != 1 {
		return "<receiver>"
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return "<other>"
	}
	typeName, ok := star.X.(*ast.Ident)
	if !ok || typeName.Name != "Store" {
		return "<other>"
	}
	return "(*Store)"
}

func validateOrdinaryPersistCaller(t *testing.T, fn *ast.FuncDecl, sites []persistCallSite) {
	t.Helper()
	name := fn.Name.Name
	if fn.Recv == nil || funcReceiverID(fn) != "(*Store)" || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Recv.List[0].Names[0].Name != "s" {
		t.Errorf("%s must be an exact (*Store) method with receiver s", name)
	}
	for _, site := range sites {
		if !isSMethodCall(site.call, "persistLocked") {
			t.Errorf("%s calls persistLocked through a receiver other than s", name)
		}
	}
	validatePersistErrors(t, fn, name)
	writeErrors := writeErrorCalls(fn)
	if len(writeErrors) != 1 {
		t.Errorf("%s has %d s.writeErrorLocked calls; want exactly one checked gate", name, len(writeErrors))
	}
	lockIndex := -1
	gateIndex := -1
	var gateCall *ast.CallExpr
	for index, stmt := range fn.Body.List {
		if lockIndex == -1 && isStoreMuCall(stmt, "Lock") {
			lockIndex = index
		}
		if gateIndex == -1 {
			if call, ok := checkedWriteErrorGate(fn, stmt, false); ok {
				gateIndex = index
				gateCall = call
			}
		}
	}
	if lockIndex < 0 || gateIndex < 2 || gateIndex != lockIndex+2 || !isStoreMuUnlockDefer(fn.Body.List[gateIndex-1]) {
		t.Errorf("%s must acquire s.mu, defer its unlock, and immediately check writeErrorLocked", name)
		return
	}
	if gateCall == nil || !containsCall(writeErrors, gateCall) {
		t.Errorf("%s gate must check the s.writeErrorLocked call", name)
	}
	if firstStatePos := firstStoreStatePos(fn); firstStatePos != 0 && gateCall != nil && gateCall.Pos() > firstStatePos {
		t.Errorf("%s accesses s.state before its checked write gate", name)
	}
}

// validateLockedPersistCaller covers helpers whose caller owns s.mu. They
// still need the same checked write gate before touching state or persisting;
// the lock itself is asserted by each public wrapper that enters them.
func validateLockedPersistCaller(t *testing.T, fn *ast.FuncDecl, sites []persistCallSite) {
	t.Helper()
	name := fn.Name.Name
	if fn.Recv == nil || funcReceiverID(fn) != "(*Store)" || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Recv.List[0].Names[0].Name != "s" {
		t.Errorf("%s must be an exact (*Store) method with receiver s", name)
	}
	for _, site := range sites {
		if !isSMethodCall(site.call, "persistLocked") {
			t.Errorf("%s calls persistLocked through a receiver other than s", name)
		}
	}
	validatePersistErrors(t, fn, name)
	writeErrors := writeErrorCalls(fn)
	if len(writeErrors) != 1 {
		t.Errorf("%s has %d s.writeErrorLocked calls; want exactly one checked gate", name, len(writeErrors))
	}
	if len(fn.Body.List) == 0 {
		t.Errorf("%s has no body for its checked write gate", name)
		return
	}
	gateCall, ok := checkedWriteErrorGate(fn, fn.Body.List[0], false)
	if !ok {
		t.Errorf("%s must immediately check writeErrorLocked before touching state", name)
		return
	}
	if gateCall == nil || !containsCall(writeErrors, gateCall) {
		t.Errorf("%s gate must check the s.writeErrorLocked call", name)
	}
	if firstStatePos := firstStoreStatePos(fn); firstStatePos != 0 && gateCall.Pos() > firstStatePos {
		t.Errorf("%s accesses s.state before its checked write gate", name)
	}
}

func validateMutateAvatarIdentityPersistCaller(t *testing.T, fn *ast.FuncDecl, sites []persistCallSite) {
	t.Helper()
	name := fn.Name.Name
	if fn.Recv == nil || funcReceiverID(fn) != "(*Store)" || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Recv.List[0].Names[0].Name != "s" || name != "mutateAvatarIdentity" {
		t.Errorf("%s must be the exact (*Store).mutateAvatarIdentity helper", name)
	}
	for _, site := range sites {
		if !isSMethodCall(site.call, "persistLocked") {
			t.Errorf("%s calls persistLocked through a receiver other than s", name)
		}
	}
	if len(sites) != 1 {
		t.Errorf("%s has %d persistLocked calls; want exactly one", name, len(sites))
	}
	validatePersistErrors(t, fn, name)
	writeErrors := writeErrorCalls(fn)
	if len(writeErrors) != 2 {
		t.Errorf("%s has %d s.writeErrorLocked calls; want exactly two gates", name, len(writeErrors))
	}
	if len(fn.Body.List) < 2 || !isStoreMuCall(fn.Body.List[0], "RLock") {
		t.Errorf("%s must begin with s.mu.RLock", name)
		return
	}
	preflight, preflightOK := checkedWriteErrorGate(fn, fn.Body.List[1], true)
	if !preflightOK || !gateBodyUnlocksReadLock(fn.Body.List[1]) {
		t.Errorf("%s must use an exact checked RLock preflight gate", name)
	}
	lockIndex := -1
	finalIndex := -1
	var finalGate *ast.CallExpr
	for index, stmt := range fn.Body.List {
		if index > 1 && lockIndex == -1 && isStoreMuCall(stmt, "Lock") {
			lockIndex = index
		}
		if index > 1 && finalIndex == -1 {
			if call, ok := checkedWriteErrorGate(fn, stmt, false); ok {
				finalIndex = index
				finalGate = call
			}
		}
	}
	if lockIndex < 0 || finalIndex < 2 || finalIndex != lockIndex+2 || !isStoreMuUnlockDefer(fn.Body.List[finalIndex-1]) {
		t.Errorf("%s must acquire s.mu, defer its unlock, and immediately check its final write gate", name)
		return
	}
	if preflight == nil || finalGate == nil || preflight == finalGate {
		t.Errorf("%s must have distinct preflight and final write gates", name)
	}
	if len(sites) != 1 || finalGate == nil || finalGate.Pos() >= sites[0].call.Pos() {
		t.Errorf("%s must run its persistLocked call only after the final write gate", name)
	}
	if countStoreMuCalls(fn, "RUnlock") != 2 {
		t.Errorf("%s must release its read lock exactly twice around the preflight gate", name)
	}
}

func validatePersistErrors(t *testing.T, fn *ast.FuncDecl, name string) {
	t.Helper()
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ExprStmt:
			if call, ok := n.X.(*ast.CallExpr); ok && isSMethodCall(call, "persistLocked") {
				t.Errorf("%s discards a persistLocked error", name)
			}
		case *ast.AssignStmt:
			for index, rhs := range n.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || index >= len(n.Lhs) || !isSMethodCall(call, "persistLocked") {
					continue
				}
				if ident, ok := n.Lhs[index].(*ast.Ident); ok && ident.Name == "_" {
					t.Errorf("%s discards a persistLocked error", name)
				}
			}
		case *ast.GoStmt:
			if n.Call != nil && isSMethodCall(n.Call, "persistLocked") {
				t.Errorf("%s launches persistLocked without checking its error", name)
			}
		case *ast.DeferStmt:
			if n.Call != nil && isSMethodCall(n.Call, "persistLocked") {
				t.Errorf("%s defers persistLocked without checking its error", name)
			}
		}
		return true
	})
}

func writeErrorCalls(fn *ast.FuncDecl) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "writeErrorLocked" {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

func firstStoreStatePos(fn *ast.FuncDecl) token.Pos {
	var first token.Pos
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selectorOnSField(selector, "state") && (first == 0 || selector.Pos() < first) {
			first = selector.Pos()
		}
		return true
	})
	return first
}

func countStoreMuCalls(fn *ast.FuncDecl, method string) int {
	count := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == method && selectorOnSField(selector.X, "mu") {
			count++
		}
		return true
	})
	return count
}

func gateBodyUnlocksReadLock(stmt ast.Stmt) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || len(ifStmt.Body.List) != 2 {
		return false
	}
	return isStoreMuRUnlockStmt(ifStmt.Body.List[0])
}

func TestZonePersistMutatorsFailBeforeSideEffectsWhenStoreIsUnhealthy(t *testing.T) {
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		setup func(*Store) error
		call  func(*Store) error
	}{
		{
			name: "place in zone",
			call: func(store *Store) error {
				return store.PlaceInZone("team-1", "zone-player", zoneReserve, "QB", now)
			},
		},
		{
			name: "clear zone",
			setup: func(store *Store) error {
				return store.PlaceInZone("team-1", "zone-player", zoneReserve, "QB", now)
			},
			call: func(store *Store) error {
				return store.ClearZone("team-1", "zone-player", zoneReserve)
			},
		},
		{
			name: "activate from IR with drop",
			setup: func(store *Store) error {
				return store.PlaceInZone("team-1", "zone-player", zoneIR, "QB", now)
			},
			call: func(store *Store) error {
				return store.ActivateFromIRWithDrop("team-1", "zone-player", Transaction{})
			},
		},
		{
			name: "auto-cut healed IR",
			setup: func(store *Store) error {
				return store.PlaceInZone("team-1", "zone-player", zoneIR, "QB", now)
			},
			call: func(store *Store) error {
				_, err := store.AutoCutHealedIR("team-1", TransactionPlayer{PlayerID: "zone-player", Name: "Zone Player"}, 2026, 1, now)
				return err
			},
		},
	}
	for _, health := range []struct {
		name string
		want error
		set  func(*Store, PersistedState, uint32)
	}{
		{
			name: "load error",
			want: errInjectedLoad,
			set: func(store *Store, _ PersistedState, _ uint32) {
				store.loadErr = errInjectedLoad
			},
		},
		{
			name: "persistence poison",
			want: ErrPersistenceIndeterminate,
			set: func(store *Store, before PersistedState, beforeDirty uint32) {
				store.identityUnhealthy = true
				store.persistencePoison = ErrPersistenceIndeterminate
				store.poisonedState = cloneState(before)
				store.poisonedDirty = beforeDirty
			},
		},
	} {
		for _, test := range tests {
			t.Run(health.name+"/"+test.name, func(t *testing.T) {
				store := newTestStore(t)
				if _, err := store.MakePick("team-1", "zone-player", "manager", now, time.Time{}); err != nil {
					t.Fatal(err)
				}
				if test.setup != nil {
					if err := test.setup(store); err != nil {
						t.Fatal(err)
					}
				}
				store.mu.Lock()
				before := cloneState(store.state)
				beforeDirty := store.dirty
				health.set(store, before, beforeDirty)
				store.mu.Unlock()

				if err := test.call(store); !errors.Is(err, health.want) {
					t.Fatalf("error = %v, want %v before validation/randomness", err, health.want)
				}
				store.mu.RLock()
				after := cloneState(store.state)
				afterDirty := store.dirty
				store.mu.RUnlock()
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("state changed before write gate:\n got: %#v\nwant: %#v", after, before)
				}
				if afterDirty != beforeDirty {
					t.Fatalf("dirty mask changed before write gate: got %#x want %#x", afterDirty, beforeDirty)
				}
			})
		}
	}
}

func selectorOnS(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "s"
}

func selectorOnSField(expr ast.Expr, field string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == field && selectorOnS(selector.X)
}

func isSMethodCall(call *ast.CallExpr, method string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == method && selectorOnS(selector.X)
}

func isStoreMuCall(stmt ast.Stmt, method string) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != method {
		return false
	}
	return selectorOnSField(selector.X, "mu")
}

func isStoreMuUnlockDefer(stmt ast.Stmt) bool {
	deferStmt, ok := stmt.(*ast.DeferStmt)
	if !ok {
		return false
	}
	call, ok := deferStmt.Call.Fun.(*ast.SelectorExpr)
	return ok && call.Sel.Name == "Unlock" && selectorOnSField(call.X, "mu")
}

func isStoreMuRUnlockStmt(stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "RUnlock" && selectorOnSField(selector.X, "mu")
}

func checkedWriteErrorGate(fn *ast.FuncDecl, stmt ast.Stmt, allowReadUnlock bool) (*ast.CallExpr, bool) {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifStmt.Init == nil || ifStmt.Cond == nil || ifStmt.Body == nil || ifStmt.Else != nil {
		return nil, false
	}
	assign, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, false
	}
	errIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || errIdent.Name != "err" {
		return nil, false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || !isSMethodCall(call, "writeErrorLocked") {
		return nil, false
	}
	condition, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || condition.Op != token.NEQ {
		return nil, false
	}
	left, leftOK := condition.X.(*ast.Ident)
	right, rightOK := condition.Y.(*ast.Ident)
	if !leftOK || !rightOK || left.Name != "err" || right.Name != "nil" {
		return nil, false
	}
	if !gateReturnMatches(fn, ifStmt.Body, allowReadUnlock) {
		return nil, false
	}
	return call, true
}

func gateReturnMatches(fn *ast.FuncDecl, body *ast.BlockStmt, allowReadUnlock bool) bool {
	if len(body.List) == 0 || (len(body.List) != 1 && !(allowReadUnlock && len(body.List) == 2)) {
		return false
	}
	returnStmt, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	if !ok {
		return false
	}
	if allowReadUnlock && !isStoreMuRUnlockStmt(body.List[0]) {
		return false
	}
	resultTypes := funcResultTypes(fn)
	if len(resultTypes) == 0 || len(returnStmt.Results) != len(resultTypes) {
		return false
	}
	errorIndex := -1
	for index, resultType := range resultTypes {
		if isErrorType(resultType) {
			if errorIndex >= 0 {
				return false
			}
			errorIndex = index
		}
	}
	if errorIndex < 0 {
		return false
	}
	for index, result := range returnStmt.Results {
		if index == errorIndex {
			ident, ok := result.(*ast.Ident)
			if !ok || ident.Name != "err" {
				return false
			}
			continue
		}
		if !isZeroGateExpr(result, resultTypes[index]) {
			return false
		}
	}
	return true
}

func funcResultTypes(fn *ast.FuncDecl) []ast.Expr {
	if fn.Type.Results == nil {
		return nil
	}
	var types []ast.Expr
	for _, field := range fn.Type.Results.List {
		if len(field.Names) == 0 {
			types = append(types, field.Type)
			continue
		}
		for range field.Names {
			types = append(types, field.Type)
		}
	}
	return types
}

func isErrorType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "error"
}

func isZeroGateExpr(expr ast.Expr, resultType ast.Expr) bool {
	switch value := expr.(type) {
	case *ast.Ident:
		switch value.Name {
		case "nil":
			return isNilableType(resultType)
		case "false":
			ident, ok := resultType.(*ast.Ident)
			return ok && ident.Name == "bool"
		}
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			ident, ok := resultType.(*ast.Ident)
			return ok && ident.Name == "string" && value.Value == `""`
		}
		if value.Value == "0" || value.Value == "0.0" || value.Value == "0i" {
			ident, ok := resultType.(*ast.Ident)
			if !ok {
				return false
			}
			return strings.HasPrefix(ident.Name, "int") || strings.HasPrefix(ident.Name, "uint") || strings.HasPrefix(ident.Name, "float") || strings.HasPrefix(ident.Name, "complex")
		}
	case *ast.CompositeLit:
		return len(value.Elts) == 0
	}
	return false
}

func isNilableType(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.StarExpr, *ast.MapType, *ast.InterfaceType, *ast.ChanType, *ast.FuncType:
		return true
	case *ast.ArrayType:
		array, _ := expr.(*ast.ArrayType)
		return array.Len == nil
	}
	return false
}

func containsCall(calls []*ast.CallExpr, want *ast.CallExpr) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

func newAvatarIdentityStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "league-state.json")
	store := NewStore(path)
	t.Cleanup(func() { _ = store.Close() })
	member, _, err := store.AssignMember(avatarTestEmail, "Manager")
	if err != nil {
		t.Fatalf("assign avatar test member: %v", err)
	}
	if member.TeamID != "team-1" {
		t.Fatalf("avatar test member assigned to %q, want team-1", member.TeamID)
	}
	return store, path
}

func avatarTestRef(t *testing.T, root string, body string) string {
	t.Helper()
	anchor := filepath.Dir(filepath.Clean(root))
	ref, err := writeAvatarBlob(anchor, root, []byte(body))
	if err != nil {
		t.Fatalf("write avatar object: %v", err)
	}
	return ref
}

func TestAvatarIdentityPairTransitionsRoundTrip(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	root := t.TempDir()
	ref := avatarTestRef(t, root, "normalized avatar")

	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatalf("initial badge claim: %v", err)
	}
	released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if err != nil {
		t.Fatalf("activate avatar: %v", err)
	}
	if !released {
		t.Fatal("activating an avatar should report the former badge release")
	}
	if _, ok := store.BadgeClaim("team-1"); ok {
		t.Fatal("avatar activation left a badge claim behind")
	}
	if got, ok := store.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("avatar ref = %q, %v; want %q, true", got, ok, ref)
	}

	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if got, ok := reloaded.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("reloaded avatar ref = %q, %v; want %q, true", got, ok, ref)
	}
	if err := reloaded.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatalf("claiming a badge after avatar activation: %v", err)
	}
	if _, ok := reloaded.AvatarRef("team-1"); ok {
		t.Fatal("claiming a catalog badge must clear the avatar ref")
	}
	if got, ok := reloaded.BadgeClaim("team-1"); !ok || got != "wolf" {
		t.Fatalf("reloaded badge claim = %q, %v; want wolf, true", got, ok)
	}
}

func TestAvatarIdentityAliasUploadReleasesOnlyOwnBadgeAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "league-state.json")
	store := NewStoreWithIdentity(path, testIdentityResolver(t))
	t.Cleanup(func() { _ = store.Close() })
	member, _, err := store.AssignMember(identityCanonicalEmail, "Manager")
	if err != nil {
		t.Fatal(err)
	}
	if member.TeamID != "team-1" {
		t.Fatalf("canonical member team = %q, want team-1", member.TeamID)
	}
	if err := store.ClaimBadge("team-1", "wolf"); err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimBadge("team-2", "helmet"); err != nil {
		t.Fatal(err)
	}
	ref := avatarTestRef(t, t.TempDir(), "alias-owned avatar")

	released, err := store.activateAvatar("team-1", ref, seatActor{email: identityAliasEmail})
	if err != nil || !released {
		t.Fatalf("alias activation = released %v, err %v; want true, nil", released, err)
	}
	if _, ok := store.BadgeClaim("team-1"); ok {
		t.Fatal("alias upload left its canonical seat's badge locked")
	}
	if got, ok := store.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("canonical seat avatar = %q, %v; want %q, true", got, ok, ref)
	}
	if got, ok := store.BadgeClaim("team-2"); !ok || got != "helmet" {
		t.Fatalf("other seat badge = %q, %v; want helmet, true", got, ok)
	}

	released, err = store.activateAvatar("team-1", ref, seatActor{email: identityAliasEmail})
	if err != nil || released {
		t.Fatalf("idempotent alias activation = released %v, err %v; want false, nil", released, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStoreWithIdentity(path, testIdentityResolver(t))
	t.Cleanup(func() { _ = reloaded.Close() })
	if got, ok := reloaded.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("reloaded canonical seat avatar = %q, %v; want %q, true", got, ok, ref)
	}
	if _, ok := reloaded.BadgeClaim("team-1"); ok {
		t.Fatal("released badge returned after reload")
	}
	if got, ok := reloaded.BadgeClaim("team-2"); !ok || got != "helmet" {
		t.Fatalf("reloaded other seat badge = %q, %v; want helmet, true", got, ok)
	}
}

func TestIdentityRepairPhysicallyCleansConflictsAndSurvivesReload(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	validRef := strings.Repeat("a", sha256.Size*2)
	unknownTeamRef := strings.Repeat("b", sha256.Size*2)

	store.mu.Lock()
	db := store.db
	if _, err := db.Exec(`PRAGMA ignore_check_constraints = ON`); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	for _, row := range []struct {
		teamID string
		ref    string
	}{
		{teamID: "team-1", ref: validRef},
		{teamID: "team-4", ref: "not-a-ref"},
		{teamID: "team-9", ref: unknownTeamRef},
	} {
		if _, err := db.Exec(`INSERT OR REPLACE INTO avatar_refs (team_id, ref) VALUES (?, ?)`, row.teamID, row.ref); err != nil {
			store.mu.Unlock()
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		teamID string
		motif  string
	}{
		{teamID: "team-1", motif: "wolf"},   // valid avatar wins this conflict.
		{teamID: "team-2", motif: "wolf"},   // sorted duplicate winner.
		{teamID: "team-3", motif: "wolf"},   // duplicate loser.
		{teamID: "team-4", motif: "flame"},  // retired motif.
		{teamID: "team-8", motif: "helmet"}, // valid independent claim.
		{teamID: "team-9", motif: "star"},   // unknown team.
	} {
		if _, err := db.Exec(`INSERT OR REPLACE INTO badge_claims (team_id, motif) VALUES (?, ?)`, row.teamID, row.motif); err != nil {
			store.mu.Unlock()
			t.Fatal(err)
		}
	}
	store.mu.Unlock()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.StartupError(); err != nil {
		t.Fatalf("repaired store startup error = %v", err)
	}
	if got, ok := reloaded.AvatarRef("team-1"); !ok || got != validRef {
		t.Fatalf("repaired avatar ref = %q, %v; want %q, true", got, ok, validRef)
	}
	if _, ok := reloaded.BadgeClaim("team-1"); ok {
		t.Fatal("valid avatar did not win the same-team badge conflict")
	}
	if got, ok := reloaded.BadgeClaim("team-2"); !ok || got != "wolf" {
		t.Fatalf("duplicate motif winner = %q, %v; want wolf, true", got, ok)
	}
	for _, teamID := range []string{"team-3", "team-4", "team-9"} {
		if _, ok := reloaded.BadgeClaim(teamID); ok {
			t.Fatalf("invalid badge claim for %s survived repair", teamID)
		}
	}
	if got, ok := reloaded.BadgeClaim("team-8"); !ok || got != "helmet" {
		t.Fatalf("independent canonical badge = %q, %v; want helmet, true", got, ok)
	}

	for _, query := range []struct {
		name string
		stmt string
		args []any
		want int
	}{
		{name: "valid avatar", stmt: `SELECT count(*) FROM avatar_refs WHERE team_id = ? AND ref = ?`, args: []any{"team-1", validRef}, want: 1},
		{name: "invalid ref", stmt: `SELECT count(*) FROM avatar_refs WHERE team_id = ?`, args: []any{"team-4"}, want: 0},
		{name: "unknown avatar team", stmt: `SELECT count(*) FROM avatar_refs WHERE team_id = ?`, args: []any{"team-9"}, want: 0},
		{name: "avatar-shadowed badge", stmt: `SELECT count(*) FROM badge_claims WHERE team_id = ?`, args: []any{"team-1"}, want: 0},
		{name: "duplicate loser", stmt: `SELECT count(*) FROM badge_claims WHERE team_id = ?`, args: []any{"team-3"}, want: 0},
		{name: "retired motif", stmt: `SELECT count(*) FROM badge_claims WHERE team_id = ?`, args: []any{"team-4"}, want: 0},
		{name: "unknown badge team", stmt: `SELECT count(*) FROM badge_claims WHERE team_id = ?`, args: []any{"team-9"}, want: 0},
	} {
		var got int
		if err := reloaded.db.QueryRow(query.stmt, query.args...).Scan(&got); err != nil {
			t.Fatalf("%s query: %v", query.name, err)
		}
		if got != query.want {
			t.Fatalf("%s rows = %d, want %d", query.name, got, query.want)
		}
	}

	want := reloaded.Snapshot()
	if err := reloaded.Close(); err != nil {
		t.Fatal(err)
	}
	again := NewStore(path)
	t.Cleanup(func() { _ = again.Close() })
	if got := again.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded repaired state differs:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestAvatarIdentityPrecommitFailureKeepsPairAndAllowsRetry(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	root := t.TempDir()
	ref := avatarTestRef(t, root, "precommit orphan")
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}

	failThisStorePersist(store)
	released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if !errors.Is(err, errInjectedPersist) {
		t.Fatalf("activate error = %v, want injected precommit error", err)
	}
	if released {
		t.Fatal("failed avatar activation must not report a badge release")
	}
	if got, ok := store.BadgeClaim("team-1"); !ok || got != "wolf" {
		t.Fatalf("live badge pair after failed activation = %q, %v; want wolf, true", got, ok)
	}
	if _, ok := store.AvatarRef("team-1"); ok {
		t.Fatal("failed activation published an avatar ref")
	}
	stored := reloadStoredState(t, path)
	if stored.BadgeClaims["team-1"] != "wolf" || stored.AvatarRefs["team-1"] != "" {
		t.Fatalf("stored pair after failed activation = claim %q, ref %q", stored.BadgeClaims["team-1"], stored.AvatarRefs["team-1"])
	}
	if _, err := os.Stat(filepath.Join(root, "objects", ref+".png")); err != nil {
		t.Fatalf("content-addressed orphan should still exist for later reuse: %v", err)
	}

	store.mu.Lock()
	store.persistHook = nil
	store.mu.Unlock()
	released, err = store.activateAvatar("team-1", ref, avatarManagerActor)
	if err != nil || !released {
		t.Fatalf("retry activation = released %v, err %v; want true, nil", released, err)
	}
}

func TestAvatarIdentityPostCommitOutcomesReconcileAuthoritativePair(t *testing.T) {
	for _, tc := range []struct {
		name        string
		disposition persistDisposition
	}{
		{name: "committed", disposition: persistCommitted},
		{name: "unknown", disposition: persistUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newAvatarIdentityStore(t)
			ref := avatarTestRef(t, t.TempDir(), "postcommit "+tc.name)
			if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			store.persistAfterCommitHook = func() (persistDisposition, error) {
				return tc.disposition, errInjectedPersist
			}
			store.mu.Unlock()

			released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
			if err != nil {
				t.Fatalf("post-commit %s should reconcile as success: %v", tc.name, err)
			}
			if !released {
				t.Fatal("reconciled activation should report the committed badge release")
			}
			if !store.IdentityHealthy() {
				t.Fatal("a readable desired commit must not poison identity health")
			}
			if got, ok := store.AvatarRef("team-1"); !ok || got != ref {
				t.Fatalf("reconciled avatar ref = %q, %v; want %q, true", got, ok, ref)
			}
			if _, ok := store.BadgeClaim("team-1"); ok {
				t.Fatal("reconciled activation left the old badge claim")
			}
			store.mu.Lock()
			store.persistAfterCommitHook = nil
			store.mu.Unlock()
		})
	}
}

func TestAvatarIdentityCommitErrorReconcilesAppliedTransaction(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	ref := avatarTestRef(t, t.TempDir(), "commit applied but reported unknown")
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.commitTx = func(tx *sql.Tx) error {
		if err := tx.Commit(); err != nil {
			return err
		}
		return errInjectedPersist
	}
	store.mu.Unlock()

	released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if err != nil || !released {
		t.Fatalf("applied unknown commit = released %v, err %v; want true, nil", released, err)
	}
	if got, ok := store.AvatarRef("team-1"); !ok || got != ref {
		t.Fatalf("reconciled ref = %q, %v; want %q, true", got, ok, ref)
	}
	if _, ok := store.BadgeClaim("team-1"); ok {
		t.Fatal("applied unknown commit retained the former badge")
	}
}

func TestAvatarIdentityCommitErrorReconcilesNotAppliedWithoutLosingDirtyState(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	ref := avatarTestRef(t, t.TempDir(), "commit not applied")
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.state.TeamNames["team-2"] = "Pending rename"
	store.dirty |= 1 << uint(colTeamNames)
	wantDirty := store.dirty
	store.commitTx = func(*sql.Tx) error { return errInjectedPersist }
	store.mu.Unlock()

	released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if !errors.Is(err, errInjectedPersist) || released {
		t.Fatalf("not-applied commit = released %v, err %v; want false, injected error", released, err)
	}
	store.mu.RLock()
	gotName := store.state.TeamNames["team-2"]
	gotDirty := store.dirty
	store.mu.RUnlock()
	if gotName != "Pending rename" || gotDirty != wantDirty {
		t.Fatalf("pending state after reconciliation = name %q dirty %#x; want name preserved, dirty %#x", gotName, gotDirty, wantDirty)
	}
	if got, ok := store.BadgeClaim("team-1"); !ok || got != "wolf" {
		t.Fatalf("badge after not-applied commit = %q, %v; want wolf, true", got, ok)
	}
	if _, ok := store.AvatarRef("team-1"); ok {
		t.Fatal("not-applied commit published an avatar ref")
	}
	stored := reloadStoredState(t, path)
	if stored.BadgeClaims["team-1"] != "wolf" || stored.AvatarRefs["team-1"] != "" {
		t.Fatalf("database pair after not-applied commit = badge %q ref %q", stored.BadgeClaims["team-1"], stored.AvatarRefs["team-1"])
	}
}

func TestAvatarIdentityReconcileReadFailurePoisonsStoreWideAndRestoresCandidate(t *testing.T) {
	store, path := newAvatarIdentityStore(t)
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	ref := avatarTestRef(t, t.TempDir(), "unreconciled read failure")
	store.mu.Lock()
	// Keep unrelated pending work in the pre-candidate state so this test
	// proves the poison restores, rather than silently discards, its dirty
	// mask and in-memory value.
	store.state.TeamNames["team-2"] = "Pending rename"
	store.dirty |= 1 << uint(colTeamNames)
	wantDirty := store.dirty
	store.commitTx = func(*sql.Tx) error { return errInjectedPersist }
	store.identityReconcileReadHook = func(*sql.DB) (PersistedState, error) {
		return PersistedState{}, errors.New("injected reconciliation read failure")
	}
	store.mu.Unlock()

	if _, err := store.activateAvatar("team-1", ref, avatarManagerActor); err != ErrPersistenceIndeterminate {
		t.Fatalf("irreconcilable identity error = %v, want ErrPersistenceIndeterminate", err)
	}
	if store.IdentityHealthy() {
		t.Fatal("irreconcilable identity outcome left Store healthy")
	}
	if store.StartupError() != ErrPersistenceIndeterminate {
		t.Fatalf("poison StartupError = %v, want ErrPersistenceIndeterminate", store.StartupError())
	}
	// Public identity reads fail closed, including Snapshot-derived maps.
	if _, ok := store.AvatarRef("team-1"); ok {
		t.Fatal("poisoned AvatarRef read returned an uncertain candidate")
	}
	if _, ok := store.BadgeClaim("team-1"); ok {
		t.Fatal("poisoned BadgeClaim read returned identity data")
	}
	if claims := store.BadgeClaims(); len(claims) != 0 {
		t.Fatalf("poisoned BadgeClaims = %#v, want empty", claims)
	}
	snapshot := store.Snapshot()
	if len(snapshot.AvatarRefs) != 0 || len(snapshot.BadgeClaims) != 0 {
		t.Fatalf("poisoned Snapshot exposed identity: refs=%#v claims=%#v", snapshot.AvatarRefs, snapshot.BadgeClaims)
	}
	store.mu.RLock()
	gotName := store.state.TeamNames["team-2"]
	gotDirty := store.dirty
	gotClaim := store.state.BadgeClaims["team-1"]
	_, gotCandidateRef := store.state.AvatarRefs["team-1"]
	store.mu.RUnlock()
	if gotName != "Pending rename" || gotDirty != wantDirty {
		t.Fatalf("poison restore = name %q dirty %#x; want pending name and %#x", gotName, gotDirty, wantDirty)
	}
	if gotClaim != "wolf" || gotCandidateRef {
		t.Fatalf("poison restore identity = claim %q candidate ref %v; want wolf and false", gotClaim, gotCandidateRef)
	}

	// Every later DB write is rejected at the common persistence boundary,
	// and that attempted unrelated in-memory mutation is rolled back too.
	readyBefore := snapshot.Ready["team-2"]
	if _, err := store.ToggleReady("team-2"); !errors.Is(err, ErrPersistenceIndeterminate) {
		t.Fatalf("unrelated write after poison = %v, want ErrPersistenceIndeterminate", err)
	}
	if got := store.Snapshot().Ready["team-2"]; got != readyBefore {
		t.Fatalf("unrelated write changed poisoned in-memory state to %v", got)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reloaded := NewStore(path)
	t.Cleanup(func() { _ = reloaded.Close() })
	if err := reloaded.StartupError(); err != nil {
		t.Fatalf("reopen startup error = %v", err)
	}
	stored := reloaded.Snapshot()
	if stored.BadgeClaims["team-1"] != "wolf" {
		t.Fatalf("reopened badge = %q, want wolf", stored.BadgeClaims["team-1"])
	}
	if _, ok := stored.AvatarRefs["team-1"]; ok {
		t.Fatal("reopened DB replayed the rejected avatar candidate")
	}
}

func TestStoreWriteErrorGatePreservesNonEmptyStateForLoadAndPoison(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*Store, PersistedState, uint32, error)
		want error
	}{
		{
			name: "load error",
			set: func(store *Store, _ PersistedState, _ uint32, loadErr error) {
				store.loadErr = loadErr
			},
			want: errInjectedLoad,
		},
		{
			name: "persistence poison",
			set: func(store *Store, before PersistedState, beforeDirty uint32, _ error) {
				store.identityUnhealthy = true
				store.persistencePoison = ErrPersistenceIndeterminate
				store.poisonedState = cloneState(before)
				store.poisonedDirty = beforeDirty
			},
			want: ErrPersistenceIndeterminate,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, path := newAvatarIdentityStore(t)
			if err := store.SetTeamName("team-2", "Before gate"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ToggleReady("team-2"); err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			before := cloneState(store.state)
			beforePublic := cloneState(before)
			beforePublic.persistenceAuthority = ""
			beforeDirty := store.dirty
			loadErr := errInjectedLoad
			tc.set(store, before, beforeDirty, loadErr)
			store.mu.Unlock()

			call := func(name string, err error) {
				t.Helper()
				if !errors.Is(err, tc.want) {
					t.Errorf("%s error = %v, want %v", name, err, tc.want)
				}
			}
			_, err := store.MakePick("team-1", "p-gated", "manager", time.Now(), time.Now().Add(time.Minute))
			call("MakePick", err)
			_, err = store.AutoPick("team-1", "p-gated", "auto", 1, time.Time{}, time.Now(), time.Now().Add(time.Minute))
			call("AutoPick", err)
			call("UndoLastPick", store.UndoLastPick(time.Now()))
			_, _, err = store.AssignMember(avatarTestEmail, "Renamed by gate")
			call("AssignMember refresh", err)
			_, _, err = store.EnsureMember(avatarTestEmail, "Renamed again")
			call("EnsureMember refresh", err)
			call("SetTeamName", store.SetTeamName("team-2", "After gate"))

			store.mu.RLock()
			got := cloneState(store.state)
			gotDirty := store.dirty
			store.mu.RUnlock()
			if snapshot := store.Snapshot(); len(snapshot.BadgeClaims) != 0 || len(snapshot.AvatarRefs) != 0 {
				t.Fatalf("%s Snapshot exposed identity while write error was set: %#v", tc.name, snapshot)
			}
			if !reflect.DeepEqual(got, before) || gotDirty != beforeDirty {
				t.Fatalf("%s changed state or dirty mask: state equal=%v dirty=%#x want %#x", tc.name, reflect.DeepEqual(got, before), gotDirty, beforeDirty)
			}
			stored := reloadStoredState(t, path)
			if !reflect.DeepEqual(stored, beforePublic) {
				t.Fatalf("%s changed database state:\n got: %#v\nwant: %#v", tc.name, stored, beforePublic)
			}
		})
	}
}

func TestBadgeIdentitySeatChurnReturnsBadgeForbidden(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	store.mu.Lock()
	store.identityPreflightHook = func() {
		store.mu.Lock()
		store.state.Members[avatarTestEmail] = Member{TeamID: "team-2", Name: "Manager", Email: avatarTestEmail}
		store.mu.Unlock()
	}
	store.mu.Unlock()
	if err := store.claimBadgeForActor("team-1", "helmet", avatarManagerActor); err != ErrBadgeForbidden {
		t.Fatalf("badge seat-churn error = %v, want ErrBadgeForbidden", err)
	}
	store.mu.Lock()
	store.identityPreflightHook = nil
	store.mu.Unlock()
}

func TestBadgeReleaseIdentitySeatChurnReturnsBadgeForbidden(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	if err := store.claimBadgeForActor("team-1", "helmet", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.identityPreflightHook = func() {
		store.mu.Lock()
		store.state.Members[avatarTestEmail] = Member{TeamID: "team-2", Name: "Manager", Email: avatarTestEmail}
		store.mu.Unlock()
	}
	store.mu.Unlock()
	if _, err := store.releaseBadgeForActor("team-1", avatarManagerActor); err != ErrBadgeForbidden {
		t.Fatalf("badge release seat-churn error = %v, want ErrBadgeForbidden", err)
	}
	store.mu.Lock()
	store.identityPreflightHook = nil
	store.mu.Unlock()
}

func TestAvatarIdentityNoopDoesNotPersistOrInvokeCommitSeam(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	ref := avatarTestRef(t, t.TempDir(), "idempotent")
	if _, err := store.activateAvatar("team-1", ref, avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.persistAfterCommitHook = func() (persistDisposition, error) {
		return persistCommitted, errInjectedPersist
	}
	store.mu.Unlock()
	released, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if err != nil || released {
		t.Fatalf("idempotent activation = released %v, err %v; want false, nil", released, err)
	}
}

func TestAvatarIdentityRejectsSeatChurnBetweenAuthorityChecks(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	ref := avatarTestRef(t, t.TempDir(), "seat churn")
	store.mu.Lock()
	store.identityPreflightHook = func() {
		store.mu.Lock()
		store.state.Members[avatarTestEmail] = Member{TeamID: "team-2", Name: "Manager", Email: avatarTestEmail}
		store.mu.Unlock()
	}
	store.mu.Unlock()
	_, err := store.activateAvatar("team-1", ref, avatarManagerActor)
	if err != ErrAvatarForbidden {
		t.Fatalf("seat-churn activation error = %v, want ErrAvatarForbidden", err)
	}
	if _, ok := store.AvatarRef("team-1"); ok {
		t.Fatal("seat-churned actor published an avatar ref")
	}
	store.mu.Lock()
	store.identityPreflightHook = nil
	store.mu.Unlock()
}

func TestAvatarIdentityDemoActorMayManageNamedSeat(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	ref := avatarTestRef(t, t.TempDir(), "demo seat")
	if _, err := store.activateAvatar("team-2", ref, seatActor{demo: true}); err != nil {
		t.Fatalf("demo activation for a named seat: %v", err)
	}
	if got, ok := store.AvatarRef("team-2"); !ok || got != ref {
		t.Fatalf("demo avatar ref = %q, %v; want %q, true", got, ok, ref)
	}
}

func TestAvatarIdentityConcurrentTransitionsKeepPairCoherent(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	refA := avatarTestRef(t, t.TempDir(), "race A")
	refB := avatarTestRef(t, t.TempDir(), "race B")
	refs := []string{refA, refB}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 6; i++ {
		ref := refs[i%len(refs)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.activateAvatar("team-1", ref, seatActor{commissioner: true})
			errs <- err
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.claimBadgeForActor("team-1", "wolf", seatActor{commissioner: true})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent identity transition: %v", err)
		}
	}
	state := store.Snapshot()
	_, hasRef := state.AvatarRefs["team-1"]
	_, hasBadge := state.BadgeClaims["team-1"]
	if hasRef == hasBadge {
		t.Fatalf("concurrent transitions must publish exactly one identity, hasRef=%v hasBadge=%v", hasRef, hasBadge)
	}
}

func TestWriteAvatarBlobDetectsObjectCorruption(t *testing.T) {
	anchor := t.TempDir()
	root := filepath.Join(anchor, "avatars")
	data := []byte("immutable bytes")
	ref := avatarTestRef(t, root, string(data))
	path := filepath.Join(root, "objects", ref+".png")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAvatarBlob(anchor, root, data); err == nil {
		t.Fatal("corrupted existing object was accepted")
	}
}

const avatarCrashBody = "crash-safe staged avatar bytes"

func TestAvatarCrashBeforeIdentityCommitLeavesOnlyInvisibleOrphan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	root := filepath.Join(dir, "avatars")
	ref := avatarRefForCrashBody(avatarCrashBody)
	cmd := exec.Command(os.Args[0], "-test.run=TestAvatarCrashHelperProcess", "-test.v")
	cmd.Env = append(os.Environ(),
		"GRIDIRON_AVATAR_CRASH_HELPER=1",
		"GRIDIRON_AVATAR_CRASH_PATH="+path,
		"GRIDIRON_AVATAR_CRASH_ANCHOR="+dir,
		"GRIDIRON_AVATAR_CRASH_ROOT="+root,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("avatar crash helper must not exit cleanly; output:\n%s", out)
	}
	if !strings.Contains(string(out), "helper: staged avatar object") {
		t.Fatalf("helper never staged the object; output:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != -1 {
		t.Fatalf("avatar helper exited with code %d, want death by signal; output:\n%s", code, out)
	}
	stored := reloadStoredState(t, path)
	if stored.BadgeClaims["team-1"] != "wolf" {
		t.Fatalf("stored badge after crash = %q, want wolf", stored.BadgeClaims["team-1"])
	}
	if _, ok := stored.AvatarRefs["team-1"]; ok {
		t.Fatal("precommit crash published an avatar ref")
	}
	if _, err := os.Stat(filepath.Join(root, "objects", ref+".png")); err != nil {
		t.Fatalf("staged orphan object missing after crash: %v", err)
	}
}

func TestAvatarCrashHelperProcess(t *testing.T) {
	if os.Getenv("GRIDIRON_AVATAR_POSTCOMMIT_CRASH_HELPER") == "1" {
		t.Skip("postcommit helper is handled by TestAvatarPostCommitCrashHelperProcess")
	}
	if os.Getenv("GRIDIRON_AVATAR_CRASH_HELPER") != "1" {
		t.Skip("helper process for TestAvatarCrashBeforeIdentityCommitLeavesOnlyInvisibleOrphan")
	}
	path := os.Getenv("GRIDIRON_AVATAR_CRASH_PATH")
	anchor := os.Getenv("GRIDIRON_AVATAR_CRASH_ANCHOR")
	root := os.Getenv("GRIDIRON_AVATAR_CRASH_ROOT")
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AssignMember(avatarTestEmail, "Manager"); err != nil {
		t.Fatal(err)
	}
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	ref, err := writeAvatarBlob(anchor, root, []byte(avatarCrashBody))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("helper: staged avatar object")
	killThisStorePersist(store)
	_, _ = store.activateAvatar("team-1", ref, avatarManagerActor)
	t.Fatal("avatar crash helper survived its persist")
}

func TestIdentityWriteFailureSurvivesEmptyDiffUntilRealDurableChange(t *testing.T) {
	store, _ := newAvatarIdentityStore(t)
	ref := strings.Repeat("a", sha256.Size*2)
	commissioner := seatActor{commissioner: true}
	failThisStorePersist(store)
	if _, err := store.activateAvatar("team-1", ref, commissioner); !errors.Is(err, errInjectedPersist) {
		t.Fatalf("identity precommit error = %v, want injected persist error", err)
	}
	if got := store.PersistenceError(); !errors.Is(got, errInjectedPersist) {
		t.Fatalf("PersistenceError after identity failure = %v, want injected persist error", got)
	}

	// This mutator declares a collection but makes no state change, so the
	// SQLite transaction has an empty diff. It must not make readiness look
	// recovered merely because the empty transaction itself succeeded.
	store.persistHook = nil
	if err := store.SetTeamName("team-1", ""); err != nil {
		t.Fatalf("idempotent empty-diff write = %v, want nil", err)
	}
	if got := store.PersistenceError(); !errors.Is(got, errInjectedPersist) {
		t.Fatalf("PersistenceError after empty diff = %v, want injected persist error", got)
	}
	if got := store.StartupError(); !errors.Is(got, errInjectedPersist) {
		t.Fatalf("StartupError/readiness after empty diff = %v, want injected persist error", got)
	}

	if err := store.SetTeamName("team-1", "Recovered write"); err != nil {
		t.Fatalf("real durable recovery write: %v", err)
	}
	if got := store.PersistenceError(); got != nil {
		t.Fatalf("PersistenceError after real durable change = %v, want nil", got)
	}
}

func TestAvatarPostCommitCrashPublishesCommittedPair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "league-state.json")
	root := filepath.Join(dir, "avatars")
	ref := avatarRefForCrashBody(avatarCrashBody + " postcommit")
	cmd := exec.Command(os.Args[0], "-test.run=TestAvatarPostCommitCrashHelperProcess", "-test.v")
	cmd.Env = append(os.Environ(),
		"GRIDIRON_AVATAR_POSTCOMMIT_CRASH_HELPER=1",
		"GRIDIRON_AVATAR_CRASH_PATH="+path,
		"GRIDIRON_AVATAR_CRASH_ANCHOR="+dir,
		"GRIDIRON_AVATAR_CRASH_ROOT="+root,
		"GRIDIRON_AVATAR_CRASH_REF="+ref,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("postcommit avatar helper must not exit cleanly; output:\n%s", out)
	}
	if !strings.Contains(string(out), "helper: committed avatar pair") {
		t.Fatalf("helper never reached postcommit seam; output:\n%s", out)
	}
	if code := cmd.ProcessState.ExitCode(); code != -1 {
		t.Fatalf("postcommit avatar helper exited with code %d, want death by signal; output:\n%s", code, out)
	}
	stored := reloadStoredState(t, path)
	if got := stored.AvatarRefs["team-1"]; got != ref {
		t.Fatalf("stored avatar ref after postcommit crash = %q, want %q", got, ref)
	}
	if _, ok := stored.BadgeClaims["team-1"]; ok {
		t.Fatal("postcommit crash restored the old badge claim")
	}
	if _, err := os.Stat(filepath.Join(root, "objects", ref+".png")); err != nil {
		t.Fatalf("committed avatar object missing after postcommit crash: %v", err)
	}
}

func TestAvatarPostCommitCrashHelperProcess(t *testing.T) {
	if os.Getenv("GRIDIRON_AVATAR_POSTCOMMIT_CRASH_HELPER") != "1" {
		t.Skip("helper process for TestAvatarPostCommitCrashPublishesCommittedPair")
	}
	path := os.Getenv("GRIDIRON_AVATAR_CRASH_PATH")
	anchor := os.Getenv("GRIDIRON_AVATAR_CRASH_ANCHOR")
	root := os.Getenv("GRIDIRON_AVATAR_CRASH_ROOT")
	ref := os.Getenv("GRIDIRON_AVATAR_CRASH_REF")
	store := NewStore(path)
	if err := store.StartupError(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AssignMember(avatarTestEmail, "Manager"); err != nil {
		t.Fatal(err)
	}
	if err := store.claimBadgeForActor("team-1", "wolf", avatarManagerActor); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAvatarBlob(anchor, root, []byte(avatarCrashBody+" postcommit")); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.persistAfterCommitHook = func() (persistDisposition, error) {
		fmt.Println("helper: committed avatar pair")
		proc, err := os.FindProcess(os.Getpid())
		if err != nil {
			return persistCommitted, err
		}
		_ = proc.Signal(os.Kill)
		return persistCommitted, nil
	}
	store.mu.Unlock()
	_, _ = store.activateAvatar("team-1", ref, avatarManagerActor)
	t.Fatal("postcommit avatar helper survived its persist")
}

func avatarRefForCrashBody(body string) string {
	digest := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", digest[:])
}
