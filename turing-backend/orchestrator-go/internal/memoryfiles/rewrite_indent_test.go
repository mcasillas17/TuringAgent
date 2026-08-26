package memoryfiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A block sequence written flush against the left margin. This is what YAML's
// own specification calls the standard form and what most editors emit, so it
// is the shape a withdrawal is most likely to meet in a vault the user has
// touched.
const zeroIndentNote = "---\n" +
	"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
	"kind: \"belief\"\n" +
	"aliases:\n" +
	"- \"Alt name\"\n" +
	"refs:\n" +
	"- \"sess_a\"\n" +
	"- \"sess_b\"\n" +
	"title: \"Zero indent\"\n" +
	"---\n" +
	"\n" +
	"# Body heading\n" +
	"\n" +
	"Body text with   odd    spacing.\n"

// The same note with the whole mapping indented, so the key itself sits at a
// column and its block sequence sits at the key's column rather than past it.
const keyIndentedNote = "---\n" +
	"  id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
	"  kind: \"belief\"\n" +
	"  refs:\n" +
	"  - \"sess_a\"\n" +
	"  # a note the user left about the title\n" +
	"  title: \"Key indented\"\n" +
	"---\n" +
	"\n" +
	"# Body heading\n" +
	"\n" +
	"Body text.\n"

func rewriteAndReadBack(t *testing.T, note string, request RewriteFrontmatterRefsRequest) RewrittenNote {
	t.Helper()
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", note)
	request.RelPath = "beliefs/note.md"
	result, err := vault.RewriteFrontmatterRefs(context.Background(), request)
	if err != nil {
		t.Fatalf("rewrite %+v: %v", request, err)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(onDisk) != result.Content {
		t.Fatalf("on-disk bytes differ from the reported content:\nwant %q\ngot  %q", result.Content, onDisk)
	}
	return result
}

// Withdrawing from a zero-indent sequence has to leave a note the user's own
// editor can still read. Rendering the marker at the sequence's column puts a
// bare scalar where YAML expects the next key, which is a file nothing can
// parse — including this package, so the note becomes unreadable memory.
func TestRewriteFrontmatterRefsWithdrawsFromAZeroIndentSequence(t *testing.T) {
	result := rewriteAndReadBack(t, zeroIndentNote, RewriteFrontmatterRefsRequest{Withdrawn: true})

	want := strings.Replace(zeroIndentNote, "- \"sess_a\"\n- \"sess_b\"\n", "  \"withdrawn\"\n", 1)
	if result.Content != want {
		t.Fatalf("withdrawal was not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the withdrawn note no longer parses: %v", err)
	}
	if !parsed.Withdrawn {
		t.Fatalf("the withdrawn note does not read as withdrawn: refs=%v", parsed.Refs)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" || parsed.Title != "Zero indent" {
		t.Fatalf("the splice disturbed a neighbouring key: id=%q title=%q", parsed.ID, parsed.Title)
	}
}

// The indent a non-inline value needs is relative to its *key*, not to where
// the sequence node happened to start. A mapping indented as a whole has both
// at the same column, so copying the sequence's column writes the marker at the
// key's own indentation — again a bare scalar where a key belongs.
func TestRewriteFrontmatterRefsWithdrawsFromAKeyIndentedSequence(t *testing.T) {
	result := rewriteAndReadBack(t, keyIndentedNote, RewriteFrontmatterRefsRequest{Withdrawn: true})

	want := strings.Replace(keyIndentedNote, "  - \"sess_a\"\n", "    \"withdrawn\"\n", 1)
	if result.Content != want {
		t.Fatalf("withdrawal was not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the withdrawn note no longer parses: %v", err)
	}
	if !parsed.Withdrawn {
		t.Fatalf("the withdrawn note does not read as withdrawn: refs=%v", parsed.Refs)
	}
	if parsed.Title != "Key indented" {
		t.Fatalf("the splice disturbed the key after it: title=%q", parsed.Title)
	}
}

// An empty list is a flow value, exactly like the withdrawal marker, and needs
// the same indentation for the same reason.
func TestRewriteFrontmatterRefsClearsAZeroIndentSequence(t *testing.T) {
	result := rewriteAndReadBack(t, zeroIndentNote, RewriteFrontmatterRefsRequest{Refs: []string{}})

	want := strings.Replace(zeroIndentNote, "- \"sess_a\"\n- \"sess_b\"\n", "  []\n", 1)
	if result.Content != want {
		t.Fatalf("cleared refs were not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the cleared note no longer parses: %v", err)
	}
	if len(parsed.Refs) != 0 || parsed.Withdrawn {
		t.Fatalf("cleared refs read back as %v (withdrawn=%t)", parsed.Refs, parsed.Withdrawn)
	}
}

// A replacement list stays a block sequence, so it may keep the column the user
// wrote it at — including the left margin.
func TestRewriteFrontmatterRefsRewritesAZeroIndentSequenceInPlace(t *testing.T) {
	result := rewriteAndReadBack(t, zeroIndentNote, RewriteFrontmatterRefsRequest{Refs: []string{"sess_b"}})

	want := strings.Replace(zeroIndentNote, "- \"sess_a\"\n- \"sess_b\"\n", "- \"sess_b\"\n", 1)
	if result.Content != want {
		t.Fatalf("the rewritten list was not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the rewritten note no longer parses: %v", err)
	}
	if len(parsed.Refs) != 1 || parsed.Refs[0] != "sess_b" {
		t.Fatalf("refs = %v", parsed.Refs)
	}
}

// A vault synced from Windows keeps its line endings through a zero-indent
// withdrawal too: the indent fix must not be the thing that mixes them.
func TestRewriteFrontmatterRefsWithdrawsFromAZeroIndentSequenceKeepingCRLF(t *testing.T) {
	note := strings.ReplaceAll(zeroIndentNote, "\n", "\r\n")
	result := rewriteAndReadBack(t, note, RewriteFrontmatterRefsRequest{Withdrawn: true})

	want := strings.Replace(note, "- \"sess_a\"\r\n- \"sess_b\"\r\n", "  \"withdrawn\"\r\n", 1)
	if result.Content != want {
		t.Fatalf("the CRLF withdrawal was not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
	if strings.Contains(strings.ReplaceAll(result.Content, "\r\n", ""), "\n") {
		t.Fatalf("the splice left mixed line endings: %q", result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the withdrawn note no longer parses: %v", err)
	}
	if !parsed.Withdrawn {
		t.Fatalf("the withdrawn note does not read as withdrawn: refs=%v", parsed.Refs)
	}
}

// The frontmatter this package writes is not the only shape a vault holds. An
// anchor the user declared on the refs value and referenced from another key
// survives a read, and disappears the moment the value it was attached to is
// replaced — leaving a note whose remaining alias points at nothing.
//
// Nothing in the splice can see that coming, which is the point: the last thing
// before the write is a full parse of the spliced note, and a note that no
// longer reads back is never written. The user keeps the file they had.
func TestRewriteFrontmatterRefsRefusesASpliceThatWouldNotParseBack(t *testing.T) {
	vault := newTestVault(t)
	note := "---\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
		"refs: &evidence\n" +
		"- \"sess_a\"\n" +
		"seen: *evidence\n" +
		"---\n" +
		"\nBody.\n"
	writeVaultFile(t, vault, "beliefs/note.md", note)

	_, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:   "beliefs/note.md",
		Withdrawn: true,
	})
	if err == nil {
		t.Fatal("expected a splice that does not parse back to be refused")
	}
	if !errors.Is(err, ErrNoteParse) {
		t.Fatalf("refusal = %v, want an ErrNoteParse refusal", err)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(vault.Root(), "beliefs", "note.md"))
	if readErr != nil {
		t.Fatalf("read note: %v", readErr)
	}
	if string(onDisk) != note {
		t.Fatalf("the refused rewrite wrote anyway:\nwant %q\ngot  %q", note, onDisk)
	}
}

// The guard is a check on this package's own splice, not a schema. A vault the
// user annotates is full of keys Turing has never heard of, and every one of
// them has to survive a withdrawal.
func TestRewriteFrontmatterRefsKeepsKeysItDoesNotKnow(t *testing.T) {
	result := rewriteAndReadBack(t, handWrittenNote, RewriteFrontmatterRefsRequest{Withdrawn: true})

	for _, key := range []string{"aliases:", "cssclasses: [wide, dense]", "tags:", "title:    'Loosely quoted'"} {
		if !strings.Contains(result.Content, key) {
			t.Fatalf("the withdrawal dropped %q:\n%s", key, result.Content)
		}
	}
	if !strings.Contains(result.Content, "# a comment the user wrote") {
		t.Fatalf("the withdrawal dropped the user's comment:\n%s", result.Content)
	}
	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the withdrawn note no longer parses: %v", err)
	}
	if !parsed.Withdrawn {
		t.Fatalf("the withdrawn note does not read as withdrawn: refs=%v", parsed.Refs)
	}
}

// A note whose own frontmatter already did not read back is not one the guard
// may refuse. Turing does not vet the user's file here; it checks its own
// splice. Refusing would make exactly the notes that are already awkward the
// ones a withdrawal can never reach.
func TestRewriteFrontmatterRefsStillWithdrawsFromANoteThatDidNotReadBackBefore(t *testing.T) {
	vault := newTestVault(t)
	note := "---\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
		"kind: \"scribble\"\n" +
		"refs:\n" +
		"- \"sess_a\"\n" +
		"---\n" +
		"\nBody.\n"
	writeVaultFile(t, vault, "beliefs/note.md", note)
	if _, err := ParseNote("beliefs/note.md", note); err == nil {
		t.Fatal("this test needs a note the lenient reader already refuses")
	}

	result, err := vault.RewriteFrontmatterRefs(context.Background(), RewriteFrontmatterRefsRequest{
		RelPath:   "beliefs/note.md",
		Withdrawn: true,
	})
	if err != nil {
		t.Fatalf("withdrawing from an already-unreadable note: %v", err)
	}
	want := strings.Replace(note, "- \"sess_a\"\n", "  \"withdrawn\"\n", 1)
	if result.Content != want {
		t.Fatalf("withdrawal was not spliced byte-for-byte:\nwant %q\ngot  %q", want, result.Content)
	}
}

// The reader drops blank citations, so the guard compares against the list it
// can expect to read back rather than the one it was handed. Without that, a
// caller passing a blank ref would have a faithful write refused.
func TestRewriteFrontmatterRefsAcceptsARequestCarryingABlankCitation(t *testing.T) {
	result := rewriteAndReadBack(t, zeroIndentNote, RewriteFrontmatterRefsRequest{
		Refs: []string{"sess_a", "   "},
	})

	parsed, err := ParseNote("beliefs/note.md", result.Content)
	if err != nil {
		t.Fatalf("the rewritten note no longer parses: %v", err)
	}
	if len(parsed.Refs) != 1 || parsed.Refs[0] != "sess_a" {
		t.Fatalf("refs = %v, want just the one real citation", parsed.Refs)
	}
}

// The guard is not only about YAML that will not parse. A splice that reads
// back as something other than what the rewrite asked for is refused too, and
// the note is left alone.
func TestVerifyRewrittenNoteRefusesASpliceThatSaysSomethingElse(t *testing.T) {
	const note = "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nrefs:\n  - \"sess_a\"\n---\n\nBody.\n"
	for _, refusal := range []struct {
		name    string
		spliced string
		request RewriteFrontmatterRefsRequest
	}{
		{
			name:    "a withdrawal that reads back as a list",
			spliced: note,
			request: RewriteFrontmatterRefsRequest{Withdrawn: true},
		},
		{
			name:    "citations that read back as different ones",
			spliced: note,
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_b"}},
		},
		{
			name:    "an identity that did not land",
			spliced: note,
			request: RewriteFrontmatterRefsRequest{NoteID: "01ARZ3NDEKTSV4RRFFQ69G5FAW"},
		},
		{
			name:    "a body the rewrite was never supposed to touch",
			spliced: strings.Replace(note, "Body.", "Something else.", 1),
			request: RewriteFrontmatterRefsRequest{Refs: []string{"sess_a"}},
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			err := verifyRewrittenNote("beliefs/note.md", note, refusal.spliced, refusal.request)
			if !errors.Is(err, ErrNoteParse) {
				t.Fatalf("verify = %v, want an ErrNoteParse refusal", err)
			}
		})
	}
}
