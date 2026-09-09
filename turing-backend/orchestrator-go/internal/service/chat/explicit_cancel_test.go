package chat

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestExplicitCancelPublicQueuedReplay(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	sessionID := h.createSession(t)
	run, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionID, Content: "stop this exact run", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", IdempotencyKey: "send:one",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.chatClient.GetRunCancellation(ctx, &turingv1.GetRunCancellationRequest{SessionId: sessionID, RunId: run.RunID})
	if err != nil {
		t.Fatalf("public cancellation read: %v", err)
	}
	if !before.Available || before.Progress != turingv1.CancellationProgress_CANCELLATION_PROGRESS_NOT_CANCELLED {
		t.Fatalf("before = %v", before)
	}
	request := &turingv1.CancelRunRequest{SessionId: sessionID, RunId: run.RunID, IdempotencyKey: "cancel:" + run.RunID}
	got, err := h.chatClient.CancelRun(ctx, request)
	if err != nil {
		t.Fatalf("public cancel: %v", err)
	}
	if got.Result != turingv1.CancelRunResult_CANCEL_RUN_RESULT_ACCEPTED ||
		got.RunState.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED ||
		got.Progress != turingv1.CancellationProgress_CANCELLATION_PROGRESS_RECONCILED {
		t.Fatalf("cancel = %v", got)
	}
	replay, err := h.chatClient.CancelRun(ctx, request)
	if err != nil || !proto.Equal(got, replay) {
		t.Fatalf("replay = %v, %v; want %v", replay, err, got)
	}
	assertCancelledEventPublished(t, h, run.RunID, 1)
	var audits int
	if err := h.database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'run.cancel' AND target = ?`, run.RunID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("cancel audits = %d, want 1", audits)
	}
}

func TestExplicitCancelPublicUnavailableIsMetadataFree(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	session := h.createSession(t)
	run, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session, Content: "hidden", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "cancel:" + run.RunID
	if _, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: session, RunId: run.RunID, IdempotencyKey: key}); err != nil {
		t.Fatal(err)
	}
	other := h.createSession(t)
	targets := [][2]string{{"missing", "missing"}, {other, run.RunID}, {session, "missing"}}
	if _, err := h.repo.BeginSessionDeletion(ctx, session); err != nil {
		t.Fatal(err)
	}
	targets = append(targets, [2]string{session, run.RunID})
	for _, target := range targets {
		result, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: target[0], RunId: target[1], IdempotencyKey: key})
		if err != nil || !proto.Equal(result, &turingv1.CancelRunResponse{Result: turingv1.CancelRunResult_CANCEL_RUN_RESULT_UNAVAILABLE}) {
			t.Fatalf("unavailable cancellation = %v, %v", result, err)
		}
		read, err := h.chatClient.GetRunCancellation(ctx, &turingv1.GetRunCancellationRequest{SessionId: target[0], RunId: target[1]})
		if err != nil || !proto.Equal(read, &turingv1.GetRunCancellationResponse{}) {
			t.Fatalf("unavailable read = %v, %v", read, err)
		}
	}
}

func TestExplicitCancelPublicRejectsInvalidKeyWithoutTransition(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: h.createSession(t), Content: "key bounds", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "   ", strings.Repeat("x", 129)} {
		if _, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: run.SessionID, RunId: run.RunID, IdempotencyKey: key}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("key validation = %v", err)
		}
	}
	state, err := h.repo.GetRunState(ctx, run.RunID)
	if err != nil || state.Lifecycle != "queued" {
		t.Fatalf("invalid key changed run: %+v, %v", state, err)
	}
}

func TestExplicitCancelPublicKeyConflictAfterVisibleTarget(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	session := h.createSession(t)
	var runs []repository.EnqueueUserMessageResult
	for range 2 {
		run, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID: session, Content: "exact identity", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	if _, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: session, RunId: runs[0].RunID, IdempotencyKey: "operation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.chatClient.CancelRun(ctx, &turingv1.CancelRunRequest{SessionId: session, RunId: runs[1].RunID, IdempotencyKey: "operation"}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict = %v", err)
	}
	state, err := h.repo.GetRunState(ctx, runs[1].RunID)
	if err != nil || state.Lifecycle != "queued" {
		t.Fatalf("conflict cancelled wrong run: %+v, %v", state, err)
	}
}

func TestExplicitCancelPublicAlreadyTerminalPreservesOriginalResult(t *testing.T) {
	for _, lifecycle := range []string{"completed", "failed"} {
		t.Run(lifecycle, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			run := h.enqueueRunningRun(t, "already terminal public cancellation")
			running, err := h.repo.GetRunState(ctx, run.RunID)
			if err != nil {
				t.Fatal(err)
			}
			var terminal repository.RunTransitionResult
			if lifecycle == "completed" {
				terminal, err = h.repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
					RunID: run.RunID, AssistantMessageID: run.AssistantMessageID, Content: "original answer",
					ExpectedStateVersion: running.StateVersion,
				})
			} else {
				terminal, err = h.repo.FailRunCanonical(ctx, repository.FailRunInput{
					RunID: run.RunID, ExpectedStateVersion: running.StateVersion,
					Failure: runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_stream_failed", runoutcome.RetryClassNever),
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			var beforeEvents int
			if err := h.database.QueryRow(`SELECT COUNT(*) FROM events WHERE run_id = ?`, run.RunID).Scan(&beforeEvents); err != nil {
				t.Fatal(err)
			}
			want := &turingv1.CancelRunResponse{
				Result: turingv1.CancelRunResult_CANCEL_RUN_RESULT_ALREADY_TERMINAL, RunState: runstate.Project(terminal.State),
				Progress: turingv1.CancellationProgress_CANCELLATION_PROGRESS_NOT_CANCELLED,
			}
			req := &turingv1.CancelRunRequest{SessionId: run.SessionID, RunId: run.RunID, IdempotencyKey: "fresh-cancel:" + run.RunID}
			for range 2 {
				got, err := h.chatClient.CancelRun(ctx, req)
				if err != nil || !proto.Equal(got, want) {
					t.Fatalf("already-terminal response = %v, %v; want %v", got, err, want)
				}
			}
			after, err := h.repo.GetRunState(ctx, run.RunID)
			if err != nil || !proto.Equal(runstate.Project(after), want.RunState) {
				t.Fatalf("terminal result rewritten: %+v, %v", after, err)
			}
			var afterEvents, audits, receipts int
			if err := h.database.QueryRow(`SELECT COUNT(*) FROM events WHERE run_id = ?`, run.RunID).Scan(&afterEvents); err != nil {
				t.Fatal(err)
			}
			if err := h.database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'run.cancel' AND target = ?`, run.RunID).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if err := h.database.QueryRow(`SELECT COUNT(*) FROM run_cancellation_receipts WHERE run_id = ?`, run.RunID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if beforeEvents != afterEvents || audits != 0 || receipts != 1 {
				t.Fatalf("terminal replay side effects: events %d -> %d, audits %d, receipts %d", beforeEvents, afterEvents, audits, receipts)
			}
		})
	}
}
