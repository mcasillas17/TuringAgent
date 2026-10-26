package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/metadata"
)

// fakeOllama answers /api/chat the way a local model would in one delegation:
// Turing, offered team.delegate, calls it, then answers once the call returns;
// the specialist, given the brief, answers it; and Turing's continuation,
// given the results, answers from them.
type fakeOllama struct {
	mu             sync.Mutex
	offeredTeam    bool
	specialistSeen bool
	// continuation is the request Turing's continuation sent, if any.
	continuation string
	// specialist is the request the specialist sent, if any.
	specialist string
}

func (f *fakeOllama) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	request := string(body)
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-ndjson")
	switch {
	case strings.Contains(request, "TURING_RETRIEVED_DELEGATION_RESULTS"):
		f.continuation = request
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"Research found the notes."},"done":true,"done_reason":"stop"}`+"\n")
	case strings.Contains(request, "Gather the notes") && !strings.Contains(request, `"team.delegate"`):
		f.specialistSeen = true
		f.specialist = request
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"Found the notes."},"done":true,"done_reason":"stop"}`+"\n")
	case strings.Contains(request, `"role":"tool"`):
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"Asked Research to gather the notes."},"done":true,"done_reason":"stop"}`+"\n")
	case strings.Contains(request, `"team.delegate"`):
		f.offeredTeam = true
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"team.delegate","arguments":{"agent":"research","task":"Gather the notes"}}}]},"done":true,"done_reason":"tool_calls"}`+"\n")
	default:
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"No team."},"done":true,"done_reason":"stop"}`+"\n")
	}
}

// A local Turing turn, with the team on, delegates through the real runtime:
// the worker advertises team.delegate, offers the run's roster to the model,
// posts the call's beacon and dispatches it to CallTeamTool, and the
// specialist's run is queued on its brief and then runs. Its result is joined
// back into Turing's conversation, and Turing's continuation answers from it,
// framed as data, without the team. Under an approval_required policy the call
// waits for the user's approval, and CallTeamTool consumes it.
func TestTuringDelegatesThroughTheRuntime(t *testing.T) {
	t.Run("safe", func(t *testing.T) { testTuringDelegatesThroughTheRuntime(t, false) })
	t.Run("approval required", func(t *testing.T) { testTuringDelegatesThroughTheRuntime(t, true) })
}

func testTuringDelegatesThroughTheRuntime(t *testing.T, approvalRequired bool) {
	app := newTeamTestAppWithProfile(t, true, "---\nname: Research\ndescription: Looks things up\ntools: [skills_list]\nmemory: none\n---\nYou research.\n")
	model := &fakeOllama{}
	server := httptest.NewServer(model)
	t.Cleanup(server.Close)
	publicConn := newBufconnClient(t, app.PublicServer)
	internalConn := newBufconnClient(t, app.InternalServer)
	publicCtx, cancelPublic := context.WithCancel(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client"))
	defer cancelPublic()

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- runtimetestkit.RunWorker(workerCtx, runtimetestkit.WorkerConfig{
			Conn: internalConn, RuntimeToken: "internal", WorkerID: "team-runtime",
			MaxConcurrentRuns: 1, MaxToolCallsPerRun: 10,
			OpenAIBaseURL: server.URL, OpenAIModel: "unused",
			OllamaBaseURL: server.URL, LocalModel: "llama3.2",
		})
	}()
	t.Cleanup(func() {
		stopWorker()
		<-workerDone
	})
	waitFor(t, "the runtime worker to serve the local model", func() bool {
		return app.RuntimeService.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			RequestedTools: []string{repository.TeamDelegateTool}, MinimumTeamProtocolVersion: 1,
		}) == nil
	})

	team := turingv1.NewTeamServiceClient(publicConn)
	profiles, err := team.ListAgentProfiles(publicCtx, &turingv1.ListAgentProfilesRequest{})
	if err != nil || len(profiles.GetProfiles()) != 1 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	if _, err := team.GrantAgentProfile(publicCtx, &turingv1.GrantAgentProfileRequest{
		ProfileId: "research", Revision: profiles.GetProfiles()[0].GetRevision(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := team.SetAgentProfileEnabled(publicCtx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if approvalRequired {
		if _, err := turingv1.NewMcpRegistryServiceClient(publicConn).UpdateToolPolicyByName(publicCtx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: "team", ToolName: "team.delegate", Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED,
		}); err != nil {
			t.Fatal(err)
		}
	}
	session, err := app.Repository.CreateSession(context.Background(), "Team")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := turingv1.NewChatServiceClient(publicConn).SendMessage(publicCtx, &turingv1.SendMessageRequest{
		SessionId: session.SessionID, Content: "prep me for the design review", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "team_e2e",
	})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	parentRunID := queued.GetRunQueued().GetRunId()
	var approvalID string
	if approvalRequired {
		waitFor(t, "the delegation to ask for approval", func() bool {
			return app.database.QueryRowContext(context.Background(), `
				SELECT id FROM approvals WHERE run_id = ? AND tool_name = 'team.delegate' AND status = 'pending'`,
				parentRunID).Scan(&approvalID) == nil
		})
		approvals := turingv1.NewApprovalServiceClient(publicConn)
		details, err := approvals.GetApprovalDetails(publicCtx, &turingv1.GetApprovalDetailsRequest{ApprovalId: approvalID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := approvals.ApproveApproval(publicCtx, &turingv1.ApproveApprovalRequest{
			ApprovalId: approvalID, PreviewHash: details.GetPreviewHash(), ArgsHash: details.GetArgsHash(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, "the parent to complete", func() bool {
		run, err := app.Repository.GetRun(context.Background(), parentRunID)
		return err == nil && run.Status == "completed"
	})
	parent, err := app.Repository.GetRun(context.Background(), parentRunID)
	if err != nil || parent.AssistantContent != "Asked Research to gather the notes." {
		t.Fatalf("parent = %+v, %v", parent, err)
	}
	if count, err := app.Repository.DelegationCount(context.Background(), parentRunID); err != nil || count != 1 {
		t.Fatalf("delegations = %d, %v; want the model's one call", count, err)
	}
	if approvalRequired {
		var approvalStatus string
		if err := app.database.QueryRowContext(context.Background(), `SELECT status FROM approvals WHERE id = ?`, approvalID).Scan(&approvalStatus); err != nil {
			t.Fatal(err)
		}
		if approvalStatus != "consumed" {
			t.Fatalf("approval = %s, want consumed by the delegation", approvalStatus)
		}
	}
	waitFor(t, "the specialist to run on its brief", func() bool {
		model.mu.Lock()
		defer model.mu.Unlock()
		return model.specialistSeen
	})
	model.mu.Lock()
	offered := model.offeredTeam
	model.mu.Unlock()
	if !offered {
		t.Fatal("the model was never offered team.delegate")
	}

	var continuationID string
	waitFor(t, "Turing's continuation to answer", func() bool {
		var status string
		err := app.database.QueryRowContext(context.Background(),
			`SELECT id, status FROM agent_runs WHERE continues_run_id = ?`, parentRunID).Scan(&continuationID, &status)
		return err == nil && status == "completed"
	})
	continuation, err := app.Repository.GetRun(context.Background(), continuationID)
	if err != nil || continuation.AssistantContent != "Research found the notes." {
		t.Fatalf("continuation = %+v, %v; want Turing's answer from the results", continuation, err)
	}
	model.mu.Lock()
	request := model.continuation
	model.mu.Unlock()
	if !strings.Contains(request, "Found the notes.") || strings.Contains(request, `"team.delegate"`) {
		t.Fatalf("continuation request = %s, want the specialist's result framed and no team tool", request)
	}
	var joined string
	if err := app.database.QueryRowContext(context.Background(), `
		SELECT content FROM messages WHERE session_id = ? AND role = 'system' AND content_type = 'delegation_results'`,
		session.SessionID).Scan(&joined); err != nil || !strings.Contains(joined, "Found the notes.") {
		t.Fatalf("join message = %q, %v; want the result in the parent's conversation", joined, err)
	}
	var finished int
	if err := app.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM events WHERE session_id = ? AND type = 'delegation.finished'`, session.SessionID).Scan(&finished); err != nil || finished != 1 {
		t.Fatalf("delegation.finished events = %d, %v; want 1", finished, err)
	}
}

// A specialist that may use a GitHub tool gets a task that waits, unclaimed,
// for the user's consent. Allowing it, through the public API, records a
// decision the real runtime accepts: the task runs with the tool offered,
// and its result is joined back.
func TestASpecialistsGitHubToolWaitsForTheUsersConsent(t *testing.T) {
	app := newTeamTestAppWithProfile(t, true, "---\nname: Research\ndescription: Looks things up\ntools: [skills_list, github.get_issue]\nmemory: none\n---\nYou research.\n")
	model := &fakeOllama{}
	server := httptest.NewServer(model)
	t.Cleanup(server.Close)
	publicConn := newBufconnClient(t, app.PublicServer)
	internalConn := newBufconnClient(t, app.InternalServer)
	publicCtx, cancelPublic := context.WithCancel(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client"))
	defer cancelPublic()
	if _, err := turingv1.NewIntegrationServiceClient(publicConn).ConnectAccount(publicCtx, &turingv1.ConnectAccountRequest{
		Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Work GitHub",
		Credential: "ghp_test", ConsentAcknowledged: true,
	}); err != nil {
		t.Fatal(err)
	}

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- runtimetestkit.RunWorker(workerCtx, runtimetestkit.WorkerConfig{
			Conn: internalConn, RuntimeToken: "internal", WorkerID: "team-runtime",
			MaxConcurrentRuns: 1, MaxToolCallsPerRun: 10,
			OpenAIBaseURL: server.URL, OpenAIModel: "unused",
			OllamaBaseURL: server.URL, LocalModel: "llama3.2",
		})
	}()
	t.Cleanup(func() {
		stopWorker()
		<-workerDone
	})
	waitFor(t, "the runtime worker to serve the local model with GitHub", func() bool {
		return app.RuntimeService.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			RequestedTools:             []string{repository.TeamDelegateTool, "integrations/github.get_issue"},
			MinimumTeamProtocolVersion: 1, RemoteEgressDecision: true,
		}) == nil
	})

	team := turingv1.NewTeamServiceClient(publicConn)
	profiles, err := team.ListAgentProfiles(publicCtx, &turingv1.ListAgentProfilesRequest{})
	if err != nil || len(profiles.GetProfiles()) != 1 {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	if consent := profiles.GetProfiles()[0].GetConsentTools(); len(consent) != 1 || consent[0] != "integrations/github.get_issue" {
		t.Fatalf("consent tools = %q, want the GitHub tool", consent)
	}
	if _, err := team.GrantAgentProfile(publicCtx, &turingv1.GrantAgentProfileRequest{
		ProfileId: "research", Revision: profiles.GetProfiles()[0].GetRevision(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := team.SetAgentProfileEnabled(publicCtx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	session, err := app.Repository.CreateSession(context.Background(), "Team")
	if err != nil {
		t.Fatal(err)
	}
	// With GitHub connected, Turing's own send carries a decision of its own.
	chat := turingv1.NewChatServiceClient(publicConn)
	send := &turingv1.SendMessageRequest{
		SessionId: session.SessionID, Content: "prep me for the design review", ContentType: "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2",
		IdempotencyKey: "team_consent_e2e",
	}
	prepared, err := chat.PrepareRemoteEgress(publicCtx, &turingv1.PrepareRemoteEgressRequest{
		SessionId: send.SessionId, Content: send.Content, ContentType: send.ContentType, AgentId: send.AgentId,
		ModelProvider: send.ModelProvider, Model: send.Model, IdempotencyKey: send.IdempotencyKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	send.RemoteEgressConsent = &turingv1.RemoteEgressConsent{
		Challenge: prepared.GetDisclosure().GetChallenge(), Acknowledged: true,
		AcknowledgedDataCategories: prepared.GetDisclosure().GetDataCategories(),
	}
	stream, err := chat.SendMessage(publicCtx, send)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	parentRunID := queued.GetRunQueued().GetRunId()
	waitFor(t, "the parent to complete", func() bool {
		run, err := app.Repository.GetRun(context.Background(), parentRunID)
		return err == nil && run.Status == "completed"
	})
	listed, err := team.ListDelegations(publicCtx, &turingv1.ListDelegationsRequest{ParentSessionId: session.SessionID})
	if err != nil || len(listed.GetDelegations()) != 1 {
		t.Fatalf("delegations = %+v, %v", listed, err)
	}
	delegation := listed.GetDelegations()[0]
	if delegation.GetState() != "waiting_consent" || delegation.GetEgressConsent() != "pending" {
		t.Fatalf("delegation = %+v, want it waiting for consent", delegation)
	}
	time.Sleep(200 * time.Millisecond)
	model.mu.Lock()
	ranEarly := model.specialistSeen
	model.mu.Unlock()
	if ranEarly {
		t.Fatal("the specialist ran before the user consented")
	}

	disclosure, err := team.PrepareDelegationEgress(publicCtx, &turingv1.PrepareDelegationEgressRequest{DelegationId: delegation.GetDelegationId()})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := team.DecideDelegationEgress(publicCtx, &turingv1.DecideDelegationEgressRequest{
		DelegationId: delegation.GetDelegationId(),
		Consent: &turingv1.RemoteEgressConsent{
			Challenge: disclosure.GetDisclosure().GetChallenge(), Acknowledged: true,
			AcknowledgedDataCategories: disclosure.GetDisclosure().GetDataCategories(),
		},
	})
	if err != nil || decided.GetEgressConsent() != "granted" {
		t.Fatalf("decision = %+v, %v", decided, err)
	}
	waitFor(t, "the specialist's task to finish", func() bool {
		run, err := app.Repository.GetRun(context.Background(), delegation.GetChildRunId())
		return err == nil && (run.Status == "completed" || run.Status == "failed")
	})
	child, err := app.Repository.GetRun(context.Background(), delegation.GetChildRunId())
	if err != nil || child.Status != "completed" {
		t.Fatalf("task = %+v, %v; want the runtime to accept its decision and complete it", child, err)
	}
	model.mu.Lock()
	request := model.specialist
	model.mu.Unlock()
	if !strings.Contains(request, "github.get_issue") {
		t.Fatalf("specialist request = %s, want the granted GitHub tool offered", request)
	}
	waitFor(t, "Turing's continuation to answer", func() bool {
		var status string
		return app.database.QueryRowContext(context.Background(),
			`SELECT status FROM agent_runs WHERE continues_run_id = ?`, parentRunID).Scan(&status) == nil && status == "completed"
	})
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
