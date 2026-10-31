package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
)

const sec001Sentence = "Run the app on this machine: until device pairing (SEC-001) ships, Turing accepts the app only from the machine it runs on."

var clientKeyFormat = regexp.MustCompile(`^tk_[0-9a-f]{64}$`)

func (h *harness) readEnvFile() *envfile.File {
	h.t.Helper()
	data, err := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
	if err != nil {
		h.t.Fatal(err)
	}
	return dotEnv(h.t, string(data))
}

func (h *harness) clientKeyInEnv() string {
	h.t.Helper()
	key, _ := h.readEnvFile().Get("TURING_CLIENT_API_KEY")
	return key
}

func TestConnectPrintsTheBackendURLAndKeyForThisMachine(t *testing.T) {
	h := newHarness(t)

	if code := h.run("connect"); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, h.errOut)
	}

	want := "Backend URL: http://localhost:3456\nAPI key: " + testKey + "\nAnyone holding this key can use Turing.\n"
	if h.out.String() != want {
		t.Fatalf("stdout = %q, want %q", h.out, want)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("connect ran %q", h.runner.commands())
	}
	if h.errOut.Len() != 0 {
		t.Fatalf("stderr = %q", h.errOut)
	}
}

func TestConnectDefaultsToPort3000(t *testing.T) {
	h := newHarness(t)
	h.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\n")

	if code := h.run("connect"); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, h.errOut)
	}
	if !strings.Contains(h.out.String(), "Backend URL: http://localhost:3000\n") {
		t.Fatalf("stdout = %q", h.out)
	}
}

func TestConnectOverSSHSaysTheAppMustRunOnThisMachineAndOffersNoTunnel(t *testing.T) {
	h := newHarness(t)
	h.Getenv = func(name string) string {
		if name == "SSH_CONNECTION" {
			return "192.0.2.10 51234 192.0.2.1 22"
		}
		return ""
	}

	if code := h.run("connect"); code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, h.errOut)
	}

	got := h.out.String()
	for _, want := range []string{"Backend URL: http://localhost:3456", "API key: " + testKey, "Anyone holding this key can use Turing.", sec001Sentence} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	}
	for _, banned := range []string{"ssh", "tunnel", "-L ", "forward", "192.0.2"} {
		if strings.Contains(strings.ToLower(got), banned) {
			t.Errorf("stdout = %q, mentions %q", got, banned)
		}
	}
	if strings.Count(got, "\n") != 4 {
		t.Errorf("stdout has %d lines, want the three lines and the one sentence:\n%s", strings.Count(got, "\n"), got)
	}
}

func TestConnectOnTheMachineSaysNothingAboutAnotherMachine(t *testing.T) {
	h := newHarness(t)
	h.Getenv = func(string) string { return "" }

	h.run("connect")

	if strings.Contains(h.out.String(), "SEC-001") {
		t.Fatalf("stdout = %q", h.out)
	}
}

func TestConnectWithoutDotEnvSaysToRunInit(t *testing.T) {
	for _, args := range [][]string{{"connect"}, {"connect", "--rotate-key", "--yes"}} {
		h := newHarness(t)
		h.removeEnv()

		if code := h.run(args...); code != 1 {
			t.Fatalf("%q: exit = %d, want 1", args, code)
		}
		if got := h.errOut.String(); !strings.Contains(got, "run ./scripts/init.sh first") {
			t.Fatalf("%q: stderr = %q", args, got)
		}
		if _, err := os.Stat(filepath.Join(h.BackendDir, ".env")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%q: .env exists afterwards: %v", args, err)
		}
		if _, err := os.Stat(filepath.Join(h.BackendDir, ".env.lock")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%q: a lock file was left behind: %v", args, err)
		}
		if len(h.runner.calls) != 0 || h.out.Len() != 0 {
			t.Fatalf("%q: ran %q, printed %q", args, h.runner.commands(), h.out)
		}
	}
}

func TestConnectWithoutAClientKeyInDotEnvFails(t *testing.T) {
	h := newHarness(t)
	h.writeEnv("ORCHESTRATOR_PUBLIC_PORT=3456\n")

	if code := h.run("connect"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "TURING_CLIENT_API_KEY") || !strings.Contains(got, "./scripts/init.sh") {
		t.Fatalf("stderr = %q", got)
	}
	if h.out.Len() != 0 {
		t.Fatalf("stdout = %q", h.out)
	}
}

func TestConnectRefusesArgumentsAndOptionsItDoesNotHave(t *testing.T) {
	for _, args := range [][]string{{"connect", "now"}, {"connect", "--tunnel"}, {"connect", "--rotate-key", "extra"}} {
		h := newHarness(t)
		before := h.readEnvFile().Bytes()

		if code := h.run(args...); code != 2 {
			t.Fatalf("%q: exit = %d, want 2", args, code)
		}
		if len(h.runner.calls) != 0 {
			t.Fatalf("%q: ran %q", args, h.runner.commands())
		}
		if string(h.readEnvFile().Bytes()) != string(before) {
			t.Fatalf("%q changed .env", args)
		}
	}
}

func TestRotateKeyWritesTheNewKeyThenAppliesItWhenConfirmed(t *testing.T) {
	h := newHarness(t)
	h.writeEnv("# the app\nTURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=3456\nOPENAI_API_KEY=sk-in-dotenv\n")
	if err := os.Chmod(filepath.Join(h.BackendDir, ".env"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.serveHealthy()

	if code := h.run("connect", "--rotate-key", "--yes"); code != 0 {
		t.Fatalf("exit = %d, want 0; stdout:\n%s\nstderr:\n%s", code, h.out, h.errOut)
	}

	newKey := h.clientKeyInEnv()
	if !clientKeyFormat.MatchString(newKey) {
		t.Fatalf("the new key = %q, want tk_ and 64 lowercase hex characters", newKey)
	}
	if newKey == testKey {
		t.Fatal("the key did not change")
	}
	info, err := os.Stat(filepath.Join(h.BackendDir, ".env"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	data, _ := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
	if want := "# the app\nTURING_CLIENT_API_KEY=" + newKey + "\nORCHESTRATOR_PUBLIC_PORT=3456\nOPENAI_API_KEY=sk-in-dotenv\n"; string(data) != want {
		t.Fatalf(".env = %q, want only the key changed: %q", data, want)
	}

	if want := []string{"compose up -d", "compose " + strings.Join(psArgs, " ")}; strings.Join(h.runner.commands(), "|") != strings.Join(want, "|") {
		t.Fatalf("ran %q, want %q", h.runner.commands(), want)
	}
	if len(h.healthCalls) == 0 || h.healthCalls[0].port != 3456 || h.healthCalls[0].key != newKey {
		t.Fatalf("health asked %+v, want the new key on the .env port", h.healthCalls)
	}

	got := h.out.String()
	if !strings.Contains(got, "API key: "+newKey+"\n") || !strings.Contains(got, "Backend URL: http://localhost:3456\n") {
		t.Fatalf("stdout = %q, want the new connection details", got)
	}
	if strings.Contains(got+h.errOut.String(), testKey) || strings.Contains(got+h.errOut.String(), "sk-in-dotenv") {
		t.Fatalf("output shows the old key or another secret:\nstdout: %s\nstderr: %s", got, h.errOut)
	}
}

func TestRotateKeyMakesADifferentKeyEachTime(t *testing.T) {
	seen := make(map[string]bool)
	for range 5 {
		h := newHarness(t)
		h.serveHealthy()
		if code := h.run("connect", "--rotate-key", "--yes"); code != 0 {
			t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
		}
		key := h.clientKeyInEnv()
		if !clientKeyFormat.MatchString(key) || seen[key] {
			t.Fatalf("key %q is malformed or repeated", key)
		}
		seen[key] = true
	}
}

// stdinProbe is a stdin that notes what had run when it was first read.
type stdinProbe struct {
	reader         io.Reader
	h              *harness
	ranAtFirstRead *int
}

func (p stdinProbe) Read(b []byte) (int, error) {
	if *p.ranAtFirstRead < 0 {
		*p.ranAtFirstRead = len(p.h.runner.calls)
	}
	return p.reader.Read(b)
}

func TestRotateKeyRunsNothingBeforeTheConfirmation(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()
	h.IsTerminal = true
	ran := -1
	h.Stdin = stdinProbe{reader: strings.NewReader("y\n"), h: h, ranAtFirstRead: &ran}

	if code := h.run("connect", "--rotate-key"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if ran != 0 {
		t.Fatalf("%d commands ran before the question was answered", ran)
	}
	if !strings.Contains(h.out.String(), "Apply now? This restarts the orchestrator and stops a reply in progress. [Y/n] ") {
		t.Fatalf("stdout = %q, want the question", h.out)
	}
	if !clientKeyFormat.MatchString(h.clientKeyInEnv()) || h.clientKeyInEnv() == testKey {
		t.Fatal(".env was not updated before the question")
	}
}

func TestRotateKeyDeclinedOffATerminalLeavesTheOldKeyRunning(t *testing.T) {
	h := newHarness(t)

	if code := h.run("connect", "--rotate-key"); code != 0 {
		t.Fatalf("exit = %d, want 0: the key was rotated and saved; stderr: %s", code, h.errOut)
	}

	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q without a confirmation", h.runner.commands())
	}
	got := h.out.String()
	for _, want := range []string{
		"Apply now? This restarts the orchestrator and stops a reply in progress. [Y/n] no (not a terminal; pass --yes to confirm)",
		"The old key keeps working until the next start; run ./turing restart to switch.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	}
	newKey := h.clientKeyInEnv()
	if !clientKeyFormat.MatchString(newKey) || newKey == testKey {
		t.Fatalf(".env key = %q, want a new key saved", newKey)
	}
	// The new key is shown even though it is not live yet, as the old script did.
	if !strings.Contains(got, "API key: "+newKey+"\n") || !strings.Contains(got, "Backend URL: http://localhost:3456\n") || !strings.Contains(got, "Anyone holding this key can use Turing.") {
		t.Fatalf("stdout = %q, want the connection details with the new key", got)
	}
	if strings.Contains(got+h.errOut.String(), testKey) {
		t.Fatalf("the old key was printed:\n%s", got)
	}
	if strings.Index(got, "API key: ") > strings.Index(got, "The old key keeps working") {
		t.Fatalf("stdout = %q, want the details before the decline sentence", got)
	}
}

func TestRotateKeyAtATerminalAsksAndFollowsTheAnswer(t *testing.T) {
	tests := []struct {
		answer  string
		applies bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"\n", true},
		{"n\n", false},
		{"no\n", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.answer), func(t *testing.T) {
			h := newHarness(t)
			h.serveHealthy()
			h.IsTerminal = true
			h.Stdin = strings.NewReader(tt.answer)

			if code := h.run("connect", "--rotate-key"); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}

			upRan := false
			for _, command := range h.runner.commands() {
				upRan = upRan || command == "compose up -d"
			}
			if upRan != tt.applies {
				t.Fatalf("ran %q, want up -d run = %v", h.runner.commands(), tt.applies)
			}
			declined := strings.Contains(h.out.String(), "The old key keeps working until the next start; run ./turing restart to switch.")
			if declined == tt.applies {
				t.Fatalf("stdout = %q", h.out)
			}
		})
	}
}

func TestRotateKeyApplyKeepsTheShellsExportOfTheKeyOutOfCompose(t *testing.T) {
	h := newHarness(t)
	h.Environ = []string{"PATH=/usr/bin", "TURING_CLIENT_API_KEY=exported-old"}
	h.serveHealthy()

	if code := h.run("connect", "--rotate-key", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	for _, call := range h.runner.calls {
		for _, entry := range call.Env {
			if strings.HasPrefix(entry, "TURING_CLIENT_API_KEY=") {
				t.Errorf("%s %q was given %q", call.Name, call.Args, entry)
			}
		}
	}
}

func TestRotateKeyUsesTheEnvLockAndRefusesWhileItIsHeld(t *testing.T) {
	h := newHarness(t)
	before := h.readEnvFile().Bytes()
	unlock, err := envfile.Lock(filepath.Join(h.BackendDir, ".env.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	if code := h.run("connect", "--rotate-key", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}

	if got := h.errOut.String(); !strings.Contains(got, "another turing command is changing .env") || !strings.Contains(got, "./turing connect --rotate-key") {
		t.Fatalf("stderr = %q, want the lock refusal and the command to try again", got)
	}
	if string(h.readEnvFile().Bytes()) != string(before) {
		t.Fatal(".env changed while another command held the lock")
	}
	if len(h.runner.calls) != 0 || h.out.Len() != 0 {
		t.Fatalf("ran %q, printed %q", h.runner.commands(), h.out)
	}
}

func TestRotateKeyReleasesTheLockBeforeAsking(t *testing.T) {
	h := newHarness(t)
	h.serveHealthy()
	h.IsTerminal = true
	answer := strings.NewReader("y\n")
	locked := false
	h.Stdin = readerFunc(func(b []byte) (int, error) {
		if unlock, err := envfile.Lock(filepath.Join(h.BackendDir, ".env.lock")); err == nil {
			unlock()
		} else {
			locked = true
		}
		return answer.Read(b)
	})

	if code := h.run("connect", "--rotate-key"); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if locked {
		t.Fatal("the lock was still held while the question waited for an answer")
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(b []byte) (int, error) { return f(b) }

func TestRotateKeyLeavesAnUnusableDotEnvAlone(t *testing.T) {
	tests := []struct {
		name  string
		setup func(h *harness)
		want  string
	}{
		{"a port that is not a number", func(h *harness) {
			h.writeEnv("TURING_CLIENT_API_KEY=" + testKey + "\nORCHESTRATOR_PUBLIC_PORT=abc\n")
		}, "ORCHESTRATOR_PUBLIC_PORT"},
		{"a NUL byte", func(h *harness) { h.writeEnv("OPENAI_API_KEY=a\x00b\n") }, "cannot parse .env"},
		{"a symlink", func(h *harness) {
			h.removeEnv()
			target := filepath.Join(h.BackendDir, "elsewhere")
			if err := os.WriteFile(target, []byte("TURING_CLIENT_API_KEY="+testKey+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(h.BackendDir, ".env")); err != nil {
				t.Fatal(err)
			}
		}, "regular file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			tt.setup(h)
			var before []byte
			if data, err := os.ReadFile(filepath.Join(h.BackendDir, ".env")); err == nil {
				before = data
			}

			if code := h.run("connect", "--rotate-key", "--yes"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}

			if got := h.errOut.String(); !strings.Contains(got, tt.want) {
				t.Fatalf("stderr = %q, want %q", got, tt.want)
			}
			after, _ := os.ReadFile(filepath.Join(h.BackendDir, ".env"))
			if string(after) != string(before) {
				t.Fatalf(".env changed: %q -> %q", before, after)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
		})
	}
}

func TestRotateKeyWhenComposeFailsSaysTheKeyIsSavedAndHowToRetry(t *testing.T) {
	h := newHarness(t)
	h.runner.on(composeCall("up", "-d"), fakeReply{code: 1})
	h.dockerUp()

	if code := h.run("connect", "--rotate-key", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}

	got := h.errOut.String()
	if !strings.Contains(got, "Compose failed") || !strings.Contains(got, "The new app key is saved in .env") || !strings.Contains(got, "./turing restart") {
		t.Fatalf("stderr = %q", got)
	}
	newKey := h.clientKeyInEnv()
	out := h.out.String()
	// The key is saved, so it is shown even though it is not live yet.
	if !strings.Contains(out, "API key: "+newKey+"\n") || !strings.Contains(out, "Backend URL: http://localhost:3456\n") || !strings.Contains(out, "Anyone holding this key can use Turing.") {
		t.Fatalf("stdout = %q, want the connection details with the new key", out)
	}
	if strings.Contains(out+got, testKey) {
		t.Fatalf("the old key was printed")
	}
	if strings.Contains(out, "Turing is running with the new key") {
		t.Fatalf("stdout = %q claims the new key is live", out)
	}
}

func TestRotateKeyWhenTheStackDoesNotComeUpHealthySaysTheKeyIsSaved(t *testing.T) {
	h := newHarness(t)
	h.HealthTimeout = 20 * time.Millisecond
	h.runner.on(composeCall("up", "-d"), fakeReply{})
	h.runner.on(composeCall(psArgs...), fakeReply{stdout: psOutput(withService(allRunning(), "turing-orchestrator", "exited", "")...)})

	if code := h.run("connect", "--rotate-key", "--yes"); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}

	got := h.errOut.String()
	for _, want := range []string{"new app key is saved in .env", "orchestrator is not healthy", "./turing logs orchestrator"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	}
	newKey := h.clientKeyInEnv()
	out := h.out.String()
	if !strings.Contains(out, "API key: "+newKey+"\n") || !strings.Contains(out, "Backend URL: http://localhost:3456\n") {
		t.Fatalf("stdout = %q, want the connection details with the new key", out)
	}
	if strings.Contains(out+got, testKey) {
		t.Fatalf("the old key was printed")
	}
	if strings.Contains(out, "Turing is running with the new key") {
		t.Fatalf("stdout = %q claims the new key is live", out)
	}
}
