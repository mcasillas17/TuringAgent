package approvals

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestApprovalDetailsDoesNotExposeRepositoryErrors(t *testing.T) {
	h := newApprovalHarness(t)
	if err := h.database.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: "approval"})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "approval details unavailable" {
		t.Fatalf("detail error must be fixed public copy, got %v", err)
	}
}

func TestApprovalDetailsPreservesCancellationAndValidation(t *testing.T) {
	h := newApprovalHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.service.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: "approval"}); status.Code(err) != codes.Canceled {
		t.Fatalf("cancelled detail request = %v", err)
	}
	if _, err := h.service.GetApprovalDetails(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid detail request = %v", err)
	}
}
