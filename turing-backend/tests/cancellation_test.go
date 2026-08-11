package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestClientCancellationStopsRuntimeAndModel(t *testing.T) {
	harness := newGRPCHarness(t, withBlockingModel())
	defer harness.close()

	sessionID := harness.createSession(t, "cancellation")
	ctx, cancel := context.WithCancel(harness.clientContext())
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "hello",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := first.GetRunQueued().GetRunId()
	if runID == "" {
		t.Fatalf("first event = %T, want run_queued with run_id", first.GetEvent())
	}
	select {
	case <-harness.fakeModel.started:
	case <-time.After(5 * time.Second):
		t.Fatal("model request did not start")
	}
	cancel()

	select {
	case <-harness.fakeModel.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("model request was not cancelled")
	}
	run, err := harness.repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" {
		t.Fatalf("run status = %q, want cancelled", run.Status)
	}
}

func TestClientCancellationTerminalizesApprovalAndKeepsWorkerUsable(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()
	harness.filesMCP.enableCreateToolWithApprovalValidation()
	harness.fakeModel.enableModelDrivenFilesCreate()

	sessionID := harness.createSession(t, "cancel while awaiting approval")
	ctx, cancel := context.WithTimeout(harness.clientContext(), 5*time.Second)
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "create a file",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	runID, approvalID := waitForApprovalRequest(t, stream)
	cancel()

	waitForCancelledRun(t, harness, runID)
	state, err := harness.runtimeApprovals.GetApprovalForRuntime(harness.internalContext(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if state.GetStatus() == turingv1.ApprovalStatus_APPROVAL_STATUS_PENDING {
		t.Fatalf("cancelled run left approval pending: %+v", state)
	}
	assertRuntimeWorkerUsableAfterCancellation(t, harness, sessionID)
}

func TestClientCancellationStopsMCPExecutionAndKeepsWorkerUsable(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()
	harness.filesMCP.enableCreateToolWithApprovalValidation()
	harness.filesMCP.blockCreateCallUntilCancelled()
	harness.fakeModel.enableModelDrivenFilesCreate()

	sessionID := harness.createSession(t, "cancel while executing MCP")
	ctx, cancel := context.WithTimeout(harness.clientContext(), 5*time.Second)
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "create a file",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	runID, approvalID := waitForApprovalRequest(t, stream)
	if _, err := harness.approvals.ApproveApproval(harness.clientContext(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-harness.filesMCP.createStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("MCP create call did not begin")
	}
	cancel()
	select {
	case <-harness.filesMCP.createCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("MCP create call did not observe cancellation")
	}
	waitForCancelledRun(t, harness, runID)
	assertRuntimeWorkerUsableAfterCancellation(t, harness, sessionID)
}

func TestReceiveApprovalRequestStopsAtStreamDeadline(t *testing.T) {
	harness := newGRPCHarness(t, withBlockingModel())
	defer harness.close()

	sessionID := harness.createSession(t, "bounded approval receive")
	ctx, cancel := context.WithTimeout(harness.clientContext(), 250*time.Millisecond)
	defer cancel()
	stream, err := harness.chat.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId:     sessionID,
		Content:       "do not request a tool",
		ContentType:   "text",
		AgentId:       turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
		Model:         "fake-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = receiveApprovalRequest(stream)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("receiveApprovalRequest error = %v, want context deadline exceeded", err)
	}
	select {
	case <-harness.fakeModel.cancelled:
	case <-time.After(time.Second):
		t.Fatal("stream deadline did not cancel the model request")
	}
}

func waitForApprovalRequest(t *testing.T, stream turingv1.ChatService_SendMessageClient) (string, string) {
	t.Helper()
	if _, ok := stream.Context().Deadline(); !ok {
		t.Fatal("approval request stream must have a deadline")
	}
	runID, approvalID, err := receiveApprovalRequest(stream)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timed out waiting for approval request")
	}
	if err != nil {
		t.Fatal(err)
	}
	return runID, approvalID
}

func receiveApprovalRequest(stream turingv1.ChatService_SendMessageClient) (string, string, error) {
	for {
		event, err := stream.Recv()
		if err != nil {
			if contextErr := stream.Context().Err(); contextErr != nil {
				return "", "", contextErr
			}
			return "", "", err
		}
		if persisted := event.GetPersistedEvent(); persisted != nil && persisted.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED {
			approvalID := stringField(persisted.GetPayload(), "approvalId")
			if event.GetRunId() == "" || approvalID == "" {
				return "", "", fmt.Errorf("approval request = %+v, want run and approval IDs", event)
			}
			return event.GetRunId(), approvalID, nil
		}
	}
}

func waitForCancelledRun(t *testing.T, harness *grpcHarness, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := harness.repo.GetRun(context.Background(), runID)
		if err == nil && run.Status == "cancelled" && !run.ExecutionActive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, err := harness.repo.GetRun(context.Background(), runID)
	t.Fatalf("run %q did not terminalize after cancellation: run=%+v error=%v", runID, run, err)
}

func assertRuntimeWorkerUsableAfterCancellation(t *testing.T, harness *grpcHarness, sessionID string) {
	t.Helper()
	harness.fakeModel.disableModelDrivenToolCall()
	events := harness.sendMessageToCompletion(t, sessionID, "later work")
	if !hasRunCompleted(events) {
		t.Fatalf("worker did not complete later run: %+v", events)
	}
	select {
	case err := <-harness.workerDone:
		t.Fatalf("runtime worker exited after cancellation: %v", err)
	default:
	}
}
