package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRefreshesStaleAutomaticHostIdentity(t *testing.T) {
	env := runInit(t, "501", "20", `
HOST_UID=2000
HOST_GID=2000
`)

	assertEnvValue(t, env, "HOST_UID", "501")
	assertEnvValue(t, env, "HOST_GID", "20")
}

func TestInitFallsBackForRootOrInvalidHostIdentity(t *testing.T) {
	env := runInit(t, "0", "not-a-number", `
HOST_UID=2000
HOST_GID=2000
`)

	assertEnvValue(t, env, "HOST_UID", "1000")
	assertEnvValue(t, env, "HOST_GID", "1000")
}

func TestInitPreservesValidExplicitHostIdentity(t *testing.T) {
	env := runInit(t, "501", "20", `
HOST_IDENTITY_MODE=manual
HOST_UID=1234
HOST_GID=2345
`)

	assertEnvValue(t, env, "HOST_UID", "1234")
	assertEnvValue(t, env, "HOST_GID", "2345")
}

func TestInitReplacesInvalidExplicitHostIdentityWithSafeFallback(t *testing.T) {
	env := runInit(t, "501", "20", `
HOST_IDENTITY_MODE=manual
HOST_UID=-1
HOST_GID=0
`)

	assertEnvValue(t, env, "HOST_UID", "1000")
	assertEnvValue(t, env, "HOST_GID", "1000")
}

func runInit(t *testing.T, uid, gid, identityConfig string) string {
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

	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fakeID := "#!/bin/sh\ncase \"$1\" in\n-u) printf '%s\\n' '" + uid + "' ;;\n-g) printf '%s\\n' '" + gid + "' ;;\n*) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(binDir, "id"), []byte(fakeID), 0700); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("bash", scriptPath)
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init.sh failed: %v\n%s", err, output)
	}
	updated, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return string(updated)
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
