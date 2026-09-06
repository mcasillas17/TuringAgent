package tests

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
)

// The integration client must perform the same review/binding handshake as
// current UI clients. Missing-binding rejection has separate coverage.
type reviewingApprovalClient struct{ turingv1.ApprovalServiceClient }

func (c reviewingApprovalClient) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest, opts ...grpc.CallOption) (*turingv1.ApprovalResponse, error) {
	d, err := c.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: req.ApprovalId}, opts...)
	if err != nil {
		return nil, err
	}
	req.PreviewHash = d.PreviewHash
	req.ArgsHash = d.ArgsHash
	return c.ApprovalServiceClient.ApproveApproval(ctx, req, opts...)
}
