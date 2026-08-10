package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInitRefreshesStaleAutomaticHostIdentity(t *testing.T) {
	result := runInit(t, "501", "20", `
HOST_UID=2000
HOST_GID=2000
`)

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
}

func TestInitUsesCurrentNonRootIdentityInsteadOfConfiguredOverrides(t *testing.T) {
	result := runInit(t, "501", "20", `
HOST_IDENTITY_MODE=manual
HOST_UID=1234
HOST_GID=2345
`)

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
	assertChownCalls(t, result)
}

func TestInitRejectsRootOrInvalidCurrentIdentityBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		gid  string
	}{
		{name: "root", uid: "0", gid: "0"},
		{name: "zero padded root", uid: "00", gid: "20"},
		{name: "zero padded positive", uid: "01", gid: "20"},
		{name: "negative", uid: "-1", gid: "20"},
		{name: "nonnumeric", uid: "not-a-number", gid: "20"},
		{name: "above portable maximum", uid: "2147483648", gid: "20"},
		{name: "far above portable maximum", uid: "99999999999999999999", gid: "20"},
		{name: "invalid group", uid: "20", gid: "01"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInit(t, test.uid, test.gid, "", 0)
			if result.err == nil {
				t.Fatalf("init.sh succeeded for current identity %s:%s; output:\n%s", test.uid, test.gid, result.output)
			}
			if !strings.Contains(result.output, "must be run by a non-root host user with canonical UID/GID values") {
				t.Fatalf("failure did not explain the host identity requirement:\n%s", result.output)
			}
			if _, err := os.Lstat(result.sandbox); !os.IsNotExist(err) {
				t.Fatalf("sandbox was mutated before identity rejection: %v", err)
			}
			assertChownCalls(t, result)
		})
	}
}

func TestInitCreatesRealOwnedWritableTraversableSandboxWithoutChown(t *testing.T) {
	result := runInit(t, "501", "20", "")

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
	assertChownCalls(t, result)
	info, err := os.Lstat(result.sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("sandbox mode = %v, want a real directory", info.Mode())
	}
}

func TestInitRejectsPreExistingSandboxSymlink(t *testing.T) {
	var target string
	result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
		target = filepath.Join(root, "outside")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(root, "sandbox")); err != nil {
			t.Fatal(err)
		}
	})

	if result.err == nil {
		t.Fatalf("init.sh succeeded; output:\n%s", result.output)
	}
	if !strings.Contains(result.output, "sandbox must be a real directory, not a symlink") {
		t.Fatalf("failure did not explain the sandbox symlink rejection:\n%s", result.output)
	}
	if strings.Contains(result.output, "backend initialized") {
		t.Fatalf("init.sh claimed readiness for a symlinked sandbox:\n%s", result.output)
	}
	assertChownCalls(t, result)
	if info, err := os.Lstat(result.sandbox); err != nil {
		t.Fatalf("sandbox symlink was removed: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("sandbox symlink was replaced: mode=%v", info.Mode())
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatalf("symlink target was removed: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("symlink target was mutated: mode=%v", info.Mode())
	}
}

func TestInitRejectsInaccessibleLegacySandboxEntries(t *testing.T) {
	tests := []struct {
		name       string
		create     func(t *testing.T, sandbox string)
		wantOutput string
	}{
		{
			name: "directory",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "locked")
				if err := os.Mkdir(path, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0700) })
			},
			wantOutput: "legacy sandbox directory is not readable, writable, and traversable: locked",
		},
		{
			name: "file",
			create: func(t *testing.T, sandbox string) {
				path := filepath.Join(sandbox, "locked.txt")
				if err := os.WriteFile(path, []byte("legacy"), 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0600) })
			},
			wantOutput: "legacy sandbox file is not readable and writable: locked.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := executeInitWithSetup(t, "501", "20", "", 0, func(t *testing.T, root string) {
				sandbox := filepath.Join(root, "sandbox")
				if err := os.Mkdir(sandbox, 0700); err != nil {
					t.Fatal(err)
				}
				test.create(t, sandbox)
			})

			if result.err == nil {
				t.Fatalf("init.sh succeeded; output:\n%s", result.output)
			}
			if !strings.Contains(result.output, test.wantOutput) {
				t.Fatalf("failure did not identify the inaccessible entry:\n%s", result.output)
			}
			if strings.Contains(result.output, "backend initialized") {
				t.Fatalf("init.sh claimed readiness with inaccessible legacy content:\n%s", result.output)
			}
			assertChownCalls(t, result)
		})
	}
}

type initResult struct {
	sandbox  string
	env      string
	output   string
	chownLog string
	err      error
}

func runInit(t *testing.T, uid, gid, identityConfig string) initResult {
	t.Helper()
	result := executeInit(t, uid, gid, identityConfig, 0)
	if result.err != nil {
		t.Fatalf("init.sh failed: %v\n%s", result.err, result.output)
	}
	return result
}

func executeInit(t *testing.T, uid, gid, identityConfig string, chownExit int) initResult {
	t.Helper()
	return executeInitWithSetup(t, uid, gid, identityConfig, chownExit, nil)
}

func executeInitWithSetup(t *testing.T, uid, gid, identityConfig string, chownExit int, setup func(*testing.T, string)) initResult {
	t.Helper()
	root := t.TempDir()
	scriptsDir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scriptsDir, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("init.sh")
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptsDir, "init.sh")
	if err := os.WriteFile(scriptPath, script, 0700); err != nil {
		t.Fatal(err)
	}
	env := "TURING_CLIENT_API_KEY=client\n" +
		"TURING_INTERNAL_TOKEN=internal\n" +
		"MCP_SYSTEM_TOKEN_GENERAL=system\n" +
		"MCP_FILES_TOKEN_GENERAL=files\n" +
		"TURING_APPROVAL_JWT_SECRET=approval\n" +
		strings.TrimSpace(identityConfig) + "\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(t, root)
	}

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeID := "#!/bin/sh\ncase \"$1\" in\n-u) printf '%s\\n' '" + uid + "' ;;\n-g) printf '%s\\n' '" + gid + "' ;;\n*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "id"), []byte(fakeID), 0700); err != nil {
		t.Fatal(err)
	}
	chownLog := filepath.Join(root, "chown.log")
	fakeChown := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CHOWN_LOG\"\nexit \"${CHOWN_EXIT:-0}\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "chown"), []byte(fakeChown), 0700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", scriptPath)
	command.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"CHOWN_LOG="+chownLog,
		"CHOWN_EXIT="+strconv.Itoa(chownExit),
	)
	output, commandErr := command.CombinedOutput()
	updated, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return initResult{
		sandbox:  filepath.Join(root, "sandbox"),
		env:      string(updated),
		output:   string(output),
		chownLog: chownLog,
		err:      commandErr,
	}
}

func assertChownCalls(t *testing.T, result initResult, want ...string) {
	t.Helper()
	data, err := os.ReadFile(result.chownLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	wantText := strings.Join(want, "\n")
	if got != wantText {
		t.Fatalf("chown calls = %q, want %q", got, wantText)
	}
}

func assertEnvValue(t *testing.T, env, name, want string) {
	t.Helper()
	prefix := name + "="
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, prefix) {
			if got := strings.TrimPrefix(line, prefix); got != want {
				t.Fatalf("%s = %q, want %q\n.env:\n%s", name, got, want, env)
			}
			return
		}
	}
	t.Fatalf("%s missing from .env:\n%s", name, env)
}
