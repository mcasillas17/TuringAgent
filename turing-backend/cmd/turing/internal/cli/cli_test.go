package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

func TestMainRefusesRootBeforeAnythingIsReadOrRun(t *testing.T) {
	for _, args := range [][]string{{"start"}, {"stop"}, {"restart", "--yes"}, {"logs"}, {"status"}, {"connect"}, {"connect", "--rotate-key", "--yes"}, {"config"}, {"config", "show"}, {"config", "set", "TURING_LOCAL_MODEL", "x", "--yes"}, {"help"}, {"bogus"}, {}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			h.Geteuid = func() int { return 0 }
			h.BackendDir = "/does/not/exist"

			if code := h.run(args...); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("root ran %q", h.runner.commands())
			}
			if !strings.Contains(h.errOut.String(), "root") || h.out.Len() != 0 {
				t.Fatalf("stdout = %q, stderr = %q", h.out, h.errOut)
			}
		})
	}
}

func TestMainWithNoCommandIsAUsageError(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(h.errOut.String(), "Usage:") {
		t.Fatalf("stderr = %q", h.errOut)
	}
}

func TestMainRefusesAnUnknownCommandAndPointsToHelp(t *testing.T) {
	h := newHarness(t)
	if code := h.run("bogus"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "unknown command") || !strings.Contains(got, "./turing help") || strings.Contains(got, "bogus") {
		t.Fatalf("stderr = %q", got)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestHelpListsTheCommandsAndNamesWhatIsComing(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		h := newHarness(t)
		if code := h.run(arg); code != 0 {
			t.Fatalf("%s: exit = %d, want 0", arg, code)
		}
		out := h.out.String()
		for _, want := range []string{"Usage:", "start", "stop", "restart", "logs", "status", "connect", "--rotate-key", "config", "config show", "config get KEY", "config set KEY", "--stdin", "--no-apply", "config unset KEY", "help", "setup", "engine", "coming in a later version"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: usage does not mention %q:\n%s", arg, want, out)
			}
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("help ran %q", h.runner.commands())
		}
	}
}

func TestHelpNoLongerCallsStatusOrConnectComingLater(t *testing.T) {
	h := newHarness(t)
	h.run("help")
	for _, line := range strings.Split(h.out.String(), "\n") {
		trimmed := strings.TrimSpace(line)
		if (strings.HasPrefix(trimmed, "status") || strings.HasPrefix(trimmed, "connect")) && strings.Contains(line, "coming in a later version") {
			t.Errorf("help says a shipped command is still coming: %q", line)
		}
	}
}

func TestACommandThatIsPlannedButNotShippedSaysSoAndDoesNothing(t *testing.T) {
	for _, command := range []string{"setup"} {
		h := newHarness(t)
		if code := h.run(command); code != 1 {
			t.Fatalf("%s: exit = %d, want 1", command, code)
		}
		if got := h.errOut.String(); !strings.Contains(got, "coming in a later version") || !strings.Contains(got, "./scripts/init.sh") {
			t.Fatalf("%s: stderr = %q", command, got)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%s ran %q", command, h.runner.commands())
		}
	}
}

func TestAFlagThatDoesNotExistIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"start", "--nope"}, {"stop", "--build"}, {"restart", "-x"}, {"start", "extra"}, {"status", "extra"}, {"connect", "extra"}} {
		h := newHarness(t)
		if code := h.run(args...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q ran %q", args, h.runner.commands())
		}
		if !strings.Contains(h.errOut.String(), "./turing help") {
			t.Fatalf("%q: stderr = %q", args, h.errOut)
		}
	}
}

func TestConfirmAnswersYesWithoutAskingWhenYesIsGiven(t *testing.T) {
	h := newHarness(t)
	h.IsTerminal = true
	if !h.confirm("Restart now?", false, true) {
		t.Fatal("--yes did not confirm")
	}
	if h.out.Len() != 0 {
		t.Fatalf("a prompt was printed: %q", h.out)
	}
}

func TestConfirmDeclinesOnANonTerminalAndSaysHowToConfirm(t *testing.T) {
	for _, defaultYes := range []bool{false, true} {
		h := newHarness(t)
		h.Stdin = strings.NewReader("y\n")
		if h.confirm("Restart now?", defaultYes, false) {
			t.Fatalf("defaultYes=%v: a non-terminal confirmed", defaultYes)
		}
		out := h.out.String()
		if !strings.Contains(out, "Restart now?") || !strings.Contains(out, "--yes") {
			t.Fatalf("defaultYes=%v: output = %q", defaultYes, out)
		}
	}
}

func TestConfirmReadsAnAnswerFromATerminal(t *testing.T) {
	for _, tc := range []struct {
		input      string
		defaultYes bool
		want       bool
	}{
		{"y\n", false, true},
		{"Y\n", false, true},
		{"yes\n", false, true},
		{" YES \r\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"\n", true, true},
		{"\n", false, false},
		{"maybe\n", true, false},
		{"", true, false},
		{"y", false, true},
	} {
		h := newHarness(t)
		h.IsTerminal = true
		h.Stdin = strings.NewReader(tc.input)
		if got := h.confirm("Go?", tc.defaultYes, false); got != tc.want {
			t.Errorf("confirm(input %q, defaultYes %v) = %v, want %v", tc.input, tc.defaultYes, got, tc.want)
		}
		want := "[y/N]"
		if tc.defaultYes {
			want = "[Y/n]"
		}
		if !strings.Contains(h.out.String(), "Go? "+want) {
			t.Errorf("input %q: prompt = %q, want %q", tc.input, h.out, want)
		}
	}
}

func TestConfirmReadsOneLineAndLeavesTheRestOfStdin(t *testing.T) {
	h := newHarness(t)
	h.IsTerminal = true
	h.Stdin = strings.NewReader("y\nn\n")
	first := h.confirm("First?", false, false)
	second := h.confirm("Second?", true, false)
	if !first || second {
		t.Fatalf("answers = %v, %v; want true, false", first, second)
	}
}

func TestWaitHealthyReturnsOnceHealthAnswersAndEveryServiceRuns(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall(psArgs...),
		fakeReply{stdout: psOutput(withService(allRunning(), "turing-agent-runtime-general", "restarting", "")...)},
		fakeReply{stdout: psOutput(withService(allRunning(), "turing-orchestrator", "running", "starting")...)},
		fakeReply{stdout: psOutput(allRunning()...)},
	)
	h.healthErr = func(call int) error {
		if call < 2 {
			return errors.New("connection refused")
		}
		return nil
	}

	if err := h.waitHealthy(context.Background(), testPort, testKey); err != nil {
		t.Fatalf("waitHealthy = %v", err)
	}
	if len(h.healthCalls) < 3 {
		t.Fatalf("polled health %d times, want at least 3", len(h.healthCalls))
	}
	for _, call := range h.healthCalls {
		if call.port != testPort || call.key != testKey {
			t.Fatalf("health asked with %+v", call)
		}
	}
}

func TestWaitHealthyAcceptsAServiceWithoutAHealthCheck(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(allRunning()...)})
	if err := h.waitHealthy(context.Background(), testPort, testKey); err != nil {
		t.Fatalf("waitHealthy = %v", err)
	}
}

func TestWaitHealthyNamesTheFirstServiceThatIsNotHealthy(t *testing.T) {
	broken := func(name string, states []host.ServiceState, healthErr error, wantService string) {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(states...)})
			h.healthErr = func(int) error { return healthErr }
			h.HealthTimeout = 60 * time.Millisecond

			started := time.Now()
			err := h.waitHealthy(context.Background(), testPort, testKey)
			if err == nil {
				t.Fatal("waitHealthy = nil")
			}
			if time.Since(started) < 50*time.Millisecond {
				t.Fatalf("gave up after %s, before the timeout", time.Since(started))
			}
			var unhealthy *UnhealthyError
			if !errors.As(err, &unhealthy) || unhealthy.Service != wantService {
				t.Fatalf("err = %v, want an UnhealthyError for %q", err, wantService)
			}
			if want := "./turing logs " + wantService; !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %q does not say %q", err, want)
			}
		})
	}
	broken("restarting", withService(allRunning(), "turing-agent-runtime-general", "restarting", ""), nil, "runtime")
	broken("exited", withService(allRunning(), "turing-mcp-files", "exited", ""), nil, "files")
	broken("unhealthy", withService(allRunning(), "turing-orchestrator", "running", "unhealthy"), nil, "orchestrator")
	broken("still starting", withService(allRunning(), "turing-mcp-system", "running", "starting"), nil, "system")
	broken("absent", allRunning()[:3], nil, "system")
	broken("first of several", withService(withService(allRunning(), "turing-mcp-files", "exited", ""), "turing-agent-runtime-general", "exited", ""), nil, "runtime")
	broken("nothing answers on the port", allRunning(), errors.New("connection refused"), "orchestrator")
}

func TestWaitHealthyNamesTheOrchestratorWhenComposeCannotListServices(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall(psArgs...), fakeReply{stderr: "no configuration file provided", code: 1})
	h.HealthTimeout = 30 * time.Millisecond

	err := h.waitHealthy(context.Background(), testPort, testKey)
	var unhealthy *UnhealthyError
	if !errors.As(err, &unhealthy) || unhealthy.Service != "orchestrator" {
		t.Fatalf("err = %v, want an UnhealthyError for orchestrator", err)
	}
}

func TestWaitHealthyStopsAtOnceWhenTheKeyIsRefused(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(allRunning()...)})
	h.healthErr = func(int) error { return host.ErrUnauthenticated }
	h.HealthTimeout = 5 * time.Second

	started := time.Now()
	err := h.waitHealthy(context.Background(), testPort, testKey)
	if !errors.Is(err, host.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if !strings.Contains(err.Error(), "run ./turing restart") {
		t.Fatalf("err = %q does not say what to run", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("waited %s for a key that will never match", time.Since(started))
	}
}

func TestWaitHealthyStopsWhenItsContextEnds(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(withService(allRunning(), "turing-mcp-files", "exited", "")...)})
	h.healthErr = func(int) error { return errors.New("connection refused") }
	h.HealthTimeout = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	err := h.waitHealthy(ctx, testPort, testKey)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("waited %s after the context ended", time.Since(started))
	}
}
