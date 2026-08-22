package agent

import (
	"errors"
	"fmt"
	"slices"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

func validateEgressDecisionShape(job *turingv1.AgentJob) error {
	providerRemote := job.GetModelProvider() == turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE ||
		job.GetExternalAgent() != nil
	decision := job.GetEgressDecision()
	if decision == nil {
		if providerRemote {
			return errors.New("remote run has no egress decision")
		}
		return nil
	}
	hasRemoteMCP := len(decision.GetRemoteMcpServers()) > 0
	hasIntegrations := len(decision.GetIntegrationEndpoints()) > 0
	if !providerRemote && !hasRemoteMCP && !hasIntegrations {
		return errors.New("local run carries an inapplicable egress decision")
	}
	if providerRemote && job.GetModelProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE {
		return errors.New("external agent run is not routed through the remote provider")
	}
	if decision.GetVersion() != backendegress.DecisionVersion ||
		decision.GetDecisionId() == "" ||
		decision.GetChallengeFingerprint() == "" ||
		decision.GetRequestDigest() == "" ||
		decision.GetProvider() != job.GetModelProvider() ||
		decision.GetModel() != job.GetModel() ||
		decision.GetConsentGrantedAt() == nil ||
		decision.GetSkillSnapshotFingerprint() == "" ||
		!slices.Equal(decision.GetSelectedTools(), job.GetSelectedTools()) {
		return errors.New("remote run decision does not match its frozen job")
	}
	if providerRemote {
		if decision.GetEndpoint() == "" || decision.GetEndpointHost() == "" {
			return errors.New("remote provider decision has no endpoint")
		}
	} else if decision.GetEndpoint() != "" || decision.GetEndpointHost() != "" ||
		decision.GetExternalAgentId() != "" || decision.GetExternalCredentialRefHash() != "" {
		return errors.New("local-provider decision contains a remote model destination")
	}
	skillFingerprint, err := runtimeSkillSnapshotFingerprint(job.GetSkills())
	if err != nil || skillFingerprint != decision.GetSkillSnapshotFingerprint() {
		return errors.New("remote run skill snapshot differs from the egress decision")
	}
	recallApplicable := providerRemote && job.GetExternalAgent() == nil
	if decision.GetRecallApplicable() != recallApplicable ||
		decision.GetMemoryProfileApplicable() {
		return errors.New("remote run context flags differ from the egress decision")
	}
	if providerRemote {
		endpoint, err := backendegress.ParseKeyedEndpoint(decision.GetEndpoint())
		if err != nil || endpoint.Canonical != decision.GetEndpoint() || endpoint.Host != decision.GetEndpointHost() {
			return errors.New("remote run decision endpoint is invalid")
		}
	}
	for index, destination := range decision.GetRemoteMcpServers() {
		if destination.GetServerName() == "" ||
			(index > 0 && decision.GetRemoteMcpServers()[index-1].GetServerName() >= destination.GetServerName()) {
			return errors.New("remote MCP destinations are invalid or unsorted")
		}
		endpoint, err := backendegress.ParseKeyedEndpoint(destination.GetEndpoint())
		if err != nil || endpoint.Canonical != destination.GetEndpoint() || endpoint.Host != destination.GetEndpointHost() {
			return errors.New("remote MCP destination endpoint is invalid")
		}
		hasSelectedTool := false
		for _, tool := range job.GetSelectedTools() {
			if len(tool) > len(destination.GetServerName()) &&
				tool[:len(destination.GetServerName())+1] == destination.GetServerName()+"/" {
				hasSelectedTool = true
				break
			}
		}
		if !hasSelectedTool {
			return errors.New("remote MCP destination has no selected tool")
		}
	}
	for index, destination := range decision.GetIntegrationEndpoints() {
		if destination.GetEndpoint() != "https://api.github.com" || destination.GetEndpointHost() != "api.github.com" ||
			destination.GetConnectionId() == "" || destination.GetDisplayName() == "" || len(destination.GetTools()) == 0 ||
			!slices.IsSorted(destination.GetTools()) {
			return errors.New("integration destinations are invalid")
		}
		if index > 0 {
			previous := decision.GetIntegrationEndpoints()[index-1]
			if previous.GetEndpoint() > destination.GetEndpoint() ||
				(previous.GetEndpoint() == destination.GetEndpoint() && previous.GetConnectionId() >= destination.GetConnectionId()) {
				return errors.New("integration destinations are unsorted")
			}
		}
		for _, tool := range destination.GetTools() {
			if !slices.Contains(job.GetSelectedTools(), "integrations/"+tool) {
				return errors.New("integration destination has an unselected tool")
			}
		}
	}

	present := make(map[turingv1.EgressDataCategory]struct{}, len(decision.GetDataCategories()))
	for _, category := range decision.GetDataCategories() {
		if category <= turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_UNSPECIFIED ||
			category > turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_ATTACHMENTS {
			return errors.New("remote run decision has an unknown data category")
		}
		if _, duplicate := present[category]; duplicate {
			return errors.New("remote run decision has duplicate data categories")
		}
		present[category] = struct{}{}
	}
	required := make([]turingv1.EgressDataCategory, 0)
	if providerRemote {
		required = append(required,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CURRENT_MESSAGE,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_SKILL_CONTENT)
		if job.GetExternalAgent() == nil {
			required = append(required, turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL)
		}
	}
	if providerRemote && len(job.GetSelectedTools()) > 0 {
		required = append(required,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_SCHEMAS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	if hasRemoteMCP {
		required = append(required,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	if hasIntegrations {
		required = append(required,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS)
	}
	slices.Sort(required)
	required = slices.Compact(required)
	for _, category := range required {
		if _, ok := present[category]; !ok {
			return fmt.Errorf("remote run decision is missing %s", category.String())
		}
	}
	if len(present) != len(required) {
		return errors.New("remote run decision contains inapplicable data categories")
	}
	return nil
}

func runtimeSkillSnapshotFingerprint(skills []*turingv1.SkillSnapshot) (string, error) {
	snapshots := make([]backendegress.SkillSnapshot, len(skills))
	for index, skill := range skills {
		snapshots[index] = backendegress.SkillSnapshot{
			SkillID: skill.GetSkillId(), Name: skill.GetName(),
			Description: skill.GetDescription(), Category: skill.GetCategory(),
			Instructions: skill.GetInstructions(), References: skill.GetReferences(),
			Withheld:            skill.GetWithheld(),
			MissingCapabilities: skill.GetMissingCapabilities(),
		}
	}
	return backendegress.SkillSnapshotFingerprint(snapshots)
}

func validateEgressProvider(job *turingv1.AgentJob, provider llm.Provider) error {
	if job.GetModelProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE {
		if remote, ok := provider.(llm.EgressProvider); ok && remote.EgressEndpoint() != "" {
			return errors.New("local run resolved to a provider with a remote endpoint")
		}
		return nil
	}
	decision := job.GetEgressDecision()
	if provider.ID() != "openai_compatible" {
		return errors.New("remote run resolved to a non-remote provider")
	}
	remote, ok := provider.(llm.EgressProvider)
	if !ok {
		return errors.New("remote provider does not expose its endpoint identity")
	}
	if remote.EgressEndpoint() != decision.GetEndpoint() {
		return errors.New("remote provider endpoint differs from the egress decision")
	}
	if target := job.GetExternalAgent(); target != nil {
		if target.GetAgentId() == "" || target.GetAgentId() != decision.GetExternalAgentId() {
			return errors.New("external agent identity differs from the egress decision")
		}
		if target.GetBaseUrl() != decision.GetEndpoint() {
			return errors.New("external agent endpoint differs from the egress decision")
		}
		if backendegress.HashCredentialReference(target.GetCredentialRef()) !=
			decision.GetExternalCredentialRefHash() {
			return errors.New("external agent credential reference differs from the egress decision")
		}
		return nil
	}
	return nil
}
