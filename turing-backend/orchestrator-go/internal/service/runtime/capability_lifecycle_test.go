package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

	eventually(t, eventuallyTimeout, func() bool {
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

func TestCapabilityPersistenceFailureRestoresThePreviousSnapshot(t *testing.T) {
	h := newHarness(t)
	initial := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	initial.Tools = []*turingv1.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{},
	}}
	stream := connectWorkerCapabilities(t, h, "worker-rollback", "registration-rollback", initial)
	defer func() { _ = stream.CloseSend() }()
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_capability_tool_persistence
		BEFORE UPDATE ON tools
		BEGIN
			SELECT RAISE(ABORT, 'capability persistence failed');
		END
	`); err != nil {
		t.Fatal(err)
	}
	connected := h.service.registeredWorker("worker-rollback")
	err := h.service.replaceWorkerCapabilities(context.Background(), "worker-rollback", connected,
		&turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-rollback", RegistrationId: "registration-rollback",
			Capabilities: modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 4096, 1,
			),
		})
	if status.Code(err) != codes.Internal {
		t.Fatalf("capability update error = %v, want Internal", err)
	}
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatalf("previous route was not restored: %v", err)
	}
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
	}); err == nil {
		t.Fatal("failed capability snapshot remained active")
	}
}

func TestWorkerRegistrationSurvivesRoutingNoticeFailure(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Registration notice failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs Ollama", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	failRoutingNoticeInserts(t, h)

	stream := connectWorkerCapabilities(t, h, "worker-notice-failure", "registration-notice-failure", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	if err := h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "openai_compatible", Model: "gpt-4o-mini",
	}); err != nil {
		t.Fatalf("registered route unavailable after advisory notice failure: %v", err)
	}
}

func TestCapabilityReplacementSurvivesRoutingNoticeFailure(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-update-notice", "registration-update-notice", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "Capability notice failure")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs Ollama", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	failRoutingNoticeInserts(t, h)

	connected := h.service.registeredWorker("worker-update-notice")
	err = h.service.replaceWorkerCapabilities(context.Background(), "worker-update-notice", connected,
		&turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-update-notice", RegistrationId: "registration-update-notice",
			Capabilities: modelCapabilities(
				turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
			),
		})
	if err != nil {
		t.Fatalf("capability replacement reported advisory notice failure: %v", err)
	}
	if h.service.registeredWorker("worker-update-notice") != connected {
		t.Fatal("capability replacement removed the healthy registration")
	}
}

func TestHeartbeatRevivalDispatchesAfterRoutingNoticeFailure(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: 40 * time.Millisecond})
	stream := connectWorkerCapabilities(t, h, "worker-revival-notice", "registration-revival-notice", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "Revival notice failure")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs revival", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	connected := h.service.registeredWorker("worker-revival-notice")
	connected.mu.Lock()
	connected.lastHeartbeat = time.Now().Add(-time.Second)
	connected.mu.Unlock()
	if err := h.service.refreshPendingCapabilityState(context.Background(), "worker expired", "", true, false); err != nil {
		t.Fatal(err)
	}
	failRoutingNoticeInserts(t, h)

	if err := h.service.renewWorkerLeases(context.Background(), "worker-revival-notice", connected,
		&turingv1.RuntimeHeartbeat{WorkerId: "worker-revival-notice"}); err != nil {
		t.Fatalf("heartbeat revival reported advisory notice failure: %v", err)
	}
	assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
		return command.GetRunAssigned() != nil
	}).GetRunAssigned()
	if assigned.GetRunId() != enqueued.RunID {
		t.Fatalf("revival assignment = %+v, want run %q", assigned, enqueued.RunID)
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

	eventually(t, eventuallyTimeout, func() bool {
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
		EgressDecision: runtimeRemoteDecision("gpt-4o-mini"),
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
			first, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "occupy worker", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.service.DispatchPending(context.Background()); err != nil {
				t.Fatal(err)
			}
			if assigned := recvUntil(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil
			}).GetRunAssigned(); assigned.GetRunId() != first.RunID {
				t.Fatalf("first assignment = %+v, want run %q", assigned, first.RunID)
			}
			enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
				SessionID: session.SessionID, Content: "wait", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "llama3.2",
			})
			if err != nil {
				t.Fatal(err)
			}

			test.lose(t, stream)
			eventually(t, eventuallyTimeout, func() bool {
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

func TestCapabilityNoticeDedupSurvivesPartialPersistenceFailure(t *testing.T) {
	h := newHarness(t)
	registerWorkerCapabilities(t, h, "worker-partial-notice", "registration-partial-notice", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))

	var enqueued []repository.EnqueueUserMessageResult
	for _, title := range []string{"First unavailable", "Second unavailable"} {
		session, err := h.repo.CreateSession(context.Background(), title)
		if err != nil {
			t.Fatal(err)
		}
		result, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: title, AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		enqueued = append(enqueued, result)
	}
	if _, err := h.database.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TRIGGER fail_second_routing_notice
		BEFORE INSERT ON events
		WHEN NEW.run_id = '%s' AND NEW.type = 'agent.run.step'
			AND json_extract(NEW.payload_json, '$.reason') = 'routing_capability_unavailable'
		BEGIN
			SELECT RAISE(FAIL, 'second notice blocked');
		END`, enqueued[1].RunID)); err != nil {
		t.Fatal(err)
	}
	h.service.mu.Lock()
	connected := h.service.workers["worker-partial-notice"]
	delete(h.service.workers, "worker-partial-notice")
	h.service.mu.Unlock()
	if connected == nil {
		t.Fatal("connected worker is missing")
	}

	if err := h.service.refreshPendingCapabilityState(context.Background(), "test loss", "", true, false); err == nil {
		t.Fatal("partial notice refresh succeeded, want persistence failure")
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_second_routing_notice`); err != nil {
		t.Fatal(err)
	}
	if err := h.service.refreshPendingCapabilityState(context.Background(), "retry loss", "", true, false); err != nil {
		t.Fatal(err)
	}
	for _, result := range enqueued {
		if got := countRoutingNotices(t, h, result.RunID, "routing_capability_unavailable"); got != 1 {
			t.Fatalf("run %s has %d loss notices, want 1", result.RunID, got)
		}
	}
}

func TestNonRestoringRefreshKeepsPublishedLossUntilRestorationCanBeEmitted(t *testing.T) {
	h := newHarness(t)
	registerWorkerCapabilities(t, h, "worker-delayed-restoration", "registration-delayed-restoration", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	session, err := h.repo.CreateSession(context.Background(), "Delayed restoration")
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
	connected := h.service.registeredWorker("worker-delayed-restoration")
	if connected == nil {
		t.Fatal("worker is not registered")
	}
	unsupported, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	if err != nil {
		t.Fatal(err)
	}
	supported, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	if err != nil {
		t.Fatal(err)
	}
	connected.mu.Lock()
	connected.capabilities = unsupported
	connected.mu.Unlock()
	if err := h.service.refreshPendingCapabilityState(context.Background(), "loss", "", true, false); err != nil {
		t.Fatal(err)
	}
	connected.mu.Lock()
	connected.capabilities = supported
	connected.mu.Unlock()

	if err := h.service.refreshPendingCapabilityState(context.Background(), "interleaved non-restoring refresh", "", false, false); err != nil {
		t.Fatal(err)
	}
	if _, tracked := h.service.unavailablePending[enqueued.RunID]; !tracked {
		t.Fatal("non-restoring refresh dropped the published loss")
	}
	if err := h.service.refreshPendingCapabilityState(context.Background(), "restored", "", false, true); err != nil {
		t.Fatal(err)
	}
	if got := countRoutingNotices(t, h, enqueued.RunID, "routing_capability_restored"); got != 1 {
		t.Fatalf("restoration notices = %d, want 1", got)
	}
}

func TestToolCapabilityLossLeavesIncompatibleJobQueued(t *testing.T) {
	h := newHarness(t)
	initial := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	initial.Tools = []*turingv1.DiscoveredTool{{
		ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{},
	}}
	registerWorkerCapabilities(t, h, "worker-tool-loss", "registration-tool-loss", initial)
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
	connected := h.service.registeredWorker("worker-tool-loss")
	if err := h.service.replaceWorkerCapabilities(context.Background(), "worker-tool-loss", connected,
		&turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-tool-loss", RegistrationId: "registration-tool-loss",
			Capabilities: modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1),
		}); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
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

func TestCapacityLossLeavesIncompatibleJobQueuedAndAppendsNotice(t *testing.T) {
	h := newHarness(t)
	registerWorkerCapabilities(t, h, "worker-capacity-loss", "registration-capacity-loss", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 2,
	))
	session, err := h.repo.CreateSession(context.Background(), "Capacity loss")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs two", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", MinimumWorkerMaxConcurrentRuns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connected := h.service.registeredWorker("worker-capacity-loss")
	if err := h.service.replaceWorkerCapabilities(context.Background(), "worker-capacity-loss", connected,
		&turingv1.RuntimeWorkerCapabilitiesUpdated{
			WorkerId: "worker-capacity-loss", RegistrationId: "registration-capacity-loss",
			Capabilities: modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1),
		}); err != nil {
		t.Fatal(err)
	}
	eventually(t, eventuallyTimeout, func() bool {
		return hasRoutingNotice(t, h, session.SessionID, enqueued.RunID, "routing_capability_unavailable")
	})
	var status string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("job status after capacity loss = %q, want pending", status)
	}
}

func TestFirstIncompatibleRegistrationPublishesPreviouslyUnreportedLoss(t *testing.T) {
	h := newHarness(t)
	session, err := h.repo.CreateSession(context.Background(), "Restarted queue")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs Ollama", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := connectWorkerCapabilities(t, h, "worker-incompatible", "registration-incompatible", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	eventually(t, eventuallyTimeout, func() bool {
		return hasRoutingNotice(t, h, session.SessionID, enqueued.RunID, "routing_capability_unavailable")
	})
}

func TestHeartbeatExpiryPublishesLossAndRevivalRestoresQueuedRoute(t *testing.T) {
	h := newHarnessWithDispatch(t, DispatchConfig{LeaseDuration: 40 * time.Millisecond})
	worker := registerWorkerCapabilities(t, h, "worker-heartbeat", "registration-heartbeat", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	session, err := h.repo.CreateSession(context.Background(), "Heartbeat")
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
	worker.mu.Lock()
	worker.lastHeartbeat = time.Now().Add(-time.Second)
	worker.mu.Unlock()
	if err := h.service.RecoverOrphanedAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasRoutingNotice(t, h, session.SessionID, enqueued.RunID, "routing_capability_unavailable") {
		t.Fatal("heartbeat expiry did not publish a capability loss")
	}
	if err := h.service.renewWorkerLeases(context.Background(), "worker-heartbeat", worker,
		&turingv1.RuntimeHeartbeat{WorkerId: "worker-heartbeat"}); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-worker.commands:
		if assigned := command.command.GetRunAssigned(); assigned.GetRunId() != enqueued.RunID {
			t.Fatalf("revived assignment = %+v, want run %q", assigned, enqueued.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("revived worker did not receive queued assignment")
	}
	eventually(t, 15*time.Second, func() bool {
		return hasRoutingNotice(t, h, session.SessionID, enqueued.RunID, "routing_capability_restored")
	})
}

func TestDispatchDoesNotHoldWorkerLockWhileWaitingForDatabase(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-lock-order", "registration-lock-order", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	tx, err := h.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `SELECT 1`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	dispatchCtx, cancelDispatch := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancelDispatch()
	waitCount := h.database.Stats().WaitCount
	dispatchDone := make(chan error, 1)
	go func() { dispatchDone <- h.service.DispatchPending(dispatchCtx) }()
	awaitDatabaseWait(t, h.database, waitCount, func() { _ = tx.Rollback() })
	validationDone := make(chan error, 1)
	go func() {
		validationDone <- h.service.ValidateRouting(context.Background(), repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		})
	}()
	select {
	case err := <-validationDone:
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("routing validation: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		_ = tx.Rollback()
		<-dispatchDone
		t.Fatal("routing validation blocked behind a dispatcher waiting for the database")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityChangeDuringClaimRequeuesTheReservedAssignment(t *testing.T) {
	h := newHarness(t)
	stream := connectWorkerCapabilities(t, h, "worker-claim-race", "registration-claim-race", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	defer func() { _ = stream.CloseSend() }()
	session, err := h.repo.CreateSession(context.Background(), "Claim race")
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
	tx, err := h.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `SELECT 1`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	waitCount := h.database.Stats().WaitCount
	dispatchDone := make(chan error, 1)
	go func() { dispatchDone <- h.service.DispatchPending(context.Background()) }()
	awaitDatabaseWait(t, h.database, waitCount, func() { _ = tx.Rollback() })
	worker := h.service.registeredWorker("worker-claim-race")
	replacement, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	worker.mu.Lock()
	worker.capabilities = replacement
	worker.maxConcurrent = replacement.maxConcurrentRuns
	worker.mu.Unlock()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}
	var jobStatus string
	var attempt int
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, attempt FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &attempt); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "pending" {
		t.Fatalf("job status = %q, want pending after capability changed during claim", jobStatus)
	}
	if attempt != 1 {
		t.Fatalf("job attempt = %d, want 1 because capability fencing is not an execution failure", attempt)
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.pendingClaims != 0 || len(worker.assignments) != 0 || len(worker.commands) != 0 {
		t.Fatalf("worker state after rejected claim = pending:%d assignments:%d commands:%d",
			worker.pendingClaims, len(worker.assignments), len(worker.commands))
	}
}

func TestCapabilityFenceRestartsDispatchForWorkerAddedDuringClaim(t *testing.T) {
	h := newHarness(t)
	registerWorkerCapabilities(t, h, "worker-claim-fenced", "registration-claim-fenced", modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	session, err := h.repo.CreateSession(context.Background(), "Claim restart")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "route after fence", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := h.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `SELECT 1`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	waitCount := h.database.Stats().WaitCount
	dispatchDone := make(chan error, 1)
	go func() { dispatchDone <- h.service.DispatchPending(context.Background()) }()
	awaitDatabaseWait(t, h.database, waitCount, func() { _ = tx.Rollback() })

	compatible, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	replacement := &worker{
		commands:       make(chan workerCommand, 1),
		done:           make(chan struct{}),
		registrationID: "registration-claim-replacement",
		capabilities:   compatible,
		maxConcurrent:  1,
		lastHeartbeat:  time.Now().UTC(),
		assignments:    map[string]assignment{},
	}
	h.service.mu.Lock()
	h.service.workers["worker-claim-replacement"] = replacement
	h.service.mu.Unlock()
	fenced := h.service.registeredWorker("worker-claim-fenced")
	unsupported, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	fenced.mu.Lock()
	fenced.capabilities = unsupported
	fenced.mu.Unlock()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-dispatchDone; err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-replacement.commands:
		if command.command.GetRunAssigned().GetRunId() != enqueued.RunID {
			t.Fatalf("replacement assignment = %+v, want run %q", command, enqueued.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("capability-fenced claim was stranded instead of restarting dispatch")
	}
}

func TestCancellationFenceAfterClaimReleasesTerminalExecution(t *testing.T) {
	h := newHarness(t)
	capabilities, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	if err != nil {
		t.Fatal(err)
	}
	h.service.workers["worker-cancelled-claim"] = &worker{
		commands:       make(chan workerCommand, 1),
		done:           make(chan struct{}),
		registrationID: "registration-cancelled-claim",
		capabilities:   capabilities,
		maxConcurrent:  1,
		lastHeartbeat:  time.Now().UTC(),
		assignments:    map[string]assignment{},
	}
	session, err := h.repo.CreateSession(context.Background(), "Cancelled claim fence")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel after claim", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := h.database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(context.Background(), `SELECT 1`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	waitCount := h.database.Stats().WaitCount
	dispatchDone := make(chan error, 1)
	go func() { dispatchDone <- h.service.DispatchPending(context.Background()) }()
	awaitDatabaseWait(t, h.database, waitCount, func() { _ = tx.Rollback() })
	worker := h.service.registeredWorker("worker-cancelled-claim")
	worker.mu.Lock()
	workerLocked := true
	defer func() {
		if workerLocked {
			worker.mu.Unlock()
		}
	}()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	awaitJobReserved(t, h, enqueued.JobID)
	cancelRunFixture(t, h, enqueued.RunID)
	unsupported, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, "gpt-4o-mini", 8192, 1,
	))
	if err != nil {
		t.Fatal(err)
	}
	worker.capabilities = unsupported
	worker.mu.Unlock()
	workerLocked = false
	if err := <-dispatchDone; err != nil {
		t.Fatalf("terminal claim fence failed dispatch: %v", err)
	}
	eventually(t, 15*time.Second, func() bool {
		run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
		if err != nil {
			t.Fatal(err)
		}
		return run.Status == "cancelled" && !run.ExecutionActive && run.ExecutionState == "exited"
	})
}

func TestCancellationFenceWhileQueueingCommandDoesNotJoinAssignmentFence(t *testing.T) {
	h := newHarness(t)
	capabilities, _, err := decodeWorkerCapabilities(modelCapabilities(
		turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1,
	))
	if err != nil {
		t.Fatal(err)
	}
	worker := &worker{
		commands:       make(chan workerCommand, 1),
		done:           make(chan struct{}),
		registrationID: "registration-cancelled-command",
		capabilities:   capabilities,
		maxConcurrent:  1,
		lastHeartbeat:  time.Now().UTC(),
		assignments:    map[string]assignment{},
	}
	worker.commands <- workerCommand{command: &turingv1.RuntimeCommand{}}
	session, err := h.repo.CreateSession(context.Background(), "Cancelled queued command")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel while queueing", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	type dispatchResult struct {
		err error
	}
	dispatchDone := make(chan dispatchResult, 1)
	go func() {
		_, _, _, err := h.service.dispatchToWorker(ctx, "worker-cancelled-command", worker)
		dispatchDone <- dispatchResult{err: err}
	}()
	awaitJobReserved(t, h, enqueued.JobID)
	cancelRunFixture(t, h, enqueued.RunID)
	cancel()
	result := <-dispatchDone
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("dispatch error = %v, want context cancellation", result.err)
	}
	if errors.Is(result.err, repository.ErrAssignmentFenced) {
		t.Fatalf("dispatch leaked benign assignment fence: %v", result.err)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.ExecutionActive || run.ExecutionState != "exited" {
		t.Fatalf("released cancelled command = %+v, want inactive exited execution", run)
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
	eventually(t, eventuallyTimeout, func() bool {
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
		AgentIds:                    []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
		MaxConcurrentRuns:           maxConcurrentRuns,
		RemoteEgressDecisionVersion: int32(repository.RunEgressDecisionVersion),
	}
}

func TestRemoveDiscoveredToolsRestoresToolsetSnapshotWhenPersistenceFails(t *testing.T) {
	h := newHarness(t)
	const workerID = "worker-toolset-rollback"
	owner := &worker{}
	h.service.toolsets[workerID] = workerToolset{
		owner: owner,
		tools: []repository.DiscoveredTool{{
			ServerName: "files",
			ToolName:   "files.create",
			SchemaJSON: `{}`,
			Policy:     "approval_required",
		}},
	}
	if err := h.repo.UpsertTools(context.Background(), h.service.toolsets[workerID].tools); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_tool_update
		BEFORE UPDATE ON tools
		BEGIN
			SELECT RAISE(FAIL, 'update blocked');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := h.service.removeDiscoveredTools(workerID, owner); err == nil {
		t.Fatal("removeDiscoveredTools succeeded, want persistence failure")
	}
	restored, ok := h.service.toolsets[workerID]
	if !ok || restored.owner != owner || len(restored.tools) != 1 {
		t.Fatalf("restored toolset = %#v, want original snapshot", restored)
	}
}

func registerWorkerCapabilities(
	t *testing.T,
	h *harness,
	workerID string,
	registrationID string,
	capabilityProto *turingv1.WorkerCapabilities,
) *worker {
	t.Helper()
	capabilities, discovered, err := decodeWorkerCapabilities(capabilityProto)
	if err != nil {
		t.Fatal(err)
	}
	commandBuffer := capabilities.maxConcurrentRuns
	if commandBuffer < 1 {
		commandBuffer = 1
	}
	connected := &worker{
		commands:       make(chan workerCommand, commandBuffer),
		done:           make(chan struct{}),
		registrationID: registrationID,
		capabilities:   capabilities,
		maxConcurrent:  capabilities.maxConcurrentRuns,
		assignments:    map[string]assignment{},
		lastHeartbeat:  time.Now().UTC(),
	}
	if err := h.service.persistDiscoveredTools(context.Background(), workerID, connected, discovered); err != nil {
		t.Fatal(err)
	}
	h.service.mu.Lock()
	h.service.workers[workerID] = connected
	h.service.mu.Unlock()
	t.Cleanup(func() {
		connected.close()
		h.service.removeWorkerRegistration(workerID, connected)
		if err := h.service.removeDiscoveredTools(workerID, connected); err != nil {
			t.Fatal(err)
		}
	})
	return connected
}

// eventuallyTimeout bounds how long a condition may take to become true.
//
// It is deliberately generous. The loop polls every 10ms and returns the
// instant the condition holds, so a longer bound costs a passing test nothing —
// it only changes how long a genuine failure takes to report. The previous
// one-second bound was tight enough that a loaded CI runner under -race failed
// these tests on timing alone, on a different subtest each run, while the same
// tests passed locally every time. A bound that distinguishes "slow" from
// "broken" has to leave room for slow.
const eventuallyTimeout = 15 * time.Second

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

// awaitDatabaseWait blocks until a goroutine has parked on the database pool,
// observed as WaitCount rising above the caller's baseline.
//
// These tests occupy the single connection (SetMaxOpenConns(1)) with an open
// transaction, launch DispatchPending, and need it *blocked on the pool* before
// asserting anything about the locks it is or is not holding. Each carried its
// own deadline — three at two seconds, one at five — and on a loaded CI runner
// under -race the goroutine was not always scheduled that fast, failing the
// test on scheduling latency rather than the property it guards.
//
// The bound is deliberately generous, for the same reason as eventuallyTimeout:
// the loop exits the instant the count moves, so a longer bound costs a passing
// test nothing and only changes how long a genuine failure takes to report.
//
// This does not replace the fix in #117. That removed a real worker stream
// whose background goroutine contended for the same single connection, which is
// why raising a bound alone had not been enough there. Unifying the bound is
// orthogonal, and leaving one hardcoded copy beside three extracted ones is the
// shape that produced this problem twice already.
//
// onTimeout runs before the fatal so callers can roll back the transaction
// holding the connection — without it the failure leaks a busy connection into
// the rest of the test binary.
func awaitDatabaseWait(t *testing.T, pool *db.DB, baseline int64, onTimeout func()) {
	t.Helper()
	deadline := time.Now().Add(eventuallyTimeout)
	for pool.Stats().WaitCount == baseline {
		if time.Now().After(deadline) {
			if onTimeout != nil {
				onTimeout()
			}
			t.Fatal("dispatch never reached the database wait")
		}
		time.Sleep(time.Millisecond)
	}
}

// awaitJobReserved blocks until dispatch has moved a job to in_progress.
//
// Two tests poll the jobs row while a dispatch goroutine races them. One
// borrowed the database-wait deadline above; the other declared its own, at one
// second — tighter than the two-second bounds that were already failing CI.
// Both are the same wait, so both get the same bound and the same message.
func awaitJobReserved(t *testing.T, h *harness, jobID string) {
	t.Helper()
	deadline := time.Now().Add(eventuallyTimeout)
	for {
		var status string
		if err := h.database.QueryRowContext(
			context.Background(), `SELECT status FROM jobs WHERE id = ?`, jobID,
		).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "in_progress" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("dispatch did not reserve the job")
		}
		time.Sleep(time.Millisecond)
	}
}

func failRoutingNoticeInserts(t *testing.T, h *harness) {
	t.Helper()
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_pending_routing_notice
		BEFORE INSERT ON events
		WHEN NEW.type = 'agent.run.step'
			AND json_extract(NEW.payload_json, '$.reason') IN (
				'routing_capability_unavailable',
				'routing_capability_restored'
			)
		BEGIN
			SELECT RAISE(ABORT, 'routing notice unavailable');
		END
	`); err != nil {
		t.Fatal(err)
	}
}

func hasRoutingNotice(t *testing.T, h *harness, sessionID, runID, reason string) bool {
	t.Helper()
	events, _, err := h.repo.ReplayEvents(context.Background(), sessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, event := range events {
		if event.Type != "agent.run.step" || !event.RunID.Valid || event.RunID.String != runID {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil && payload["reason"] == reason {
			return true
		}
	}
	return false
}

func countRoutingNotices(t *testing.T, h *harness, runID, reason string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM events
		WHERE run_id = ?
			AND type = 'agent.run.step'
			AND json_extract(payload_json, '$.reason') = ?
	`, runID, reason).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
