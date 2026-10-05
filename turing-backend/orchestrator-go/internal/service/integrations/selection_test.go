package integrations

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// A job that enforces its frozen set reaches no integration tool outside it,
// even one its egress decision covers, and the refusal comes before the
// network.
func TestAnIntegrationToolOutsideAnEnforcedSetIsRefusedBeforeTheNetwork(t *testing.T) {
	server, _, database, runID, connectionID := integrationCallHarness(t, "selection-token")
	requests := 0
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})})
	enforce := func(selected string) {
		t.Helper()
		if _, err := database.ExecContext(context.Background(), `
			UPDATE jobs SET payload_json = json_set(payload_json,
				'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
			WHERE run_id = ?`, selected, runID); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	request := &turingv1.CallIntegrationToolRequest{RunId: runID, ToolName: "github.list_issues", Args: args}

	enforce(`["integrations/github.get_issue"]`)
	_, err := server.CallIntegrationTool(context.Background(), request)
	if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "integration tool is not selected for this run" {
		t.Fatalf("out-of-set call = %v, want PermissionDenied not selected", err)
	}
	if requests != 0 {
		t.Fatalf("network requests = %d, want none", requests)
	}

	enforce(`["integrations/github.list_issues"]`)
	if _, err := server.CallIntegrationTool(context.Background(), request); err != nil {
		t.Fatalf("in-set call: %v", err)
	}
	if requests != 1 {
		t.Fatalf("network requests = %d, want the in-set call to reach it once", requests)
	}
}

// The selection is checked before an approval is spent: an approval-gated
// integration tool outside the enforced set is refused without consuming the
// approval the user granted, and without reaching the network.
func TestAnApprovalGatedIntegrationToolOutsideAnEnforcedSetConsumesNoApproval(t *testing.T) {
	server, repo, database, runID, connectionID := integrationCallHarness(t, "selection-approval-token")
	if err := repo.SetToolPolicyByName(context.Background(), "integrations", "github.list_issues", "approval_required"); err != nil {
		t.Fatal(err)
	}
	consumed := 0
	server.SetApprovalEnforcer(approvalEnforcerFunc(func(context.Context, string, string, string, string, string, map[string]any) error {
		consumed++
		return nil
	}))
	requests := 0
	server.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})})
	enforce := func(selected string) {
		t.Helper()
		if _, err := database.ExecContext(context.Background(), `
			UPDATE jobs SET payload_json = json_set(payload_json,
				'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
			WHERE run_id = ?`, selected, runID); err != nil {
			t.Fatal(err)
		}
	}
	args, _ := structpb.NewStruct(map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"})
	request := &turingv1.CallIntegrationToolRequest{RunId: runID, ApprovalId: "approval_once", ToolName: "github.list_issues", Args: args}

	enforce(`["integrations/github.get_issue"]`)
	_, err := server.CallIntegrationTool(context.Background(), request)
	if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "integration tool is not selected for this run" {
		t.Fatalf("out-of-set call = %v, want PermissionDenied not selected", err)
	}
	if consumed != 0 || requests != 0 {
		t.Fatalf("approvals consumed = %d, network requests = %d, want neither for an out-of-set call", consumed, requests)
	}

	enforce(`["integrations/github.list_issues"]`)
	if _, err := server.CallIntegrationTool(context.Background(), request); err != nil {
		t.Fatalf("in-set call: %v", err)
	}
	if consumed != 1 || requests != 1 {
		t.Fatalf("approvals consumed = %d, network requests = %d, want the in-set call to spend one and reach it once", consumed, requests)
	}
}
