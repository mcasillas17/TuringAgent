package agent

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLocalRunAcceptsAnEgressDecisionForItsRemoteMCPServer(t *testing.T) {
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	job := &turingv1.AgentJob{
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "local-model",
		SelectedTools: []string{"vendor/vendor.lookup"},
		EgressDecision: &turingv1.RunEgressDecision{
			DecisionId: "egress_remote_mcp",
			Version:    int32(backendegress.DecisionVersion),
			Provider:   turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:      "local-model",
			DataCategories: []turingv1.EgressDataCategory{
				turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
				turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
			},
			ConsentGrantedAt:         timestamppb.Now(),
			ChallengeFingerprint:     "fingerprint",
			SelectedTools:            []string{"vendor/vendor.lookup"},
			SkillSnapshotFingerprint: skillFingerprint,
			RequestDigest:            "digest",
			RemoteMcpServers: []*turingv1.RemoteMcpEgressDestination{{
				ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
			}},
		},
	}

	if err := validateEgressDecisionShape(job); err != nil {
		t.Fatalf("validateEgressDecisionShape: %v", err)
	}
}
