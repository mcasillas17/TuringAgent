package memoryfiles

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNoteReadsTuringFrontmatter(t *testing.T) {
	content := "---\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
		"kind: \"belief\"\n" +
		"title: \"Prefers dark mode\"\n" +
		"managed: true\n" +
		"refs:\n" +
		"  - \"sess_a\"\n" +
		"  - \"sess_b\"\n" +
		"---\n" +
		"\nThe user prefers dark mode.\n"
	parsed, err := ParseNote("beliefs/note.md", content)
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("id = %q", parsed.ID)
	}
	if parsed.Kind != KindBelief {
		t.Fatalf("kind = %q", parsed.Kind)
	}
	if parsed.Title != "Prefers dark mode" {
		t.Fatalf("title = %q", parsed.Title)
	}
	if !parsed.Managed {
		t.Fatal("expected the note to be managed")
	}
	if strings.Join(parsed.Refs, ",") != "sess_a,sess_b" {
		t.Fatalf("refs = %v", parsed.Refs)
	}
	if parsed.Body != "\nThe user prefers dark mode.\n" {
		t.Fatalf("body = %q", parsed.Body)
	}
	if !strings.Contains(parsed.RawFrontmatter, "kind: \"belief\"") {
		t.Fatalf("raw frontmatter = %q", parsed.RawFrontmatter)
	}
	if !parsed.HasFrontmatter {
		t.Fatal("expected frontmatter to be reported as present")
	}
}

func TestParseNotePreservesUnknownFrontmatterKeys(t *testing.T) {
	content := "---\n" +
		"aliases: [\"a\", \"b\"]\n" +
		"cssclass: reading\n" +
		"id: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n" +
		"---\n" +
		"body\n"
	parsed, err := ParseNote("beliefs/note.md", content)
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("id = %q", parsed.ID)
	}
	for _, key := range []string{"aliases", "cssclass"} {
		if !strings.Contains(parsed.RawFrontmatter, key) {
			t.Fatalf("unknown key %q was not preserved in the raw frontmatter", key)
		}
	}
}

func TestParseNoteTreatsMissingFrontmatterAsUnidentified(t *testing.T) {
	content := "# A note the user wrote\n\nJust prose.\n"
	parsed, err := ParseNote("beliefs/hand-written.md", content)
	if err != nil {
		t.Fatalf("a note without frontmatter must not be a parse error: %v", err)
	}
	if parsed.HasFrontmatter {
		t.Fatal("expected no frontmatter to be reported")
	}
	if parsed.ID != "" {
		t.Fatalf("expected no identity, got %q", parsed.ID)
	}
	if parsed.Managed {
		t.Fatal("a hand-written note must default to unmanaged")
	}
	if parsed.Body != content {
		t.Fatalf("body = %q", parsed.Body)
	}
	if parsed.Title != "A note the user wrote" {
		t.Fatalf("expected the heading to serve as the title, got %q", parsed.Title)
	}
}

func TestParseNoteReportsUnclosedFrontmatter(t *testing.T) {
	_, err := ParseNote("beliefs/broken.md", "---\nid: \"x\"\nstill going\n")
	if err == nil {
		t.Fatal("expected an unclosed frontmatter fence to be reported")
	}
	var parseError *NoteParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("expected a typed per-note parse error, got %T", err)
	}
	if parseError.RelPath != "beliefs/broken.md" {
		t.Fatalf("parse error does not name the note: %q", parseError.RelPath)
	}
	if !errors.Is(err, ErrNoteParse) {
		t.Fatalf("expected ErrNoteParse, got %v", err)
	}
}

func TestParseNoteReportsMalformedYAML(t *testing.T) {
	_, err := ParseNote("beliefs/broken.md", "---\nid: \"x\n  - unbalanced\n\t- tabs\n---\nbody\n")
	var parseError *NoteParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("expected a typed per-note parse error, got %v", err)
	}
}

func TestParseNoteReportsNonMappingFrontmatter(t *testing.T) {
	_, err := ParseNote("beliefs/broken.md", "---\n- one\n- two\n---\nbody\n")
	var parseError *NoteParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("expected a typed per-note parse error, got %v", err)
	}
}

func TestParseNoteIgnoresUnrecognisedKindWithoutFailing(t *testing.T) {
	parsed, err := ParseNote("inbox/note.md", "---\nid: \"x\"\nkind: \"nonsense\"\n---\nbody\n")
	if err != nil {
		t.Fatalf("an unrecognised kind must stay lenient: %v", err)
	}
	if parsed.Kind != "" {
		t.Fatalf("expected an unrecognised kind to be dropped, got %q", parsed.Kind)
	}
}

func TestParseNoteSkipsNonStringRefs(t *testing.T) {
	parsed, err := ParseNote("beliefs/note.md", "---\nrefs:\n  - \"sess_a\"\n  - 17\n  - {a: b}\n---\nbody\n")
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if strings.Join(parsed.Refs, ",") != "sess_a" {
		t.Fatalf("refs = %v", parsed.Refs)
	}
}

func TestParseNoteHandlesEmptyFrontmatter(t *testing.T) {
	parsed, err := ParseNote("beliefs/note.md", "---\n---\nbody\n")
	if err != nil {
		t.Fatalf("empty frontmatter must stay lenient: %v", err)
	}
	if !parsed.HasFrontmatter {
		t.Fatal("expected the empty frontmatter block to be reported as present")
	}
	if parsed.Body != "body\n" {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestParseNoteRoundTripsWhatCreateInboxNoteWrites(t *testing.T) {
	content := renderNote(noteFrontmatter{
		ID:      "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Kind:    KindProfileEdit,
		Title:   `A "quoted" title: with punctuation`,
		Managed: true,
		Refs:    []string{"sess_a"},
	}, "The body.")
	parsed, err := ParseNote("inbox/note.md", content)
	if err != nil {
		t.Fatalf("parse rendered note: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("id = %q", parsed.ID)
	}
	if parsed.Kind != KindProfileEdit {
		t.Fatalf("kind = %q", parsed.Kind)
	}
	if parsed.Title != `A "quoted" title: with punctuation` {
		t.Fatalf("title = %q", parsed.Title)
	}
	if strings.Join(parsed.Refs, ",") != "sess_a" {
		t.Fatalf("refs = %v", parsed.Refs)
	}
	if strings.TrimSpace(parsed.Body) != "The body." {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestParseNoteAcceptsCarriageReturns(t *testing.T) {
	content := "---\r\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\r\n---\r\nbody\r\n"
	parsed, err := ParseNote("beliefs/note.md", content)
	if err != nil {
		t.Fatalf("parse note: %v", err)
	}
	if parsed.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("id = %q", parsed.ID)
	}
	if parsed.Body != "body\r\n" {
		t.Fatalf("body = %q", parsed.Body)
	}
}
