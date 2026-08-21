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
