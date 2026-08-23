package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

func TestLocalRemoteMCPConsentNoticeAndAuditNameDestination(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "remote MCP")
	if err != nil {
		t.Fatal(err)
	}
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "look up", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local",
		SelectedTools: []string{"vendor/vendor.lookup"},
		EgressDecision: &PendingEgressDecision{
			Version: RunEgressDecisionVersion, ChallengeNonce: "nonce_notice", ChallengeFingerprint: "fingerprint_notice",
			RequestDigest: "digest_notice", Provider: "ollama", Model: "local",
			DataCategories: []string{
				"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS",
				"EGRESS_DATA_CATEGORY_TOOL_RESULTS",
			},
			SelectedTools:            []string{"vendor/vendor.lookup"},
			SkillSnapshotFingerprint: skillFingerprint,
			ConsentGrantedAt:         "2026-08-21T00:00:00Z",
			RemoteMCPServers: []RemoteMCPServerEgress{{
				ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enqueued.RoutingEvents) != 1 ||
		!strings.Contains(enqueued.RoutingEvents[0].PayloadJSON, "vendor.example") {
		t.Fatalf("routing notice = %+v, want remote MCP destination", enqueued.RoutingEvents)
	}
	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT payload_json FROM audit_logs
		WHERE correlation_id = ? AND action = 'egress.consent.recorded'
	`, enqueued.RunID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["endpointHost"] != "vendor.example" {
		t.Fatalf("audit payload = %+v, want remote MCP destination", payload)
	}
}

func TestLocalIntegrationConsentNoticeAndAuditNameDestination(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "integration")
	if err != nil {
		t.Fatal(err)
	}
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "list issues", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local", SelectedTools: []string{"integrations/github.list_issues"},
		EgressDecision: &PendingEgressDecision{
			Version: RunEgressDecisionVersion, ChallengeNonce: "nonce_integration_notice", ChallengeFingerprint: "fingerprint_integration_notice",
			RequestDigest: "digest_integration_notice", Provider: "ollama", Model: "local",
			DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"},
			SelectedTools:  []string{"integrations/github.list_issues"}, SkillSnapshotFingerprint: skillFingerprint,
			ConsentGrantedAt: "2026-08-21T00:00:00Z",
			IntegrationEndpoints: []IntegrationEndpointEgress{{
				Endpoint: GitHubIntegrationEndpoint, EndpointHost: GitHubIntegrationEndpointHost,
				ConnectionID: "conn_notice", DisplayName: "GitHub", Tools: []string{"github.list_issues"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enqueued.RoutingEvents) != 1 || !strings.Contains(enqueued.RoutingEvents[0].PayloadJSON, GitHubIntegrationEndpointHost) {
		t.Fatalf("routing notice=%+v, want GitHub destination", enqueued.RoutingEvents)
	}
	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM audit_logs WHERE correlation_id=? AND action='egress.consent.recorded'`, enqueued.RunID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["endpointHost"] != GitHubIntegrationEndpointHost {
		t.Fatalf("audit payload=%+v, want GitHub destination", payload)
	}
}
