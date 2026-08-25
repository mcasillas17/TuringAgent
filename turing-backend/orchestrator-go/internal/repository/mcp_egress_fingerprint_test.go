package repository

import "testing"

func TestEnqueueFingerprintChangesWithRemoteMCPDestination(t *testing.T) {
	input := EnqueueUserMessageInput{
		SessionID: "session", Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local", IdempotencyKey: "same-key",
		EgressDecision: &PendingEgressDecision{
			Version: RunEgressDecisionVersion, Provider: "ollama", Model: "local",
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
	input := EnqueueUserMessageInput{SessionID: "session", Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "local", IdempotencyKey: "same-key", EgressDecision: &PendingEgressDecision{Version: RunEgressDecisionVersion, Provider: "ollama", Model: "local", RequestDigest: "digest", DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"}, SelectedTools: []string{"integrations/github.list_issues"}, IntegrationEndpoints: []IntegrationEndpointEgress{{Endpoint: GitHubIntegrationEndpoint, EndpointHost: GitHubIntegrationEndpointHost, ConnectionID: "conn_a", DisplayName: "A", Tools: []string{"github.list_issues"}}}}}
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

// The local fingerprint version guards idempotency replay: a request accepted
// under the old canonical shape must not silently satisfy a request composed
// under the new one. Both literals are real fingerprints this exact input
// produced — one at local version 5, captured before the memory bump, and one
// after — so the test fails if the version is reverted, left behind, or moved
// past the value the rest of this change assumes.
func TestEnqueueFingerprintVersionMovedForMemorySnapshot(t *testing.T) {
	const (
		preMemoryBumpFingerprint  = "afbd1ccaad482e75dbc4bbf7bfecfd7319208b16d48581248220368e17ce009d"
		postMemoryBumpFingerprint = "7b6e076114bb2c05942e0f30d450ac851695e16f03f28463b40caa58e0036e71"
	)
	input := EnqueueUserMessageInput{
		SessionID: "session", Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local", IdempotencyKey: "same-key",
		EgressDecision: &PendingEgressDecision{
			Version: 2, Provider: "ollama", Model: "local", RequestDigest: "digest",
			DataCategories: []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS"},
			SelectedTools:  []string{"vendor/vendor.lookup"},
		},
	}
	got, err := EnqueueRequestFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	if got == preMemoryBumpFingerprint {
		t.Fatal("enqueue fingerprint still matches the pre-memory-bump canonical shape")
	}
	if got != postMemoryBumpFingerprint {
		t.Fatalf("enqueue fingerprint = %s, want the post-memory-bump %s", got, postMemoryBumpFingerprint)
	}
}
