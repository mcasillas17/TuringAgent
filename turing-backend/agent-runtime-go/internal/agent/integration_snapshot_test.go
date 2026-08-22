package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	runnertools "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type integrationExecuteClient struct {
	description  string
	connectionID string
	callErr      error
	calls        atomic.Int32
}

func (c *integrationExecuteClient) ListTools(context.Context) ([]map[string]any, error) {
	return []map[string]any{{
		"name": "github.list_issues", "description": c.description,
		"inputSchema": map[string]any{"type": "object"},
	}}, nil
}
func (*integrationExecuteClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, errors.New("integration caller-side dispatch was bypassed")
}
func (c *integrationExecuteClient) CallToolWithCallerApproval(_ context.Context, runID, approvalID, name string, args map[string]any) (map[string]any, error) {
	if runID == "" || approvalID != "approval_read" || name != "github.list_issues" {
		return nil, errors.New("integration dispatch context mismatch")
	}
	connectionID, _ := args["connection_id"].(string)
	c.connectionID = connectionID
	c.calls.Add(1)
	if c.callErr != nil {
		return nil, c.callErr
	}
	return map[string]any{"content": "[]"}, nil
}

type descriptionDrivenIntegrationProvider struct {
	calls    atomic.Int32
	requests []llm.ChatRequest
}

func (*descriptionDrivenIntegrationProvider) ID() string { return "ollama" }
func (*descriptionDrivenIntegrationProvider) ContextWindowTokens() int {
	return llm.DefaultContextWindowTokens
}
func (*descriptionDrivenIntegrationProvider) MaxOutputTokens() int { return llm.DefaultMaxOutputTokens }
func (*descriptionDrivenIntegrationProvider) EstimateRequestTokens(req llm.ChatRequest) (int, error) {
	return estimateTestProviderRequest(req)
}
func (p *descriptionDrivenIntegrationProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	out := make(chan llm.StreamEvent, 1)
	p.requests = append(p.requests, req)
	call := p.calls.Add(1)
	if call == 1 {
		if len(req.Tools) != 1 {
			return nil, errors.New("GitHub tool was not offered")
		}
		description := req.Tools[0].Description
		start := strings.Index(description, "(")
		end := strings.Index(description, ",")
		if start < 0 || end <= start+1 {
			return nil, errors.New("connection pair missing from description")
		}
		connectionID := description[start+1 : end]
		out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
			ID: "provider_integration_call", Name: "github.list_issues",
			Arguments: map[string]any{"connection_id": connectionID, "owner": "owner", "repo": "repo"},
		}}}
	} else {
		out <- llm.StreamEvent{Type: "delta", Text: "Issues listed."}
	}
	close(out)
	return out, nil
}

func integrationExecuteJob(t *testing.T, connectionID string) *turingv1.AgentJob {
	t.Helper()
	fingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob()
	job.SelectedTools = []string{"integrations/github.list_issues"}
	job.EgressDecision = &turingv1.RunEgressDecision{
		DecisionId: "decision", Version: backendegress.DecisionVersion,
		ChallengeFingerprint: "fingerprint", RequestDigest: "digest",
		Provider: job.GetModelProvider(), Model: job.GetModel(), SkillSnapshotFingerprint: fingerprint,
		ConsentGrantedAt: timestamppb.New(time.Now()), SelectedTools: append([]string(nil), job.GetSelectedTools()...),
		DataCategories: []turingv1.EgressDataCategory{
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
		},
		IntegrationEndpoints: []*turingv1.IntegrationEgressDestination{{
			Endpoint: "https://api.github.com", EndpointHost: "api.github.com",
			ConnectionId: connectionID, DisplayName: "Personal GitHub", Tools: []string{"github.list_issues"},
		}},
	}
	return job
}

func integrationApprovalRunner(afterErr error) *runnertools.Runner {
	return &runnertools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER && afterErr != nil {
				return nil, afterErr
			}
			decision := turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				decision = turingv1.ToolPolicyDecision_DECISION_ALLOW
			}
			return &turingv1.ToolPolicyDecision{
				Decision: decision, ApprovalId: "approval_read", ToolCallId: beacon.GetToolCallId(), ReadOnly: true,
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) { return "approved", nil },
	}
}

func TestFrozenIntegrationToolFailsWhenCurrentDiscoveryIsEmpty(t *testing.T) {
	fingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob()
	job.SelectedTools = []string{"integrations/github.list_issues"}
	job.EgressDecision = &turingv1.RunEgressDecision{
		DecisionId: "decision", Version: backendegress.DecisionVersion,
		ChallengeFingerprint: "fingerprint", RequestDigest: "digest",
		Provider: job.GetModelProvider(), Model: job.GetModel(), SkillSnapshotFingerprint: fingerprint,
		ConsentGrantedAt: timestamppb.New(time.Now()), SelectedTools: append([]string(nil), job.GetSelectedTools()...),
		DataCategories: []turingv1.EgressDataCategory{
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
			turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
		},
		IntegrationEndpoints: []*turingv1.IntegrationEgressDestination{{
			Endpoint: "https://api.github.com", EndpointHost: "api.github.com",
			ConnectionId: "conn_revoked", DisplayName: "Revoked GitHub", Tools: []string{"github.list_issues"},
		}},
	}
	provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "delta", Text: "must not run"}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{IntegrationTools: func(context.Context) (ToolLister, error) {
			return &assistantTestToolLister{}, nil
		}},
	)
	var updates []*turingv1.RuntimeUpdate
	if err := assistant.Execute(context.Background(), job, func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider requests=%d, want none after snapshot became unavailable", len(provider.requests))
	}
	var failure *turingv1.RuntimeRunFailed
	for _, update := range updates {
		if update.GetRunFailed() != nil {
			failure = update.GetRunFailed()
		}
	}
	if failure == nil || failure.GetCode() != "egress_decision_invalid" || !strings.Contains(failure.GetMessage(), "selected tool snapshot is unavailable") {
		t.Fatalf("failure=%+v, want snapshot-unavailable run failure", failure)
	}
}

func TestExecuteDispatchesIntegrationUsingConnectionIDFromToolDescription(t *testing.T) {
	const connectionID = "conn_from_description"
	job := integrationExecuteJob(t, connectionID)
	client := &integrationExecuteClient{description: "List issues. Available connections: (" + connectionID + ", Personal GitHub)"}
	provider := &descriptionDrivenIntegrationProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: integrationApprovalRunner(nil), IntegrationTools: func(context.Context) (ToolLister, error) { return client, nil }},
	)
	updates := collectUpdates(t, assistant, job)
	if client.calls.Load() != 1 || client.connectionID != connectionID {
		t.Fatalf("integration calls=%d connection=%q, want one call using advertised id", client.calls.Load(), client.connectionID)
	}
	if provider.calls.Load() != 2 || updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("provider calls=%d final update=%+v", provider.calls.Load(), updates[len(updates)-1])
	}
}

func TestExecuteReturnsApprovedIntegrationReadFailureToModelAndContinues(t *testing.T) {
	const connectionID = "conn_provider_500"
	client := &integrationExecuteClient{
		description: "List issues. Available connections: (" + connectionID + ", Personal GitHub)",
		callErr:     errors.New("rpc error: code = Unavailable desc = GitHub request failed with HTTP status 500"),
	}
	provider := &descriptionDrivenIntegrationProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: integrationApprovalRunner(nil), IntegrationTools: func(context.Context) (ToolLister, error) { return client, nil }},
	)
	updates := collectUpdates(t, assistant, integrationExecuteJob(t, connectionID))
	if client.calls.Load() != 1 || provider.calls.Load() != 2 || updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("client calls=%d provider calls=%d final=%+v, want one failed read followed by model completion", client.calls.Load(), provider.calls.Load(), updates[len(updates)-1])
	}
	var toolResult string
	for _, message := range provider.requests[1].Messages {
		if message.Role == "tool" && message.Name == "github.list_issues" {
			toolResult = message.Content
		}
	}
	var payload map[string]string
	if toolResult == "" || len(toolResult) > 512 || json.Unmarshal([]byte(toolResult), &payload) != nil ||
		!strings.Contains(payload["error"], "HTTP status 500") {
		t.Fatalf("bounded model tool result=%q", toolResult)
	}
}

func TestExecuteClassifiesSuccessfulIntegrationReadAfterReportFailureAsReporting(t *testing.T) {
	const connectionID = "conn_after_report_failure"
	reportErr := errors.New("integration completed beacon failed")
	client := &integrationExecuteClient{description: "List issues. Available connections: (" + connectionID + ", Personal GitHub)"}
	provider := &descriptionDrivenIntegrationProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Runner: integrationApprovalRunner(reportErr), IntegrationTools: func(context.Context) (ToolLister, error) { return client, nil }},
	)
	var updates []*turingv1.RuntimeUpdate
	err := assistant.Execute(context.Background(), integrationExecuteJob(t, connectionID), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})
	if !errors.Is(err, reportErr) || !runnertools.ReportingFailed(err) || runnertools.SideEffectWasCommitted(err) {
		t.Fatalf("Execute error=%T %v, want non-side-effecting reporting failure", err, err)
	}
	if client.calls.Load() != 1 || provider.calls.Load() != 1 {
		t.Fatalf("client calls=%d provider calls=%d, want completed read and no model retry", client.calls.Load(), provider.calls.Load())
	}
	for _, update := range updates {
		if update.GetRunCompleted() != nil || update.GetRunFailed() != nil {
			t.Fatalf("reporting failure emitted terminal update %+v", update)
		}
	}
}
