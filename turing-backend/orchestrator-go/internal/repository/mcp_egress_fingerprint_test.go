package repository

import "testing"

func TestEnqueueFingerprintChangesWithRemoteMCPDestination(t *testing.T) {
	input := EnqueueUserMessageInput{
		SessionID: "session", Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local", IdempotencyKey: "same-key",
		EgressDecision: &PendingEgressDecision{
			Version: 1, Provider: "ollama", Model: "local",
			RequestDigest: "digest", DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS"},
			SelectedTools: []string{"vendor/vendor.lookup"},
			RemoteMCPServers: []RemoteMCPServerEgress{{
				ServerName: "vendor", Endpoint: "https://one.example/mcp", EndpointHost: "one.example",
			}},
		},
	}
	first, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	input.EgressDecision.RemoteMCPServers[0] = RemoteMCPServerEgress{
		ServerName: "vendor", Endpoint: "https://two.example/mcp", EndpointHost: "two.example",
	}
	second, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("remote MCP destination did not affect idempotency fingerprint")
	}
}

func TestEnqueueFingerprintChangesWithIntegrationConnectionSet(t *testing.T) {
	input := EnqueueUserMessageInput{SessionID: "session", Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "local", IdempotencyKey: "same-key", EgressDecision: &PendingEgressDecision{Version: 1, Provider: "ollama", Model: "local", RequestDigest: "digest", DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"}, SelectedTools: []string{"integrations/github.list_issues"}, IntegrationEndpoints: []IntegrationEndpointEgress{{Endpoint: GitHubIntegrationEndpoint, EndpointHost: GitHubIntegrationEndpointHost, ConnectionID: "conn_a", DisplayName: "A", Tools: []string{"github.list_issues"}}}}}
	first, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	input.EgressDecision.IntegrationEndpoints[0].ConnectionID = "conn_b"
	second, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("integration connection set did not affect idempotency fingerprint")
	}
}
