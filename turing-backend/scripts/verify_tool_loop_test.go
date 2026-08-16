package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestVerifyToolLoopScriptPreservesLiveVerificationContract(t *testing.T) {
	data, err := os.ReadFile("verify-tool-loop.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	preflightIndex := strings.Index(script, "/api/tags")
	composeIndex := strings.Index(script, "compose up --build -d --wait --wait-timeout 60")
	if preflightIndex < 0 {
		t.Fatal("verify-tool-loop.sh does not probe Ollama before starting Compose")
	}
	if composeIndex < 0 {
		t.Fatal("verify-tool-loop.sh does not bound its wait for Compose healthchecks")
	}
	if preflightIndex >= composeIndex {
		t.Fatal("verify-tool-loop.sh starts Compose before checking Ollama")
	}

	for _, want := range []string{
		`TURING_VERIFY_OLLAMA_URL`,
		`host.docker.internal`,
		`attempts="${TURING_VERIFY_ATTEMPTS:-3}"`,
		`./scripts/compose.sh`,
		`trap cleanup EXIT`,
		`"$client_bin" -model-driven -attempts "$attempts"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("verify-tool-loop.sh does not contain %q", want)
		}
	}
	if strings.Contains(script, "docker compose") {
		t.Fatal("verify-tool-loop.sh bypasses scripts/compose.sh")
	}
}

func TestVerifyToolLoopScriptPropagatesInconclusiveAndCleansUp(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 2, 0, "", "", "", "")
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.composeLog, "up --build -d --wait --wait-timeout 60\ndown\n") {
		t.Fatalf("compose log = %q, want up followed by down", result.composeLog)
	}
}

func TestVerifyToolLoopScriptCleansUpAfterComposeFailure(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 17, 0, 0, "", "", "", "")
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want environment-level 2; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.composeLog, "up --build -d --wait --wait-timeout 60\ndown\n") {
		t.Fatalf("compose log = %q, want cleanup after failed up", result.composeLog)
	}
}

func TestVerifyToolLoopScriptDoesNotStopStackWhenBuildFails(t *testing.T) {
	result := runVerifyToolLoopHarness(
		t,
		0,
		0,
		0,
		0,
		"",
		"",
		"",
		"",
		verifyHarnessOptions{goBuildExit: 23},
	)
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want environment-level 2; output:\n%s", result.exitCode, result.output)
	}
	if result.composeLog != "" {
		t.Fatalf("compose ran when client build failed: %q", result.composeLog)
	}
}

func TestVerifyToolLoopScriptUsesHostReachableOllamaURL(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 0, "", "", "", "")
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.curlLog, "http://localhost:11434/api/tags") {
		t.Fatalf("curl log = %q, want host-rewritten Ollama URL", result.curlLog)
	}
	if !strings.Contains(result.curlLog, "http://localhost:11434/api/show") {
		t.Fatalf("curl log = %q, want model availability probe", result.curlLog)
	}
}

func TestVerifyToolLoopScriptHonorsEnvironmentOllamaModel(t *testing.T) {
	result := runVerifyToolLoopHarness(
		t,
		0,
		0,
		0,
		0,
		"",
		"",
		"",
		"",
		verifyHarnessOptions{ollamaModel: "qwen2.5"},
	)
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.curlLog, `"qwen2.5"`) {
		t.Fatalf("curl log = %q, want environment model probe", result.curlLog)
	}
}

func TestVerifyToolLoopScriptHarnessIgnoresAmbientOllamaModel(t *testing.T) {
	t.Setenv("OLLAMA_MODEL", "ambient-model")
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 0, "", "", "", "")
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.curlLog, `"qwen2.5:7b"`) {
		t.Fatalf("curl log = %q, want .env model probe", result.curlLog)
	}
}

func TestVerifyToolLoopScriptReportsMissingModelWithoutStartingCompose(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 22, "", "", "", "")
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.output, "model qwen2.5:7b is not available") {
		t.Fatalf("output = %q, want missing-model guidance", result.output)
	}
	if result.composeLog != "" {
		t.Fatalf("compose ran after missing model: %q", result.composeLog)
	}
}

func TestVerifyToolLoopScriptReportsInitFailureAsInconclusive(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 19, 0, 0, 0, "", "", "", "")
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.output, "Could not initialize") {
		t.Fatalf("output = %q, want initialization guidance", result.output)
	}
	if result.composeLog != "" {
		t.Fatalf("compose ran after init failure: %q", result.composeLog)
	}
}

func TestVerifyToolLoopScriptPreservesFailureExitThroughCleanup(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 1, 0, "", "", "", "")
	if result.exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", result.exitCode, result.output)
	}
	if !strings.HasSuffix(result.composeLog, "down\n") {
		t.Fatalf("compose cleanup log = %q", result.composeLog)
	}
}

func TestVerifyToolLoopScriptRejectsInvalidAttemptsBeforeSetup(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 0, "zero", "", "", "")
	if result.exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; output:\n%s", result.exitCode, result.output)
	}
	if !strings.Contains(result.output, "must be a positive integer") {
		t.Fatalf("output = %q, want attempts guidance", result.output)
	}
	if result.composeLog != "" {
		t.Fatalf("compose ran after invalid attempts: %q", result.composeLog)
	}
}

func TestVerifyToolLoopScriptPropagatesCustomOllamaURLToCompose(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 0, "", "", "http://127.0.0.1:2244", "")
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if got := strings.TrimSpace(result.composeEnvLog); got != "http://host.docker.internal:2244" {
		t.Fatalf("Compose OLLAMA_BASE_URL = %q", got)
	}
}

func TestVerifyToolLoopScriptMapsCallerOllamaURLForCompose(t *testing.T) {
	result := runVerifyToolLoopHarness(t, 0, 0, 0, 0, "", "http://localhost:2244", "", "")
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if got := strings.TrimSpace(result.composeEnvLog); got != "http://host.docker.internal:2244" {
		t.Fatalf("Compose OLLAMA_BASE_URL = %q", got)
	}
}

func TestVerifyToolLoopScriptUsesExplicitContainerOllamaURL(t *testing.T) {
	result := runVerifyToolLoopHarness(
		t,
		0,
		0,
		0,
		0,
		"",
		"",
		"http://localhost:2244",
		"http://ollama.internal:11434",
	)
	if result.exitCode != 0 {
		t.Fatalf("exit code = %d; output:\n%s", result.exitCode, result.output)
	}
	if got := strings.TrimSpace(result.composeEnvLog); got != "http://ollama.internal:11434" {
		t.Fatalf("Compose OLLAMA_BASE_URL = %q", got)
	}
}

type verifyHarnessResult struct {
	exitCode      int
	output        string
	composeLog    string
	curlLog       string
	composeEnvLog string
}

type verifyHarnessOptions struct {
	goBuildExit int
	ollamaModel string
}

func runVerifyToolLoopHarness(
	t *testing.T,
	initExit,
	composeUpExit,
	clientExit,
	curlShowExit int,
	attempts string,
	ollamaURL string,
	verifyURL string,
	containerURL string,
	options ...verifyHarnessOptions,
) verifyHarnessResult {
	t.Helper()
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(scriptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("verify-tool-loop.sh")
	if err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(scriptsDir, "verify-tool-loop.sh"), string(source))
	writeExecutable(t, filepath.Join(scriptsDir, "init.sh"), "#!/usr/bin/env bash\nexit \"$VERIFY_INIT_EXIT\"\n")
	writeExecutable(t, filepath.Join(scriptsDir, "compose.sh"), `#!/usr/bin/env bash
	printf '%s\n' "$*" >> "$VERIFY_COMPOSE_LOG"
	if [[ "$1" == "up" ]]; then
	  printf '%s\n' "$OLLAMA_BASE_URL" > "$VERIFY_COMPOSE_ENV_LOG"
	  exit "$VERIFY_COMPOSE_UP_EXIT"
	fi
	exit 0
	`)
	writeExecutable(t, filepath.Join(binDir, "curl"), `#!/usr/bin/env bash
	printf '%s\n' "$*" >> "$VERIFY_CURL_LOG"
	if [[ "${*: -1}" == */api/show ]]; then
	  exit "$VERIFY_CURL_SHOW_EXIT"
	fi
	exit 0
	`)
	writeExecutable(t, filepath.Join(binDir, "go"), `#!/usr/bin/env bash
	if [[ "$1" != "build" ]]; then
	  exit 99
	fi
	if [[ "$VERIFY_GO_BUILD_EXIT" != "0" ]]; then
	  exit "$VERIFY_GO_BUILD_EXIT"
	fi
	shift
	while [[ "$1" != "-o" ]]; do shift; done
	client_bin="$2"
	printf '%s\n' '#!/usr/bin/env bash' \
	  'if [[ "$1" == "-health-only" ]]; then exit 0; fi' \
	  'exit "$VERIFY_CLIENT_EXIT"' > "$client_bin"
	chmod +x "$client_bin"
	`)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(
		"TURING_CLIENT_API_KEY=token\nOLLAMA_BASE_URL=http://host.docker.internal:11434\nOLLAMA_MODEL=qwen2.5:7b\n",
	), 0600); err != nil {
		t.Fatal(err)
	}

	composeLog := filepath.Join(root, "compose.log")
	composeEnvLog := filepath.Join(root, "compose-env.log")
	curlLog := filepath.Join(root, "curl.log")
	cmd := exec.Command("bash", filepath.Join(scriptsDir, "verify-tool-loop.sh"))
	var opts verifyHarnessOptions
	if len(options) > 0 {
		opts = options[0]
	}
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TURING_VERIFY_MODEL=",
		"OLLAMA_MODEL="+opts.ollamaModel,
		"TURING_VERIFY_OLLAMA_URL="+verifyURL,
		"TURING_VERIFY_OLLAMA_CONTAINER_URL="+containerURL,
		"OLLAMA_BASE_URL="+ollamaURL,
		"VERIFY_COMPOSE_LOG="+composeLog,
		"VERIFY_COMPOSE_ENV_LOG="+composeEnvLog,
		"VERIFY_CURL_LOG="+curlLog,
		"VERIFY_COMPOSE_UP_EXIT="+strconv.Itoa(composeUpExit),
		"VERIFY_CLIENT_EXIT="+strconv.Itoa(clientExit),
		"VERIFY_CURL_SHOW_EXIT="+strconv.Itoa(curlShowExit),
		"VERIFY_INIT_EXIT="+strconv.Itoa(initExit),
		"VERIFY_GO_BUILD_EXIT="+strconv.Itoa(opts.goBuildExit),
		"TURING_VERIFY_ATTEMPTS="+attempts,
	)
	output, runErr := cmd.CombinedOutput()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatal(runErr)
		}
		exitCode = exitErr.ExitCode()
	}
	composeData, _ := os.ReadFile(composeLog)
	composeEnvData, _ := os.ReadFile(composeEnvLog)
	curlData, _ := os.ReadFile(curlLog)
	return verifyHarnessResult{
		exitCode:      exitCode,
		output:        string(output),
		composeLog:    string(composeData),
		composeEnvLog: string(composeEnvData),
		curlLog:       string(curlData),
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}
