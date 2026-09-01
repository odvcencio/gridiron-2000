package league

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// randomCommissionerEventID draws "ce-" + 8 random hex characters from
// crypto/rand, the same randomness source randomTransactionID
// (transactions.go) and randomScheduleSeed (admin.go) already use.
func randomCommissionerEventID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ce-" + hex.EncodeToString(buf[:]), nil
}

// RecordCommissionerEvent appends one person-attributed CommissionerEvent
// (model.go) to the durable audit trail. actorEmail is the acting
// identity's canonical email; actorName is the display name captured at
// action time. kind and summary are both required — see CommissionerEvent's
// doc comment for their meaning. refs is optional and may be the zero
// value. now is the caller's clock.
//
// This is the storage primitive: Service.RecordCommissionerEvent (the
// stable, request-aware entry point every commissioner action calls) is
// the one exported seam oak's admin actions and this package's own
// commissioner-only mutations (postseason.go's preview/publish/correct,
// for one) use. Both wrap this method identically, so a Store-level test
// can exercise the primitive without a request.
func (s *Store) RecordCommissionerEvent(actorEmail, actorName, kind, summary string, refs CommissionerEventRefs, now time.Time) (CommissionerEvent, error) {
	actorEmail = strings.TrimSpace(actorEmail)
	actorName = strings.TrimSpace(actorName)
	kind = strings.TrimSpace(kind)
	summary = strings.TrimSpace(summary)
	if actorEmail == "" {
		return CommissionerEvent{}, fmt.Errorf("a signed-in commissioner identity is required to record an event")
	}
	if kind == "" {
		return CommissionerEvent{}, fmt.Errorf("a commissioner event kind is required")
	}
	if summary == "" {
		return CommissionerEvent{}, fmt.Errorf("a commissioner event summary is required")
	}
	id, err := randomCommissionerEventID()
	if err != nil {
		return CommissionerEvent{}, err
	}
	refs.TeamID = strings.TrimSpace(refs.TeamID)
	refs.PlayerID = strings.TrimSpace(refs.PlayerID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writeErrorLocked(); err != nil {
		return CommissionerEvent{}, err
	}
	event := CommissionerEvent{
		ID:         id,
		ActorEmail: actorEmail,
		ActorName:  actorName,
		Kind:       kind,
		Summary:    summary,
		Refs:       refs,
		At:         now.UTC(),
	}
	s.state.CommissionerEvents = append(s.state.CommissionerEvents, event)
	if err := s.persistLocked(colCommissionerEvents); err != nil {
		return CommissionerEvent{}, err
	}
	return event, nil
}

// commissionerEventActorIdentity resolves the acting person's canonical
// email and display name from r, the same "freeze the actor's identity at
// the moment of the action" resolution lockerRequireWriter (locker.go)
// already performs for Locker Room posts. Demo mode gets one stable
// synthetic identity — a local rehearsal league still needs an attributed
// actor, and every demo action is already read-only-adjacent by policy
// elsewhere — mirroring lockerViewerIdentity's own demo-guest branch.
func (s *Service) commissionerEventActorIdentity(r *http.Request) (email, name string) {
	if s.demoMode {
		return "demo-commissioner@example.invalid", "The Commissioner"
	}
	user, signedIn := s.CurrentUser(r)
	if !signedIn {
		return "", ""
	}
	name = strings.TrimSpace(user.Name)
	if name == "" {
		name = "The Commissioner"
	}
	return strings.TrimSpace(user.Email), name
}

// RecordCommissionerEvent is the stable, request-aware entry point every
// commissioner-only action calls to leave a durable, person-attributed
// audit record (wave-2 commissioner console). r supplies both the
// authorization check (commissioner access is required, exactly like
// every other Admin*/postseason.go commissioner action) and the acting
// identity, so a call site never has to resolve or forge an actor itself:
//
//	if _, err := s.RecordCommissionerEvent(r, "announcement.post",
//		"posted an announcement", league.CommissionerEventRefs{}); err != nil {
//		log.Printf("commissioner event: %v", err)
//	}
//
// kind is a short machine tag ("announcement.post", "playoff.publish",
// "week.force-close", ...); summary is the plain-language action phrase
// /activity and /admin render next to the actor's name ("posted an
// announcement", not "Alex posted an announcement" — the actor's name is
// rendered separately, from ActorName). refs names the entities the
// action touched (CommissionerEventRefs' fields are all optional).
//
// A failure here is deliberately non-fatal to the caller's own action: the
// action being audited has already succeeded (or is committing in the
// same request) by the time most call sites reach this line, and losing
// the audit row must never roll back or block a already-applied league
// change. Callers should log a returned error, not surface it to the
// commissioner as if the underlying action failed.
func (s *Service) RecordCommissionerEvent(r *http.Request, kind, summary string, refs CommissionerEventRefs) (CommissionerEvent, error) {
	if err := s.requireCommissioner(r); err != nil {
		return CommissionerEvent{}, err
	}
	email, name := s.commissionerEventActorIdentity(r)
	if email == "" {
		return CommissionerEvent{}, fmt.Errorf("a signed-in commissioner identity is required to record an event")
	}
	return s.store.RecordCommissionerEvent(email, name, kind, summary, refs, s.clock())
}
