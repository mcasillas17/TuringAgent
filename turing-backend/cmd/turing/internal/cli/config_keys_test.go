package cli

import (
	"slices"
	"strings"
	"testing"
)

const (
	engineKeyRemovedNotice = "The previous engine's API key was removed; if this server needs one, run ./turing config set TURING_LOCAL_ENGINE_API_KEY."
	remoteKeyRemovedNotice = "The previous remote model's API key was removed; to use the new address, run ./turing config set OPENAI_API_KEY."
	keyRemovedWords        = "API key was removed"
)

// openAIEngineWithKey is a .env whose local engine is an OpenAI-compatible
// server at url, with its own API key.
func openAIEngineWithKey(url string) string {
	return fullEnv(
		"TURING_LOCAL_ENGINE=openai",
		"TURING_LOCAL_ENGINE_URL="+url,
		"TURING_LOCAL_MODEL=deepseek-chat",
		"TURING_LOCAL_ENGINE_API_KEY="+engineKeyCanary,
	)
}

// everyCheckHas fails the test unless both check-config runs were given entry.
func (h *harness) everyCheckHas(entry string) {
	h.t.Helper()
	calls := h.checkCalls()
	if len(calls) != 2 {
		h.t.Fatalf("check-config ran %d times, want 2", len(calls))
	}
	for _, call := range calls {
		if !slices.Contains(call.Env, entry) {
			h.t.Errorf("check-config env %q has no %q", call.Env, entry)
		}
	}
}

// noCheckGets fails the test when a check-config run was given name as a
// change.
func (h *harness) noCheckGets(name string) {
	h.t.Helper()
	for _, call := range h.checkCalls() {
		for _, entry := range call.Env {
			if strings.HasPrefix(entry, name+"=") {
				h.t.Fatalf("check-config was given %s as a change: %q", name, call.Env)
			}
		}
	}
}

// --- the local engine's key ---

func TestConfigSetOfAnotherEngineAddressRemovesTheEngineKey(t *testing.T) {
	for name, url := range map[string]string{
		"another port": "http://host.docker.internal:8001/v1",
		"another path": "http://host.docker.internal:8000/v2",
	} {
		t.Run(name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(openAIEngineWithKey("http://host.docker.internal:8000/v1"))

			if code := h.config("set", "TURING_LOCAL_ENGINE_URL", url, "--yes"); code != 0 {
				t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
			}
			h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_URL": url, "TURING_LOCAL_ENGINE_API_KEY": ""})
			h.everyCheckHas("TURING_LOCAL_ENGINE_API_KEY=")
			out := h.out.String()
			if n := strings.Count(out, engineKeyRemovedNotice); n != 1 {
				t.Fatalf("the key notice was printed %d times, want once:\n%s", n, out)
			}
			inOrder(t, out, engineKeyRemovedNotice, "Saved in .env.\n", engineUsable)
			h.noOutputContains(engineKeyCanary)
		})
	}
}

func TestConfigSetOfTheSameEngineAddressWrittenAnotherWayKeepsTheKey(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(openAIEngineWithKey("http://localhost:8000/v1"))

	if code := h.config("set", "TURING_LOCAL_ENGINE_URL", "http://host.docker.internal:8000/v1", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:8000/v1", "TURING_LOCAL_ENGINE_API_KEY": engineKeyCanary})
	h.noCheckGets(engineAPIKeyKey)
	if strings.Contains(h.out.String(), keyRemovedWords) {
		t.Fatalf("stdout = %q", h.out)
	}
	h.noOutputContains(engineKeyCanary)
}

// Ollama takes no key, so it is removed, but there is nothing to say about
// giving it one: config set would refuse a key for Ollama.
func TestConfigSetOfTheOllamaEngineRemovesTheEngineKeyWithoutANotice(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(openAIEngineWithKey("http://host.docker.internal:8000/v1"))

	if code := h.config("set", "TURING_LOCAL_ENGINE", "ollama", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE": "ollama", "TURING_LOCAL_ENGINE_API_KEY": ""})
	h.everyCheckHas("TURING_LOCAL_ENGINE_API_KEY=")
	if strings.Contains(h.out.String(), keyRemovedWords) {
		t.Fatalf("stdout = %q", h.out)
	}
	h.noOutputContains(engineKeyCanary)
}

func TestConfigMenuChangingTheEngineAddressRemovesTheEngineKey(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(openAIEngineWithKey("http://host.docker.internal:8000/v1"))
	h.IsTerminal = true
	h.Stdin = answers(menuSection(t, "Local engine"), menuSetting(t, "TURING_LOCAL_ENGINE_URL"), "http://host.docker.internal:8001/v1", "n", "q", "q")

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"TURING_LOCAL_ENGINE_URL": "http://host.docker.internal:8001/v1", "TURING_LOCAL_ENGINE_API_KEY": ""})
	h.everyCheckHas("TURING_LOCAL_ENGINE_API_KEY=")
	if n := strings.Count(h.out.String(), engineKeyRemovedNotice); n != 1 {
		t.Fatalf("the key notice was printed %d times, want once:\n%s", n, h.out)
	}
	h.noOutputContains(engineKeyCanary)
}

func TestARefusedChangeOfAddressKeepsTheKeyAndSaysNothingAboutIt(t *testing.T) {
	tests := []struct {
		name, env, key, value, canary string
	}{
		{"the local engine", openAIEngineWithKey("http://host.docker.internal:8000/v1"), "TURING_LOCAL_ENGINE_URL", "http://host.docker.internal:8001/v1", engineKeyCanary},
		{"the remote model", fullEnv(), "OPENAI_BASE_URL", "https://api.example.com/v1", canaryOpenAI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.writeEnv(tt.env)
			h.runner.on(checkConfigCall("turing-agent-runtime-general"), fakeReply{stderr: tt.key + " is refused\n", code: 1})
			before := h.envBytes()

			if code := h.config("set", tt.key, tt.value, "--yes"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			h.unchanged(before)
			if strings.Contains(h.out.String()+h.errOut.String(), keyRemovedWords) {
				t.Fatalf("a refused change said a key was removed:\nstdout: %s\nstderr: %s", h.out, h.errOut)
			}
			h.noOutputContains(tt.canary)
		})
	}
}

// --- the remote model's key ---

func TestConfigSetOfAnotherRemoteAddressRemovesTheRemoteKey(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "OPENAI_BASE_URL", "https://api.example.com/v1", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "https://api.example.com/v1", "OPENAI_API_KEY": ""})
	h.everyCheckHas("OPENAI_API_KEY=")
	out := h.out.String()
	if n := strings.Count(out, remoteKeyRemovedNotice); n != 1 {
		t.Fatalf("the key notice was printed %d times, want once:\n%s", n, out)
	}
	inOrder(t, out, remoteKeyRemovedNotice, "Saved in .env.\n")
	h.noOutputContains(canaryOpenAI)
}

// An unset OPENAI_BASE_URL is the services' own default, so naming that
// default, written another way, is no change of server.
func TestConfigSetOfTheSameRemoteAddressWrittenAnotherWayKeepsTheRemoteKey(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config("set", "OPENAI_BASE_URL", "HTTPS://API.OpenAI.com:443/v1/", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "HTTPS://API.OpenAI.com:443/v1/", "OPENAI_API_KEY": canaryOpenAI})
	h.noCheckGets("OPENAI_API_KEY")
	if strings.Contains(h.out.String(), keyRemovedWords) {
		t.Fatalf("stdout = %q", h.out)
	}
	h.noOutputContains(canaryOpenAI)
}

func TestConfigUnsetOfTheRemoteAddressRemovesAKeySetForAnotherServer(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv("OPENAI_BASE_URL=https://api.example.com/v1"))

	if code := h.config("unset", "OPENAI_BASE_URL", "--yes"); code != 0 {
		t.Fatalf("exit = %d; stdout: %s\nstderr: %s", code, h.out, h.errOut)
	}
	h.wantEnv(t, map[string]string{"OPENAI_BASE_URL": "", "OPENAI_API_KEY": ""})
	h.everyCheckHas("OPENAI_API_KEY=")
	if n := strings.Count(h.out.String(), remoteKeyRemovedNotice); n != 1 {
		t.Fatalf("the key notice was printed %d times, want once:\n%s", n, h.out)
	}
	h.noOutputContains(canaryOpenAI)
}

func TestSameRemoteDestination(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		same bool
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1", true},
		{"https://api.openai.com/v1", "HTTPS://API.OpenAI.com:443/v1/", true},
		{"http://host.docker.internal:80/v1", "http://host.docker.internal/v1", true},
		{"https://api.openai.com/v1", "https://api.openai.com/v2", false},
		{"https://api.openai.com/v1", "https://api.openai.com:8443/v1", false},
		{"https://api.openai.com/v1", "http://api.openai.com/v1", false},
		{"https://api.openai.com/v1", "https://api.example.com/v1", false},
		{"https://api.openai.com/v1", "https://" + "owner:pw@api.openai.com/v1", false},
		{"not a url", "https://api.openai.com/v1", false},
	} {
		if got := sameRemoteDestination(tt.a, tt.b); got != tt.same {
			t.Errorf("sameRemoteDestination(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.same)
		}
	}
}
