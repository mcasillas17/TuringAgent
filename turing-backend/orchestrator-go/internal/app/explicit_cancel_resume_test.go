package app

import (
	"context"
	"database/sql"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

func TestPublicCancelLosingApprovalReadyDoesNotDisconnectOtherRun(t *testing.T) {
	for _, scenario := range []string{"approved", "consumed", "duplicate-ready"} {
		t.Run(scenario, func(t *testing.T) {
			testPublicCancelLosingApprovalReady(t, scenario)
		})
	}
}

func testPublicCancelLosingApprovalReady(t *testing.T, scenario string) {
	app, err := New(config.Config{
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret", DatabasePath: t.TempDir() + "/turing.db",
		OllamaModel: "llama3.2", MaxConcurrentRunsGeneral: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	publicCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer client"))
	internalCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer internal"))
	chat := turingv1.NewChatServiceClient(newBufconnClient(t, app.PublicServer))
	var runs []repository.EnqueueUserMessageResult
	for _, title := range []string{"stop waiting run", "unrelated running session"} {
		session, err := app.Repository.CreateSession(ctx, title)
		if err != nil {
			t.Fatal(err)
		}
		run, err := app.Repository.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: title, AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	worker, err := turingv1.NewRuntimeServiceClient(newBufconnClient(t, app.InternalServer)).ConnectWorker(internalCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
		WorkerId: "two-run-cancel-worker", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 2,
	}}}); err != nil {
		t.Fatal(err)
	}
	jobs := make(map[string]*turingv1.AgentJob)
	for len(jobs) < 2 {
		job := recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetRunAssigned() != nil }).GetRunAssigned()
		jobs[job.RunId] = job
	}
	stopping, other := runs[0], runs[1]
	approval, _, err := app.Repository.CreateApprovalWithEvent(ctx, stopping.RunID, "", "general_assistant",
		"files.create", `{"path":"notes.txt"}`, "sha256:cancel-resume", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Repository.ApproveApproval(ctx, approval.ApprovalID, "approved-unconsumed-token", sql.NullString{}, ""); err != nil {
		t.Fatal(err)
	}
	waiting, err := app.Repository.GetRunState(ctx, stopping.RunID)
	if err != nil {
		t.Fatal(err)
	}
	ready := &turingv1.RuntimeApprovalResumeReady{
		RunId: stopping.RunID, ApprovalId: approval.ApprovalID, AssignmentAttemptId: jobs[stopping.RunID].AssignmentAttemptId,
		ExpectedStateVersion: waiting.StateVersion,
	}
	if scenario == "consumed" {
		if _, err := app.Repository.ConsumeApproval(ctx, approval.ApprovalID, ""); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "duplicate-ready" {
		if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ApprovalResumeReady{ApprovalResumeReady: ready}}); err != nil {
			t.Fatal(err)
		}
		accepted := recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool {
			return cmd.GetApprovalResumeAccepted() != nil
		}).GetApprovalResumeAccepted()
		if accepted.StateVersion != waiting.StateVersion+1 {
			t.Fatalf("initial resume = %v", accepted)
		}
	}
	req := &turingv1.CancelRunRequest{SessionId: stopping.SessionID, RunId: stopping.RunID, IdempotencyKey: "cancel:" + stopping.RunID}
	stopped, err := chat.CancelRun(publicCtx, req)
	if err != nil || stopped.GetResult() != turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED {
		t.Fatalf("public Stop = %v, %v", stopped, err)
	}
	recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool { return cmd.GetRunCancelled() != nil })
	for range 2 {
		if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ApprovalResumeReady{ApprovalResumeReady: ready}}); err != nil {
			t.Fatal(err)
		}
		// The response to the losing Ready is another stop, never permission to
		// resume. Receiving it is the barrier before inspecting either run.
		for {
			command, err := worker.Recv()
			if err != nil {
				t.Fatalf("valid in-flight Ready disconnected shared worker: %v", err)
			}
			if command.GetApprovalResumeAccepted() != nil {
				t.Fatal("cancelled approval was resumed")
			}
			if command.GetRunCancelled() != nil {
				if command.GetRunCancelled().RunId != stopping.RunID || command.GetRunCancelled().StateVersion != stopped.RunState.StateVersion {
					t.Fatalf("wrong cancellation redelivery: %v", command)
				}
				break
			}
		}
		current, err := app.Repository.GetRun(ctx, stopping.RunID)
		if err != nil || !current.ExecutionActive || current.StateVersion != stopped.RunState.StateVersion {
			t.Fatalf("Ready released cancelled containment: %+v, %v", current, err)
		}
		unrelated, err := app.Repository.GetRun(ctx, other.RunID)
		if err != nil || unrelated.Status != "running" || !unrelated.ExecutionActive {
			t.Fatalf("Stop affected unrelated run: %+v, %v", unrelated, err)
		}
	}
	approvalAfter, err := app.Repository.GetApproval(ctx, approval.ApprovalID)
	wantStatus, wantToken := "expired", ""
	if scenario == "consumed" {
		wantStatus, wantToken = "consumed", "approved-unconsumed-token"
	}
	if err != nil || approvalAfter.Status != wantStatus || approvalAfter.ApprovalToken != wantToken {
		t.Fatalf("approval authorization/provenance changed: %+v, %v", approvalAfter, err)
	}
	if _, err := app.Repository.ConsumeApproval(ctx, approval.ApprovalID, ""); err == nil {
		t.Fatal("cancelled authorization could be consumed")
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{
		RunId: stopping.RunID, ObservedStateVersion: stopped.RunState.StateVersion,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: other.RunID, AssistantMessageId: other.AssistantMessageID, Content: "other session finished",
		ExpectedStateVersion: jobs[other.RunID].ExpectedStateVersion,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: &turingv1.ToolCallBeacon{
		RunId: stopping.RunID, ToolCallId: "two-run-stop-sync", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: "system", ToolName: "system.time", Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, worker, func(cmd *turingv1.RuntimeCommand) bool {
		return cmd.GetToolPolicyDecision().GetToolCallId() == "two-run-stop-sync"
	})
	current, err := chat.GetRunCancellation(publicCtx, &turingv1.GetRunCancellationRequest{SessionId: stopping.SessionID, RunId: stopping.RunID})
	if err != nil || current.Progress != turingv1.CancellationProgress_CANCELLATION_PROGRESS_RECONCILED || !proto.Equal(current.RunState, stopped.RunState) {
		t.Fatalf("matching exit ack = %v, %v", current, err)
	}
	unrelated, err := app.Repository.GetRun(ctx, other.RunID)
	if err != nil || unrelated.Status != "completed" || unrelated.AssistantContent != "other session finished" {
		t.Fatalf("other run could not finish on original registration: %+v, %v", unrelated, err)
	}
}
