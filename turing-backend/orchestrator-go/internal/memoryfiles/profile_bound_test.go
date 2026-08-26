package memoryfiles

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// The two bounds an apply lives between, said out loud so a change to either
// one has to be deliberate. The candidate body is a claim a model wrote and
// stays at 16 KiB; the profile is the user's own document and is bounded by
// what this package can read back, exactly as a hand-authored save is.
func TestApplyProfileEditBoundsTheWholeDocumentLikeAnAuthoredSave(t *testing.T) {
	if MaxProfileEditBytes != MaxAuthoredDocumentBytes {
		t.Fatalf(
			"MaxProfileEditBytes = %d, want MaxAuthoredDocumentBytes (%d): the content of an apply is the whole resulting profile, not the candidate's claim",
			MaxProfileEditBytes, MaxAuthoredDocumentBytes,
		)
	}
	if MaxCandidateBodyBytes >= MaxAuthoredDocumentBytes {
		t.Fatalf(
			"MaxCandidateBodyBytes = %d is not below MaxAuthoredDocumentBytes (%d); the two bounds have collapsed into one",
			MaxCandidateBodyBytes, MaxAuthoredDocumentBytes,
		)
	}
}

// A user whose profile is already longer than a candidate body must still be
// able to accept a one-line proposal about themselves. Bounding the resulting
// document at the candidate limit made every such profile permanently
// un-appliable, which is the trap this bound exists to avoid.
func TestApplyProfileEditAcceptsASmallProposalOverALongProfile(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	long := "# Profile\n\n" + strings.Repeat("The user has written a great deal about themselves. ", 700)
	if len(long) <= MaxCandidateBodyBytes {
		t.Fatalf("test profile is %d bytes, which is not past the %d-byte candidate bound", len(long), MaxCandidateBodyBytes)
	}
	profilePath := writeVaultFile(t, vault, ProfileFileName, long)
	result := long + "\nGoes by Miguel.\n"

	applied, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:      candidate.RelPath,
		TargetRelPath:         ProfileFileName,
		ExpectedContentHash:   ContentHash(long),
		ExpectedCandidateHash: candidate.ContentHash,
		Content:               result,
	})
	if err != nil {
		t.Fatalf("apply a small proposal over a long profile: %v", err)
	}
	if applied.ContentHash != ContentHash(result) {
		t.Fatalf("content hash = %q, want the hash of the whole resulting document", applied.ContentHash)
	}
	onDisk, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if string(onDisk) != result {
		t.Fatalf("profile on disk is %d bytes, want the %d-byte result", len(onDisk), len(result))
	}
}

// Just under the read ceiling is still a document this package can read back,
// so it is a document the user may keep.
func TestApplyProfileEditAcceptsADocumentJustUnderTheReadCeiling(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	writeVaultFile(t, vault, ProfileFileName, "# Profile\n")
	result := "# Profile\n" + strings.Repeat("a", MaxAuthoredDocumentBytes-len("# Profile\n"))

	if _, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:      candidate.RelPath,
		TargetRelPath:         ProfileFileName,
		ExpectedContentHash:   ContentHash("# Profile\n"),
		ExpectedCandidateHash: candidate.ContentHash,
		Content:               result,
	}); err != nil {
		t.Fatalf("apply a document at the read ceiling: %v", err)
	}
	document := vault.EditableProfile(context.Background())
	if document.Content != result {
		t.Fatalf("the document written at the ceiling could not be read back whole (%d bytes read)", len(document.Content))
	}
}

// One byte past it is refused, because a document saved here and unreadable
// afterwards is worse than a refusal the user can act on.
func TestApplyProfileEditRefusesADocumentPastTheReadCeiling(t *testing.T) {
	vault := newTestVault(t)
	candidate := seedProfileEditCandidate(t, vault)
	writeVaultFile(t, vault, ProfileFileName, "# Profile\n")

	_, err := vault.ApplyProfileEdit(context.Background(), ApplyProfileEditRequest{
		CandidateRelPath:      candidate.RelPath,
		TargetRelPath:         ProfileFileName,
		ExpectedContentHash:   ContentHash("# Profile\n"),
		ExpectedCandidateHash: candidate.ContentHash,
		Content:               strings.Repeat("a", MaxAuthoredDocumentBytes+1),
	})
	var overLimit *LimitError
	if !errors.As(err, &overLimit) {
		t.Fatalf("apply past the read ceiling = %v, want a LimitError", err)
	}
	if overLimit.Limit != MaxAuthoredDocumentBytes {
		t.Fatalf("LimitError.Limit = %d, want %d", overLimit.Limit, MaxAuthoredDocumentBytes)
	}
}
