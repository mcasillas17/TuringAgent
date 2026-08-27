package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const handWrittenNote = "---\n" +
	"# a comment the user wrote\n" +
	"aliases:\n" +
	"  - \"Alt name\"\n" +
	"cssclasses: [wide, dense]\n" +
	"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
	"refs:\n" +
	"  - \"sess_withdrawn\"\n" +
	"  - \"sess_kept\"\n" +
	"\n" +
	"# trailing note about tags\n" +
	"tags:\n" +
	"  - memory\n" +
	"title:    'Loosely quoted'\n" +
	"---\n" +
	"\n" +
	"# Body heading\n" +
	"\n" +
	"Body text with   odd    spacing.\n" +
	"- a list\ttab\n"

func TestRewriteFrontmatterRefsPreservesEveryOtherByte(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{"sess_kept"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	want := strings.Replace(
		handWrittenNote,
		"  - \"sess_withdrawn\"\n  - \"sess_kept\"\n",
		"  - \"sess_kept\"\n",
		1,
	)
	if result.Content != want {
		t.Fatalf("rewrite was not byte-preserving:\nwant %q\ngot  %q", want, result.Content)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(onDisk) != want {
		t.Fatalf("on-disk bytes differ from the reported content:\nwant %q\ngot  %q", want, onDisk)
	}
}

func TestRewriteFrontmatterRefsWithdrawnRoundTripKeepsUserFormatting(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	withdrawn, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{"sess_kept"},
	})
	if err != nil {
		t.Fatalf("withdraw a ref: %v", err)
	}
	restored, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:             "beliefs/note.md",
		Refs:                []string{"sess_withdrawn", "sess_kept"},
		ExpectedContentHash: withdrawn.ContentHash,
	})
	if err != nil {
		t.Fatalf("restore the ref: %v", err)
	}
	if restored.Content != handWrittenNote {
		t.Fatalf("round trip lost the user's formatting:\nwant %q\ngot  %q", handWrittenNote, restored.Content)
	}
}

func TestRewriteFrontmatterRefsClearsTheListWithoutRemovingTheKey(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{},
	})
	if err != nil {
		t.Fatalf("clear refs: %v", err)
	}
	want := strings.Replace(
		handWrittenNote,
		"  - \"sess_withdrawn\"\n  - \"sess_kept\"\n",
		"  []\n",
		1,
	)
	if result.Content != want {
		t.Fatalf("cleared refs were not spliced:\nwant %q\ngot  %q", want, result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if len(parsed.Refs) != 0 {
		t.Fatalf("refs = %v", parsed.Refs)
	}
}

func TestRewriteFrontmatterRefsLeavesRefsAloneWhenUnset(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		NoteID:  "01ARZ3NDEKTSV4RRFFQ69G5FAW",
	})
	if err != nil {
		t.Fatalf("assign id: %v", err)
	}
	want := strings.Replace(
		handWrittenNote,
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n",
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n",
		1,
	)
	if result.Content != want {
		t.Fatalf("id assignment touched more than the id:\nwant %q\ngot  %q", want, result.Content)
	}
}

func TestRewriteFrontmatterRefsRewritesAFlowSequenceInPlace(t *testing.T) {
	vault := newTestVault(t)
	content := "---\nrefs: [\"a\", \"b\"]\ntitle: keep\n---\nbody\n"
	writeVaultFile(t, vault, "beliefs/flow.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/flow.md",
		Refs:    []string{"b"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	want := "---\nrefs: [\"b\"]\ntitle: keep\n---\nbody\n"
	if result.Content != want {
		t.Fatalf("flow rewrite:\nwant %q\ngot  %q", want, result.Content)
	}
}

func TestRewriteFrontmatterRefsFillsAnEmptyRefsKey(t *testing.T) {
	vault := newTestVault(t)
	content := "---\nrefs:\ntitle: keep\n---\nbody\n"
	writeVaultFile(t, vault, "beliefs/empty.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/empty.md",
		Refs:    []string{"sess_a"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	parsed, err := ParseNote("beliefs/empty.md", result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if strings.Join(parsed.Refs, ",") != "sess_a" {
		t.Fatalf("refs = %v (content %q)", parsed.Refs, result.Content)
	}
	if !strings.Contains(result.Content, "title: keep\n---\nbody\n") {
		t.Fatalf("surrounding bytes changed: %q", result.Content)
	}
}

func TestRewriteFrontmatterRefsAppendsMissingKeys(t *testing.T) {
	vault := newTestVault(t)
	content := "---\ntitle: keep\n---\nbody\n"
	writeVaultFile(t, vault, "beliefs/bare.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/bare.md",
		NoteID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Refs:    []string{"sess_a"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	if !strings.HasPrefix(result.Content, "---\ntitle: keep\n") {
		t.Fatalf("existing keys were reordered: %q", result.Content)
	}
	if !strings.HasSuffix(result.Content, "---\nbody\n") {
		t.Fatalf("body changed: %q", result.Content)
	}
	parsed, err := ParseNote("beliefs/bare.md", result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || strings.Join(parsed.Refs, ",") != "sess_a" {
		t.Fatalf("appended keys did not take: id=%q refs=%v", parsed.ID, parsed.Refs)
	}
}

func TestRewriteFrontmatterRefsAddsFrontmatterToAHandWrittenNote(t *testing.T) {
	vault := newTestVault(t)
	body := "# Just prose\n\nThe user wrote this in Obsidian.\n"
	writeVaultFile(t, vault, "beliefs/prose.md", body)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/prose.md",
		NoteID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	})
	if err != nil {
		t.Fatalf("assign an id to a hand-written note: %v", err)
	}
	if !strings.HasSuffix(result.Content, body) {
		t.Fatalf("the user's body was not preserved byte-for-byte: %q", result.Content)
	}
	parsed, err := ParseNote("beliefs/prose.md", result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("id = %q", parsed.ID)
	}
	if parsed.Managed {
		t.Fatal("assigning an identity must not claim the note as Turing-managed")
	}
}

func TestRewriteFrontmatterRefsWritesCandidatesUnderInbox(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedCandidate(t, vault, KindBelief, "Prefers dark mode", "The user prefers dark mode.")

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: candidate.RelPath,
		Refs:    []string{"sess_a", "sess_b"},
	})
	if err != nil {
		t.Fatalf("rewrite candidate refs: %v", err)
	}
	parsed, err := ParseNote(candidate.RelPath, result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if strings.Join(parsed.Refs, ",") != "sess_a,sess_b" {
		t.Fatalf("refs = %v", parsed.Refs)
	}
}

func TestRewriteFrontmatterRefsRefusesEverythingOutsideNotes(t *testing.T) {
	vault := newTestVault(t)
	personaPath := writeVaultFile(t, vault, PersonaFileName, "persona text")
	profilePath := writeVaultFile(t, vault, ProfileFileName, "profile text")

	for _, relPath := range escapingRelPathValues() {
		_, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
			RelPath: relPath,
			Refs:    []string{"sess_a"},
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected %q to be refused, got %v", relPath, err)
		}
	}
	for path, want := range map[string]string{personaPath: "persona text", profilePath: "profile text"} {
		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		if string(onDisk) != want {
			t.Fatalf("%q was rewritten: %q", path, onDisk)
		}
	}
}

func TestRewriteFrontmatterRefsRefusesASymlinkedNote(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---\nrefs: []\n---\nuntouched\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), BeliefsDirName, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/link.md",
		Refs:    []string{"sess_a"},
	}); err == nil {
		t.Fatal("expected a symlinked note to be refused")
	}
	onDisk, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if !strings.Contains(string(onDisk), "untouched") {
		t.Fatalf("the symlink target was written through: %q", onDisk)
	}
}

func TestRewriteFrontmatterRefsRefusesDuplicateKeys(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/dup.md", "---\nrefs: [\"a\"]\nrefs: [\"b\"]\n---\nbody\n")

	_, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/dup.md",
		Refs:    []string{"c"},
	})
	if !errors.Is(err, ErrNoteParse) {
		t.Fatalf("expected an ambiguous frontmatter key to be refused, got %v", err)
	}
}

func TestRewriteFrontmatterRefsRefusesAStaleHash(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	_, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:             "beliefs/note.md",
		Refs:                []string{"sess_kept"},
		ExpectedContentHash: ContentHash("something else"),
	})
	if !errors.Is(err, ErrStaleContent) {
		t.Fatalf("expected a stale compare-and-set to be refused, got %v", err)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if readErr != nil {
		t.Fatalf("read note: %v", readErr)
	}
	if string(onDisk) != handWrittenNote {
		t.Fatal("a refused rewrite still changed the file")
	}
}

func TestRewriteFrontmatterRefsKeepsTheNoteInode(t *testing.T) {
	vault := newTestVault(t)
	path := writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)
	before := inodeOf(t, path)

	if _, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{"sess_kept"},
	}); err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	if after := inodeOf(t, path); after != before {
		t.Fatalf("the note was replaced (inode %d -> %d) instead of edited in place", before, after)
	}
}

func TestRewriteFrontmatterRefsRefusesAMissingNote(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/never-existed.md",
		Refs:    []string{"sess_a"},
	}); err == nil {
		t.Fatal("expected a missing note to be refused")
	}
}

func TestRewriteFrontmatterRefsHonoursContextCancellation(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.RewriteFrontmatterRefs(ctx, RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{"sess_a"},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestRewriteFrontmatterRefsPreservesCarriageReturnFencesAndBody(t *testing.T) {
	vault := newTestVault(t)
	content := "---\r\n" +
		"title: keep\r\n" +
		"refs:\r\n" +
		"  - \"sess_withdrawn\"\r\n" +
		"  - \"sess_kept\"\r\n" +
		"---\r\n" +
		"Body with \r\n windows endings.\r\n"
	writeVaultFile(t, vault, "beliefs/crlf.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/crlf.md",
		Refs:    []string{"sess_kept"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	if !strings.HasPrefix(result.Content, "---\r\ntitle: keep\r\n") {
		t.Fatalf("the opening fence or an untouched key lost its line ending: %q", result.Content)
	}
	if !strings.HasSuffix(result.Content, "---\r\nBody with \r\n windows endings.\r\n") {
		t.Fatalf("the closing fence or body lost its line endings: %q", result.Content)
	}
	parsed, err := ParseNote("beliefs/crlf.md", result.Content)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if strings.Join(parsed.Refs, ",") != "sess_kept" {
		t.Fatalf("refs = %v (content %q)", parsed.Refs, result.Content)
	}
}

func TestRewriteFrontmatterRefsKeepsAnInlineCommentOnTheEditedLine(t *testing.T) {
	vault := newTestVault(t)
	content := "---\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"  # provenance the user typed\n" +
		"refs: [\"a\", \"b\"]\t# evidence the user annotated\n" +
		"title: keep\n" +
		"---\n" +
		"body\n"
	writeVaultFile(t, vault, "beliefs/annotated.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/annotated.md",
		NoteID:  "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		Refs:    []string{"b"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	want := "---\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"  # provenance the user typed\n" +
		"refs: [\"b\"]\t# evidence the user annotated\n" +
		"title: keep\n" +
		"---\n" +
		"body\n"
	if result.Content != want {
		t.Fatalf("an inline comment was clobbered:\nwant %q\ngot  %q", want, result.Content)
	}
}

func TestRewriteFrontmatterRefsKeepsAHashInsideAQuotedValue(t *testing.T) {
	vault := newTestVault(t)
	content := "---\nrefs: [\"a#1\", \"b\"]\ntitle: keep\n---\nbody\n"
	writeVaultFile(t, vault, "beliefs/hash.md", content)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/hash.md",
		Refs:    []string{"c"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	want := "---\nrefs: [\"c\"]\ntitle: keep\n---\nbody\n"
	if result.Content != want {
		t.Fatalf("a hash inside a quoted value was mistaken for a comment:\nwant %q\ngot  %q", want, result.Content)
	}
}

func TestRewriteFrontmatterRefsRefusesAnEmptyRequest(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)
	if _, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
	}); err == nil {
		t.Fatal("expected a rewrite that asks for nothing to be refused")
	}
}

func TestRewriteFrontmatterRefsWritesNothingWhenTheContentIsUnchanged(t *testing.T) {
	recorder := &syncRecorder{}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)
	note := inodeOf(t, filepath.Join(vault.Root(), "beliefs", "note.md"))

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath: "beliefs/note.md",
		Refs:    []string{"sess_withdrawn", "sess_kept"},
	})
	if err != nil {
		t.Fatalf("rewrite refs: %v", err)
	}
	if result.Changed {
		t.Fatal("an identical rewrite must not report a change")
	}
	if recorder.syncedFile(note) {
		t.Fatal("an identical rewrite touched the file; Obsidian would see churn on every pass")
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if readErr != nil {
		t.Fatalf("read note: %v", readErr)
	}
	if string(onDisk) != handWrittenNote {
		t.Fatalf("an identical rewrite changed the file: %q", onDisk)
	}
}

// Withdrawal is not the same as "no citations". A note whose supporting
// conversations were deleted has to say so in the file the user opens —
// `refs: []` reads as a note nobody ever grounded, which is a different claim
// about their own memory. The literal marker also cannot be read back as a
// citation, so a later pass cannot re-insert what a deletion withdrew.
func TestRewriteFrontmatterRefsWritesWithdrawnAndPreservesEveryOtherByte(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", handWrittenNote)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:   "beliefs/note.md",
		Withdrawn: true,
	})
	if err != nil {
		t.Fatalf("withdraw refs: %v", err)
	}
	// Only the bytes the refs value occupied change, and the marker lands in
	// the position and indentation the user's own file already used: the range
	// this splice replaces starts at the value, so the key line — and any
	// comment the user left on it — is never inside it.
	want := strings.Replace(
		handWrittenNote,
		"  - \"sess_withdrawn\"\n  - \"sess_kept\"\n",
		"  \""+WithdrawnRefsMarker+"\"\n",
		1,
	)
	if result.Content != want {
		t.Fatalf("withdrawal was not byte-preserving:\nwant %q\ngot  %q", want, result.Content)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(onDisk) != want {
		t.Fatalf("on-disk bytes differ from the reported content:\nwant %q\ngot  %q", want, onDisk)
	}

	// Read back: the marker is a withdrawal, never a citation. Anything that
	// re-links evidence from frontmatter sees nothing to link.
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("parse the withdrawn note: %v", err)
	}
	if !parsed.Withdrawn {
		t.Fatalf("the withdrawn note does not read back as withdrawn: %+v", parsed)
	}
	if len(parsed.Refs) != 0 {
		t.Fatalf("refs = %v, want a withdrawal to carry no citations", parsed.Refs)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || parsed.Title != "Loosely quoted" {
		t.Fatalf("withdrawal disturbed the keys around it: %+v", parsed)
	}

	// Idempotent: withdrawing an already-withdrawn note changes nothing, so a
	// pass that runs on a timer does not rewrite the user's file forever.
	again, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:             "beliefs/note.md",
		Withdrawn:           true,
		ExpectedContentHash: result.ContentHash,
	})
	if err != nil {
		t.Fatalf("second withdrawal: %v", err)
	}
	if again.Changed || again.Content != want {
		t.Fatalf("a second withdrawal rewrote the note: changed=%v", again.Changed)
	}
}

// A note the user wrote by hand with no frontmatter at all still has to be
// able to say its evidence was withdrawn, without their prose moving.
func TestRewriteFrontmatterRefsWithdrawsOnANoteWithNoFrontmatter(t *testing.T) {
	vault := newTestVault(t)
	body := "# Written by hand\n\nThe user keeps bees.\n"
	writeVaultFile(t, vault, "beliefs/plain.md", body)

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:   "beliefs/plain.md",
		Withdrawn: true,
	})
	if err != nil {
		t.Fatalf("withdraw refs: %v", err)
	}
	if !strings.HasSuffix(result.Content, body) {
		t.Fatalf("the user's prose moved:\n%q", result.Content)
	}
	parsed, err := ParseNote("beliefs/plain.md", result.Content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Withdrawn || len(parsed.Refs) != 0 {
		t.Fatalf("parsed = %+v, want a withdrawal with no citations", parsed)
	}
}

// A user who types the marker themselves is telling Turing the same thing, and
// a lenient parser has to hear it the same way rather than treating the value
// as a session named "withdrawn".
func TestParseNoteReadsAHandWrittenWithdrawalMarker(t *testing.T) {
	for _, written := range []string{
		"refs: withdrawn\n",
		"refs: \"withdrawn\"\n",
		"refs:   Withdrawn  \n",
	} {
		parsed, err := ParseNote("beliefs/note.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n"+written+"---\n\nBody.\n")
		if err != nil {
			t.Fatalf("parse %q: %v", written, err)
		}
		if !parsed.Withdrawn {
			t.Fatalf("%q did not read back as withdrawn", written)
		}
		if len(parsed.Refs) != 0 {
			t.Fatalf("%q produced citations %v", written, parsed.Refs)
		}
	}
}
