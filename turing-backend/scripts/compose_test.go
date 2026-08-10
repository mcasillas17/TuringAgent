package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeLaunchUsesCanonicalCurrentIdentityInsteadOfEnvironment(t *testing.T) {
	result := executeCompose(t, "501", "20", "0", "999", "up", "--build")
	if result.err != nil {
		t.Fatalf("compose.sh failed: %v\n%s", result.err, result.output)
	}
	if got := strings.TrimSpace(result.dockerLog); got != "HOST_UID=501 HOST_GID=20\ncompose --env-file .env -f infra/docker-compose.yml up --build" {
		t.Fatalf("docker invocation = %q", got)
	}
}

func TestComposeLaunchAllowsRecoveryDownWithoutEnvFile(t *testing.T) {
	result := executeComposeWithEnv(t, false, "501", "20", "0", "999", "down", "--remove-orphans")
	if result.err != nil {
		t.Fatalf("compose.sh failed recovery down: %v\n%s", result.err, result.output)
	}
	if got := strings.TrimSpace(result.dockerLog); got != "HOST_UID=501 HOST_GID=20\ncompose -f infra/docker-compose.yml down --remove-orphans" {
		t.Fatalf("docker invocation = %q", got)
	}
}

func TestComposeLaunchRejectsRootOrInvalidCurrentIdentity(t *testing.T) {
	for _, identity := range []struct {
		name string
		uid  string
		gid  string
	}{
		{name: "root", uid: "0", gid: "0"},
		{name: "zero padded", uid: "0501", gid: "20"},
		{name: "nonnumeric", uid: "bad", gid: "20"},
		{name: "out of range", uid: "2147483648", gid: "20"},
		{name: "invalid group", uid: "501", gid: "0"},
	} {
		t.Run(identity.name, func(t *testing.T) {
			result := executeCompose(t, identity.uid, identity.gid, "501", "20", "up")
			if result.err == nil {
				t.Fatalf("compose.sh succeeded for %s:%s", identity.uid, identity.gid)
			}
			if !strings.Contains(result.output, "must be run by a non-root host user with canonical UID/GID values") {
				t.Fatalf("failure did not explain identity requirement:\n%s", result.output)
			}
			if result.dockerLog != "" {
				t.Fatalf("docker was called after failed preflight:\n%s", result.dockerLog)
			}
		})
	}
}

func TestRepositoryComposeLaunchersUseValidatedWrapper(t *testing.T) {
	for _, name := range []string{"dev.sh", "reset.sh", "smoke-grpc.sh"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		script := string(data)
		if strings.Contains(script, "docker compose") {
			t.Errorf("%s bypasses scripts/compose.sh", name)
		}
		if !strings.Contains(script, "scripts/compose.sh") {
			t.Errorf("%s does not use scripts/compose.sh", name)
		}
		if name == "reset.sh" && strings.Contains(script, "if [[ -f .env ]]") {
			t.Error("reset.sh skips compose down when .env is missing")
		}
	}
}

func TestResetRejectsRootBeforeDeletingLocalState(t *testing.T) {
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.Mkdir(scriptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compose.sh", "init.sh", "reset.sh"} {
		copyScript(t, name, filepath.Join(scriptsDir, name))
	}
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("KEEP=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dataDir, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeID := "#!/bin/sh\ncase \"$1\" in\n-u|-g) printf '0\\n' ;;\n*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "id"), []byte(fakeID), 0700); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(root, "docker.log")
	fakeDocker := "#!/bin/sh\nprintf 'called\\n' >> \"$DOCKER_LOG\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDocker), 0700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", filepath.Join(scriptsDir, "reset.sh"))
	command.Stdin = strings.NewReader("RESET\n")
	command.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"DOCKER_LOG="+dockerLog,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("reset.sh succeeded as root:\n%s", output)
	}
	if !strings.Contains(string(output), "must be run by a non-root host user with canonical UID/GID values") {
		t.Fatalf("reset failure did not explain identity requirement:\n%s", output)
	}
	if env, readErr := os.ReadFile(envPath); readErr != nil || string(env) != "KEEP=1\n" {
		t.Fatalf(".env was mutated before root rejection: content=%q err=%v", env, readErr)
	}
	if data, readErr := os.ReadFile(marker); readErr != nil || string(data) != "keep" {
		t.Fatalf("data was mutated before root rejection: content=%q err=%v", data, readErr)
	}
	if _, statErr := os.Stat(dockerLog); !os.IsNotExist(statErr) {
		t.Fatalf("docker was called before root rejection: %v", statErr)
	}
}

type composeResult struct {
	output    string
	dockerLog string
	err       error
}

func executeCompose(t *testing.T, uid, gid, exportedUID, exportedGID string, args ...string) composeResult {
	t.Helper()
	return executeComposeWithEnv(t, true, uid, gid, exportedUID, exportedGID, args...)
}

func executeComposeWithEnv(t *testing.T, withEnv bool, uid, gid, exportedUID, exportedGID string, args ...string) composeResult {
	t.Helper()
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.Mkdir(scriptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptsDir, "compose.sh")
	copyScript(t, "compose.sh", scriptPath)
	if withEnv {
		if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TURING_CLIENT_API_KEY=client\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeID := "#!/bin/sh\ncase \"$1\" in\n-u) printf '%s\\n' '" + uid + "' ;;\n-g) printf '%s\\n' '" + gid + "' ;;\n*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "id"), []byte(fakeID), 0700); err != nil {
		t.Fatal(err)
	}
	dockerLog := filepath.Join(root, "docker.log")
	fakeDocker := "#!/bin/sh\nprintf 'HOST_UID=%s HOST_GID=%s\\n' \"$HOST_UID\" \"$HOST_GID\" > \"$DOCKER_LOG\"\nprintf '%s\\n' \"$*\" >> \"$DOCKER_LOG\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDocker), 0700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", append([]string{scriptPath}, args...)...)
	command.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"DOCKER_LOG="+dockerLog,
		"HOST_UID="+exportedUID,
		"HOST_GID="+exportedGID,
	)
	output, commandErr := command.CombinedOutput()
	log, err := os.ReadFile(dockerLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return composeResult{output: string(output), dockerLog: string(log), err: commandErr}
}

func copyScript(t *testing.T, source, destination string) {
	t.Helper()
	script, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, script, 0700); err != nil {
		t.Fatal(err)
	}
}
