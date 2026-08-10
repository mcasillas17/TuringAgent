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

func TestInitFallsBackForRootOrInvalidHostIdentity(t *testing.T) {
	result := runInit(t, "0", "not-a-number", `
HOST_UID=2000
HOST_GID=2000
`)

	assertEnvValue(t, result.env, "HOST_UID", "1000")
	assertEnvValue(t, result.env, "HOST_GID", "1000")
	assertChownCalls(t, result, "1000:1000 "+result.sandbox)
}

func TestInitPreservesValidExplicitHostIdentity(t *testing.T) {
	result := runInit(t, "0", "0", `
HOST_IDENTITY_MODE=manual
HOST_UID=1234
HOST_GID=2345
`)

	assertEnvValue(t, result.env, "HOST_UID", "1234")
	assertEnvValue(t, result.env, "HOST_GID", "2345")
	assertChownCalls(t, result, "1234:2345 "+result.sandbox)
}

func TestInitRejectsNoncanonicalOrOutOfRangeManualIDs(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		gid  string
	}{
		{name: "zero padded zero", uid: "00", gid: "20"},
		{name: "zero padded positive", uid: "01", gid: "20"},
		{name: "zero", uid: "0", gid: "20"},
		{name: "negative", uid: "-1", gid: "20"},
		{name: "above portable maximum", uid: "2147483648", gid: "20"},
		{name: "far above portable maximum", uid: "99999999999999999999", gid: "20"},
		{name: "invalid group", uid: "20", gid: "01"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runInit(t, "0", "0", `
HOST_IDENTITY_MODE=manual
HOST_UID=`+test.uid+`
HOST_GID=`+test.gid+`
`)

			assertEnvValue(t, result.env, "HOST_UID", "1000")
			assertEnvValue(t, result.env, "HOST_GID", "1000")
			assertChownCalls(t, result, "1000:1000 "+result.sandbox)
		})
	}
}

func TestInitAcceptsPortableMaximumID(t *testing.T) {
	result := runInit(t, "0", "0", `
HOST_IDENTITY_MODE=manual
HOST_UID=2147483647
HOST_GID=2147483647
`)

	assertEnvValue(t, result.env, "HOST_UID", "2147483647")
	assertEnvValue(t, result.env, "HOST_GID", "2147483647")
	assertChownCalls(t, result, "2147483647:2147483647 "+result.sandbox)
}

func TestInitDoesNotChownForValidNonRootHostIdentity(t *testing.T) {
	result := runInit(t, "501", "20", "")

	assertEnvValue(t, result.env, "HOST_UID", "501")
	assertEnvValue(t, result.env, "HOST_GID", "20")
	assertChownCalls(t, result)
}

func TestInitFailsWhenNonRootFallbackCannotBeProvisioned(t *testing.T) {
	result := executeInit(t, "501", "not-a-number", "", 0)

	if result.err == nil {
		t.Fatalf("init.sh succeeded; output:\n%s", result.output)
	}
	if !strings.Contains(result.output, "cannot safely provision sandbox ownership") {
		t.Fatalf("failure did not explain unsafe ownership provisioning:\n%s", result.output)
	}
	if strings.Contains(result.output, "backend initialized") {
		t.Fatalf("init.sh claimed readiness after ownership failure:\n%s", result.output)
	}
	assertChownCalls(t, result)
}

func TestInitFailsClearlyWhenRootCannotChownSandbox(t *testing.T) {
	result := executeInit(t, "0", "0", "", 1)

	if result.err == nil {
		t.Fatalf("init.sh succeeded; output:\n%s", result.output)
	}
	if !strings.Contains(result.output, "failed to set sandbox ownership to 1000:1000") {
		t.Fatalf("failure did not explain chown failure:\n%s", result.output)
	}
	if strings.Contains(result.output, "backend initialized") {
		t.Fatalf("init.sh claimed readiness after chown failure:\n%s", result.output)
	}
	assertChownCalls(t, result, "1000:1000 "+result.sandbox)
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
