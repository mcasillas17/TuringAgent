package memoryfiles

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

const truncationNoticeMarker = "\n\n[Only the first "

// splitTruncationNotice separates what was pinned from the notice appended to
// it, so a test can talk about the two independently.
func splitTruncationNotice(t *testing.T, document PinnedDocument) (string, string) {
	t.Helper()
	index := strings.LastIndex(document.Content, truncationNoticeMarker)
	if index < 0 {
		t.Fatalf("no in-context truncation notice was added: %q", tail(document.Content, 120))
	}
	return document.Content[:index], document.Content[index:]
}

func tail(text string, count int) string {
	if len(text) <= count {
		return text
	}
	return text[len(text)-count:]
}

// The pin budget and the read ceiling are two different numbers answering two
// different questions. 4096 is how much of a document reaches a prompt; the
// ceiling is how large a file this package will read into memory at all. A
// document over the budget is pinned truncated and says so. Only a document
// past the ceiling is refused, and then it pins nothing rather than a prefix.

func TestPinnedBudgetIsNotAReadLimit(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat("a", MaxPersonaBytes+1))

	pinned := vault.LoadPersona(context.Background())
	if !pinned.Available {
		t.Fatalf("a document one byte over the pin budget is still readable: %+v", pinned)
	}
	if pinned.Reason != UnavailableNone {
		t.Fatalf("reason = %q; going over the budget is truncation, not unreadability", pinned.Reason)
	}
	if !pinned.Truncated {
		t.Fatal("expected truncation to be reported")
	}
	body, _ := splitTruncationNotice(t, pinned)
	if len(body) != MaxPersonaBytes {
		t.Fatalf("pinned %d bytes, want %d", len(body), MaxPersonaBytes)
	}
}

func TestPinnedSourceCeilingAcceptsExactlyTheLimitAndRefusesOneMore(t *testing.T) {
	t.Run("exactly at the ceiling still pins", func(t *testing.T) {
		vault := newTestVault(t)
		writeVaultFile(t, vault, PersonaFileName, strings.Repeat("a", MaxPinnedSourceBytes))
		pinned := vault.LoadPersona(context.Background())
		if !pinned.Available {
			t.Fatalf("a document exactly at the read ceiling must still pin: %+v", pinned)
		}
		body, _ := splitTruncationNotice(t, pinned)
		if len(body) != MaxPersonaBytes {
			t.Fatalf("pinned %d bytes, want %d", len(body), MaxPersonaBytes)
		}
	})

	t.Run("one byte over the ceiling pins nothing", func(t *testing.T) {
		vault := newTestVault(t)
		writeVaultFile(t, vault, PersonaFileName, strings.Repeat("a", MaxPinnedSourceBytes+1))
		pinned := vault.LoadPersona(context.Background())
		if pinned.Available {
			t.Fatal("a document past the read ceiling must not be pinned")
		}
		if pinned.Content != "" {
			t.Fatalf("a partial load happened: %d bytes", len(pinned.Content))
		}
		if pinned.Reason != UnavailableContentTooLarge {
			t.Fatalf("reason = %q", pinned.Reason)
		}
		if !strings.Contains(pinned.Detail, "524288") {
			t.Fatalf("detail %q does not name the ceiling it hit", pinned.Detail)
		}
	})
}

// The hash is what an enqueue compares against later to notice the user edited
// their vault mid-run. Hashing the file's bytes while pinning something else
// would compare a preimage nobody ever sent to the model.
func TestPinnedContentHashCoversTheBytesThatWerePinned(t *testing.T) {
	vault := newTestVault(t)
	raw := strings.Repeat("a", MaxProfileBytes+500)
	writeVaultFile(t, vault, ProfileFileName, raw)

	pinned := vault.LoadProfile(context.Background())
	if !pinned.Truncated {
		t.Fatal("expected truncation")
	}
	if pinned.ContentHash != ContentHash(pinned.Content) {
		t.Fatalf("hash %q is not the hash of the pinned bytes %q", pinned.ContentHash, ContentHash(pinned.Content))
	}
	if pinned.ContentHash == ContentHash(raw) {
		t.Fatal("the hash covers the whole file, not the fragment that was pinned")
	}
}

func TestPinnedContentHashOfAnEmptyPinIsTheHashOfNothing(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat(" ", MaxPersonaBytes+10)+"real text")

	pinned := vault.LoadPersona(context.Background())
	if pinned.Content != "" {
		t.Fatalf("whitespace-only truncation must pin nothing, got %q", pinned.Content)
	}
	if pinned.ContentHash != ContentHash("") {
		t.Fatalf("hash = %q, want the hash of the empty pin", pinned.ContentHash)
	}
}

// The notice is the model's only way to learn that what it is holding is a
// fragment. Reporting the budget instead of what was actually kept makes it
// wrong by up to three bytes on every multi-byte document — small, and exactly
// the kind of small that makes a byte offset useless.
func TestTruncationNoticeReportsTheBytesActuallyKept(t *testing.T) {
	vault := newTestVault(t)
	// Three-byte runes: 4096 is not divisible by 3, so the rune-safe cut lands
	// below the budget and the notice has to say so.
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat("界", 3000))

	pinned := vault.LoadPersona(context.Background())
	body, notice := splitTruncationNotice(t, pinned)
	if len(body) == MaxPersonaBytes {
		t.Fatal("this document was supposed to cut mid-rune; the test no longer exercises it")
	}
	if !utf8.ValidString(body) {
		t.Fatal("truncation split a rune")
	}
	if notice != truncationNotice(PersonaFileName, len(body)) {
		t.Fatalf("notice = %q, want it to report the %d bytes that were kept", notice, len(body))
	}
	if !strings.Contains(notice, "4095") {
		t.Fatalf("notice %q does not report the retained byte count", notice)
	}
	if strings.Contains(notice, "4096") {
		t.Fatalf("notice %q reports the budget rather than what was kept", notice)
	}
}

// A profile edit writes the whole resulting profile, which is the user's own
// document — so it is bounded by what this package can read back, exactly like
// a hand-authored save. A document saved past that ceiling would be reported
// unreadable forever, which is a trap rather than a limit.
//
// The candidate body's own 16 KiB bound is not weakened by this: it is enforced
// where the claim is created, and the text bounded here is not that claim.
func TestApplyProfileEditIsBoundedByTheAuthoredDocumentLimit(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	writeVaultFile(t, vault, ProfileFileName, "original")

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:    candidate.RelPath,
		TargetRelPath:       ProfileFileName,
		ExpectedContentHash: ContentHash("original"),
		Content:             strings.Repeat("a", MaxProfileEditBytes+1),
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected an over-limit profile edit to be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "524288") {
		t.Fatalf("refusal %q does not name the limit", err.Error())
	}
}

func TestApplyProfileEditAcceptsContentExactlyAtTheAuthoredDocumentLimit(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	writeVaultFile(t, vault, ProfileFileName, "original")
	content := strings.Repeat("a", MaxProfileEditBytes)

	applied, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:      candidate.RelPath,
		TargetRelPath:         ProfileFileName,
		ExpectedContentHash:   ContentHash("original"),
		ExpectedCandidateHash: candidate.ContentHash,
		Content:               content,
	})
	if err != nil {
		t.Fatalf("content exactly at the limit must be accepted: %v", err)
	}
	if applied.Content != content {
		t.Fatalf("applied %d bytes, want %d", len(applied.Content), len(content))
	}
}

func TestPromoteToBeliefsAcceptsANoteExactlyAtTheReadCeiling(t *testing.T) {
	vault := newTestVault(t)
	front := "---\nkind: \"belief\"\n---\n"
	writeVaultFile(t, vault, "inbox/big.md", front+strings.Repeat("a", MaxNoteBytes-len(front)))

	if _, err := vault.PromoteToBeliefs(context.Background(), PromoteToBeliefsRequest{
		SourceRelPath:       "inbox/big.md",
		Mode:                PromoteManagedCandidate,
		ExpectedContentHash: vaultFileHash(t, vault, "inbox/big.md"),
	}); err != nil {
		t.Fatalf("a note exactly at the read ceiling must promote: %v", err)
	}
}
