package runtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestCapabilityUpdateReplacesTheRegistrationSnapshot(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-updated", "registration-current", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 2,
	))
	defer func() { _ = stream.CloseSend() }()

	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
		WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId:       "worker-updated",
			RegistrationId: "registration-current",
			Capabilities: modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 4096, 1,
			),
		},
	}}); err != nil {
		t.Fatal(err)
	}

	eventually(t, time.Second, func() bool {
		return h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
			RequiredContextTokens: 4096, MinimumWorkerMaxConcurrentRuns: 1,
		}) == nil
	})
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err == nil {
		t.Fatal("replaced Ollama capability remained routable")
	}
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
		MinimumWorkerMaxConcurrentRuns: 2,
	}); err == nil {
		t.Fatal("reduced capacity remained routable")
	}
}

func TestStaleCapabilityUpdateDisconnectsOnlyItsRegistrationAndReconnectRestoresIt(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-fenced", "registration-current", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
		WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId:       "worker-fenced",
			RegistrationId: "registration-stale",
			Capabilities: modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 4096, 1,
			),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("stale capability update kept the stream connected")
	}

	eventually(t, time.Second, func() bool {
		return h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		}) != nil
	})
	reconnected := connectWorkerCapabilities(t, h, "worker-fenced", "registration-fresh", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = reconnected.CloseSend() }()
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatalf("reconnected registration was not restored: %v", err)
	}
}

func TestMultiWorkerDispatchClaimsOnlyCompatibleJobs(t *testing.T) {
	h := newHarness(t)
	openAISession, err := h.repo.CreateSession(context.Background(), "OpenAI")
	if err != nil {
		t.Fatal(err)
	}
	openAIJob, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: openAISession.SessionID, Content: "openai", AgentID: "general_assistant",
		ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	ollamaSession, err := h.repo.CreateSession(context.Background(), "Ollama")
	if err != nil {
		t.Fatal(err)
	}
	ollamaJob, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: ollamaSession.SessionID, Content: "ollama", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	ollama := connectWorkerCapabilities(t, h, "worker-ollama", "registration-ollama", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = ollama.CloseSend() }()
	if assigned := recvUntil(t, ollama, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned(); assigned.GetJobId() != ollamaJob.JobID {
		t.Fatalf("Ollama worker received %q, want %q", assigned.GetJobId(), ollamaJob.JobID)
	}

	openAI := connectWorkerCapabilities(t, h, "worker-openai", "registration-openai", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	defer func() { _ = openAI.CloseSend() }()
	if assigned := recvUntil(t, openAI, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned(); assigned.GetJobId() != openAIJob.JobID {
		t.Fatalf("OpenAI worker received %q, want %q", assigned.GetJobId(), openAIJob.JobID)
	}
}

func TestCapabilityLossAndDisconnectAppendQueueNotices(t *testing.T) {
	tests := []struct {
		name string
		lose func(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient)
	}{
		{
			name: "snapshot replacement",
			lose: func(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient) {
				t.Helper()
				if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
					WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
						WorkerId: "worker-loss", RegistrationId: "registration-loss",
						Capabilities: modelCapabilities(
							turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
						),
					},
				}}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "disconnect",
			lose: func(t *testing.T, stream turingv1.RuntimeService_ConnectWorkerClient) {
				t.Helper()
				if err := stream.CloseSend(); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			stream := connectWorkerCapabilities(t, h, "worker-loss", "registration-loss", modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
			))
			defer func() { _ = stream.CloseSend() }()
			session, err := h.repo.CreateSession(context.Background(), "Queued")
			if err != nil {
				t.Fatal(err)
			}
			enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "wait", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}

			test.lose(t, stream)
			eventually(t, time.Second, func() bool {
				events, _, err := h.repo.ReplayEvents(context.Background(), session.SessionID, 0, 50)
				if err != nil {
					return false
				}
				for _, event := range events {
					if event.Type != "agent.run.step" || !event.RunID.Valid || event.RunID.String != enqueued.RunID {
						continue
					}
					var payload map[string]any
					if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil &&
						payload["reason"] == "routing_capability_unavailable" {
						return true
					}
				}
				return false
			})
		})
	}
}

func TestToolCapabilityLossLeavesIncompatibleJobQueued(t *testing.T) {
	h := newHarness(t)
	initial := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	initial.Tools = []*turingv1.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{},
	}}
	stream := connectWorkerCapabilities(t, h, "worker-tool-loss", "registration-tool-loss", initial)
	defer func() { _ = stream.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "Tool loss")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "time", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", RequestedTools: []string{"system/system.time"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
		WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-tool-loss", RegistrationId: "registration-tool-loss",
			Capabilities: modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		events, _, err := h.repo.ReplayEvents(context.Background(), session.SessionID, 0, 50)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.Type != "agent.run.step" {
				continue
			}
			var payload map[string]any
			if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil &&
				payload["unavailableCapability"] == turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_TOOL.String() {
				return true
			}
		}
		return false
	})
	var status string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("job status after tool loss = %q, want pending", status)
	}
}

func TestWorkerReconnectRestoresQueuedRouteAndAppendsNotice(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Reconnect")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "resume", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	stream := connectWorkerCapabilities(t, h, "worker-restored", "registration-restored", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	if assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned(); assigned.GetRunId() != enqueued.RunID {
		t.Fatalf("restored assignment = %+v, want run %q", assigned, enqueued.RunID)
	}
	events, _, err := h.repo.ReplayEvents(context.Background(), session.SessionID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != "agent.run.step" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil &&
			payload["reason"] == "routing_capability_restored" {
			return
		}
	}
	t.Fatalf("run %q has no routing capability restoration notice", enqueued.RunID)
}

func TestCapabilityRegistryConcurrentLifecycleIsRaceSafe(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-race", "registration-race", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 2,
	))

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
				AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			if err := h.service.DispatchPending(context.Background()); err != nil {
				errs <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 50 {
			provider := turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
			model := "llama3.2"
			if i%2 == 1 {
				provider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
				model = "gpt-4o-mini"
			}
			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
				WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
					WorkerId: "worker-race", RegistrationId: "registration-race",
					Capabilities: modelCapabilities(provider, model, 8192, 2),
				},
			}}); err != nil {
				errs <- err
				return
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent lifecycle error: %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Second, func() bool {
		return h.service.registeredWorker("worker-race") == nil
	})
}

func modelCapabilities(
	provider turingv1.ModelProvider,
	model string,
	maxContextTokens int32,
	maxConcurrentRuns int32,
) *turingv1.WorkerCapabilities {
	return &turingv1.WorkerCapabilities{
		Models: []*turingv1.ModelCapability{{
			Provider: provider, Model: model, MaxContextTokens: maxContextTokens,
		}},
		AgentIds:          []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		MaxConcurrentRuns: maxConcurrentRuns,
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied before timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
