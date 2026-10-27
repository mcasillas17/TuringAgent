package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/host"
)

func TestStartBringsTheStackUpThenReportsItHealthy(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()

	if code := h.run("start"); code != 0 {
		t.Fatalf("exit = %d\nstdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	commands := h.runner.commands()
	if len(commands) < 2 || commands[0] != "compose up -d" || commands[1] != "compose ps --all --format json" {
		t.Fatalf("commands = %q, want up -d then ps", commands)
	}
	if want := "Turing is running. To connect the app, run ./turing connect"; !strings.Contains(h.out.String(), want) {
		t.Fatalf("stdout = %q, want %q", h.out, want)
	}
	if len(h.healthCalls) == 0 || h.healthCalls[0] != (healthCall{testPort, testKey}) {
		t.Fatalf("health calls = %+v, want the port and key from .env", h.healthCalls)
	}
	h.noSecrets()
}

func TestStartBuildAddsTheBuildFlag(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("up", "-d", "--build"), fakeReply{})
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(allRunning()...)})

	if code := h.run("start", "--build"); code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
	}
	if got := h.runner.commands()[0]; got != "compose up -d --build" {
		t.Fatalf("first command = %q", got)
	}
}

func TestStartUsesTheDefaultPortWhenDotEnvNamesNone(t *testing.T) {
	h := newHarness(t)
	h.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\n")
	h.serveHealthy()

	if code := h.run("start"); code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
	}
	if h.healthCalls[0].port != 3000 {
		t.Fatalf("port = %d, want 3000", h.healthCalls[0].port)
	}
}

func TestStartKeepsShellExportsOutOfCompose(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()

	if code := h.run("start"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, call := range h.runner.calls {
		if slices.Contains(call.Env, "OPENAI_API_KEY=sk-exported") {
			t.Fatalf("compose saw an exported setting: %q", call.Env)
		}
		if !slices.Contains(call.Env, "PATH=/usr/bin:/bin") {
			t.Fatalf("compose lost PATH: %q", call.Env)
		}
		if call.Dir != h.BackendDir {
			t.Fatalf("compose ran in %q, want %q", call.Dir, h.BackendDir)
		}
	}
}

func TestStartAndRestartWithoutADotEnvSayWhatToRun(t *testing.T) {
	for _, args := range [][]string{{"start"}, {"restart", "--yes"}} {
		h := newHarness(t)
		h.removeEnv()

		if code := h.run(args...); code != 1 {
			t.Fatalf("%q: exit = %d, want 1", args, code)
		}
		if !strings.Contains(h.errOut.String(), "run ./scripts/init.sh first") {
			t.Fatalf("%q: stderr = %q", args, h.errOut)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q ran %q", args, h.runner.commands())
		}
	}
}

func TestStartRefusesADotEnvItCannotUse(t *testing.T) {
	for name, content := range map[string]string{
		"no key":     "ORCHESTRATOR_PUBLIC_PORT=3456\n",
		"empty key":  "TURING_CLIENT_API_KEY=\nORCHESTRATOR_PUBLIC_PORT=3456\n",
		"text port":  "TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=abc\n",
		"zero port":  "TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=0\n",
		"huge port":  "TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=65536\n",
		"port space": "TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT='30 00'\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.writeEnv(content)
			if code := h.run("start"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			if got := h.errOut.String(); !strings.Contains(got, ".env") {
				t.Fatalf("stderr = %q", got)
			}
			h.noSecrets()
		})
	}
}

func TestStartReportsAComposeFailureAndDoesNotWaitForHealth(t *testing.T) {
	h := newHarness(t)
	h.dockerUp()
	h.runner.on(composeCall("up", "-d"), fakeReply{stderr: "Compose launch failed: sandbox must be a real directory.\n", code: 1})

	if code := h.run("start"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if want := []string{"compose up -d", "docker info --format {{.ServerVersion}}"}; !slices.Equal(h.runner.commands(), want) || len(h.healthCalls) != 0 {
		t.Fatalf("ran %q and asked health %d times after a failed up", h.runner.commands(), len(h.healthCalls))
	}
	got := h.errOut.String()
	if !strings.Contains(got, "Compose launch failed: sandbox must be a real directory.") {
		t.Fatalf("compose's own message was not passed on: %q", got)
	}
	if !strings.Contains(got, "exit status 1") || !strings.Contains(got, "./turing logs") {
		t.Fatalf("stderr = %q", got)
	}
	if strings.Contains(got, "Docker is not running") {
		t.Fatalf("a running Docker was called down: %q", got)
	}
}

func TestComposeIsGivenTheTerminalItselfSoItsProgressDisplayWorks(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()

	if code := h.run("start"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	up := h.runner.calls[0]
	if up.Stdout != h.Stdout || up.Stderr != h.Stderr {
		t.Fatalf("compose up was given wrapped streams: %T, %T", up.Stdout, up.Stderr)
	}
}

func TestStartSaysHowToStartDockerWhenItIsNotRunning(t *testing.T) {
	for goos, want := range map[string]string{
		"darwin": "Docker Desktop",
		"linux":  "sudo systemctl start docker",
	} {
		t.Run(goos, func(t *testing.T) {
			h := newHarness(t)
			h.GOOS = goos
			h.dockerDown()
			h.runner.on(composeCall("up", "-d"), fakeReply{code: 1})

			if code := h.run("start"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			got := h.errOut.String()
			if !strings.Contains(got, "Docker is not running") || !strings.Contains(got, want) || !strings.Contains(got, "./turing start") {
				t.Fatalf("stderr = %q, want it to name %q", got, want)
			}
		})
	}
}

func TestStartReportsACommandThatCouldNotRun(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("up", "-d"), fakeReply{code: -1, err: errCouldNotStart})

	if code := h.run("start"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "could not run ./scripts/compose.sh") || !strings.Contains(got, errCouldNotStart.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}

func TestStartFailsWhenTheStackDoesNotBecomeHealthyAndNamesTheService(t *testing.T) {
	h := newHarness(t)
	h.HealthTimeout = 50 * time.Millisecond
	h.runner.on(composeCall("up", "-d"), fakeReply{})
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(withService(allRunning(), "turing-agent-runtime-general", "restarting", "")...)})

	if code := h.run("start"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	got := h.errOut.String()
	if !strings.Contains(got, "runtime") || !strings.Contains(got, "./turing logs runtime") {
		t.Fatalf("stderr = %q, want the service and its logs command", got)
	}
	if strings.Contains(h.out.String(), "Turing is running") {
		t.Fatalf("stdout claims success: %q", h.out)
	}
	h.noSecrets()
}

func TestStartExplainsAKeyTheOrchestratorRefuses(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()
	h.healthErr = func(int) error { return host.ErrUnauthenticated }

	if code := h.run("start"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	want := "the key in .env is not the one the running orchestrator started with; run ./turing restart"
	if got := h.errOut.String(); !strings.Contains(got, want) {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	h.noSecrets()
}

func TestStopStopsTheStackAndSaysItStaysStopped(t *testing.T) {
	h := newHarness(t)
	h.removeEnv()
	h.runner.on(composeCall("stop"), fakeReply{})

	if code := h.run("stop"); code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
	}
	if !slices.Equal(h.runner.commands(), []string{"compose stop"}) {
		t.Fatalf("commands = %q", h.runner.commands())
	}
	out := h.out.String()
	if !strings.Contains(out, "stays stopped after a reboot") || !strings.Contains(out, "./turing start") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestStopReportsAComposeFailure(t *testing.T) {
	h := newHarness(t)
	h.dockerUp()
	h.runner.on(composeCall("stop"), fakeReply{stderr: "boom\n", code: 1})

	if code := h.run("stop"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if strings.Contains(h.out.String(), "stays stopped") {
		t.Fatalf("stdout claims it stopped: %q", h.out)
	}
	if got := h.errOut.String(); !strings.Contains(got, "boom") || !strings.Contains(got, "exit status 1") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestStopWithDockerDownSaysSoAndHowToTryAgain(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.dockerDown()
	h.runner.on(composeCall("stop"), fakeReply{code: 1})

	if code := h.run("stop"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "Docker is not running") || !strings.Contains(got, "run ./turing stop") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRestartOnANonTerminalWithoutYesDoesNothing(t *testing.T) {
	h := newHarness(t)

	if code := h.run("restart"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	out := h.out.String()
	if !strings.Contains(out, "Restarting stops a reply in progress. Restart now?") || !strings.Contains(out, "--yes") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestRestartWithYesStopsThenStarts(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("stop"), fakeReply{})
	h.serveHealthy()

	if code := h.run("restart", "--yes"); code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
	}
	commands := h.runner.commands()
	if len(commands) < 3 || commands[0] != "compose stop" || commands[1] != "compose up -d" || commands[2] != "compose ps --all --format json" {
		t.Fatalf("commands = %q, want stop, up -d, ps", commands)
	}
	out := h.out.String()
	if !strings.Contains(out, "Turing is running") {
		t.Fatalf("stdout = %q", out)
	}
	if strings.Contains(out, "stays stopped") {
		t.Fatalf("restart says it stays stopped: %q", out)
	}
}

func TestRestartPassesBuildThrough(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("stop"), fakeReply{})
	h.runner.on(composeCall("up", "-d", "--build"), fakeReply{})
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(allRunning()...)})

	if code := h.run("restart", "--build", "--yes"); code != 0 {
		t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
	}
	if got := h.runner.commands()[1]; got != "compose up -d --build" {
		t.Fatalf("second command = %q", got)
	}
}

func TestRestartAsksOnATerminalAndDefaultsToNo(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		h := newHarness(t)
		h.IsTerminal = true
		h.Stdin = strings.NewReader("y\n")
		h.runner.on(composeCall("stop"), fakeReply{})
		h.serveHealthy()
		if code := h.run("restart"); code != 0 {
			t.Fatalf("exit = %d\nstderr: %s", code, h.errOut)
		}
		if !strings.Contains(h.out.String(), "Restart now? [y/N]") {
			t.Fatalf("stdout = %q", h.out)
		}
	})
	t.Run("enter", func(t *testing.T) {
		h := newHarness(t)
		h.IsTerminal = true
		h.Stdin = strings.NewReader("\n")
		if code := h.run("restart"); code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("ran %q", h.runner.commands())
		}
		if !strings.Contains(h.out.String(), "Not restarted") {
			t.Fatalf("stdout = %q", h.out)
		}
	})
}

func TestRestartDoesNotStartWhenItCouldNotStop(t *testing.T) {
	h := newHarness(t)
	h.GOOS = "darwin"
	h.dockerDown()
	h.runner.on(composeCall("stop"), fakeReply{stderr: "cannot stop\n", code: 1})

	if code := h.run("restart", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if want := []string{"compose stop", "docker info --format {{.ServerVersion}}"}; !slices.Equal(h.runner.commands(), want) {
		t.Fatalf("commands = %q, want %q", h.runner.commands(), want)
	}
	if got := h.errOut.String(); !strings.Contains(got, "run ./turing restart") {
		t.Fatalf("stderr = %q, want it to say to run restart again", got)
	}
}

func TestRestartThatStoppedButCouldNotStartSaysToStart(t *testing.T) {
	h := newHarness(t)
	h.dockerDown()
	h.runner.on(composeCall("stop"), fakeReply{})
	h.runner.on(composeCall("up", "-d"), fakeReply{code: 1})

	if code := h.run("restart", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "run ./turing start") {
		t.Fatalf("stderr = %q, want it to say to run start, since the stack is stopped", got)
	}
}

func TestLogsMapsTheServiceNameAndPassesTheExitCodeThrough(t *testing.T) {
	h := newHarness(t)
	h.removeEnv()
	h.runner.on(composeCall("logs", "--tail", "200", "-f", "turing-agent-runtime-general"),
		fakeReply{stdout: "runtime line\n", code: 130})

	if code := h.run("logs", "runtime", "-f"); code != 130 {
		t.Fatalf("exit = %d, want 130", code)
	}
	if h.out.String() != "runtime line\n" {
		t.Fatalf("stdout = %q", h.out)
	}
	if h.runner.calls[0].Stdout == nil || h.runner.calls[0].Stderr == nil {
		t.Fatal("the logs were not streamed to the terminal")
	}
}

func TestLogsAcceptsEveryFriendlyNameAndEitherOrder(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"logs"}, "compose logs --tail 200"},
		{[]string{"logs", "-f"}, "compose logs --tail 200 -f"},
		{[]string{"logs", "orchestrator"}, "compose logs --tail 200 turing-orchestrator"},
		{[]string{"logs", "-f", "files"}, "compose logs --tail 200 -f turing-mcp-files"},
		{[]string{"logs", "system"}, "compose logs --tail 200 turing-mcp-system"},
		{[]string{"logs", "runtime"}, "compose logs --tail 200 turing-agent-runtime-general"},
	} {
		h := newHarness(t)
		h.runner.on(func(cmd host.Command) bool { return len(cmd.Args) > 0 && cmd.Args[0] == "logs" }, fakeReply{})
		if code := h.run(tc.args...); code != 0 {
			t.Fatalf("%q: exit = %d", tc.args, code)
		}
		if got := h.runner.commands(); !slices.Equal(got, []string{tc.want}) {
			t.Errorf("%q ran %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestLogsRefusesAnUnknownServiceAndListsTheNames(t *testing.T) {
	h := newHarness(t)

	if code := h.run("logs", "database"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	got := h.errOut.String()
	for _, name := range []string{`"database"`, "orchestrator", "runtime", "files", "system"} {
		if !strings.Contains(got, name) {
			t.Errorf("stderr = %q, want it to name %s", got, name)
		}
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
}

func TestLogsRefusesOptionsAndServicesItDoesNotKnow(t *testing.T) {
	for _, args := range [][]string{{"logs", "--since", "1h"}, {"logs", "runtime", "files"}, {"logs", "-n"}} {
		h := newHarness(t)
		if code := h.run(args...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q ran %q", args, h.runner.commands())
		}
	}
}

func TestLogsReportsACommandThatCouldNotRun(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("logs", "--tail", "200"), fakeReply{code: -1, err: errCouldNotStart})

	if code := h.run("logs"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "could not run ./scripts/compose.sh") {
		t.Fatalf("stderr = %q", h.errOut)
	}
}
