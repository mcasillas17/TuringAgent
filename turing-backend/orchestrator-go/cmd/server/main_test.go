package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd"
	"github.com/mcasillas17/TuringAgent/turing-backend/internal/servicecmd/servicecmdtest"
)

// validEnviron is the smallest environment the orchestrator starts with. It
// mirrors the config package's own requiredEnv, which is internal to that
// package's tests.
func validEnviron(extra ...string) []string {
	return append([]string{
		"TURING_CLIENT_API_KEY=client-key",
		"TURING_RUNTIME_TOKEN=runtime-token",
		"TURING_APPROVAL_CONSUMER_TOKEN=approval-consumer-token",
		"MCP_FILES_ENABLED=true",
		"TURING_APPROVAL_JWT_SECRET=approval-secret",
		"TURING_EGRESS_SIGNING_SECRET=egress-secret",
		"TURING_CURSOR_HMAC_SECRET=" + strings.Repeat("ab", 32),
	}, extra...)
}

func without(environ []string, key string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		if !strings.HasPrefix(entry, key+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

func failServe(t *testing.T) func() error {
	return func() error {
		t.Fatal("check-config started the orchestrator")
		return nil
	}
}

func checkConfigWith(t *testing.T, environ []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := servicecmd.Main([]string{"check-config"}, environ, checks, failServe(t), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestCheckConfigAcceptsAValidEnvironment(t *testing.T) {
	code, stdout, stderr := checkConfigWith(t, validEnviron())
	if code != 0 || stdout != "configuration is valid\n" || stderr != "" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

// Compose passes every setting it does not default as ${KEY:-}, so an unset
// setting arrives as an empty string and must mean "use the default".
func TestCheckConfigTreatsEmptyValuesAsUnset(t *testing.T) {
	code, stdout, stderr := checkConfigWith(t, validEnviron(
		"TURING_LOCAL_MODEL=",
		"TURING_QUEUE_MAX_WAIT_MS=",
		"ORCHESTRATOR_PUBLIC_PORT=",
		"TURING_AGENT_TEAM_ENABLED=",
		"TURING_AGENT_API_KEYS=",
	))
	if code != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestCheckConfigNamesTheRefusedKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		environ []string
		key     string
	}{
		{"missing client key", without(validEnviron(), "TURING_CLIENT_API_KEY"), "TURING_CLIENT_API_KEY"},
		{"port not a number", validEnviron("ORCHESTRATOR_PUBLIC_PORT=abc"), "ORCHESTRATOR_PUBLIC_PORT"},
		{"unknown queue policy", validEnviron("TURING_QUEUE_TIMEOUT_POLICY=never"), "TURING_QUEUE_TIMEOUT_POLICY"},
		{
			"team on with an unbounded queue wait",
			validEnviron("TURING_AGENT_TEAM_ENABLED=true", "TURING_QUEUE_MAX_WAIT_MS=0"),
			"TURING_QUEUE_MAX_WAIT_MS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := checkConfigWith(t, tc.environ)
			if code != 1 || stdout != "" || !strings.Contains(stderr, tc.key) {
				t.Fatalf("code = %d, stdout = %q, stderr = %q; want 1 and stderr naming %s", code, stdout, stderr, tc.key)
			}
		})
	}
}

func TestCheckConfigNeverPrintsASecret(t *testing.T) {
	code, stdout, stderr := checkConfigWith(t, validEnviron(`TURING_AGENT_API_KEYS={"claude":"sk-ant-SECRETVALUE"`))
	if code != 1 || !strings.Contains(stderr, "TURING_AGENT_API_KEYS") {
		t.Fatalf("code = %d, stderr = %q; want 1 and stderr naming TURING_AGENT_API_KEYS", code, stderr)
	}
	if strings.Contains(stdout+stderr, "SECRETVALUE") {
		t.Fatalf("output repeats the secret: stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestCheckConfigWarnsAboutDeprecatedKeys(t *testing.T) {
	code, _, stderr := checkConfigWith(t, validEnviron("OLLAMA_MODEL=qwen3"))
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q; a deprecated name is a warning, not a refusal", code, stderr)
	}
	if !strings.HasPrefix(stderr, "warning: ") || !strings.Contains(stderr, "TURING_LOCAL_MODEL") {
		t.Fatalf("stderr = %q, want a warning naming TURING_LOCAL_MODEL", stderr)
	}
}

// The check must neither need nor make anything on disk. With a writable
// directory, anything it created would be found there; with a read-only one,
// a check that tried to open its database would be refused.
func TestCheckConfigCreatesNothing(t *testing.T) {
	environUnder := func(dir string) []string {
		absent := filepath.Join(dir, "absent")
		return validEnviron(
			"DATABASE_PATH="+filepath.Join(absent, "data", "turing.db"),
			"MEMORY_ROOT="+filepath.Join(absent, "memory"),
			"SKILLS_ROOT="+filepath.Join(absent, "skills"),
			"TEAM_ROOT="+filepath.Join(absent, "team"),
			"MCP_CONFIG_ROOT="+filepath.Join(absent, "mcp"),
		)
	}
	t.Run("writable", func(t *testing.T) {
		dir := t.TempDir()
		code, stdout, stderr := checkConfigWith(t, environUnder(dir))
		if code != 0 {
			t.Fatalf("code = %d, stdout = %q, stderr = %q; missing paths are not a configuration error", code, stdout, stderr)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("check-config created %v", entries)
		}
	})
	t.Run("read-only", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		code, stdout, stderr := checkConfigWith(t, environUnder(dir))
		if code != 0 {
			t.Fatalf("code = %d, stdout = %q, stderr = %q; the check must not need to write", code, stdout, stderr)
		}
	})
}

func TestTheOrchestratorRefusesUnknownArguments(t *testing.T) {
	for _, args := range [][]string{{"check-engine"}, {"--port", "3000"}} {
		var stdout, stderr bytes.Buffer
		code := servicecmd.Main(args, validEnviron(), checks, failServe(t), &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), args[0]) {
			t.Fatalf("args %q: code = %d, stderr = %q; want 2 naming the argument", args, code, stderr.String())
		}
	}
}

func TestTheOrchestratorImageLabelsItsChecks(t *testing.T) {
	got := servicecmdtest.FinalStageLabel(t, filepath.Join("..", "..", "Dockerfile"), servicecmd.LabelKey)
	if want := servicecmd.LabelValue(checks); got != want {
		t.Fatalf("Dockerfile %s = %q, want %q: the label must list exactly the checks the binary dispatches",
			servicecmd.LabelKey, got, want)
	}
}
