package runtime

import (
	"testing"

	"google.golang.org/protobuf/proto"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestMapJobSendsTheSpecialistContract(t *testing.T) {
	mapped := mapJob(repository.Job{
		AgentID: "general_assistant", SelectedTools: []string{"files/files.read"},
		AgentProfile: &repository.AgentProfileSnapshot{
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬",
			Instructions: "Find sources.", MaxToolCalls: 4,
		},
		EnforceSelectedTools: true,
		SkipAutomaticRecall:  true,
	})
	want := &turingv1.AgentProfileSnapshot{
		ProfileId: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬",
		Instructions: "Find sources.", MaxToolCalls: 4,
	}
	if !proto.Equal(mapped.GetAgentProfile(), want) {
		t.Fatalf("agent profile = %v, want %v", mapped.GetAgentProfile(), want)
	}
	if !mapped.GetEnforceSelectedTools() || !mapped.GetSkipAutomaticRecall() {
		t.Fatalf("enforce = %v, skip recall = %v; want both set", mapped.GetEnforceSelectedTools(), mapped.GetSkipAutomaticRecall())
	}
}

func TestMapJobSendsNoContractForAnOrdinaryTurn(t *testing.T) {
	mapped := mapJob(repository.Job{AgentID: "general_assistant"})
	if mapped.AgentProfile != nil || mapped.GetEnforceSelectedTools() || mapped.GetSkipAutomaticRecall() {
		t.Fatalf("ordinary job = profile %v enforce %v skip recall %v; want none",
			mapped.AgentProfile, mapped.GetEnforceSelectedTools(), mapped.GetSkipAutomaticRecall())
	}
}
