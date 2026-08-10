package tools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

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

func TestRunUsesSuppliedToolCallIDForAllBeacons(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	_, err := runner.Run(context.Background(), RunInput{
		ToolCallID: "provider_call_1",
		ToolName:   "system.echo",
		MCPClient:  fakeMCPClient{},
	})

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(beacons) != 2 {
		t.Fatalf("beacons = %d, want before and after", len(beacons))
	}
	for _, beacon := range beacons {
		if beacon.GetToolCallId() != "provider_call_1" {
			t.Fatalf("tool_call_id = %q, want provider_call_1", beacon.GetToolCallId())
		}
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
		ToolName:  "system.echo",
		MCPClient: fakeMCPClient{},
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

func TestRunPostsFailureAfterWhenPolicyDecisionWaitFailsAfterBeforeSent(t *testing.T) {
	waitErr := beaconPostedTestError{err: context.Canceled}
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{
		PostBeacon: func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
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
		MCPClient:  fakeMCPClient{},
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if len(beacons) != 2 {
		t.Fatalf("beacons = %d, want before and failure after", len(beacons))
	}
	if beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER || beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED {
		t.Fatalf("after beacon = %+v, want failed after beacon", beacons[1])
	}
	if beacons[1].GetError().GetCode() != "tool_policy_decision_failed" {
		t.Fatalf("after error = %+v, want tool_policy_decision_failed", beacons[1].GetError())
	}
	if beacons[1].GetToolCallId() != beacons[0].GetToolCallId() {
		t.Fatalf("after tool_call_id = %q, want %q", beacons[1].GetToolCallId(), beacons[0].GetToolCallId())
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
	assertBeaconSequence(t, beacons,
		beaconExpectation{phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE},
		beaconExpectation{
			phase:  turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
			status: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
			code:   "approval_wait_failed",
		},
	)
}

func TestRunMCPFailurePostsFailedAfter(t *testing.T) {
	callErr := errors.New("MCP failed")
	var beacons []*turingv1.ToolCallBeacon
	runner := &Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}}

	_, err := runner.Run(context.Background(), RunInput{
		ToolName:  "system.echo",
		MCPClient: failingMCPClient{err: callErr},
	})

	if !errors.Is(err, callErr) || !BeaconWasPosted(err) {
		t.Fatalf("Run error = %T %v, want posted MCP error", err, err)
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
		beforeErr  error
		wait       func(context.Context, string) (string, error)
		client     MCPClient
		wantStatus turingv1.ToolCallStatus
	}{
		{
			name:       "policy decision",
			beforeErr:  beaconPostedTestError{err: operationErr},
			client:     fakeMCPClient{},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		},
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
			decision:   &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW},
			client:     failingMCPClient{err: operationErr},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED,
		},
		{
			name:       "unsupported decision",
			decision:   &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_UNSPECIFIED},
			client:     fakeMCPClient{},
			wantStatus: turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED,
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
					if test.beforeErr != nil {
						return nil, test.beforeErr
					}
					return test.decision, nil
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
				Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
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

func (e beaconPostedTestError) Error() string { return e.err.Error() }
func (e beaconPostedTestError) Unwrap() error { return e.err }
func (e beaconPostedTestError) BeaconPosted() bool {
	return true
}

type fakeMCPClient struct{}

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
