package main

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd/servicecmdtest"
)

// offlineHarness stands in for everything the runtime would reach at startup:
// an orchestrator listener nobody may dial, and a local engine nobody may
// call. check-config must leave both untouched.
type offlineHarness struct {
	t            *testing.T
	orchestrator *net.TCPListener
	engineURL    string
}

func newOfflineHarness(t *testing.T) *offlineHarness {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	engine := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("check-config called the local engine: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(engine.Close)
	return &offlineHarness{t: t, orchestrator: listener.(*net.TCPListener), engineURL: engine.URL}
}

func (h *offlineHarness) environ(extra ...string) []string {
	return append([]string{
		"TURING_RUNTIME_TOKEN=internal",
		"ORCHESTRATOR_GRPC_ADDR=" + h.orchestrator.Addr().String(),
		"TURING_LOCAL_ENGINE_URL=" + h.engineURL,
	}, extra...)
}

func (h *offlineHarness) assertNothingDialed() {
	h.t.Helper()
	if err := h.orchestrator.SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		h.t.Fatal(err)
	}
	conn, err := h.orchestrator.Accept()
	if err == nil {
		_ = conn.Close()
		h.t.Fatal("check-config dialed the orchestrator")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		h.t.Fatalf("Accept = %v, want a timeout", err)
	}
}

func failServe(t *testing.T) func() error {
	return func() error {
		t.Fatal("check-config started the runtime")
		return nil
	}
}

func runtimeCheckConfig(t *testing.T, environ []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := servicecmd.Main([]string{"check-config"}, environ, checks, failServe(t), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRuntimeCheckConfigAcceptsAValidEnvironmentWithoutDialing(t *testing.T) {
	h := newOfflineHarness(t)
	code, stdout, stderr := runtimeCheckConfig(t, h.environ())
	if code != 0 || stdout != "configuration is valid\n" || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	h.assertNothingDialed()
}

func TestRuntimeCheckConfigAcceptsTheOpenAICompatibleEngine(t *testing.T) {
	h := newOfflineHarness(t)
	code, stdout, stderr := runtimeCheckConfig(t, append(h.environ(),
		"TURING_LOCAL_ENGINE=openai",
		"TURING_LOCAL_ENGINE_URL="+h.engineURL+"/v1",
		"TURING_LOCAL_ENGINE_LABEL=ds4",
		"TURING_LOCAL_MODEL=deepseek-chat",
		"OPENAI_API_KEY=configured",
		"OPENAI_BASE_URL=https://api.example.test/v1",
	))
	if code != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	h.assertNothingDialed()
}

// Compose passes the local-engine keys as ${KEY:-}, so an unset one arrives
// as an empty string and must mean "use the default".
func TestRuntimeCheckConfigTreatsEmptyValuesAsUnset(t *testing.T) {
	code, stdout, stderr := runtimeCheckConfig(t, []string{
		"TURING_RUNTIME_TOKEN=internal",
		"TURING_LOCAL_ENGINE=",
		"TURING_LOCAL_ENGINE_URL=",
		"TURING_LOCAL_MODEL=",
		"OLLAMA_KEEP_ALIVE=",
		"OPENAI_API_KEY=",
	})
	if code != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestRuntimeCheckConfigNamesTheRefusedKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
		key   string
	}{
		{"unknown engine", []string{"TURING_LOCAL_ENGINE=vllm"}, "TURING_LOCAL_ENGINE"},
		{"openai without a URL", []string{"TURING_LOCAL_ENGINE=openai", "TURING_LOCAL_ENGINE_URL="}, "TURING_LOCAL_ENGINE_URL"},
		{"engine on the LAN", []string{"TURING_LOCAL_ENGINE_URL=http://192.168.1.5:8000/v1"}, "TURING_LOCAL_ENGINE_URL"},
		{"API key for Ollama", []string{"TURING_LOCAL_ENGINE_API_KEY=x"}, "TURING_LOCAL_ENGINE_API_KEY"},
		{"unprintable label", []string{"TURING_LOCAL_ENGINE_LABEL=ds4\x07"}, "TURING_LOCAL_ENGINE_LABEL"},
		{
			"output larger than the window",
			[]string{"TURING_LOCAL_CONTEXT_WINDOW_TOKENS=4096", "TURING_LOCAL_MAX_OUTPUT_TOKENS=8192"},
			"TURING_LOCAL_MAX_OUTPUT_TOKENS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newOfflineHarness(t)
			code, stdout, stderr := runtimeCheckConfig(t, append(h.environ(), tc.extra...))
			if code != 1 || stdout != "" || !strings.Contains(stderr, tc.key) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q; want 1 and stderr naming %s", code, stdout, stderr, tc.key)
			}
			h.assertNothingDialed()
		})
	}
}

func TestRuntimeCheckConfigNeverPrintsASecret(t *testing.T) {
	code, stdout, stderr := runtimeCheckConfig(t, []string{
		"TURING_RUNTIME_TOKEN=internal",
		`TURING_AGENT_API_KEYS={"claude":"sk-ant-SECRETVALUE"`,
	})
	if code != 1 || !strings.Contains(stderr, "TURING_AGENT_API_KEYS") {
		t.Fatalf("code = %d, stderr = %q; want 1 and stderr naming TURING_AGENT_API_KEYS", code, stderr)
	}
	if strings.Contains(stdout+stderr, "SECRETVALUE") {
		t.Fatalf("output repeats the secret: stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestRuntimeCheckConfigWarnsAboutDeprecatedKeys(t *testing.T) {
	code, _, stderr := runtimeCheckConfig(t, []string{"TURING_RUNTIME_TOKEN=internal", "OLLAMA_MODEL=qwen3"})
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q; a deprecated name is a warning, not a refusal", code, stderr)
	}
	if !strings.HasPrefix(stderr, "warning: ") || !strings.Contains(stderr, "TURING_LOCAL_MODEL") {
		t.Fatalf("stderr = %q, want a warning naming TURING_LOCAL_MODEL", stderr)
	}
}

func TestTheRuntimeRefusesUnknownArguments(t *testing.T) {
	// check-engine arrives with OPS-001 PR 3; until then it is unknown.
	for _, args := range [][]string{{"check-engine"}, {"check-config", "x"}} {
		var stdout, stderr bytes.Buffer
		code := servicecmd.Main(args, []string{"TURING_RUNTIME_TOKEN=internal"}, checks, failServe(t), &stdout, &stderr)
		if code != 2 {
			t.Fatalf("args %q: code = %d, stderr = %q; want 2", args, code, stderr.String())
		}
	}
}

func TestTheRuntimeImageLabelsItsChecks(t *testing.T) {
	got := servicecmdtest.FinalStageLabel(t, filepath.Join("..", "..", "Dockerfile"), servicecmd.LabelKey)
	if want := servicecmd.LabelValue(checks); got != want {
		t.Fatalf("Dockerfile %s = %q, want %q: the label must list exactly the checks the binary dispatches",
			servicecmd.LabelKey, got, want)
	}
}

// The check builds the providers with a client that refuses every request,
// so a constructor that ever started reaching the network would fail here
// rather than quietly calling the engine.
func TestRuntimeCheckConfigUsesAClientThatRefusesRequests(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := offlineClient.Do(request); err == nil {
		t.Fatal("offlineClient made a request")
	}
}
