package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateModelDrivenPromptRejectsDebugShortcut(t *testing.T) {
	if err := validateModelDrivenPrompt(modelDrivenPrompt); err != nil {
		t.Fatalf("model-driven prompt is invalid: %v", err)
	}
	if err := validateModelDrivenPrompt("  /tool system.time"); err == nil {
		t.Fatal("validateModelDrivenPrompt accepted the debug shortcut")
	}
}

func TestAnalyzeModelDrivenEventsAcceptsPersistedToolLifecycle(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15.123Z."),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
	if !got.toolCalled || got.toolCallID != "call_1" || got.toolName != "system.time" {
		t.Fatalf("tool result = %+v", got)
	}
	if got.answer != "The exact tool timestamp is 2026-08-12T06:32:15.123Z." {
		t.Fatalf("answer = %q", got.answer)
	}
}

func TestAnalyzeModelDrivenEventsAcceptsHumanReadableToolTime(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		messageCompletedEvent("The current time is 06:32:15 UTC on August 12, 2026."),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
	if !got.toolCalled || got.answer == "" {
		t.Fatalf("result = %+v", got)
	}
}

func TestAnalyzeModelDrivenEventsRecoversAfterToolArgumentFailure(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`unknown argument "timezone"`,
		),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_ok", "system.time"),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
	if !got.toolCalled || got.toolCallID != "call_ok" {
		t.Fatalf("result = %+v", got)
	}
}

func TestVerifyModelDrivenAttemptsTreatsToolArgumentFailureAsInconclusive(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`unknown argument "timezone"`,
		),
		messageCompletedEvent("The tool rejected my arguments."),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationInconclusive {
		t.Fatalf("verdict = %+v", verdict)
	}
	if got := verdict.String(); !strings.Contains(got, `unknown argument`) {
		t.Fatalf("verdict text = %q", got)
	}
}

func TestVerifyModelDrivenAttemptsTreatsRuntimeToolFailureAsFailure(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			"unknown_tool",
		),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestVerifyModelDrivenAttemptsDoesNotRecoverRuntimeFailureAfterLaterSuccess(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			"tool_runner_unavailable",
		),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_ok", "system.time"),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestVerifyModelDrivenAttemptsFailsRunErrorAfterRecoveredArgumentFailure(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_ok", "system.time"),
		runFailedEvent("model_timeout", "model timed out after tool completion"),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestAnalyzeModelDrivenEventsRejectsConflictingToolTerminalStates(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_1",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); err == nil ||
		!strings.Contains(err.Error(), "conflicting terminal states") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeModelDrivenEventsRejectsFailureWithoutMatchingStart(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_ok", "system.time"),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); err == nil ||
		!strings.Contains(err.Error(), "failed without a matching start") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyModelDrivenAttemptsDoesNotPassWhenLatestCallFailed(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_ok", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationInconclusive {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestAnalyzeModelDrivenEventsTreatsUntimestampedDirectLifecycleAsUncorrelated(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		{Event: &turingv1.ChatStreamEvent_ToolCallStarted{ToolCallStarted: &turingv1.ToolEvent{
			ToolCallId: "call_1",
			ServerName: "system",
			ToolName:   "system.time",
		}}},
		{Event: &turingv1.ChatStreamEvent_ToolCallCompleted{ToolCallCompleted: &turingv1.ToolEvent{
			ToolCallId: "call_1",
			ServerName: "system",
			ToolName:   "system.time",
		}}},
		messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if !errors.Is(err, errAnswerUncorrelated) {
		t.Fatalf("analyzeModelDrivenEvents error = %v, want %v", err, errAnswerUncorrelated)
	}
	if !got.toolCalled || got.toolCallID != "call_1" || got.toolName != "system.time" {
		t.Fatalf("tool result = %+v", got)
	}
}

func TestAnalyzeModelDrivenEventsPreservesReplyWhenNoToolWasCalled(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		messageCompletedEvent("I cannot determine the current time."),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
	if got.toolCalled {
		t.Fatal("attempt unexpectedly reports a tool call")
	}
	if got.answer != "I cannot determine the current time." {
		t.Fatalf("answer = %q", got.answer)
	}
}

func TestAnalyzeModelDrivenEventsRejectsBrokenToolLifecycle(t *testing.T) {
	tests := []struct {
		name       string
		events     []*turingv1.ChatStreamEvent
		wantError  string
		toolCalled bool
	}{
		{
			name: "missing frozen payload key",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "toolCallId",
			toolCalled: false,
		},
		{
			name: "missing frozen tool name",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", ""),
				runCompletedEvent(),
			},
			wantError:  "toolName",
			toolCalled: false,
		},
		{
			name: "completion without start",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				runCompletedEvent(),
			},
			wantError:  "without a matching start",
			toolCalled: true,
		},
		{
			name: "completion precedes start",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				runCompletedEvent(),
			},
			wantError:  "completed before it started",
			toolCalled: true,
		},
		{
			name: "failure precedes start",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEventWithError(
					t,
					turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
					"call_1",
					"system.time",
					`invalid params: unknown argument "timezone"`,
				),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "failed before it started",
			toolCalled: true,
		},
		{
			name: "completion does not match start",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_2", "system.time"),
				messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				runCompletedEvent(),
			},
			wantError:  "without a matching start",
			toolCalled: true,
		},
		{
			name: "empty final answer",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "no final answer",
			toolCalled: true,
		},
		{
			name: "final answer precedes completion",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "after tool completion",
			toolCalled: true,
		},
		{
			name: "token deltas are not a final answer",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				tokenDeltaEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				runCompletedEvent(),
			},
			wantError:  "no final answer",
			toolCalled: true,
		},
		{
			name: "final answer omits exact tool timestamp",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				messageCompletedEvent("It is afternoon in UTC."),
				runCompletedEvent(),
			},
			wantError:  "reflect the timestamp",
			toolCalled: true,
		},
		{
			name: "target tool failed",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "failed",
			toolCalled: true,
		},
		{
			name: "target tool never terminates",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "did not reach a terminal tool event",
			toolCalled: true,
		},
		{
			name: "target tool denied",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED, "call_1", "system.time"),
				runCompletedEvent(),
			},
			wantError:  "was denied",
			toolCalled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := analyzeModelDrivenEvents(test.events)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
			if got.toolCalled != test.toolCalled {
				t.Fatalf("toolCalled = %v, want %v", got.toolCalled, test.toolCalled)
			}
		})
	}
}

func TestAnalyzeModelDrivenEventsRejectsDuplicateFailures(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			"tool_runner_unavailable",
		),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); err == nil ||
		!strings.Contains(err.Error(), "duplicate failure") {
		t.Fatalf("error = %v", err)
	}
}

func TestAnalyzeModelDrivenEventsTreatsOtherToolsAsInconclusive(t *testing.T) {
	tests := []struct {
		name   string
		events []*turingv1.ChatStreamEvent
	}{
		{
			name: "target approval event without execution",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED, "call_1", "system.time"),
				messageCompletedEvent("The exact tool timestamp is 2026-08-12T06:32:15Z."),
				runCompletedEvent(),
			},
		},
		{
			name: "approval gated",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "files.create"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED, "call_1", "files.create"),
				runFailedEvent("approval_expired", "approval expired"),
			},
		},
		{
			name: "denied",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED, "call_1", "files.delete"),
				runCompletedEvent(),
			},
		},
		{
			name: "failed",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.echo"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED, "call_1", "system.echo"),
				messageCompletedEvent("The echo tool rejected those arguments."),
				runCompletedEvent(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := analyzeModelDrivenEvents(test.events)
			if err != nil {
				t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
			}
			if got.toolCalled {
				t.Fatalf("non-target tool counted as successful target call: %+v", got)
			}
			if len(got.observedTools) == 0 {
				t.Fatalf("other tool lifecycle was not recorded: %+v", got)
			}
		})
	}
}

func TestVerifyModelDrivenAttemptsFailsFrozenPayloadContractViolation(t *testing.T) {
	brokenEvents := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "", "system.time"),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 3, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(brokenEvents)
	})

	if verdict.status != verificationFail || verdict.exitCode() != 1 {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestTimestampCorrelationAcceptsToolExecutionWindow(t *testing.T) {
	startedAt := time.Date(2026, time.August, 12, 6, 31, 45, 0, time.UTC)
	completedAt := time.Date(2026, time.August, 12, 6, 32, 15, 0, time.UTC)
	base := []*turingv1.ChatStreamEvent{
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time", startedAt),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time", completedAt),
	}

	passing := append(append([]*turingv1.ChatStreamEvent{}, base...),
		messageCompletedEvent("The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	)
	if _, err := analyzeModelDrivenEvents(passing); err != nil {
		t.Fatalf("completion-correlated answer failed: %v", err)
	}

	startCorrelated := append(append([]*turingv1.ChatStreamEvent{}, base...),
		messageCompletedEvent("The current time is 06:31:45 UTC."),
		runCompletedEvent(),
	)
	if _, err := analyzeModelDrivenEvents(startCorrelated); err != nil {
		t.Fatalf("start-correlated answer failed: %v", err)
	}

	outsideWindow := append(append([]*turingv1.ChatStreamEvent{}, base...),
		messageCompletedEvent("The current time is 06:31:30 UTC."),
		runCompletedEvent(),
	)
	if _, err := analyzeModelDrivenEvents(outsideWindow); !errors.Is(err, errAnswerUncorrelated) {
		t.Fatalf("outside-window answer error = %v, want %v", err, errAnswerUncorrelated)
	}
}

func TestAnalyzeModelDrivenEventsIgnoresPreToolTimestamp(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		tokenDeltaEvent("The current time is 06:32:15 UTC."),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		tokenDeltaEvent("I checked the tool."),
		messageCompletedEvent("The current time is 06:32:15 UTC. I checked the tool."),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); !errors.Is(err, errAnswerUncorrelated) {
		t.Fatalf("error = %v, want %v", err, errAnswerUncorrelated)
	}
}

func TestVerifyModelDrivenAttemptsFailsWhenNoContentFollowsTool(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		tokenDeltaEvent("Let me check."),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		messageCompletedEvent("Let me check."),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
	if errors.Is(verdict.cause, errAnswerUncorrelated) {
		t.Fatalf("empty post-tool answer was treated as uncorrelated: %v", verdict.cause)
	}
}

func TestAnalyzeModelDrivenEventsCorrelatesPostToolDelta(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		tokenDeltaEvent("Let me check. "),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		tokenDeltaEvent("The current time is 06:32:15 UTC."),
		messageCompletedEvent("Let me check. The current time is 06:32:15 UTC."),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
}

func TestAnalyzeModelDrivenEventsCorrelatesDeltaAfterLastCompletion(t *testing.T) {
	firstAt := time.Date(2026, time.August, 12, 6, 32, 15, 0, time.UTC)
	secondAt := firstAt.Add(10 * time.Second)
	events := []*turingv1.ChatStreamEvent{
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time", firstAt),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time", firstAt),
		tokenDeltaEvent("The first result was 06:32:15 UTC. "),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_2", "system.time", secondAt),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_2", "system.time", secondAt),
		tokenDeltaEvent("The latest result is 06:32:25 UTC."),
		messageCompletedEvent(
			"The first result was 06:32:15 UTC. The latest result is 06:32:25 UTC.",
		),
		runCompletedEvent(),
	}

	got, err := analyzeModelDrivenEvents(events)
	if err != nil {
		t.Fatalf("analyzeModelDrivenEvents returned an error: %v", err)
	}
	if got.toolCallID != "call_1" {
		t.Fatalf("reported toolCallID = %q, want first successful call", got.toolCallID)
	}
}

func TestAnalyzeModelDrivenEventsDiscardsDeltaBeforeLastCompletion(t *testing.T) {
	firstAt := time.Date(2026, time.August, 12, 6, 32, 15, 0, time.UTC)
	secondAt := firstAt.Add(time.Second)
	events := []*turingv1.ChatStreamEvent{
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time", firstAt),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time", firstAt),
		tokenDeltaEvent("The first result was 06:32:15 UTC. "),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_2", "system.time", secondAt),
		persistedToolEventAt(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_2", "system.time", secondAt),
		tokenDeltaEvent("The second check is complete."),
		messageCompletedEvent(
			"The first result was 06:32:15 UTC. The second check is complete.",
		),
		runCompletedEvent(),
	}

	if _, err := analyzeModelDrivenEvents(events); !errors.Is(err, errAnswerUncorrelated) {
		t.Fatalf("error = %v, want %v", err, errAnswerUncorrelated)
	}
}

func TestTimestampCorrelationAcceptsUnixAndTwelveHourFormats(t *testing.T) {
	startedAt := time.Date(2026, time.August, 12, 8, 21, 10, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	tests := []string{
		fmt.Sprintf("The Unix timestamp is %d.", completedAt.Unix()),
		fmt.Sprintf("The Unix timestamp in milliseconds is %d.", completedAt.UnixMilli()),
		fmt.Sprintf(`The tool returned {"unixMs":%d}.`, completedAt.UnixMilli()),
		fmt.Sprintf(`The tool returned {"iso":"%s"}.`, completedAt.Format(time.RFC3339Nano)),
		"The current time is 8:21 AM UTC.",
		"The current time is 8:21:12 AM UTC.",
		"The current time is 08:21 UTC.",
		"The current time is 8:21:12 UTC.",
	}
	for _, answer := range tests {
		if !answerMatchesToolTime(answer, startedAt, completedAt) {
			t.Errorf("answer %q did not correlate", answer)
		}
	}
	afternoonStart := time.Date(2026, time.August, 12, 14, 32, 10, 0, time.UTC)
	if !answerMatchesToolTime("It is 14:32 UTC.", afternoonStart, afternoonStart.Add(time.Second)) {
		t.Error("documented 24-hour minute format did not correlate")
	}
}

func TestVerifyModelDrivenAttemptsTreatsModelGuardrailsAsInconclusive(t *testing.T) {
	for _, code := range []string{
		"tool_call_limit_exceeded",
		"tool_result_limit_exceeded",
		"model_output_limit_exceeded",
		"model_stream_failed",
		"model_stream_error",
		"model_unavailable",
		"model_request_failed",
		"model_auth_failed",
		"model_bad_chunk",
		"model_error",
		"model_quota_exceeded",
	} {
		t.Run(code, func(t *testing.T) {
			events := []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
				runFailedEvent(code, "model guardrail stopped the run"),
			}
			verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
				return analyzeModelDrivenEvents(events)
			})

			if verdict.status != verificationInconclusive {
				t.Fatalf("verdict = %+v", verdict)
			}
			if !strings.Contains(verdict.String(), code) {
				t.Fatalf("verdict text = %q", verdict.String())
			}
		})
	}
}

func TestVerifyModelDrivenAttemptsFailsTimeoutAfterRecoverableToolFailure(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_bad", "system.time"),
		persistedToolEventWithError(
			t,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
			"call_bad",
			"system.time",
			`invalid params: unknown argument "timezone"`,
		),
		runFailedEvent("model_timeout", "model timed out before recovering"),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestAnalyzeModelDrivenEventsRejectsNonTerminalStream(t *testing.T) {
	got, err := analyzeModelDrivenEvents([]*turingv1.ChatStreamEvent{
		messageCompletedEvent("No tool was called."),
	})
	if err == nil || !strings.Contains(err.Error(), "terminal run event") {
		t.Fatalf("result = %+v, error = %v", got, err)
	}
}

func TestVerifyModelDrivenAttemptsStopsAfterPass(t *testing.T) {
	var calls int
	verdict := verifyModelDrivenAttempts("llama3.2", 3, func(attempt int) (modelDrivenAttempt, error) {
		calls++
		if attempt == 1 {
			return modelDrivenAttempt{answer: "I cannot determine the time."}, nil
		}
		return modelDrivenAttempt{
			toolCalled: true,
			toolCallID: "call_1",
			toolName:   "system.time",
			answer:     "The exact tool timestamp is 2026-08-12T06:32:15Z.",
		}, nil
	})

	if verdict.status != verificationPass || verdict.attempt != 2 || calls != 2 {
		t.Fatalf("verdict = %+v, calls = %d", verdict, calls)
	}
	if got := verdict.String(); !strings.Contains(got, "PASS: model=llama3.2 chose system.time on attempt 2/3") {
		t.Fatalf("verdict text = %q", got)
	}
}

func TestVerifyModelDrivenAttemptsReportsInconclusiveWithReplies(t *testing.T) {
	verdict := verifyModelDrivenAttempts("llama3.2", 2, func(attempt int) (modelDrivenAttempt, error) {
		return modelDrivenAttempt{answer: "reply " + string(rune('0'+attempt))}, nil
	})

	if verdict.status != verificationInconclusive {
		t.Fatalf("status = %v", verdict.status)
	}
	got := verdict.String()
	for _, want := range []string{
		"INCONCLUSIVE: model=llama3.2 did not complete the verified system.time loop in 2 attempts",
		`attempt 1 answer: "reply 1"`,
		`attempt 2 answer: "reply 2"`,
		"TURING_VERIFY_MODEL=<a stronger tool-calling model>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("verdict text %q does not contain %q", got, want)
		}
	}
}

func TestVerifyModelDrivenAttemptsReportsFailureImmediately(t *testing.T) {
	wantErr := errors.New("tool call_1 did not complete")
	verdict := verifyModelDrivenAttempts("llama3.2", 3, func(int) (modelDrivenAttempt, error) {
		return modelDrivenAttempt{toolCalled: true, toolName: "system.time"}, wantErr
	})

	if verdict.status != verificationFail || !errors.Is(verdict.cause, wantErr) {
		t.Fatalf("verdict = %+v", verdict)
	}
	if got := verdict.String(); !strings.Contains(got, "FAIL: model=llama3.2 called system.time but tool call_1 did not complete") {
		t.Fatalf("verdict text = %q", got)
	}
}

func TestVerifyModelDrivenAttemptsRetriesPreToolErrorsAsInconclusive(t *testing.T) {
	var calls int
	verdict := verifyModelDrivenAttempts("missing-model", 2, func(int) (modelDrivenAttempt, error) {
		calls++
		return modelDrivenAttempt{}, errors.New("run failed: model not found")
	})

	if verdict.status != verificationInconclusive || calls != 2 {
		t.Fatalf("verdict = %+v, calls = %d", verdict, calls)
	}
	if got := verdict.String(); !strings.Contains(got, "attempt 1 error") {
		t.Fatalf("verdict text = %q", got)
	}
}

func TestVerifyModelDrivenAttemptsTreatsDeadlineAfterToolStartAsFailure(t *testing.T) {
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return modelDrivenAttempt{toolCalled: true, toolName: "system.time"},
			fmt.Errorf("receive stream: %w", status.Error(codes.DeadlineExceeded, "context deadline exceeded"))
	})

	if verdict.status != verificationFail {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestVerifyModelDrivenAttemptsTreatsPreToolDeadlineAsInconclusive(t *testing.T) {
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return modelDrivenAttempt{},
			fmt.Errorf("receive stream: %w", status.Error(codes.DeadlineExceeded, "context deadline exceeded"))
	})

	if verdict.status != verificationInconclusive {
		t.Fatalf("verdict = %+v", verdict)
	}
}

func TestReadModelDrivenStreamPreservesToolStateOnReceiveError(t *testing.T) {
	tests := []struct {
		name           string
		events         []*turingv1.ChatStreamEvent
		wantToolCalled bool
		wantStatus     verificationStatus
	}{
		{
			name: "after target tool start",
			events: []*turingv1.ChatStreamEvent{
				persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
			},
			wantToolCalled: true,
			wantStatus:     verificationFail,
		},
		{
			name:           "before target tool start",
			wantToolCalled: false,
			wantStatus:     verificationInconclusive,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &recordingClientConn{
				events:     test.events,
				receiveErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			}
			stream, err := turingv1.NewChatServiceClient(conn).SendMessage(
				context.Background(),
				&turingv1.SendMessageRequest{SessionId: "sess_1", Content: modelDrivenPrompt},
			)
			if err != nil {
				t.Fatal(err)
			}

			attempt, receiveErr := readModelDrivenStream(stream)
			if status.Code(receiveErr) != codes.DeadlineExceeded {
				t.Fatalf("receive error = %v", receiveErr)
			}
			if attempt.toolCalled != test.wantToolCalled {
				t.Fatalf("toolCalled = %v, want %v", attempt.toolCalled, test.wantToolCalled)
			}
			verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
				return attempt, receiveErr
			})
			if verdict.status != test.wantStatus {
				t.Fatalf("verdict = %+v", verdict)
			}
		})
	}
}

func TestVerifyModelDrivenAttemptsTreatsUncorrelatedAnswerAsInconclusive(t *testing.T) {
	events := []*turingv1.ChatStreamEvent{
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
		persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
		messageCompletedEvent("It is late tonight."),
		runCompletedEvent(),
	}
	verdict := verifyModelDrivenAttempts("llama3.2", 1, func(int) (modelDrivenAttempt, error) {
		return analyzeModelDrivenEvents(events)
	})

	if verdict.status != verificationInconclusive {
		t.Fatalf("verdict = %+v", verdict)
	}
	if got := verdict.String(); !strings.Contains(got, "It is late tonight.") {
		t.Fatalf("verdict text = %q", got)
	}
}

func TestRunModelDrivenAttemptSendsNaturalLanguageModelRequest(t *testing.T) {
	conn := &recordingClientConn{
		events: []*turingv1.ChatStreamEvent{
			persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, "call_1", "system.time"),
			persistedToolEvent(t, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED, "call_1", "system.time"),
			messageCompletedEvent("The current time is 06:32:15 UTC."),
			runCompletedEvent(),
		},
	}
	client := smokeClient{
		token:    "token",
		sessions: turingv1.NewSessionServiceClient(conn),
		chat:     turingv1.NewChatServiceClient(conn),
	}

	attempt, err := client.runModelDrivenAttempt(context.Background(), "verify-model", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !attempt.toolCalled {
		t.Fatalf("attempt = %+v", attempt)
	}
	request := conn.request
	if request == nil {
		t.Fatal("SendMessage request was not captured")
	}
	if request.GetContent() != modelDrivenPrompt || strings.HasPrefix(strings.TrimSpace(request.GetContent()), "/tool") {
		t.Fatalf("content = %q", request.GetContent())
	}
	if request.GetModel() != "verify-model" {
		t.Fatalf("model = %q", request.GetModel())
	}
	if request.GetModelProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA {
		t.Fatalf("provider = %s", request.GetModelProvider())
	}
	if conn.sessionTitle != "Live tool-loop verification 2" {
		t.Fatalf("session title = %q", conn.sessionTitle)
	}
}

func TestSmokeExitCodeDistinguishesVerificationOutcomes(t *testing.T) {
	if got := smokeExitCode(nil); got != 0 {
		t.Fatalf("success exit code = %d", got)
	}
	if got := smokeExitCode(errors.New("broken")); got != 1 {
		t.Fatalf("failure exit code = %d", got)
	}
	if got := smokeExitCode(&smokeExitError{code: 2, message: "inconclusive"}); got != 2 {
		t.Fatalf("inconclusive exit code = %d", got)
	}
}

func TestModelDrivenVerdictExitCodes(t *testing.T) {
	tests := []struct {
		status verificationStatus
		want   int
	}{
		{status: verificationPass, want: 0},
		{status: verificationFail, want: 1},
		{status: verificationInconclusive, want: 2},
	}
	for _, test := range tests {
		if got := (modelDrivenVerdict{status: test.status}).exitCode(); got != test.want {
			t.Fatalf("status %d exit code = %d, want %d", test.status, got, test.want)
		}
	}
}

func TestRunRejectsInvalidModelDrivenFlagsBeforeLoadingConfig(t *testing.T) {
	for _, args := range [][]string{
		{"-health-only", "-model-driven"},
		{"-model-driven", "-attempts", "0"},
	} {
		if err := run(args); err == nil {
			t.Fatalf("run(%v) succeeded", args)
		}
	}
}

func TestRunReportsModelDrivenConfigFailureAsInconclusive(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TURING_CLIENT_API_KEY", "")

	runErr := run([]string{"-model-driven"})
	if got := smokeExitCode(runErr); got != 2 {
		t.Fatalf("exit code = %d, error = %v", got, runErr)
	}
}

func TestLoadConfigUsesVerificationModelPrecedence(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(workingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, ".env"), []byte(
		"TURING_CLIENT_API_KEY=token\nOLLAMA_MODEL=env-model\nTURING_JOB_TIMEOUT_MS=400000\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TURING_CLIENT_API_KEY", "")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("TURING_VERIFY_MODEL", "")
	t.Setenv("TURING_JOB_TIMEOUT_MS", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.model != "env-model" {
		t.Fatalf("model = %q, want env-model", cfg.model)
	}
	if cfg.attemptTimeout != 430*time.Second {
		t.Fatalf("attempt timeout = %s, want 430s", cfg.attemptTimeout)
	}

	t.Setenv("TURING_VERIFY_MODEL", "verify-model")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.model != "verify-model" {
		t.Fatalf("model = %q, want verify-model", cfg.model)
	}
}

func persistedToolEvent(t *testing.T, eventType turingv1.TuringEventType, toolCallID, toolName string) *turingv1.ChatStreamEvent {
	t.Helper()
	return persistedToolEventAt(
		t,
		eventType,
		toolCallID,
		toolName,
		time.Date(2026, time.August, 12, 6, 32, 15, 500_000_000, time.UTC),
	)
}

func persistedToolEventAt(
	t *testing.T,
	eventType turingv1.TuringEventType,
	toolCallID string,
	toolName string,
	observedAt time.Time,
) *turingv1.ChatStreamEvent {
	t.Helper()
	payload, err := structpb.NewStruct(map[string]any{
		"toolCallId": toolCallID,
		"toolName":   toolName,
		"serverName": "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &turingv1.ChatStreamEvent{
		Event: &turingv1.ChatStreamEvent_PersistedEvent{
			PersistedEvent: &turingv1.TuringEvent{
				Type:      eventType,
				Payload:   payload,
				CreatedAt: timestamppb.New(observedAt),
			},
		},
	}
}

func persistedToolEventWithError(
	t *testing.T,
	eventType turingv1.TuringEventType,
	toolCallID string,
	toolName string,
	errorMessage string,
) *turingv1.ChatStreamEvent {
	t.Helper()
	event := persistedToolEvent(t, eventType, toolCallID, toolName)
	event.GetPersistedEvent().Payload.Fields["error"] = structpb.NewStringValue(errorMessage)
	return event
}

type recordingClientConn struct {
	request      *turingv1.SendMessageRequest
	sessionTitle string
	events       []*turingv1.ChatStreamEvent
	receiveErr   error
}

func (c *recordingClientConn) Invoke(
	_ context.Context,
	method string,
	args any,
	reply any,
	_ ...grpc.CallOption,
) error {
	if method != turingv1.SessionService_CreateSession_FullMethodName {
		return status.Errorf(codes.Unimplemented, "unexpected method %s", method)
	}
	request := args.(*turingv1.CreateSessionRequest)
	c.sessionTitle = request.GetTitle()
	response := reply.(*turingv1.CreateSessionResponse)
	response.SessionId = "sess_1"
	return nil
}

func (c *recordingClientConn) NewStream(
	ctx context.Context,
	_ *grpc.StreamDesc,
	method string,
	_ ...grpc.CallOption,
) (grpc.ClientStream, error) {
	if method != turingv1.ChatService_SendMessage_FullMethodName {
		return nil, status.Errorf(codes.Unimplemented, "unexpected stream %s", method)
	}
	return &recordingClientStream{ctx: ctx, conn: c}, nil
}

type recordingClientStream struct {
	ctx   context.Context
	conn  *recordingClientConn
	index int
}

func (s *recordingClientStream) Header() (metadata.MD, error) { return nil, nil }
func (s *recordingClientStream) Trailer() metadata.MD         { return nil }
func (s *recordingClientStream) CloseSend() error             { return nil }
func (s *recordingClientStream) Context() context.Context     { return s.ctx }
func (s *recordingClientStream) SendMsg(message any) error {
	request := message.(*turingv1.SendMessageRequest)
	s.conn.request = proto.Clone(request).(*turingv1.SendMessageRequest)
	return nil
}
func (s *recordingClientStream) RecvMsg(message any) error {
	if s.index >= len(s.conn.events) {
		if s.conn.receiveErr != nil {
			return s.conn.receiveErr
		}
		return io.EOF
	}
	proto.Merge(message.(proto.Message), s.conn.events[s.index])
	s.index++
	return nil
}

func messageCompletedEvent(content string) *turingv1.ChatStreamEvent {
	return &turingv1.ChatStreamEvent{
		Event: &turingv1.ChatStreamEvent_MessageCompleted{
			MessageCompleted: &turingv1.MessageCompleted{Content: content},
		},
	}
}

func tokenDeltaEvent(content string) *turingv1.ChatStreamEvent {
	return &turingv1.ChatStreamEvent{
		Event: &turingv1.ChatStreamEvent_TokenDelta{
			TokenDelta: &turingv1.TokenDelta{Delta: content},
		},
	}
}

func runFailedEvent(code, message string) *turingv1.ChatStreamEvent {
	return &turingv1.ChatStreamEvent{
		Event: &turingv1.ChatStreamEvent_RunFailed{
			RunFailed: &turingv1.RunFailed{RunId: "run_1", Code: code, Message: message},
		},
	}
}

func runCompletedEvent() *turingv1.ChatStreamEvent {
	return &turingv1.ChatStreamEvent{
		Event: &turingv1.ChatStreamEvent_RunCompleted{
			RunCompleted: &turingv1.RunCompleted{RunId: "run_1"},
		},
	}
}
