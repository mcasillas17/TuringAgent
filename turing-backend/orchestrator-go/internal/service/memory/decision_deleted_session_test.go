package memory

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A decision about a proposal whose conversation is being withdrawn is refused
// by the repository, and the page has to be told what that means.
//
// The inbox is a list the user may be looking at from before they deleted the
// conversation, so pressing accept or reject on a stale card is an ordinary
// thing to do — not a bug and not a server fault. Left to the fallback it
// arrives as Internal, which tells them Turing broke and invites them to try
// again forever. It is a precondition: the conversation this claim came out of
// is going away, and everything it was the only support for is going with it.
func requireDeletingSessionRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("a decision about a withdrawn conversation was accepted")
	}
	reported, ok := status.FromError(err)
	if !ok {
		t.Fatalf("the refusal is not a gRPC status: %v", err)
	}
	if reported.Code() != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition (message %q)", reported.Code(), reported.Message())
	}
	if reported.Message() == "" {
		t.Fatal("the refusal carries no sentence a person could act on")
	}
}

func TestDecisionsAboutADeletedConversationAreRefusedAsAPrecondition(t *testing.T) {
	service, repo, vault, callCtx := newMemoryService(t)
	_, sessionID := newRun(t, repo, callCtx)
	belief, err := repo.CreateMemoryCandidate(callCtx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief,
		Title: "Dark mode", Body: "The user prefers dark mode.",
	})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := repo.CreateMemoryCandidate(callCtx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindProfileEdit,
		Title: "Call me Miguel", Body: "The user goes by Miguel.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginSessionDeletion(callCtx, sessionID); err != nil {
		t.Fatalf("begin session deletion: %v", err)
	}

	_, promoteErr := service.PromoteMemoryCandidate(callCtx, &turingv1.PromoteMemoryCandidateRequest{
		CandidateId: belief.CandidateID,
	})
	requireDeletingSessionRefusal(t, promoteErr)

	_, rejectErr := service.RejectMemoryCandidate(callCtx, &turingv1.RejectMemoryCandidateRequest{
		CandidateId: belief.CandidateID,
	})
	requireDeletingSessionRefusal(t, rejectErr)

	_, applyErr := service.ApplyMemoryProfile(callCtx, &turingv1.ApplyMemoryProfileRequest{
		CandidateId: edit.CandidateID,
		Content:     "# Profile\n\nGoes by Miguel.\n",
	})
	requireDeletingSessionRefusal(t, applyErr)

	if profile := vault.EditableProfile(callCtx); profile.Available {
		t.Fatalf("a refused apply wrote the profile: %q", profile.Content)
	}
}
