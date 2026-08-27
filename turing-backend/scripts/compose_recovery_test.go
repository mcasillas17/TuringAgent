package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Teardown has to work on a broken install. That is the whole point of it: the
// containers are up, something is wrong, and `down` is what a person reaches
// for — often on a checkout whose .env predates a setting, or has been emptied,
// or was never written at all. `reset.sh` reaches for it too, and then deletes
// the local data, so a `down` that refuses is a `down` that leaves containers
// running over data that is about to disappear.
//
// So no variable the recovery path cannot supply may be *required* by the
// compose file. Requiring the display root there made teardown depend on a
// value only init.sh can compute; the requirement lives in compose.sh now,
// where it gates the paths that actually start services and nothing else.

// composeRequiredVariables lists the ${NAME:?...} interpolations the compose
// file will not resolve without.
func composeRequiredVariables(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile("../infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):\?`)
	seen := map[string]bool{}
	names := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		// A comment can spell the syntax it is explaining, and a rule that
		// counted those would be a rule nobody could document.
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			if seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			names = append(names, match[1])
		}
	}
	return names
}

// TestComposeRequiresOnlyWhatTeardownItselfSupplies is the static guarantee: a
// required variable is allowed only if compose.sh puts it in the environment on
// every invocation, teardown included.
func TestComposeRequiresOnlyWhatTeardownItselfSupplies(t *testing.T) {
	supplied := map[string]bool{"HOST_UID": true, "HOST_GID": true}
	for _, name := range composeRequiredVariables(t) {
		if !supplied[name] {
			t.Fatalf("docker-compose.yml requires %s, which nothing supplies on the recovery path", name)
		}
	}
	content, err := os.ReadFile("../infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "MEMORY_DISPLAY_ROOT: ${MEMORY_DISPLAY_ROOT:-}") {
		t.Fatal("the compose file no longer passes MEMORY_DISPLAY_ROOT through without requiring it")
	}
}

// And the same question asked of the tool that actually does the interpolating,
// because a guarantee about a file is a guarantee about what somebody thinks
// the file means. `config` resolves every variable and needs no daemon, so it
// is the exact failure `down` used to hit.
func TestComposeInterpolatesWithoutAHostVaultPath(t *testing.T) {
	requireDockerCompose(t)

	command := exec.Command("docker", "compose", "-f", "../infra/docker-compose.yml", "config")
	command.Env = append(cleanComposeEnvironment(), "HOST_UID=501", "HOST_GID=20")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("compose could not interpolate without MEMORY_DISPLAY_ROOT: %v\n%s", err, output)
	}
}

// The requirement did not disappear; it moved to where a person can be told
// what to do about it, and where it cannot block a teardown.
func TestComposeLaunchRefusesAnUnusableHostVaultPath(t *testing.T) {
	for _, unusable := range []struct {
		name  string
		value string
	}{
		{name: "missing", value: ""},
		{name: "blank", value: "MEMORY_DISPLAY_ROOT=\n"},
		{name: "quoted blank", value: "MEMORY_DISPLAY_ROOT=''\n"},
		{name: "relative", value: "MEMORY_DISPLAY_ROOT='turing-backend/memory'\n"},
		{name: "traversal", value: "MEMORY_DISPLAY_ROOT='/srv/turing/../memory'\n"},
		{name: "trailing slash", value: "MEMORY_DISPLAY_ROOT='/srv/turing/memory/'\n"},
		// A bare or double-quoted value is interpolated by Compose, so a
		// legacy or hand-edited line naming a variable is a value that reaches
		// the orchestrator — and the Memory page — as whatever that variable
		// holds. It looks like a clean absolute path here and is a secret
		// there.
		{name: "interpolating", value: "MEMORY_DISPLAY_ROOT=/srv/${TURING_INTEGRATION_KEY}/memory\n"},
		{name: "double quoted interpolating", value: "MEMORY_DISPLAY_ROOT=\"/srv/$TURING_INTEGRATION_KEY/memory\"\n"},
	} {
		t.Run(unusable.name, func(t *testing.T) {
			result := executeComposeWithSetup(t, true, "501", "20", "501", "20",
				func(t *testing.T, root string) {
					writeComposeEnv(t, root, "TURING_CLIENT_API_KEY=client\n"+unusable.value)
				}, "up")
			if result.err == nil {
				t.Fatalf("compose.sh launched without a usable vault path; docker log:\n%s", result.dockerLog)
			}
			if !strings.Contains(result.output, "MEMORY_DISPLAY_ROOT") {
				t.Fatalf("failure does not name the setting to fix:\n%s", result.output)
			}
			if result.dockerLog != "" {
				t.Fatalf("docker was called before the vault path was checked:\n%s", result.dockerLog)
			}
		})
	}
}

// Every path that only stops things runs on the .env a broken install actually
// has: one that predates the setting, or carries it empty.
func TestComposeRecoveryRunsWithoutAHostVaultPath(t *testing.T) {
	for _, recovery := range []string{"down", "stop", "rm", "kill"} {
		for _, env := range []struct {
			name    string
			content string
		}{
			{name: "legacy", content: "TURING_CLIENT_API_KEY=client\n"},
			{name: "blank", content: "TURING_CLIENT_API_KEY=client\nMEMORY_DISPLAY_ROOT=\n"},
		} {
			t.Run(recovery+"/"+env.name, func(t *testing.T) {
				result := executeComposeWithSetup(t, true, "501", "20", "501", "20",
					func(t *testing.T, root string) {
						writeComposeEnv(t, root, env.content)
					}, recovery)
				if result.err != nil {
					t.Fatalf("compose.sh refused %s on a stale install: %v\n%s", recovery, result.err, result.output)
				}
				if !strings.Contains(result.dockerLog, recovery) {
					t.Fatalf("docker was never asked to %s: %q", recovery, result.dockerLog)
				}
			})
		}
	}
}

// A teardown must not be blocked by the bind-source checks either. They exist
// to stop a launch mounting something unsafe; refusing to stop a container over
// one is refusing to fix the very thing it is complaining about.
func TestComposeRecoveryRunsOverAnUnsafeBindSource(t *testing.T) {
	result := executeComposeWithSetup(t, true, "501", "20", "501", "20",
		func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, "memory")); err != nil {
				t.Fatal(err)
			}
		}, "down", "--remove-orphans")
	if result.err != nil {
		t.Fatalf("compose.sh refused teardown over a missing vault: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.dockerLog, "down --remove-orphans") {
		t.Fatalf("docker was never asked to tear down: %q", result.dockerLog)
	}
}

// writeComposeEnv replaces the .env the compose harness seeds, so a test can
// say exactly what a stale or hand-edited install looks like.
func writeComposeEnv(t *testing.T, root string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// requireDockerCompose skips a test that needs the real interpolator. It needs
// no daemon — `config` resolves variables and stops — but it does need the
// plugin to be installed.
func requireDockerCompose(t *testing.T) {
	t.Helper()
	command := exec.Command("docker", "compose", "version")
	command.Env = cleanComposeEnvironment()
	if err := command.Run(); err != nil {
		t.Skipf("docker compose is not available here: %v", err)
	}
}

// cleanComposeEnvironment strips every variable the compose file interpolates
// out of the ambient environment, so what a test measures is what the .env and
// the command line supply rather than whatever the developer running it has
// exported.
func cleanComposeEnvironment() []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch {
		case strings.HasPrefix(name, "TURING_"),
			strings.HasPrefix(name, "MEMORY_"),
			strings.HasPrefix(name, "MCP_"),
			strings.HasPrefix(name, "OLLAMA_"),
			strings.HasPrefix(name, "OPENAI_"),
			strings.HasPrefix(name, "ORCHESTRATOR_"),
			strings.HasPrefix(name, "HOST_"),
			name == "DATABASE_PATH":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// Compose takes its own options before the subcommand, and a person tearing a
// broken install down may well pass one. Classifying `--project-name x down` as
// a launch would run the launch checks — and refuse the teardown over the very
// thing being fixed.
func TestComposeRecoveryRunsBehindComposeOptions(t *testing.T) {
	result := executeComposeWithSetup(t, true, "501", "20", "501", "20",
		func(t *testing.T, root string) {
			writeComposeEnv(t, root, "TURING_CLIENT_API_KEY=client\n")
			if err := os.RemoveAll(filepath.Join(root, "memory")); err != nil {
				t.Fatal(err)
			}
		}, "--project-name", "turing", "down", "--remove-orphans")
	if result.err != nil {
		t.Fatalf("compose.sh refused a teardown behind a compose option: %v\n%s", result.err, result.output)
	}
	if !strings.Contains(result.dockerLog, "down --remove-orphans") {
		t.Fatalf("docker was never asked to tear down: %q", result.dockerLog)
	}
}

// Compose reads the shell environment ahead of --env-file, so a value exported
// in the terminal wins over the one this script just validated. The stack must
// get the value that was checked, not whatever happens to be exported.
func TestComposeLaunchSendsTheValidatedHostVaultPath(t *testing.T) {
	root := t.TempDir()
	result := executeComposeInRoot(t, root, "501", "20", "501", "20",
		map[string]string{"MEMORY_DISPLAY_ROOT": "/somewhere/nobody/validated"}, "up")
	if result.err != nil {
		t.Fatalf("compose.sh failed to launch: %v\n%s", result.err, result.output)
	}
	want := "MEMORY_DISPLAY_ROOT=" + filepath.Join(root, "memory")
	if !strings.Contains(result.dockerLog, want) {
		t.Fatalf("docker was invoked with %q, want the validated path %q", result.dockerLog, want)
	}
	if strings.Contains(result.dockerLog, "/somewhere/nobody/validated") {
		t.Fatalf("the inherited value reached compose: %q", result.dockerLog)
	}
}

// A .env left by an older install can hold anything, including a line Compose
// itself refuses to interpolate. Teardown must not depend on that file being
// readable: it is being run precisely because the install is broken.
func TestComposeRecoverySurvivesAnEnvFileComposeWillNotRead(t *testing.T) {
	result := executeComposeWithSetupIn(t, t.TempDir(), true, "501", "20", "501", "20",
		func(t *testing.T, root string) {
			writeComposeEnv(t, root,
				"TURING_CLIENT_API_KEY=client\nMEMORY_DISPLAY_ROOT=${NOTHING_SETS_THIS:?run init}\n")
		}, map[string]string{"DOCKER_REFUSE_ENV_FILE": "1"}, "down", "--remove-orphans")
	if result.err != nil {
		t.Fatalf("compose.sh gave up on teardown over an unreadable .env: %v\n%s", result.err, result.output)
	}
	if strings.Contains(result.dockerLog, "--env-file") {
		t.Fatalf("the retry still handed compose the .env it refused: %q", result.dockerLog)
	}
	if !strings.Contains(result.dockerLog, "down --remove-orphans") {
		t.Fatalf("docker was never asked to tear down: %q", result.dockerLog)
	}
}

// The retry must not be blocked by the same file, whichever way it was named,
// and must not carry a project name Compose would reject — a .env that failed
// interpolation is exactly the kind that holds one.
func TestComposeRecoveryRetryDropsTheEnvFileAndABadProjectName(t *testing.T) {
	result := executeComposeWithSetupIn(t, t.TempDir(), true, "501", "20", "501", "20",
		func(t *testing.T, root string) {
			writeComposeEnv(t, root,
				"TURING_CLIENT_API_KEY=client\nCOMPOSE_PROJECT_NAME=${NOTHING_SETS_THIS:?run init}\n")
		}, map[string]string{"DOCKER_REFUSE_ENV_FILE": "1"},
		"down", "--env-file", ".env", "--remove-orphans")
	if result.err != nil {
		t.Fatalf("compose.sh gave up on teardown: %v\n%s", result.err, result.output)
	}
	if strings.Contains(result.dockerLog, "--env-file") {
		t.Fatalf("the retry still handed compose the .env it refused: %q", result.dockerLog)
	}
	if strings.Contains(result.dockerLog, "--project-name") {
		t.Fatalf("the retry carried a project name compose would reject: %q", result.dockerLog)
	}
	if !strings.Contains(result.dockerLog, "down --remove-orphans") {
		t.Fatalf("docker was never asked to tear down: %q", result.dockerLog)
	}
}
