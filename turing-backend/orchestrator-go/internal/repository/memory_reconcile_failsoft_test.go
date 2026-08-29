package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// flowMappingBelief is a note whose frontmatter the user wrote as a YAML flow
// mapping. It parses, it indexes, and it is the one shape a frontmatter splice
// cannot edit: every key shares one bracketed range, so setting a value means
// re-encoding the mapping and rewriting the rest of their keys with it.
//
// The vault refuses that write by name, which is right. What is not right is
// that one such note used to abort the whole writing pass — so a single file
// the user happened to write on one line stopped every *other* note from being
// adopted, kept the deletion resume from finishing, and took the app down at
// startup.
func flowMappingBelief(noteID string, body string) string {
	identity := ""
	if noteID != "" {
		identity = `id: "` + noteID + `", `
	}
	return "---\n{" + identity + `kind: "belief", title: "one line", managed: true, refs: []` + "}\n---\n\n" + body + "\n"
}

// sealVaultNote makes one note read-only, which is what a synced vault, a
// permissions mistake or a file open for writing elsewhere looks like from
// here.
func sealVaultNote(t *testing.T, root string, relPath string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.Chmod(full, 0o400); err != nil {
		t.Fatalf("seal %q: %v", relPath, err)
	}
	t.Cleanup(func() { _ = os.Chmod(full, 0o600) })
}

func reportNames(issues []MemoryNoteIssue) []string {
	names := make([]string, 0, len(issues))
	for _, issue := range issues {
		names = append(names, issue.RelPath)
	}
	return names
}

func issueFor(issues []MemoryNoteIssue, relPath string) (MemoryNoteIssue, bool) {
	for _, issue := range issues {
		if issue.RelPath == relPath {
			return issue, true
		}
	}
	return MemoryNoteIssue{}, false
}

// One note the pass cannot adopt is one note. The rest of the vault is still
// the user's memory, and it still has to be healed and indexed.
func TestReconcileAdoptsEveryOtherBeliefWhenOneCannotBeGivenAnIdentity(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writeVaultNote(t, vault, "beliefs/one-line.md", flowMappingBelief("", "The user writes YAML on one line."))
	writeVaultNote(t, vault, "beliefs/ordinary.md", "---\nkind: \"belief\"\ntitle: \"ordinary\"\nmanaged: true\nrefs: []\n---\n\nThe user keeps bees.\n")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("one note nobody can write took the whole pass down: %v", err)
	}
	if report.IdentitiesAssigned != 1 {
		t.Fatalf("identities assigned = %d, want the note that could be adopted", report.IdentitiesAssigned)
	}
	issue, found := issueFor(report.Index.Errors, "beliefs/one-line.md")
	if !found {
		t.Fatalf("the note that could not be adopted is invisible; errors = %v", reportNames(report.Index.Errors))
	}
	if issue.Reason == "" {
		t.Fatal("the reported failure says nothing about why the note could not be adopted")
	}
	if strings.Contains(issue.Reason, "one line") || strings.Contains(issue.Reason, "bees") {
		t.Fatalf("the reported failure carries the user's own prose: %q", issue.Reason)
	}
	if strings.Contains(readVaultNote(t, vault, "beliefs/one-line.md"), "id:") {
		t.Fatal("the refused note was rewritten anyway")
	}
	if !strings.Contains(readVaultNote(t, vault, "beliefs/ordinary.md"), "id:") {
		t.Fatal("the note beside it never got its identity")
	}
}

// The same rule for the second file-writing step. A note whose citations cannot
// be brought back in line is reported; the notes beside it are still rewritten,
// and the index still catches up with them.
func TestReconcileRewritesEveryOtherNoteWhenOneCannotBeRewritten(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	sessionID := newMemoryTestSession(t, repo)
	stubborn := newTestNoteID(t)
	ordinary := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/stubborn.md", managedBelief(stubborn, []string{sessionID}, "The user keeps bees."))
	writeVaultNote(t, vault, "beliefs/ordinary.md", managedBelief(ordinary, []string{sessionID}, "The user keeps chickens."))
	if _, err := repo.ReconcileMemoryVault(ctx()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	// The conversation both notes cite is deleted, so both files now have to
	// stop citing it — and one of them cannot be written.
	if err := repo.DeleteSessionForTests(ctx(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	sealVaultNote(t, vault.Root(), "beliefs/stubborn.md")

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("one unwritable note took the whole pass down: %v", err)
	}
	if report.RefsRewritten != 1 {
		t.Fatalf("refs rewritten = %d, want the note that could be written", report.RefsRewritten)
	}
	if _, found := issueFor(report.Index.Errors, "beliefs/stubborn.md"); !found {
		t.Fatalf("the note that could not be rewritten is invisible; errors = %v", reportNames(report.Index.Errors))
	}
	if !strings.Contains(readVaultNote(t, vault, "beliefs/ordinary.md"), "withdrawn") {
		t.Fatal("the note beside it never had its citations withdrawn")
	}
	if note, found := noteRowFor(t, repo, ordinary); !found || note.Status != MemoryNoteStatusWithdrawn {
		t.Fatalf("the rewritten note's row = %+v, want it marked withdrawn", note)
	}
}

// A pass that reported a per-note failure still did its whole job on every
// other note, so the deletion that triggered it is finished rather than left
// retryable forever.
func TestReconcileStillIndexesAFreshBeliefBesideANoteItCouldNotWrite(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	healed := newTestNoteID(t)
	writeVaultNote(t, vault, "beliefs/one-line.md", flowMappingBelief("", "The user writes YAML on one line."))
	writeVaultNote(t, vault, "beliefs/healed.md", managedBelief(healed, nil, "The user keeps bees."))

	report, err := repo.ReconcileMemoryVault(ctx())
	if err != nil {
		t.Fatalf("ReconcileMemoryVault: %v", err)
	}
	if note, found := noteRowFor(t, repo, healed); !found || note.Path != "beliefs/healed.md" {
		t.Fatalf("the belief beside the refused note was not indexed: %+v", note)
	}
	if _, found := issueFor(report.Index.Errors, "beliefs/one-line.md"); !found {
		t.Fatalf("the refused note is invisible; errors = %v", reportNames(report.Index.Errors))
	}
	found, err := repo.SearchMemoryNotes(ctx(), "bees", 10)
	if err != nil {
		t.Fatalf("SearchMemoryNotes: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("search found %d notes, want the belief the pass did index", len(found))
	}
}
