package fleetconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func publicationTestBundle(files ...File) Bundle {
	return Bundle{Files: files}
}

func TestPublicationCleanRenderCheckAndDeterministicRepeat(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	bundle := publicationTestBundle(
		File{Path: "z.txt", Data: []byte("z\n")},
		File{Path: "instances/alpha/a.yaml", Data: []byte("a\n")},
	)
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	drift, err := Check(bundle, out)
	if err != nil {
		t.Fatal(err)
	}
	if !drift.Clean() {
		t.Fatalf("clean drift = %#v", drift)
	}
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	after := snapshotTree(t, out)
	if !sameTreeBytes(before, after) {
		t.Fatalf("repeat render changed tree:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestPublicationDriftExactSortedPaths(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	bundle := publicationTestBundle(
		File{Path: "b.txt", Data: []byte("b\n")},
		File{Path: "a.txt", Data: []byte("a\n")},
		File{Path: "nested/c.txt", Data: []byte("c\n")},
	)
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "b.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(out, "nested", "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "unexpected.txt"), []byte("unexpected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	drift, err := Check(bundle, out)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := drift.Added, []string{"nested/c.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("added = %#v, want %#v", got, want)
	}
	if got, want := drift.Changed, []string{"b.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %#v, want %#v", got, want)
	}
	if got, want := drift.Removed, []string{"unexpected.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("removed = %#v, want %#v", got, want)
	}
	if got := drift.String(); strings.Index(got, "nested/c.txt") > strings.Index(got, "b.txt") {
		t.Fatalf("drift categories/paths are not stable: %q", got)
	}
}

func TestPublicationRejectsUnsafeGeneratedPathsBeforeStaging(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	bundle := publicationTestBundle(File{Path: "../escape.txt", Data: []byte("no\n")})
	if _, err := ExpectedFiles(bundle); err == nil {
		t.Fatal("ExpectedFiles accepted an escaping path")
	}
	if err := Publish(bundle, out); err == nil {
		t.Fatal("Publish accepted an escaping path")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsafe publication mutated parent: %#v", entries)
	}
}

func TestPublicationRejectsBroadRootsAndMissingCheckIsReadOnly(t *testing.T) {
	parent := t.TempDir()
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})
	for _, out := range []string{"", ".", string(filepath.Separator)} {
		if err := Publish(bundle, out); err == nil {
			t.Fatalf("broad output %q was accepted", out)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := Publish(bundle, home); err == nil {
			t.Fatalf("home output %q was accepted", home)
		}
	}
	out := filepath.Join(parent, "missing")
	before := snapshotTree(t, parent)
	drift, err := Check(bundle, out)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := drift.Added, []string{OwnershipMarkerPath, "a.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing added = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, parent)) {
		t.Fatal("missing check mutated its parent")
	}
}

func TestPublicationOwnershipAndEmptyTargetRules(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})
	if err := Publish(bundle, out); err != nil {
		t.Fatalf("empty output rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, OwnershipMarkerPath)); err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(out, "unowned.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(bundle, out); err != nil {
		t.Fatalf("owned output replacement rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "unowned.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old extra file survived replacement, err=%v", err)
	}

	unowned := filepath.Join(parent, "unowned")
	if err := os.Mkdir(unowned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unowned, "data"), []byte("operator\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Publish(bundle, unowned); err == nil || !strings.Contains(err.Error(), "not a fleetgen-owned tree") {
		t.Fatalf("unowned output error = %v", err)
	}
}

func TestPublicationRejectsSymlinkRootParentAndNested(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "real")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})

	rootLink := filepath.Join(parent, "root-link")
	if err := os.Symlink(target, rootLink); err != nil {
		t.Fatal(err)
	}
	if err := Publish(bundle, rootLink); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink root error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink root was followed, err=%v", err)
	}

	parentReal := filepath.Join(parent, "parent-real")
	if err := os.Mkdir(parentReal, 0o700); err != nil {
		t.Fatal(err)
	}
	parentLink := filepath.Join(parent, "parent-link")
	if err := os.Symlink(parentReal, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := Publish(bundle, filepath.Join(parentLink, "bundle")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink parent error = %v", err)
	}

	nested := filepath.Join(parent, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, OwnershipMarkerPath), []byte(OwnershipMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(nested, "link")); err != nil {
		t.Fatal(err)
	}
	if err := Publish(bundle, nested); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("nested symlink error = %v", err)
	}
}

func TestPublicationCheckIsByteAndMetadataReadOnly(t *testing.T) {
	out := filepath.Join(t.TempDir(), "bundle")
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	time.Sleep(5 * time.Millisecond)
	drift, err := Check(bundle, out)
	if err != nil {
		t.Fatal(err)
	}
	if !drift.Clean() {
		t.Fatalf("check drift = %#v", drift)
	}
	after := snapshotTree(t, out)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("check changed tree:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestPublicationFailureAfterStagingRollsBackByteIdentically(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	oldBundle := publicationTestBundle(File{Path: "old.txt", Data: []byte("old\n")})
	newBundle := publicationTestBundle(File{Path: "new.txt", Data: []byte("new\n")})
	if err := Publish(oldBundle, out); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	fault := &renameFaultFS{publicationFS: osPublicationFS{}, failAt: 2}
	publisher := &Publisher{fs: fault, nonce: func() string { return "fixed" }}
	if err := publisher.Publish(newBundle, out); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	after := snapshotTree(t, out)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("swap failure changed old tree:\nbefore=%#v\nafter=%#v", before, after)
	}
	assertNoTransactionArtifacts(t, parent, "bundle")
}

func TestPublicationFailureDuringOldTreeSwapLeavesOldTree(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, out)
	fault := &renameFaultFS{publicationFS: osPublicationFS{}, failAt: 1}
	publisher := &Publisher{fs: fault, nonce: func() string { return "fixed" }}
	if err := publisher.Publish(bundle, out); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	after := snapshotTree(t, out)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("old-tree swap failure changed output:\nbefore=%#v\nafter=%#v", before, after)
	}
	assertNoTransactionArtifacts(t, parent, "bundle")
}

func TestPublicationCleanupFailureIsCommittedAndLeavesArtifactForInspection(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	oldBundle := publicationTestBundle(
		File{Path: "old.txt", Data: []byte("old\n")},
		File{Path: "nested/old.txt", Data: []byte("nested-old\n")},
	)
	newBundle := publicationTestBundle(File{Path: "new.txt", Data: []byte("new\n")})
	if err := Publish(oldBundle, out); err != nil {
		t.Fatal(err)
	}
	fault := &renameFaultFS{
		publicationFS: osPublicationFS{},
		failRemoveAt:  2,
	}
	publisher := &Publisher{fs: fault, nonce: func() string { return "cleanup" }}
	err := publisher.Publish(newBundle, out)
	var committed *CommittedPublicationError
	if !errors.As(err, &committed) {
		t.Fatalf("cleanup failure = %v, want committed publication error", err)
	}
	if committed.Artifact == "" {
		t.Fatal("committed error omitted artifact path")
	}
	if _, err := os.Stat(filepath.Join(out, "new.txt")); err != nil {
		t.Fatalf("new publication is not visible: %v", err)
	}
	if _, err := os.Stat(committed.Artifact); err != nil {
		t.Fatalf("cleanup artifact disappeared: %v", err)
	}
	if _, err := Check(newBundle, out); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("check after committed cleanup failure = %v", err)
	}
}

func TestPublicationCleanupSwapCannotEscapeBackupDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux openat2 directory handles")
	}
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("must-survive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldBundle := publicationTestBundle(
		File{Path: "old.txt", Data: []byte("old\n")},
		File{Path: "nested/old.txt", Data: []byte("nested-old\n")},
	)
	newBundle := publicationTestBundle(File{Path: "new.txt", Data: []byte("new\n")})
	if err := Publish(oldBundle, out); err != nil {
		t.Fatal(err)
	}
	fault := &swapOnReadFS{
		publicationFS: osPublicationFS{},
		baseName:      "nested",
		triggerAt:     3, // initial snapshot, recheck, then backup cleanup
		target:        outside,
	}
	publisher := &Publisher{fs: fault, nonce: func() string { return "escape" }}
	err := publisher.Publish(newBundle, out)
	var committed *CommittedPublicationError
	if !errors.As(err, &committed) {
		t.Fatalf("backup swap error = %v, want committed publication error", err)
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "must-survive\n" {
		t.Fatalf("backup cleanup escaped into symlink target: %q", got)
	}
	if fault.swapErr != nil {
		t.Fatalf("injected backup swap failed: %v", fault.swapErr)
	}
	if _, err := os.Lstat(committed.Artifact); err != nil {
		t.Fatalf("backup artifact disappeared: %v", err)
	}
	if err := os.Remove(fault.swappedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fault.savedPath, fault.swappedPath); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationCleanupPreOpenSwapCannotEscapeBackupDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux openat2 directory handles")
	}
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("must-survive-pre-open\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldBundle := publicationTestBundle(
		File{Path: "old.txt", Data: []byte("old\n")},
		File{Path: "nested/old.txt", Data: []byte("nested-old\n")},
	)
	newBundle := publicationTestBundle(File{Path: "new.txt", Data: []byte("new\n")})
	if err := Publish(oldBundle, out); err != nil {
		t.Fatal(err)
	}
	fault := &swapBeforeOpenFS{
		publicationFS: osPublicationFS{},
		baseName:      "nested",
		triggerAt:     3, // initial snapshot, recheck, then backup cleanup
		replacement:   outside,
	}
	publisher := &Publisher{fs: fault, nonce: func() string { return "pre-open" }}
	err := publisher.Publish(newBundle, out)
	var committed *CommittedPublicationError
	if !errors.As(err, &committed) {
		t.Fatalf("backup pre-open swap error = %v, want committed publication error", err)
	}
	if !fault.swapped {
		t.Fatal("pre-open swap regression did not execute the hook")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "must-survive-pre-open\n" {
		t.Fatalf("backup cleanup escaped during pre-open substitution: %q", got)
	}
	if fault.swapErr != nil {
		t.Fatalf("injected pre-open swap failed: %v", fault.swapErr)
	}
	if _, err := os.Lstat(committed.Artifact); err != nil {
		t.Fatalf("backup artifact disappeared: %v", err)
	}
}

func TestPublicationParentSwapCannotRedirectOwnedOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux openat2 directory handles")
	}
	anchor := t.TempDir()
	parent := filepath.Join(anchor, "parent")
	outside := filepath.Join(anchor, "outside")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(parent, "bundle")
	outOutside := filepath.Join(outside, "bundle")
	bundle := publicationTestBundle(File{Path: "old.txt", Data: []byte("old\n")})
	outsideBundle := publicationTestBundle(File{Path: "outside.txt", Data: []byte("outside\n")})
	if err := Publish(bundle, out); err != nil {
		t.Fatal(err)
	}
	if err := Publish(outsideBundle, outOutside); err != nil {
		t.Fatal(err)
	}
	beforeOutside := snapshotTree(t, outOutside)
	fault := &swapOnRenameFS{
		publicationFS: osPublicationFS{},
		parentPath:    parent,
		target:        outside,
	}
	publisher := &Publisher{fs: fault, nonce: func() string { return "parent-swap" }}
	if err := publisher.Publish(bundle, out); err == nil {
		t.Fatal("parent swap publication unexpectedly succeeded")
	}
	if fault.swapErr != nil {
		t.Fatalf("injected parent swap failed: %v", fault.swapErr)
	}
	afterOutside := snapshotTree(t, outOutside)
	if !reflect.DeepEqual(beforeOutside, afterOutside) {
		t.Fatalf("parent swap redirected mutation to outside tree:\nbefore=%#v\nafter=%#v", beforeOutside, afterOutside)
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("parent swap did not leave a symlink at %q", parent)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fault.savedPath, parent); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRejectsHandleOpenedOnSwappedAndRestoredParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux openat2 directory handles")
	}
	anchor := t.TempDir()
	oldBundle := publicationTestBundle(File{Path: "old.txt", Data: []byte("old\n")})
	expectedBundle := publicationTestBundle(File{Path: "expected.txt", Data: []byte("expected\n")})
	otherBundle := publicationTestBundle(File{Path: "other.txt", Data: []byte("other\n")})

	publishA := filepath.Join(anchor, "publish-a")
	publishB := filepath.Join(anchor, "publish-b")
	if err := os.Mkdir(publishA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(publishB, 0o700); err != nil {
		t.Fatal(err)
	}
	publishOutA := filepath.Join(publishA, "bundle")
	publishOutB := filepath.Join(publishB, "bundle")
	if err := Publish(oldBundle, publishOutA); err != nil {
		t.Fatal(err)
	}
	if err := Publish(otherBundle, publishOutB); err != nil {
		t.Fatal(err)
	}
	beforePublishA := snapshotTree(t, publishOutA)
	beforePublishB := snapshotTree(t, publishOutB)
	publishFault := &swapOpenRestoreFS{
		publicationFS: osPublicationFS{},
		target:        publishA,
		replacement:   publishB,
	}
	publishErr := (&Publisher{fs: publishFault, nonce: func() string { return "open-restore-publish" }}).Publish(expectedBundle, publishOutA)
	if publishErr == nil || !strings.Contains(publishErr.Error(), "opened directory") {
		t.Fatalf("publish swap-open-restore error = %v, want opened-directory identity error", publishErr)
	}
	if !publishFault.swapped {
		t.Fatal("publish regression did not execute the swap-open-restore hook")
	}
	if after := snapshotTree(t, publishOutA); !reflect.DeepEqual(beforePublishA, after) {
		t.Fatalf("publish changed validated parent tree:\nbefore=%#v\nafter=%#v", beforePublishA, after)
	}
	if after := snapshotTree(t, publishOutB); !reflect.DeepEqual(beforePublishB, after) {
		t.Fatalf("publish changed swapped-in parent tree:\nbefore=%#v\nafter=%#v", beforePublishB, after)
	}

	checkA := filepath.Join(anchor, "check-a")
	checkB := filepath.Join(anchor, "check-b")
	if err := os.Mkdir(checkA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(checkB, 0o700); err != nil {
		t.Fatal(err)
	}
	checkOutA := filepath.Join(checkA, "bundle")
	checkOutB := filepath.Join(checkB, "bundle")
	if err := Publish(expectedBundle, checkOutA); err != nil {
		t.Fatal(err)
	}
	if err := Publish(otherBundle, checkOutB); err != nil {
		t.Fatal(err)
	}
	beforeCheckA := snapshotTree(t, checkOutA)
	beforeCheckB := snapshotTree(t, checkOutB)
	checkFault := &swapOpenRestoreFS{
		publicationFS: osPublicationFS{},
		target:        checkA,
		replacement:   checkB,
	}
	if _, err := (&Publisher{fs: checkFault}).Check(expectedBundle, checkOutA); err == nil || !strings.Contains(err.Error(), "opened directory") {
		t.Fatalf("check swap-open-restore error = %v, want opened-directory identity error", err)
	}
	if !checkFault.swapped {
		t.Fatal("check regression did not execute the swap-open-restore hook")
	}
	if after := snapshotTree(t, checkOutA); !reflect.DeepEqual(beforeCheckA, after) {
		t.Fatalf("check changed validated parent tree:\nbefore=%#v\nafter=%#v", beforeCheckA, after)
	}
	if after := snapshotTree(t, checkOutB); !reflect.DeepEqual(beforeCheckB, after) {
		t.Fatalf("check changed swapped-in parent tree:\nbefore=%#v\nafter=%#v", beforeCheckB, after)
	}
}

func TestPublicationRejectsMarkerOpenedOnSwappedAndRestoredFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux openat2 directory handles")
	}
	anchor := t.TempDir()
	parentA := filepath.Join(anchor, "file-a")
	parentB := filepath.Join(anchor, "file-b")
	if err := os.Mkdir(parentA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parentB, 0o700); err != nil {
		t.Fatal(err)
	}
	outA := filepath.Join(parentA, "bundle")
	outB := filepath.Join(parentB, "bundle")
	if err := os.Mkdir(outA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outA, OwnershipMarkerPath), []byte("operator-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outA, "operator.txt"), []byte("operator\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherBundle := publicationTestBundle(File{Path: "other.txt", Data: []byte("other\n")})
	if err := Publish(otherBundle, outB); err != nil {
		t.Fatal(err)
	}
	expectedBundle := publicationTestBundle(File{Path: "expected.txt", Data: []byte("expected\n")})
	beforeA := snapshotTree(t, outA)
	beforeB := snapshotTree(t, outB)
	fault := &swapOpenRestoreFileFS{
		publicationFS: osPublicationFS{},
		target:        filepath.Join(outA, OwnershipMarkerPath),
		replacement:   filepath.Join(outB, OwnershipMarkerPath),
	}
	err := (&Publisher{fs: fault, nonce: func() string { return "file-open-restore" }}).Publish(expectedBundle, outA)
	if err == nil || !strings.Contains(err.Error(), "changed during open") {
		t.Fatalf("marker swap-open-restore error = %v, want file identity error", err)
	}
	if fault.swaps == 0 {
		t.Fatal("marker regression did not execute the swap-open-restore hook")
	}
	if fault.swapErr != nil {
		t.Fatalf("injected marker swap failed: %v", fault.swapErr)
	}
	if after := snapshotTree(t, outA); !reflect.DeepEqual(beforeA, after) {
		t.Fatalf("publish changed unowned marker tree:\nbefore=%#v\nafter=%#v", beforeA, after)
	}
	if after := snapshotTree(t, outB); !reflect.DeepEqual(beforeB, after) {
		t.Fatalf("publish changed swapped-in marker tree:\nbefore=%#v\nafter=%#v", beforeB, after)
	}
}

func TestPublicationRefusesInterruptedTransactionArtifacts(t *testing.T) {
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	stale := filepath.Join(parent, ".bundle.fleetgen-stage-stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	bundle := publicationTestBundle(File{Path: "a.txt", Data: []byte("a\n")})
	if err := Publish(bundle, out); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("stale transaction error = %v", err)
	}
}

type renameFaultFS struct {
	publicationFS
	failAt       int
	count        int
	failRemoveAt int
	removeCount  int
}

func (f *renameFaultFS) OpenDir(path string) (publicationDir, error) {
	dir, err := f.publicationFS.OpenDir(path)
	if err != nil {
		return nil, err
	}
	return &renameFaultDir{publicationDir: dir, owner: f, path: path}, nil
}

type renameFaultDir struct {
	publicationDir
	owner *renameFaultFS
	path  string
}

func (d *renameFaultDir) OpenDir(name string) (publicationDir, error) {
	dir, err := d.publicationDir.OpenDir(name)
	if err != nil {
		return nil, err
	}
	return &renameFaultDir{publicationDir: dir, owner: d.owner, path: filepath.Join(d.path, name)}, nil
}

func (d *renameFaultDir) Rename(oldPath, newPath string) error {
	f := d.owner
	f.count++
	if f.count == f.failAt {
		return fmt.Errorf("injected rename failure")
	}
	return d.publicationDir.Rename(oldPath, newPath)
}

func (d *renameFaultDir) Remove(path string, directory bool) error {
	f := d.owner
	f.removeCount++
	if f.failRemoveAt > 0 && f.removeCount == f.failRemoveAt {
		return fmt.Errorf("injected remove failure")
	}
	return d.publicationDir.Remove(path, directory)
}

type swapOnReadFS struct {
	publicationFS
	baseName    string
	triggerAt   int
	target      string
	readCount   int
	swapErr     error
	swappedPath string
	savedPath   string
	mu          sync.Mutex
}

func (f *swapOnReadFS) OpenDir(path string) (publicationDir, error) {
	dir, err := f.publicationFS.OpenDir(path)
	if err != nil {
		return nil, err
	}
	return &swapOnReadDir{publicationDir: dir, owner: f, path: path}, nil
}

type swapOnReadDir struct {
	publicationDir
	owner *swapOnReadFS
	path  string
}

type swapBeforeOpenFS struct {
	publicationFS
	baseName    string
	triggerAt   int
	replacement string
	openCount   int
	swapped     bool
	swapErr     error
	mu          sync.Mutex
}

func (f *swapBeforeOpenFS) OpenDir(path string) (publicationDir, error) {
	dir, err := f.publicationFS.OpenDir(path)
	if err != nil {
		return nil, err
	}
	return &swapBeforeOpenDir{publicationDir: dir, owner: f, path: path}, nil
}

type swapBeforeOpenDir struct {
	publicationDir
	owner *swapBeforeOpenFS
	path  string
}

func (d *swapBeforeOpenDir) OpenDir(name string) (publicationDir, error) {
	childPath := filepath.Join(d.path, name)
	d.owner.mu.Lock()
	shouldSwap := filepath.Base(childPath) == d.owner.baseName
	if shouldSwap {
		d.owner.openCount++
		shouldSwap = d.owner.openCount == d.owner.triggerAt
	}
	d.owner.mu.Unlock()
	if shouldSwap {
		return d.owner.openSwapped(d.publicationDir, name, childPath)
	}
	child, err := d.publicationDir.OpenDir(name)
	if err != nil {
		return nil, err
	}
	return &swapBeforeOpenDir{publicationDir: child, owner: d.owner, path: childPath}, nil
}

func (f *swapBeforeOpenFS) openSwapped(parent publicationDir, name, path string) (publicationDir, error) {
	saved := path + ".fleetgen-open-saved"
	if err := os.Rename(path, saved); err != nil {
		return nil, err
	}
	if err := os.Rename(f.replacement, path); err != nil {
		_ = os.Rename(saved, path)
		f.mu.Lock()
		f.swapErr = err
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Lock()
	f.swapped = true
	f.mu.Unlock()
	opened, openErr := parent.OpenDir(name)
	restoreErr := errors.Join(os.Rename(path, f.replacement), os.Rename(saved, path))
	if openErr != nil || restoreErr != nil {
		if opened != nil {
			_ = opened.Close()
		}
		err := errors.Join(openErr, restoreErr)
		f.mu.Lock()
		f.swapErr = err
		f.mu.Unlock()
		return nil, err
	}
	return &swapBeforeOpenDir{publicationDir: opened, owner: f, path: path}, nil
}

func (d *swapOnReadDir) OpenDir(name string) (publicationDir, error) {
	dir, err := d.publicationDir.OpenDir(name)
	if err != nil {
		return nil, err
	}
	return &swapOnReadDir{publicationDir: dir, owner: d.owner, path: filepath.Join(d.path, name)}, nil
}

func (d *swapOnReadDir) ReadDir() ([]os.DirEntry, error) {
	f := d.owner
	f.mu.Lock()
	shouldSwap := filepath.Base(d.path) == f.baseName
	if shouldSwap {
		f.readCount++
		shouldSwap = f.readCount == f.triggerAt
	}
	if shouldSwap {
		f.swappedPath = d.path
		f.savedPath = d.path + ".fleetgen-saved"
		if err := os.Rename(f.swappedPath, f.savedPath); err != nil {
			f.swapErr = err
		} else if err := os.Symlink(f.target, f.swappedPath); err != nil {
			f.swapErr = err
			_ = os.Rename(f.savedPath, f.swappedPath)
		}
	}
	swapErr := f.swapErr
	f.mu.Unlock()
	if swapErr != nil {
		return nil, swapErr
	}
	return d.publicationDir.ReadDir()
}

type swapOnRenameFS struct {
	publicationFS
	parentPath string
	target     string
	savedPath  string
	swapErr    error
	swapped    bool
	mu         sync.Mutex
}

func (f *swapOnRenameFS) OpenDir(path string) (publicationDir, error) {
	dir, err := f.publicationFS.OpenDir(path)
	if err != nil {
		return nil, err
	}
	return &swapOnRenameDir{publicationDir: dir, owner: f, path: path}, nil
}

type swapOpenRestoreFS struct {
	publicationFS
	target      string
	replacement string
	swapped     bool
}

func (f *swapOpenRestoreFS) OpenDir(path string) (publicationDir, error) {
	if path != f.target {
		return f.publicationFS.OpenDir(path)
	}
	f.swapped = true
	saved := f.target + ".fleetgen-open-saved"
	if err := os.Rename(f.target, saved); err != nil {
		return nil, fmt.Errorf("swap target aside: %w", err)
	}
	if err := os.Rename(f.replacement, f.target); err != nil {
		_ = os.Rename(saved, f.target)
		return nil, fmt.Errorf("swap replacement into target: %w", err)
	}
	opened, openErr := f.publicationFS.OpenDir(path)
	restoreErr := errors.Join(os.Rename(f.target, f.replacement), os.Rename(saved, f.target))
	if openErr != nil || restoreErr != nil {
		if opened != nil {
			_ = opened.Close()
		}
		return nil, errors.Join(openErr, restoreErr)
	}
	return opened, nil
}

type swapOpenRestoreFileFS struct {
	publicationFS
	target      string
	replacement string
	swaps       int
	swapErr     error
}

func (f *swapOpenRestoreFileFS) OpenDir(path string) (publicationDir, error) {
	dir, err := f.publicationFS.OpenDir(path)
	if err != nil {
		return nil, err
	}
	return &swapOpenRestoreFileDir{publicationDir: dir, owner: f, path: path}, nil
}

type swapOpenRestoreFileDir struct {
	publicationDir
	owner *swapOpenRestoreFileFS
	path  string
}

func (d *swapOpenRestoreFileDir) OpenDir(name string) (publicationDir, error) {
	child, err := d.publicationDir.OpenDir(name)
	if err != nil {
		return nil, err
	}
	return &swapOpenRestoreFileDir{publicationDir: child, owner: d.owner, path: filepath.Join(d.path, name)}, nil
}

func (d *swapOpenRestoreFileDir) OpenFile(name string, flags int, mode os.FileMode) (publicationFile, error) {
	path := filepath.Join(d.path, name)
	if path != d.owner.target {
		return d.publicationDir.OpenFile(name, flags, mode)
	}
	saved := path + ".fleetgen-open-saved"
	if err := os.Rename(path, saved); err != nil {
		return nil, err
	}
	if err := os.Rename(d.owner.replacement, path); err != nil {
		_ = os.Rename(saved, path)
		d.owner.swapErr = err
		return nil, err
	}
	d.owner.swaps++
	opened, openErr := d.publicationDir.OpenFile(name, flags, mode)
	restoreErr := errors.Join(os.Rename(path, d.owner.replacement), os.Rename(saved, path))
	if openErr != nil || restoreErr != nil {
		if opened != nil {
			_ = opened.Close()
		}
		err := errors.Join(openErr, restoreErr)
		d.owner.swapErr = err
		return nil, err
	}
	return opened, nil
}

type swapOnRenameDir struct {
	publicationDir
	owner *swapOnRenameFS
	path  string
}

func (d *swapOnRenameDir) OpenDir(name string) (publicationDir, error) {
	dir, err := d.publicationDir.OpenDir(name)
	if err != nil {
		return nil, err
	}
	return &swapOnRenameDir{publicationDir: dir, owner: d.owner, path: filepath.Join(d.path, name)}, nil
}

func (d *swapOnRenameDir) Rename(oldName, newName string) error {
	f := d.owner
	f.mu.Lock()
	if d.path == f.parentPath && !f.swapped {
		f.swapped = true
		f.savedPath = f.parentPath + ".fleetgen-saved"
		if err := os.Rename(f.parentPath, f.savedPath); err != nil {
			f.swapErr = err
		} else if err := os.Symlink(f.target, f.parentPath); err != nil {
			f.swapErr = err
			_ = os.Rename(f.savedPath, f.parentPath)
		}
	}
	swapErr := f.swapErr
	f.mu.Unlock()
	if swapErr != nil {
		return swapErr
	}
	return d.publicationDir.Rename(oldName, newName)
}

type treeSnapshotEntry struct {
	mode    fs.FileMode
	size    int64
	modTime time.Time
	data    []byte
}

func snapshotTree(t *testing.T, root string) map[string]treeSnapshotEntry {
	t.Helper()
	result := map[string]treeSnapshotEntry{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := treeSnapshotEntry{mode: info.Mode(), size: info.Size(), modTime: info.ModTime()}
		if info.Mode().IsRegular() {
			value.data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertNoTransactionArtifacts(t *testing.T, parent, base string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+base+".fleetgen-") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) != 0 {
		t.Fatalf("transaction artifacts remain: %v", names)
	}
}

func sameTreeBytes(left, right map[string]treeSnapshotEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for path, leftEntry := range left {
		rightEntry, ok := right[path]
		if !ok || leftEntry.mode.IsDir() != rightEntry.mode.IsDir() || !bytesEqual(leftEntry.data, rightEntry.data) {
			return false
		}
	}
	return true
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
