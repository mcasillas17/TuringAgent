package memory

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// A proposal's frontmatter is what makes the bytes a note. Once it no longer
// parses there is no proposal in that file — only bytes — and the row beside it
// is Turing's record of what it once wrote, not a second copy to serve in place
// of what the user is actually looking at.
//
// Serving the row's copy is the same failure as serving it for a vault nobody
// could open, dressed differently: text the user is told is their proposal
// above a compare-and-set token taken over bytes nobody could read. The token
// is checked against the file, so every decision the page offered would be
// refused — except the rejection, which is the one way out and needs no token
// at all.
//
// Every surface answers the same way, and this asserts them one by one so no
// single reader can drift.
func TestEveryReadWithholdsTheBytesOfAProposalThatNoLongerParses(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	candidate := corruptedProposal(t, callCtx, repo, vault.Root(), sessionID)

	reads := map[string]func(t *testing.T) *turingv1.MemoryCandidate{
		"ListMemoryState": func(t *testing.T) *turingv1.MemoryCandidate {
			state, err := service.ListMemoryState(callCtx, &turingv1.ListMemoryStateRequest{})
			if err != nil {
				t.Fatalf("ListMemoryState: %v", err)
			}
			return listedCandidate(t, state.GetCandidates(), candidate.CandidateID)
		},
		"ListMemoryCandidates": func(t *testing.T) *turingv1.MemoryCandidate {
			listing, err := service.ListMemoryCandidates(callCtx, &turingv1.ListMemoryCandidatesRequest{})
			if err != nil {
				t.Fatalf("ListMemoryCandidates: %v", err)
			}
			return listedCandidate(t, listing.GetCandidates(), candidate.CandidateID)
		},
		"ListMemoryCandidates filtered by state": func(t *testing.T) *turingv1.MemoryCandidate {
			listing, err := service.ListMemoryCandidates(callCtx, &turingv1.ListMemoryCandidatesRequest{
				State: turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING,
			})
			if err != nil {
				t.Fatalf("ListMemoryCandidates: %v", err)
			}
			return listedCandidate(t, listing.GetCandidates(), candidate.CandidateID)
		},
		"GetMemoryCandidate": func(t *testing.T) *turingv1.MemoryCandidate {
			fetched, err := service.GetMemoryCandidate(callCtx, &turingv1.GetMemoryCandidateRequest{
				CandidateId: candidate.CandidateID,
			})
			if err != nil {
				t.Fatalf("GetMemoryCandidate: %v", err)
			}
			return fetched
		},
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			served := read(t)
			if served.GetContent() != "" {
				t.Fatalf("content = %q, want nothing for a proposal nobody could parse", served.GetContent())
			}
			if served.GetContentHash() != "" {
				t.Fatalf("content hash = %q, want no token for bytes nobody read", served.GetContentHash())
			}
			if served.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED {
				t.Fatalf("unavailable reason = %v, want the parse failure said out loud",
					served.GetUnavailableReason())
			}
			// The detail stays: it is what tells the user to open the file and
			// fix it rather than leaving them with a card that says only that
			// something is wrong.
			if strings.TrimSpace(served.GetParseError()) == "" {
				t.Fatal("parse error is empty; the page has nothing to tell the user to fix")
			}
			// And the identity the row owns is untouched, because it is the
			// row's and not the file's.
			if served.GetCandidateId() != candidate.CandidateID {
				t.Fatalf("candidate id = %q, want %q", served.GetCandidateId(), candidate.CandidateID)
			}
			if served.GetInboxPath() != candidate.InboxPath {
				t.Fatalf("inbox path = %q, want %q", served.GetInboxPath(), candidate.InboxPath)
			}
			if !served.GetManaged() {
				t.Fatal("a proposal Turing wrote was served as something it does not own")
			}
			if served.GetState() != turingv1.MemoryCandidateState_MEMORY_CANDIDATE_STATE_PENDING {
				t.Fatalf("state = %v, want a proposal still waiting on the user", served.GetState())
			}
		})
	}
}
