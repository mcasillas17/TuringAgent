package repository

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// The vault is the user's Obsidian folder, and in Obsidian a rename is a rename:
// two notes trade names, a note is renamed onto the name a deleted one just
// gave up, three notes shift down a chain. The index keys notes by identity, so
// each of those is a path change on an existing row — but the path column is
// UNIQUE, so a pass that writes the new paths one at a time walks straight into
// the old ones. These tests are about that window: every one of them fails with
// a UNIQUE constraint error, and takes the whole pass down with it, unless the
// contested paths are vacated before the real ones are written.

// storedNotePaths returns every path in the projection, read as Go strings.
//
// The comparison is done here rather than in SQL on purpose: a parked path
// carries a NUL byte, and SQLite's own string functions stop at the first one,
// so `instr(path, ...)` would happily report a sentinel as absent. The driver
// hands back the whole byte string, so Go can see what SQLite's builtins would
// hide.
func storedNotePaths(t *testing.T, database *db.DB) map[string]string {
	t.Helper()
	rows, err := database.QueryContext(ctx(), `SELECT id, path FROM memory_notes`)
	if err != nil {
		t.Fatalf("read note paths: %v", err)
	}
	defer func() { _ = rows.Close() }()
	paths := map[string]string{}
	for rows.Next() {
		var noteID, path string
		if err := rows.Scan(&noteID, &path); err != nil {
			t.Fatalf("scan note path: %v", err)
		}
		paths[noteID] = path
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read note paths: %v", err)
	}
	return paths
}

// requireNoParkedPaths is the invariant every one of these tests ends on: a
// vacated path is a device for getting through one transaction, and a user who
// opens the database afterwards must never find one. Commit or rollback, the
// projection holds real vault paths only.
func requireNoParkedPaths(t *testing.T, database *db.DB) {
	t.Helper()
	for noteID, path := range storedNotePaths(t, database) {
		if strings.Contains(path, memoryNotePathParkPrefix) {
			t.Fatalf("note %s kept a parked path %q", noteID, path)
		}
		if strings.ContainsRune(path, 0) {
			t.Fatalf("note %s holds a path with a NUL byte: %q", noteID, path)
		}
		if !strings.HasPrefix(path, memoryfiles.BeliefsDirName+"/") {
			t.Fatalf("note %s holds a path outside beliefs/: %q", noteID, path)
		}
	}
}

func requireNotePath(t *testing.T, repo *Repository, noteID string, want string) MemoryNote {
	t.Helper()
	note, ok := noteRowFor(t, repo, noteID)
	if !ok {
		t.Fatalf("note %s left the index", noteID)
	}
	if note.Path != want {
		t.Fatalf("note %s is at %q, want %q", noteID, note.Path, want)
	}
	return note
}

func renameVaultNote(t *testing.T, vault *memoryfiles.Vault, from string, to string) {
	t.Helper()
	source := filepath.Join(vault.Root(), filepath.FromSlash(from))
	target := filepath.Join(vault.Root(), filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("prepare %q: %v", to, err)
	}
	if err := os.Rename(source, target); err != nil {
		t.Fatalf("rename %q to %q: %v", from, to, err)
	}
}

func removeVaultNote(t *testing.T, vault *memoryfiles.Vault, relPath string) {
	t.Helper()
	if err := os.Remove(filepath.Join(vault.Root(), filepath.FromSlash(relPath))); err != nil {
		t.Fatalf("remove %q: %v", relPath, err)
	}
}

// Two notes trading names is the plainest form of the collision: neither file
// is gone, neither identity changed, and every path the pass wants to write is
// held by the other note. Nothing may be lost to it — not the rows, not the
// evidence, and not the bodies, which must end up under the names the user
// gave them.
func TestSwappingTwoNotePathsKeepsBothNotesAndTheirEvidence(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	alpha, beta := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(alpha, []string{session}, "prefers espresso"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(beta, []string{session}, "cycles to work"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	requireNotePath(t, repo, alpha, "beliefs/alpha.md")
	requireNotePath(t, repo, beta, "beliefs/beta.md")

	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/swap.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/swap.md", "beliefs/beta.md")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh after swap: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("a swap retired %d notes, want none", report.Removed)
	}

	movedAlpha := requireNotePath(t, repo, alpha, "beliefs/beta.md")
	movedBeta := requireNotePath(t, repo, beta, "beliefs/alpha.md")
	if !strings.Contains(movedAlpha.Content, "prefers espresso") {
		t.Fatalf("note %s lost its body: %q", alpha, movedAlpha.Content)
	}
	if !strings.Contains(movedBeta.Content, "cycles to work") {
		t.Fatalf("note %s lost its body: %q", beta, movedBeta.Content)
	}
	for _, noteID := range []string{alpha, beta} {
		if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
			t.Fatalf("note %s lost its evidence: %v", noteID, got)
		}
	}
	if actions := auditActions(t, database); actions[memoryNoteRemovedAction] != 0 {
		t.Fatalf("a swap recorded %d removals, want none", actions[memoryNoteRemovedAction])
	}
	requireNoParkedPaths(t, database)
}

// The same swap, seen through search: a memory the user can no longer find is
// gone whatever the row says, so the index behind the search must follow the
// rename rather than keep answering with the old name.
func TestSearchFollowsNotesThroughAPathSwap(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	alpha, beta := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(alpha, []string{session}, "prefers espresso"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(beta, []string{session}, "cycles to work"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/swap.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/swap.md", "beliefs/beta.md")
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after swap: %v", err)
	}

	for _, want := range []struct {
		query  string
		noteID string
		path   string
	}{
		{"espresso", alpha, "beliefs/beta.md"},
		{"cycles", beta, "beliefs/alpha.md"},
	} {
		hits, err := repo.SearchMemoryNotes(ctx(), want.query, 10)
		if err != nil {
			t.Fatalf("search %q: %v", want.query, err)
		}
		if len(hits) != 1 {
			t.Fatalf("search %q returned %d hits, want 1", want.query, len(hits))
		}
		if hits[0].NoteID != want.noteID || hits[0].Path != want.path {
			t.Fatalf("search %q found %s at %q, want %s at %q", want.query, hits[0].NoteID, hits[0].Path, want.noteID, want.path)
		}
	}
	requireNoParkedPaths(t, database)
}

// A chain is what a batch rename in Obsidian looks like: every note shifts onto
// the name the next one is still holding. This one closes into a cycle, so
// there is no note to start from whose new name happens to be free — a pass
// that writes the new paths one at a time collides whatever order it picks.
func TestAChainOfRenamesResolvesInASinglePass(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	first, second, third := newTestNoteID(t), newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/one.md", managedBelief(first, []string{session}, "note one"))
	writeVaultNote(t, vault, "beliefs/two.md", managedBelief(second, []string{session}, "note two"))
	writeVaultNote(t, vault, "beliefs/three.md", managedBelief(third, []string{session}, "note three"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// one -> two -> three -> one, rotated through a scratch name because the
	// filesystem cannot hold two files under one name either.
	renameVaultNote(t, vault, "beliefs/three.md", "beliefs/rotate.md")
	renameVaultNote(t, vault, "beliefs/two.md", "beliefs/three.md")
	renameVaultNote(t, vault, "beliefs/one.md", "beliefs/two.md")
	renameVaultNote(t, vault, "beliefs/rotate.md", "beliefs/one.md")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh after chain rename: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("a chain rename retired %d notes, want none", report.Removed)
	}
	requireNotePath(t, repo, first, "beliefs/two.md")
	requireNotePath(t, repo, second, "beliefs/three.md")
	requireNotePath(t, repo, third, "beliefs/one.md")
	for _, noteID := range []string{first, second, third} {
		if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
			t.Fatalf("note %s lost its evidence: %v", noteID, got)
		}
	}
	requireNoParkedPaths(t, database)

	// A plain shift down the chain, where every name a note wants is one the
	// next note is vacating, has to come out the same way.
	renameVaultNote(t, vault, "beliefs/three.md", "beliefs/four.md")
	renameVaultNote(t, vault, "beliefs/two.md", "beliefs/three.md")
	renameVaultNote(t, vault, "beliefs/one.md", "beliefs/two.md")
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after shifting the chain: %v", err)
	}
	requireNotePath(t, repo, first, "beliefs/three.md")
	requireNotePath(t, repo, second, "beliefs/four.md")
	requireNotePath(t, repo, third, "beliefs/two.md")
	requireNoParkedPaths(t, database)
}

// Deleting one note and renaming another onto the name it just gave up is one
// gesture in a file manager and two facts to the index: a row whose file is
// gone, and a row whose path is now held by the deleted note's name. The pass
// has to do both, in the right order, or it collides on the way in.
func TestRenamingANoteOntoAVacatedPathRetiresOnlyTheDeletedNote(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	deleted, survivor := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(deleted, []string{session}, "about to go"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(survivor, []string{session}, "here to stay"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	removeVaultNote(t, vault, "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh after rename over a vacated path: %v", err)
	}
	if report.Removed != 1 {
		t.Fatalf("removed = %d, want only the deleted note retired", report.Removed)
	}
	moved := requireNotePath(t, repo, survivor, "beliefs/alpha.md")
	if !strings.Contains(moved.Content, "here to stay") {
		t.Fatalf("the surviving note lost its body: %q", moved.Content)
	}
	if got := evidenceSessions(t, repo, survivor); len(got) != 1 || got[0] != session {
		t.Fatalf("the surviving note lost its evidence: %v", got)
	}
	if _, ok := noteRowFor(t, repo, deleted); ok {
		t.Fatalf("the deleted note %s is still indexed", deleted)
	}
	removals := 0
	for _, row := range auditRows(t, database) {
		if row.Action != memoryNoteRemovedAction {
			continue
		}
		removals++
		if row.Target != deleted {
			t.Fatalf("a removal was recorded for %s, want only %s", row.Target, deleted)
		}
	}
	if removals != 1 {
		t.Fatalf("recorded %d removals, want 1", removals)
	}
	requireNoParkedPaths(t, database)
}

// A note whose identity the scan can see but cannot trust — two files claiming
// it — is not a note whose row may be pushed off its path to make room for
// somebody else, because its evidence is the thing at stake and the contest may
// be a copy the user is about to delete. The claimant waits instead: its own
// row stays where it was, valid and intact, and neither note is retired.
func TestAContestedIncumbentIsNeverEvictedToMakeRoom(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	contested, claimant := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(contested, []string{session}, "contested belief"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(claimant, []string{session}, "claiming belief"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// The contested note is now duplicated under two new names, so the scan
	// knows its identity but refuses to index it; the claimant moves onto the
	// path the contested note's row still holds.
	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/copy-one.md")
	writeVaultNote(t, vault, "beliefs/copy-two.md", managedBelief(contested, []string{session}, "contested belief"))
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh with a contested incumbent: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("removed = %d, want nothing retired while an identity is contested", report.Removed)
	}
	requireNotePath(t, repo, contested, "beliefs/alpha.md")
	requireNotePath(t, repo, claimant, "beliefs/beta.md")
	for _, noteID := range []string{contested, claimant} {
		if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
			t.Fatalf("note %s lost its evidence: %v", noteID, got)
		}
	}
	requireNoParkedPaths(t, database)

	// Once the user deletes the copy the contest is over and the same pass
	// resolves the rename it had been holding back.
	removeVaultNote(t, vault, "beliefs/copy-two.md")
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after the contest ends: %v", err)
	}
	requireNotePath(t, repo, contested, "beliefs/copy-one.md")
	requireNotePath(t, repo, claimant, "beliefs/alpha.md")
	requireNoParkedPaths(t, database)
}

// A note the walk could not enumerate is not a note whose row may be evicted:
// with beliefs incomplete nothing is retired, so an incumbent pushed off its
// path would be left holding a vacated one forever. The claimant waits, and
// both rows stay exactly as they were.
func TestNoPathIsVacatedWhenBeliefsCouldNotBeEnumerated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a folder with no permissions, so there is no enumeration failure to cause")
	}
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	stale, claimant, hidden := newTestNoteID(t), newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(stale, []string{session}, "an older belief"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(claimant, []string{session}, "a claiming belief"))
	writeVaultNote(t, vault, "beliefs/locked/gamma.md", managedBelief(hidden, []string{session}, "a hidden belief"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	// The same rename-over-a-vacated-path the pass would normally resolve, but
	// with a corner of beliefs/ the walk cannot read: it no longer knows that
	// the incumbent's file is gone rather than sitting behind the locked door.
	removeVaultNote(t, vault, "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")
	locked := filepath.Join(vault.Root(), memoryfiles.BeliefsDirName, "locked")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("lock %q: %v", locked, err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh with an unreadable subfolder: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("removed = %d, want nothing retired from a walk that could not look", report.Removed)
	}
	requireNotePath(t, repo, stale, "beliefs/alpha.md")
	requireNotePath(t, repo, claimant, "beliefs/beta.md")
	requireNotePath(t, repo, hidden, "beliefs/locked/gamma.md")
	for _, noteID := range []string{stale, claimant, hidden} {
		if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
			t.Fatalf("note %s lost its evidence: %v", noteID, got)
		}
	}
	requireNoParkedPaths(t, database)

	// Once the walk can read the whole area again, the rename it was holding
	// back resolves and the note whose file really is gone leaves the index.
	if err := os.Chmod(locked, 0o700); err != nil {
		t.Fatalf("unlock %q: %v", locked, err)
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after unlocking: %v", err)
	}
	requireNotePath(t, repo, claimant, "beliefs/alpha.md")
	requireNotePath(t, repo, hidden, "beliefs/locked/gamma.md")
	if _, ok := noteRowFor(t, repo, stale); ok {
		t.Fatalf("the deleted note %s is still indexed once the walk could see", stale)
	}
	requireNoParkedPaths(t, database)
}

// Vacating a path and writing the real one are two statements, and a crash
// between them would leave a row holding a value no vault could ever produce.
// They are in one transaction so that cannot outlive the pass: this drives a
// failure into exactly that window and holds the projection to its previous,
// valid state.
func TestAFailureAfterVacatingPathsRollsBackToValidPaths(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	alpha, beta := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(alpha, []string{session}, "prefers espresso"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(beta, []string{session}, "cycles to work"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	before := storedNotePaths(t, database)

	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/swap.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/swap.md", "beliefs/beta.md")

	failure := errors.New("interrupted between vacating and writing")
	calls := 0
	repo.memoryIndexParkBarrier = func() error {
		calls++
		return failure
	}
	if _, err := repo.RefreshMemoryIndex(ctx()); !errors.Is(err, failure) {
		t.Fatalf("refresh error = %v, want the injected failure", err)
	}
	if calls != 1 {
		t.Fatalf("the barrier ran %d times, want exactly one pass through the vacating window", calls)
	}
	repo.memoryIndexParkBarrier = nil

	after := storedNotePaths(t, database)
	if len(after) != len(before) {
		t.Fatalf("row count changed across a rolled-back pass: %d -> %d", len(before), len(after))
	}
	for noteID, path := range before {
		if after[noteID] != path {
			t.Fatalf("note %s moved to %q across a rolled-back pass, want %q", noteID, after[noteID], path)
		}
	}
	requireNoParkedPaths(t, database)

	// The pass that follows the failure still resolves the swap, so the
	// rollback costs the user nothing beyond one pass.
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after the failure: %v", err)
	}
	requireNotePath(t, repo, alpha, "beliefs/beta.md")
	requireNotePath(t, repo, beta, "beliefs/alpha.md")
	requireNoParkedPaths(t, database)
}

// A vacated path must be something the vault would refuse outright, and it must
// name exactly one note: a value that could collide with another parked row, or
// that some other code path could mistake for a file, would trade one UNIQUE
// failure for a worse one.
func TestAVacatedPathCannotBeAVaultPathAndNamesOneNote(t *testing.T) {
	_, vault, _ := newMemoryTestRepo(t)
	first, second := "note-one", "note-two"
	parkedFirst := parkedMemoryNotePath(first)
	parkedSecond := parkedMemoryNotePath(second)

	if parkedFirst == parkedSecond {
		t.Fatalf("two notes park to the same value %q", parkedFirst)
	}
	if !strings.Contains(parkedFirst, first) {
		t.Fatalf("parked value %q does not name its note", parkedFirst)
	}
	for _, parked := range []string{parkedFirst, parkedSecond} {
		// Asked of the vault itself rather than of a copy of its rules: the
		// guarantee is that this value can never come back as a real file.
		var confinement *memoryfiles.ConfinementError
		if err := vault.RemoveInboxNote(ctx(), parked); !errors.As(err, &confinement) {
			t.Fatalf("the vault did not refuse the parked value %q outright: %v", parked, err)
		}
		if !strings.Contains(parked, "\x00") {
			t.Fatalf("parked value %q could be typed as a filename", parked)
		}
	}
}

// Parking is bookkeeping, not a decision about the user's memory, so a pass
// that only shuffles paths must read the same as one that changed nothing.
func TestVacatingPathsRecordsNothingOfItsOwn(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	alpha, beta := newTestNoteID(t), newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(alpha, []string{session}, "prefers espresso"))
	writeVaultNote(t, vault, "beliefs/beta.md", managedBelief(beta, []string{session}, "cycles to work"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	before := auditActions(t, database)

	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/swap.md")
	renameVaultNote(t, vault, "beliefs/beta.md", "beliefs/alpha.md")
	renameVaultNote(t, vault, "beliefs/swap.md", "beliefs/beta.md")
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after swap: %v", err)
	}

	after := auditActions(t, database)
	added := map[string]int{}
	for action, count := range after {
		if delta := count - before[action]; delta != 0 {
			added[action] = delta
		}
	}
	if len(added) != 0 {
		actions := make([]string, 0, len(added))
		for action := range added {
			actions = append(actions, action)
		}
		sort.Strings(actions)
		t.Fatalf("a pure rename recorded %v", actions)
	}
	requireNoParkedPaths(t, database)
}

// The sweep is supposed to spare a row whose identity the walk saw, not only
// one whose recorded path it saw — otherwise a note the user duplicated and
// renamed in the same sitting is read as a note whose file is gone, and its
// citations go with it. Duplication is exactly when that happens: the identity
// is contested, so the note is not indexed and its row never learns the new
// name, while the old name is no longer in the vault.
func TestADuplicatedIdentityIsNotRetiredWhenItsFileWasRenamed(t *testing.T) {
	repo, vault, database := newMemoryTestRepo(t)
	session := newMemoryTestSession(t, repo)
	noteID := newTestNoteID(t)

	writeVaultNote(t, vault, "beliefs/alpha.md", managedBelief(noteID, []string{session}, "prefers espresso"))
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	requireNotePath(t, repo, noteID, "beliefs/alpha.md")

	// One gesture in Obsidian: duplicate the note, then rename the original.
	// Both files now claim the identity, and neither is called alpha.md.
	writeVaultNote(t, vault, "beliefs/copy.md", managedBelief(noteID, []string{session}, "prefers espresso"))
	renameVaultNote(t, vault, "beliefs/alpha.md", "beliefs/final.md")

	report, err := repo.RefreshMemoryIndex(ctx())
	if err != nil {
		t.Fatalf("refresh with a duplicated, renamed identity: %v", err)
	}
	if report.Removed != 0 {
		t.Fatalf("removed = %d, want a note the walk saw twice kept", report.Removed)
	}
	if _, ok := noteRowFor(t, repo, noteID); !ok {
		t.Fatalf("note %s was retired while both of its files were in the vault", noteID)
	}
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
		t.Fatalf("note %s lost its evidence: %v", noteID, got)
	}
	if actions := auditActions(t, database); actions[memoryNoteRemovedAction] != 0 {
		t.Fatalf("recorded %d removals for a note whose files are both present", actions[memoryNoteRemovedAction])
	}
	requireNoParkedPaths(t, database)

	// Once the user deletes the copy the identity is settled again and the
	// rename lands, on the row that was there all along.
	removeVaultNote(t, vault, "beliefs/copy.md")
	if _, err := repo.RefreshMemoryIndex(ctx()); err != nil {
		t.Fatalf("refresh after the duplicate is deleted: %v", err)
	}
	requireNotePath(t, repo, noteID, "beliefs/final.md")
	if got := evidenceSessions(t, repo, noteID); len(got) != 1 || got[0] != session {
		t.Fatalf("note %s lost its evidence: %v", noteID, got)
	}
	requireNoParkedPaths(t, database)
}
