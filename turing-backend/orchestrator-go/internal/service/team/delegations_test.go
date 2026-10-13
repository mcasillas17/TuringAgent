package team

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A delegation reads back through the public facet as its card shows it, the
// task as the model wrote it included.
func TestADelegationIsListedAndReadAsItsCardShowsIt(t *testing.T) {
	h := newHarness(t)
	d := h.delegatingRun(t, "safe")
	response, err := h.delegate(d, "call_1", "", researchTask)
	if err != nil {
		t.Fatal(err)
	}
	delegationID := response.GetResult().AsMap()["delegation_id"].(string)
	run, err := h.repo.GetRun(context.Background(), d.runID)
	if err != nil {
		t.Fatal(err)
	}
	public := NewPublicServer(h.server)

	listed, err := public.ListDelegations(context.Background(), &turingv1.ListDelegationsRequest{ParentSessionId: run.SessionID})
	if err != nil || len(listed.GetDelegations()) != 1 {
		t.Fatalf("ListDelegations = %+v, %v; want the one", listed, err)
	}
	got := listed.GetDelegations()[0]
	if got.GetDelegationId() != delegationID || got.GetParentSessionId() != run.SessionID || got.GetParentRunId() != d.runID ||
		got.GetChildSessionId() == "" || got.GetChildRunId() == "" || got.GetProfileId() != "research" ||
		got.GetDisplayName() != "Research" || got.GetEmoji() != "🔬" || got.GetTask() != "Gather the design review notes" ||
		got.GetState() != "queued" || got.GetSummary() != "🔬 Research is queued" || got.GetJoined() ||
		got.GetCreatedAt() == nil || got.GetFinishedAt() != nil {
		t.Fatalf("listed delegation = %+v", got)
	}
	read, err := public.GetDelegation(context.Background(), &turingv1.GetDelegationRequest{DelegationId: delegationID})
	if err != nil || read.GetDelegationId() != delegationID || read.GetTask() != got.GetTask() {
		t.Fatalf("GetDelegation = %+v, %v", read, err)
	}
}

// Asking without an ID is a bad request; asking about something that is not
// there, or is going away, finds nothing.
func TestDelegationReadsRefuseWhatIsNotThere(t *testing.T) {
	h := newHarness(t)
	public := NewPublicServer(h.server)
	ctx := context.Background()
	if _, err := public.ListDelegations(ctx, &turingv1.ListDelegationsRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListDelegations without a session = %v, want InvalidArgument", err)
	}
	if _, err := public.GetDelegation(ctx, &turingv1.GetDelegationRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetDelegation without an ID = %v, want InvalidArgument", err)
	}
	if _, err := public.ListDelegations(ctx, &turingv1.ListDelegationsRequest{ParentSessionId: "sess_missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("ListDelegations(missing) = %v, want NotFound", err)
	}
	if _, err := public.GetDelegation(ctx, &turingv1.GetDelegationRequest{DelegationId: "dlg_missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetDelegation(missing) = %v, want NotFound", err)
	}
	session, err := h.repo.CreateSession(ctx, "Empty")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := public.ListDelegations(ctx, &turingv1.ListDelegationsRequest{ParentSessionId: session.SessionID})
	if err != nil || len(listed.GetDelegations()) != 0 {
		t.Fatalf("ListDelegations(a conversation that never delegated) = %+v, %v; want none", listed, err)
	}
}

// Reading delegations is the user's: the internal facet refuses it.
func TestTheInternalFacetReadsNoDelegations(t *testing.T) {
	internal := NewInternalServer(newHarness(t).server)
	ctx := context.Background()
	if _, err := internal.ListDelegations(ctx, &turingv1.ListDelegationsRequest{ParentSessionId: "sess_1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal ListDelegations = %v, want PermissionDenied", err)
	}
	if _, err := internal.GetDelegation(ctx, &turingv1.GetDelegationRequest{DelegationId: "dlg_1"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("internal GetDelegation = %v, want PermissionDenied", err)
	}
}
