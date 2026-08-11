package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestNewToolCallIDIsUniqueAcrossConcurrentSamples(t *testing.T) {
	const sampleSize = 512
	ids := make(chan string, sampleSize)
	var group sync.WaitGroup
	for range sampleSize {
		group.Add(1)
		go func() {
			defer group.Done()
			ids <- NewToolCallID()
		}()
	}
	group.Wait()
	close(ids)

	seen := make(map[string]struct{}, sampleSize)
	for id := range ids {
		if !strings.HasPrefix(id, "call_") {
			t.Fatalf("NewToolCallID() = %q, want call_ prefix", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("NewToolCallID() returned duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestRunTreatsTerminalPolicyDecisionAsTerminalRun(t *testing.T) {
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		return &turingv1.ToolPolicyDecision{
			Decision:    turingv1.ToolPolicyDecision_DECISION_DENY,
			ToolCallId:  beacon.GetToolCallId(),
			Reason:      "approval_delivery_failed",
			TerminalRun: true,
		}, nil
	}}

	_, err := runner.RunWithOutcome(context.Background(), RunInput{ToolName: "files.create"})

	if !RunWasTerminalized(err) {
		t.Fatalf("RunWithOutcome error = %T %v, want terminal run error", err, err)
	}
}

func TestRunRejectsUnserializableArgumentsBeforeBeaconOrExecution(t *testing.T) {
	beaconPosted := false
	toolCalled := false
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beaconPosted = true
		return allowToolDecision(context.Background(), beacon)
	}}

	_, err := runner.Run(context.Background(), RunInput{
		ToolName: "system.echo",
		Args:     map[string]any{"value": math.Inf(1)},
		MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
			toolCalled = true
			return map[string]any{"ok": true}, nil
		}),
	})

	if err == nil || !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("Run error = %v, want argument serialization failure", err)
	}
	if beaconPosted {
		t.Fatal("invalid arguments were posted in a policy beacon")
	}
	if toolCalled {
		t.Fatal("tool executed with arguments omitted from the policy beacon")
	}
}

func TestRunGeneratesFreshBeaconIDForEveryExecutionAttempt(t *testing.T) {
	var attemptIDs []string
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			attemptIDs = append(attemptIDs, beacon.GetToolCallId())
		}
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	input := RunInput{
		ToolName:  "system.echo",
		MCPClient: fakeMCPClient{},
	}
	if _, err := runner.Run(context.Background(), input); err != nil {
		t.Fatalf("first Run error: %v", err)
	}
	if _, err := runner.Run(context.Background(), input); err != nil {
		t.Fatalf("second Run error: %v", err)
	}

	if len(attemptIDs) != 2 {
		t.Fatalf("attempt IDs = %v, want two", attemptIDs)
	}
	if attemptIDs[0] == "" || attemptIDs[1] == "" {
		t.Fatalf("attempt IDs = %v, want generated beacon IDs", attemptIDs)
	}
	if attemptIDs[0] == attemptIDs[1] {
		t.Fatalf("retry reused beacon ID %q", attemptIDs[0])
	}
}

func TestRunGeneratesConsistentToolCallIDWhenNotSupplied(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	_, err := runner.Run(context.Background(), RunInput{
		ModelToolCallID: "provider_call_1",
		ToolName:        "system.echo",
		MCPClient:       fakeMCPClient{},
	})

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(beacons) != 2 {
		t.Fatalf("beacons = %d, want before and after", len(beacons))
	}
	if got := beacons[0].GetToolCallId(); !strings.HasPrefix(got, "call_") {
		t.Fatalf("generated tool_call_id = %q, want call_ prefix", got)
	}
	if beacons[1].GetToolCallId() != beacons[0].GetToolCallId() {
		t.Fatalf("after tool_call_id = %q, want %q", beacons[1].GetToolCallId(), beacons[0].GetToolCallId())
	}
	for index, beacon := range beacons {
		if beacon.GetModelToolCallId() != "provider_call_1" {
			t.Fatalf("beacon %d model_tool_call_id = %q, want provider_call_1", index, beacon.GetModelToolCallId())
		}
	}
}

func TestRunErrorIndicatesWhetherBeforeBeaconWasPosted(t *testing.T) {
	t.Run("metadata failure is before beacon", func(t *testing.T) {
		runner := &Runner{
			MetadataFetchers: []func(context.Context) error{
				func(context.Context) error { return errors.New("metadata failed") },
			},
		}

		_, err := runner.Run(context.Background(), RunInput{})

		if err == nil || BeaconWasPosted(err) {
			t.Fatalf("Run error = %v, BeaconWasPosted = %t; want unposted error", err, BeaconWasPosted(err))
		}
	})

	t.Run("MCP failure follows before beacon", func(t *testing.T) {
		runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		}}

		_, err := runner.Run(context.Background(), RunInput{
			ToolName:  "system.echo",
			MCPClient: failingMCPClient{err: errors.New("MCP failed")},
		})

		if err == nil || !BeaconWasPosted(err) {
			t.Fatalf("Run error = %v, BeaconWasPosted = %t; want posted error", err, BeaconWasPosted(err))
		}
	})
}

func TestRunPostsFailedAfterWhenBeforeDecisionWaitFailsAfterBeaconSent(t *testing.T) {
	waitErr := beaconPostedTestError{err: context.DeadlineExceeded}
	var beacons []*turingv1.ToolCallBeacon
	var mcpCalls int
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
				return nil, waitErr
			}
			return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.GetToolCallId()}, nil
		},
	}

	_, err := runner.Run(context.Background(), RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      "run_1",
		TraceID:    "trace_1",
		ServerName: "system",
		ToolName:   "system.echo",
		MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
			mcpCalls++
			return map[string]any{"ok": true}, nil
		}),
	})

	if !errors.Is(err, context.DeadlineExceeded) || !BeaconWasPosted(err) {
		t.Fatalf("Run error = %T %v, want posted error wrapping context.DeadlineExceeded", err, err)
	}
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "tool_policy_decision_failed",
		},
	)
	if mcpCalls != 0 {
		t.Fatalf("MCP calls = %d, want 0", mcpCalls)
	}
}

func TestRunRejectsMissingOrMismatchedBeforeDecisionID(t *testing.T) {
	tests := []struct {
		name     string
		decision func(*turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision
	}{
		{name: "nil decision", decision: func(*turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision { return nil }},
		{name: "empty ID", decision: func(*turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
			return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW}
		}},
		{name: "mismatched ID", decision: func(*turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
			return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: "call_other"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var beacons []*turingv1.ToolCallBeacon
			mcpCalls := 0
			runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				beacons = append(beacons, beacon)
				return test.decision(beacon), nil
			}}

			_, err := runner.Run(context.Background(), RunInput{
				ToolName: "system.echo",
				MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
					mcpCalls++
					return map[string]any{"ok": true}, nil
				}),
			})

			if err == nil || !strings.Contains(err.Error(), "tool policy decision") || !ReportingFailed(err) || !BeaconWasPosted(err) {
				t.Fatalf("Run error = %T %v, want posted decision correlation reporting failure", err, err)
			}
			if mcpCalls != 0 {
				t.Fatalf("MCP calls = %d, want 0", mcpCalls)
			}
			assertBeaconSequence(t, beacons,
				beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
				beaconExpectation{
					phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
					status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
					code:   "tool_policy_decision_invalid",
				},
			)
		})
	}
}

func TestRunPostsFailedAfterForUnsupportedBeforePolicyDecision(t *testing.T) {
	for _, decision := range []turingv1.ToolPolicyDecision_Decision{
		turingv1.ToolPolicyDecision_DECISION_UNSPECIFIED,
		turingv1.ToolPolicyDecision_Decision(99),
	} {
		t.Run(decision.String(), func(t *testing.T) {
			var beacons []*turingv1.ToolCallBeacon
			mcpCalls := 0
			runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				beacons = append(beacons, beacon)
				if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
					return allowDecision(beacon), nil
				}
				return &turingv1.ToolPolicyDecision{
					Decision:   decision,
					ToolCallId: beacon.GetToolCallId(),
				}, nil
			}}

			_, err := runner.Run(context.Background(), RunInput{
				ToolName: "system.echo",
				MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
					mcpCalls++
					return map[string]any{"ok": true}, nil
				}),
			})

			if err == nil || !strings.Contains(err.Error(), "unsupported tool policy decision") || !BeaconWasPosted(err) {
				t.Fatalf("Run error = %T %v, want unsupported posted policy error", err, err)
			}
			if mcpCalls != 0 {
				t.Fatalf("MCP calls = %d, want 0", mcpCalls)
			}
			assertBeaconSequence(t, beacons,
				beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
				beaconExpectation{
					phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
					status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
					code:   "tool_policy_decision_invalid",
				},
			)
		})
	}
}

func TestRunClassifiesMismatchedAfterDecisionID(t *testing.T) {
	for _, test := range []struct {
		name          string
		before        turingv1.ToolPolicyDecision_Decision
		wantCommitted bool
	}{
		{name: "safe tool reporting failure", before: turingv1.ToolPolicyDecision_DECISION_ALLOW},
		{name: "side effect committed", before: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, wantCommitted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mcpCalls := 0
			runner := &Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					id := beacon.GetToolCallId()
					if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
						id = "call_other"
					}
					return &turingv1.ToolPolicyDecision{
						Decision:   test.before,
						ApprovalId: "approval_1",
						ToolCallId: id,
					}, nil
				},
				WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
			}

			_, err := runner.Run(context.Background(), RunInput{
				ToolName: "system.echo",
				MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
					mcpCalls++
					return map[string]any{"ok": true}, nil
				}),
			})

			if err == nil || !strings.Contains(err.Error(), "tool policy decision") || !BeaconWasPosted(err) {
				t.Fatalf("Run error = %T %v, want decision correlation error", err, err)
			}
			if got := SideEffectWasCommitted(err); got != test.wantCommitted {
				t.Fatalf("SideEffectWasCommitted(%v) = %t, want %t", err, got, test.wantCommitted)
			}
			if got := ReportingFailed(err); got == test.wantCommitted {
				t.Fatalf("ReportingFailed(%v) = %t, want %t", err, got, !test.wantCommitted)
			}
			if mcpCalls != 1 {
				t.Fatalf("MCP calls = %d, want 1 completed call", mcpCalls)
			}
		})
	}
}

func TestRunRejectsNonAllowAfterDecision(t *testing.T) {
	for _, decision := range []turingv1.ToolPolicyDecision_Decision{
		turingv1.ToolPolicyDecision_DECISION_UNSPECIFIED,
		turingv1.ToolPolicyDecision_DECISION_DENY,
		turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
	} {
		t.Run(decision.String(), func(t *testing.T) {
			runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				responseDecision := turingv1.ToolPolicyDecision_DECISION_ALLOW
				if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
					responseDecision = decision
				}
				return &turingv1.ToolPolicyDecision{
					Decision:   responseDecision,
					ToolCallId: beacon.GetToolCallId(),
				}, nil
			}}

			_, err := runner.Run(context.Background(), RunInput{
				ToolName:  "system.echo",
				MCPClient: fakeMCPClient{},
			})

			if err == nil || !ReportingFailed(err) ||
				!strings.Contains(err.Error(), "after tool policy decision must be allow") {
				t.Fatalf("Run error = %T %v, want reporting failure for non-allow after decision", err, err)
			}
		})
	}
}

func TestRunPolicyDenialReturnsRecoverableRejectionWithoutAfterBeacon(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_DENY,
			Reason:     "blocked",
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	_, err := runner.Run(context.Background(), RunInput{ToolName: "system.echo", MCPClient: fakeMCPClient{}})

	var rejected ToolRejectedError
	if !errors.As(err, &rejected) || !BeaconWasPosted(err) || RunWasTerminalized(err) {
		t.Fatalf("Run error = %T %v, want recoverable posted ToolRejectedError", err, err)
	}
	if !rejected.Recoverable() {
		t.Fatalf("ToolRejectedError = %+v, want recoverable", rejected)
	}
	if len(beacons) != 1 || beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("beacons = %+v, want one before beacon", beacons)
	}
}

func TestRunApprovalWaitFailurePostsFailedAfter(t *testing.T) {
	waitErr := errors.New("approval polling timed out")
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				return allowDecision(beacon), nil
			}
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) {
			return "", waitErr
		},
	}

	_, err := runner.Run(context.Background(), RunInput{ToolName: "system.echo", MCPClient: fakeMCPClient{}})

	if !errors.Is(err, waitErr) || !BeaconWasPosted(err) || RunWasTerminalized(err) {
		t.Fatalf("Run error = %T %v, want recoverable posted wait error", err, err)
	}
	if !strings.Contains(fmt.Sprintf("%T", err), "ApprovalWaitError") {
		t.Fatalf("Run error = %T %v, want ApprovalWaitError", err, err)
	}
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "approval_wait_failed",
		},
	)
}

func TestRunMissingApprovalWaiterReturnsTypedFailure(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return allowDecision(beacon), nil
		}
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ApprovalId: "approval_1",
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	_, err := runner.Run(context.Background(), RunInput{ToolName: "system.echo", MCPClient: fakeMCPClient{}})

	if err == nil || !strings.Contains(fmt.Sprintf("%T", err), "ApprovalWaitError") ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("Run error = %T %v, want configured ApprovalWaitError", err, err)
	}
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "approval_wait_failed",
		},
	)
}

func TestRunApprovalGatedMCPFailurePostsFailedAfter(t *testing.T) {
	callErr := errors.New("MCP failed")
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return allowDecision(beacon), nil
		}
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ApprovalId: "approval_1",
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}, WaitApproval: func(context.Context, string) (string, error) { return "token", nil }}

	_, err := runner.Run(context.Background(), RunInput{
		ToolName:  "system.echo",
		MCPClient: failingMCPClient{err: callErr},
	})

	if !errors.Is(err, callErr) || !BeaconWasPosted(err) {
		t.Fatalf("Run error = %T %v, want posted MCP error", err, err)
	}
	var uncertain interface{ SideEffectUncertain() bool }
	if !errors.As(err, &uncertain) || !uncertain.SideEffectUncertain() ||
		!strings.Contains(fmt.Sprintf("%T", err), "SideEffectUnknownError") {
		t.Fatalf("Run error = %T %v, want SideEffectUnknownError", err, err)
	}
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "mcp_call_failed",
		},
	)
}

func TestRunRecognizesTerminalApprovalCancellationCauseWithoutFailedAfter(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	waiting := make(chan struct{})
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(ctx context.Context, _ string) (string, error) {
			close(waiting)
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	result := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, RunInput{ToolName: "files.create", MCPClient: fakeMCPClient{}})
		result <- err
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("runner did not begin waiting for approval")
	}
	cancel(terminalApprovalCancellation{status: "denied"})
	err := <-result

	var terminal interface{ TerminalApproval() bool }
	if !errors.As(err, &terminal) || !terminal.TerminalApproval() || err.Error() != "approval denied" || !RunWasTerminalized(err) {
		t.Fatalf("Run error = %T %v, want terminal approval cancellation", err, err)
	}
	if len(beacons) != 1 || beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("beacons = %+v, want only BEFORE after terminal approval cancellation", beacons)
	}
}

func TestRunReturnsTerminalApprovalErrorWithoutPostingAfterBeacon(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	var mcpCalls int
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) {
			return "", terminalRunTestError{message: "approval denied"}
		},
	}

	_, err := runner.Run(context.Background(), RunInput{
		ToolName: "system.echo",
		MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
			mcpCalls++
			return map[string]any{"ok": true}, nil
		}),
	})

	if err == nil || err.Error() != "approval denied" || !RunWasTerminalized(err) {
		t.Fatalf("Run error = %T %v, want terminal approval error", err, err)
	}
	if !BeaconWasPosted(err) {
		t.Fatalf("Run error = %T %v, want before beacon ownership", err, err)
	}
	if len(beacons) != 1 || beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("beacons = %+v, want only before beacon", beacons)
	}
	if mcpCalls != 0 {
		t.Fatalf("MCP calls = %d, want 0", mcpCalls)
	}
}

func TestRunReturnsReportingFailureWhenFailedAfterCannotBePosted(t *testing.T) {
	reportErr := errors.New("after beacon unavailable")
	operationErr := errors.New("operation failed")
	tests := []struct {
		name       string
		decision   *turingv1.ToolPolicyDecision
		wait       func(context.Context, string) (string, error)
		client     MCPClient
		wantStatus turingv1.ToolCallStatus
	}{
		{
			name: "approval wait",
			decision: &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
			},
			wait:       func(context.Context, string) (string, error) { return "", operationErr },
			client:     fakeMCPClient{},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		},
		{
			name:       "MCP failure",
			decision:   &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED},
			wait:       func(context.Context, string) (string, error) { return "token", nil },
			client:     failingMCPClient{err: operationErr},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		},
		{
			name:       "unsupported decision",
			decision:   &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_UNSPECIFIED},
			client:     fakeMCPClient{},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var beacons []*turingv1.ToolCallBeacon
			runner := &Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					beacons = append(beacons, beacon)
					if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
						return nil, reportErr
					}
					return &turingv1.ToolPolicyDecision{
						Decision:   test.decision.GetDecision(),
						Reason:     test.decision.GetReason(),
						ApprovalId: test.decision.GetApprovalId(),
						ToolCallId: beacon.GetToolCallId(),
					}, nil
				},
				WaitApproval: test.wait,
			}

			_, err := runner.Run(context.Background(), RunInput{ToolName: "system.echo", MCPClient: test.client})

			var reporting ReportingFailureError
			if !errors.As(err, &reporting) || !errors.Is(err, reportErr) || !ReportingFailed(err) || !BeaconWasPosted(err) {
				t.Fatalf("Run error = %T %v, want ReportingFailureError wrapping report error", err, err)
			}
			if SideEffectWasCommitted(err) {
				t.Fatalf("Run error = %T %v, must not report committed side effect", err, err)
			}
			if test.name == "MCP failure" {
				if !errors.Is(err, operationErr) ||
					!strings.Contains(fmt.Sprintf("%T", reporting.operationErr), "SideEffectUnknownError") {
					t.Fatalf("Run error = %T %v, want reporting failure preserving side-effect uncertainty", err, err)
				}
			}
			if test.name == "approval wait" {
				if !errors.Is(err, operationErr) ||
					!strings.Contains(fmt.Sprintf("%T", reporting.operationErr), "ApprovalWaitError") {
					t.Fatalf("Run error = %T %v, want reporting failure preserving approval wait context", err, err)
				}
			}
			assertBeaconSequence(t, beacons,
				beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
				beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER, status: test.wantStatus},
			)
		})
	}
}

func TestRunMarksSuccessfulMCPCallWhenCompletedBeaconFails(t *testing.T) {
	reportErr := errors.New("completed beacon failed")
	var beacons []*turingv1.ToolCallBeacon
	var mcpCalls int
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				return nil, reportErr
			}

			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
	}

	_, err := runner.Run(context.Background(), RunInput{
		ToolName: "system.write",
		MCPClient: mcpClientFunc(func(context.Context, string, map[string]any, ...string) (map[string]any, error) {
			mcpCalls++
			return map[string]any{"ok": true}, nil
		}),
	})

	if !errors.Is(err, reportErr) || !SideEffectWasCommitted(err) {
		t.Fatalf("Run error = %T %v, want committed-side-effect error wrapping report failure", err, err)
	}
	if !BeaconWasPosted(err) {
		t.Fatalf("Run error = %T %v, want beacon ownership", err, err)
	}
	if mcpCalls != 1 {
		t.Fatalf("MCP calls = %d, want 1 successful call", mcpCalls)
	}
	if len(beacons) != 2 ||
		beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED {
		t.Fatalf("beacons = %+v, want attempted completed after beacon", beacons)
	}
}

func TestRunWithOutcomeDistinguishesSafeAndApprovalGatedSuccess(t *testing.T) {
	for _, test := range []struct {
		name          string
		decision      turingv1.ToolPolicyDecision_Decision
		sideEffecting bool
	}{
		{name: "allow is safe", decision: turingv1.ToolPolicyDecision_DECISION_ALLOW},
		{name: "approval required is side effecting", decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, sideEffecting: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					decision := test.decision
					if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
						decision = turingv1.ToolPolicyDecision_DECISION_ALLOW
					}
					return &turingv1.ToolPolicyDecision{
						Decision:   decision,
						ApprovalId: "approval_1",
						ToolCallId: beacon.GetToolCallId(),
					}, nil
				},
				WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
			}

			outcome, err := runner.RunWithOutcome(context.Background(), RunInput{
				ToolName:  "system.echo",
				MCPClient: fakeMCPClient{},
			})

			if err != nil {
				t.Fatalf("RunWithOutcome error: %v", err)
			}
			if outcome.SideEffecting != test.sideEffecting || outcome.Result["ok"] != true {
				t.Fatalf("outcome = %+v, want sideEffecting=%t and result", outcome, test.sideEffecting)
			}
		})
	}
}

func TestRunWithOutcomeMakesOnlyApprovalGatedMCPFailureUncertain(t *testing.T) {
	callErr := errors.New("MCP failed")
	for _, test := range []struct {
		name      string
		decision  turingv1.ToolPolicyDecision_Decision
		uncertain bool
	}{
		{name: "allow is recoverable", decision: turingv1.ToolPolicyDecision_DECISION_ALLOW},
		{name: "approval required is uncertain", decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, uncertain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
						return allowDecision(beacon), nil
					}
					return &turingv1.ToolPolicyDecision{
						Decision:   test.decision,
						ApprovalId: "approval_1",
						ToolCallId: beacon.GetToolCallId(),
					}, nil
				},
				WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
			}

			_, err := runner.RunWithOutcome(context.Background(), RunInput{
				ToolName:  "system.echo",
				MCPClient: failingMCPClient{err: callErr},
			})

			if !errors.Is(err, callErr) || !BeaconWasPosted(err) {
				t.Fatalf("RunWithOutcome error = %T %v, want posted MCP error", err, err)
			}
			if SideEffectWasUncertain(err) != test.uncertain {
				t.Fatalf("SideEffectWasUncertain(%T %v) = %t, want %t", err, err, SideEffectWasUncertain(err), test.uncertain)
			}
			if !test.uncertain {
				var recoverable interface{ Recoverable() bool }
				if !errors.As(err, &recoverable) || !recoverable.Recoverable() {
					t.Fatalf("RunWithOutcome error = %T %v, want recoverable safe-tool error", err, err)
				}
			}
		})
	}
}

func TestRunAppliesFreshTimeoutToEachNetworkStage(t *testing.T) {
	const timeout = 10 * time.Millisecond
	block := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	tests := []struct {
		name   string
		runner *Runner
		client MCPClient
	}{
		{
			name: "metadata",
			runner: &Runner{MetadataFetchers: []func(context.Context) error{
				block,
			}},
			client: fakeMCPClient{},
		},
		{
			name: "before beacon",
			runner: &Runner{PostBeacon: func(ctx context.Context, _ *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				return nil, block(ctx)
			}},
			client: fakeMCPClient{},
		},
		{
			name:   "MCP call",
			runner: &Runner{PostBeacon: allowToolDecision},
			client: mcpClientFunc(func(ctx context.Context, _ string, _ map[string]any, _ ...string) (map[string]any, error) {
				return nil, block(ctx)
			}),
		},
		{
			name: "after beacon",
			runner: &Runner{PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
					return nil, block(ctx)
				}
				return allowToolDecision(ctx, beacon)
			}},
			client: fakeMCPClient{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := time.Now()
			_, err := test.runner.Run(context.Background(), RunInput{
				Timeout:   timeout,
				ToolName:  "system.echo",
				MCPClient: test.client,
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Run error = %T %v, want deadline exceeded", err, err)
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("Run took %v, want bounded stage", elapsed)
			}
		})
	}
}

func TestRunDoesNotApplyToolTimeoutToApprovalWait(t *testing.T) {
	const timeout = 5 * time.Millisecond
	runner := &Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				return allowDecision(beacon), nil
			}
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(ctx context.Context, _ string) (string, error) {
			select {
			case <-time.After(3 * timeout):
				return "token", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}

	outcome, err := runner.RunWithOutcome(context.Background(), RunInput{
		Timeout:   timeout,
		ToolName:  "system.echo",
		MCPClient: fakeMCPClient{},
	})

	if err != nil || !outcome.SideEffecting {
		t.Fatalf("RunWithOutcome = %+v, %v; want approval wait beyond tool timeout to succeed", outcome, err)
	}
}

func TestRunEnforcesTotalTimeoutAcrossApprovalWait(t *testing.T) {
	const totalTimeout = 10 * time.Millisecond
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{
		PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return allowDecision(beacon), nil
			}
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}

	started := time.Now()
	_, err := runner.Run(context.Background(), RunInput{
		Timeout:      time.Second,
		TotalTimeout: totalTimeout,
		ToolName:     "system.echo",
		MCPClient:    fakeMCPClient{},
	})

	if !errors.Is(err, context.DeadlineExceeded) || ReportingFailed(err) {
		t.Fatalf("Run error = %T %v, want context deadline exceeded", err, err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Run took %v, want total timeout near %v", elapsed, totalTimeout)
	}
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "approval_wait_failed",
		},
	)
}

func allowDecision(beacon *turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
	return &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: beacon.GetToolCallId(),
	}
}

type beaconExpectation struct {
	phase  turingv1.ToolCallPhase
	status turingv1.ToolCallStatus
	code   string
}

func assertBeaconSequence(t *testing.T, got []*turingv1.ToolCallBeacon, want ...beaconExpectation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("beacons = %+v, want %d", got, len(want))
	}
	for index, expected := range want {
		if got[index].GetPhase() != expected.phase ||
			got[index].GetStatus() != expected.status ||
			(expected.code != "" && got[index].GetError().GetCode() != expected.code) {
			t.Fatalf("beacon %d = %+v, want %+v", index, got[index], expected)
		}
	}
}

type beaconPostedTestError struct {
	err error
}

type terminalRunTestError struct {
	message string
}

func (e terminalRunTestError) Error() string     { return e.message }
func (e terminalRunTestError) RunTerminal() bool { return true }

type terminalApprovalCancellation struct {
	status string
}

func (e terminalApprovalCancellation) Error() string          { return "approval " + e.status }
func (e terminalApprovalCancellation) TerminalApproval() bool { return true }

func (e beaconPostedTestError) Error() string { return e.err.Error() }
func (e beaconPostedTestError) Unwrap() error { return e.err }
func (e beaconPostedTestError) BeaconPosted() bool {
	return true
}

type fakeMCPClient struct{}

func allowToolDecision(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	return &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: beacon.GetToolCallId(),
	}, nil
}

func (fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]any, approvalToken ...string) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

type failingMCPClient struct {
	err error
}

type mcpClientFunc func(context.Context, string, map[string]any, ...string) (map[string]any, error)

func (f mcpClientFunc) CallTool(
	ctx context.Context,
	name string,
	args map[string]any,
	approvalToken ...string,
) (map[string]any, error) {
	return f(ctx, name, args, approvalToken...)
}

func (c failingMCPClient) CallTool(context.Context, string, map[string]any, ...string) (map[string]any, error) {
	return nil, c.err
}
