package app

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestExplicitCancelPublicAuthenticationAndInternalIsolation(t *testing.T) {
	app := newTestApp(t)
	public := turingv1.NewChatServiceClient(newBufconnClient(t, app.PublicServer))
	internal := turingv1.NewChatServiceClient(newBufconnClient(t, app.InternalServer))
	for _, token := range []string{"", "wrong", "internal", "internal-approval-consumer"} {
		ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
		if _, err := public.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: "hidden", RunId: "hidden", IdempotencyKey: "key"}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("public cancel token %q = %v", token, err)
		}
		if _, err := public.GetRunCancellation(ctx, &turingv1.GetRunCancellationRequest{SessionId: "hidden", RunId: "hidden"}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("public read token %q = %v", token, err)
		}
	}
	for _, token := range []string{"internal", "internal-approval-consumer", "client"} {
		ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
		if _, err := internal.CancelRun(ctx, &turingv1.CancelRunRequest{}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("internal server exposed cancel: %v", err)
		}
		if _, err := internal.GetRunCancellation(ctx, &turingv1.GetRunCancellationRequest{}); status.Code(err) != codes.Unimplemented {
			t.Fatalf("internal server exposed cancellation read: %v", err)
		}
	}
}

func TestExplicitCancelAuthenticatedEndToEndLateOutputCannotRevive(t *testing.T) {
	app := newTestApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	publicCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer client"))
	internalCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer internal"))
	public := turingv1.NewChatServiceClient(newBufconnClient(t, app.PublicServer))
	session, err := app.Repository.CreateSession(ctx, "Explicit public cancel")
	if err != nil {
		t.Fatal(err)
	}
	run, err := app.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "stop me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := turingv1.NewRuntimeServiceClient(newBufconnClient(t, app.InternalServer)).ConnectWorker(internalCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "cancel-e2e", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
	}}}); err != nil {
		t.Fatal(err)
	}
	assigned := recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetRunAssigned() != nil }).GetRunAssigned()
	req := &turingv1.CancelRunRequest{SessionId: run.SessionID, RunId: run.RunID, IdempotencyKey: "cancel:" + run.RunID}
	stopped, err := public.CancelRun(publicCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Result != turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED ||
		stopped.RunState.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED ||
		stopped.Progress != turingv1.CancellationProgress_CANCELLATION_PROGRESS_STOPPING {
		t.Fatalf("stop = %v", stopped)
	}
	command := recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetRunCancelled() != nil }).GetRunCancelled()
	if command.StateVersion != stopped.RunState.StateVersion {
		t.Fatal("command did not carry committed version")
	}
	const lateDelta = "late side effect output"
	payload, err := structpb.NewStruct(map[string]any{"messageId": run.AssistantMessageID, "delta": lateDelta})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{
		RunId: run.RunID, SessionId: run.SessionID, Type: turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, Payload: payload,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: run.RunID, AssistantMessageId: run.AssistantMessageID, Content: "late completion", ExpectedStateVersion: assigned.ExpectedStateVersion,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
		RunId: run.RunID, ObservedStateVersion: command.StateVersion,
	}}}); err != nil {
		t.Fatal(err)
	}
	// A reply to the next command proves the preceding ack and late reports
	// passed the single runtime receive loop before the durable read.
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: run.RunID, ToolCallId: "cancel-sync", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system", ToolName: "system.time", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision().GetToolCallId() == "cancel-sync"
	})
	var lateEvents int
	if err := app.database.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM events
		WHERE run_id = ? AND (type = 'message.delta' OR instr(payload_json, ?) > 0)`,
		run.RunID, lateDelta).Scan(&lateEvents); err != nil {
		t.Fatal(err)
	}
	if lateEvents != 0 {
		t.Fatalf("late output persisted in %d events after cancellation", lateEvents)
	}
	replayed, err := public.CancelRun(publicCtx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replayed.RunState, stopped.RunState) || replayed.Result != stopped.Result || replayed.Progress != turingv1.CancellationProgress_CANCELLATION_PROGRESS_RECONCILED {
		t.Fatalf("receipt replay = %v, first = %v", replayed, stopped)
	}
	current, err := app.Repository.GetRun(ctx, run.RunID)
	if err != nil || current.Status != "cancelled" || current.OutcomeReason != "user_cancelled" || current.AssistantContent != "" || current.ExecutionActive {
		t.Fatalf("late update rewrote cancelled run: %+v, %v", current, err)
	}
}
