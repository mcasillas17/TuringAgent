package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The orchestrator opens the vault at `/memory`, which is a path inside one
// container. The desktop client used to show that string to the user as the
// folder to open in Obsidian — a place they cannot go, cannot find, and cannot
// search for.
//
// So Compose passes a second, display-only value: the host directory the vault
// is bind-mounted from, as an absolute path on the user's own disk. init.sh
// computes it, because init.sh is the only thing that knows where the checkout
// actually is; MEMORY_ROOT stays `/memory` and everything that opens a file
// still goes through it.

// TestComposePassesTheHostVaultPathForDisplay holds the compose file to sending
// both, and to keeping them distinct.
func TestComposePassesTheHostVaultPathForDisplay(t *testing.T) {
	content, err := os.ReadFile("../infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	compose := string(content)

	if !strings.Contains(compose, "MEMORY_ROOT: /memory") {
		t.Fatal("MEMORY_ROOT is no longer the container path the vault is opened at")
	}
	if !strings.Contains(compose, "MEMORY_DISPLAY_ROOT: ${MEMORY_DISPLAY_ROOT:?") {
		t.Fatal("docker-compose.yml does not require the host vault path for display")
	}
	// Required, in the mould of the other host-identity values, rather than
	// defaulted: a fallback would be a container path silently presented as the
	// user's folder on every install whose .env predates the setting, and
	// failing to launch is the louder and more honest of the two answers.
	if strings.Contains(compose, "MEMORY_DISPLAY_ROOT: ${MEMORY_DISPLAY_ROOT:-") {
		t.Fatal("MEMORY_DISPLAY_ROOT has a fallback, so a stale .env would show a container path")
	}
}

// The template carries the name so an operator editing .env by hand can see
// what it is for, and carries it empty so nothing ships a path from somebody
// else's machine.
func TestEnvExampleDocumentsTheDisplayVaultPath(t *testing.T) {
	content, err := os.ReadFile("../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	example := string(content)
	if !strings.Contains(example, "\nMEMORY_DISPLAY_ROOT=\n") {
		t.Fatal(".env.example does not carry an empty MEMORY_DISPLAY_ROOT")
	}
}

// init.sh writes it, because it is the only thing that knows where the checkout
// is. The value is the canonical absolute path of turing-backend/memory — the
// same directory the compose file binds — and it is rewritten on every run, so
// a checkout that moves is followed rather than remembered wrongly.
func TestInitWritesTheCanonicalHostVaultPath(t *testing.T) {
	result := runInit(t, "501", "20", "")

	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	assertEnvValue(t, result.env, "MEMORY_DISPLAY_ROOT", canonical)
}

// A stale value from a previous checkout is replaced rather than kept: the
// point of the setting is to name a folder that is there.
func TestInitReplacesAStaleHostVaultPath(t *testing.T) {
	result := runInit(t, "501", "20", "\nMEMORY_DISPLAY_ROOT=/somewhere/that/moved\n")

	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	assertEnvValue(t, result.env, "MEMORY_DISPLAY_ROOT", canonical)
}

// Running it twice changes nothing, and touches nothing else. An operator's own
// settings live in the same file, and a run that rewrote them would be a reason
// not to run it.
func TestInitIsIdempotentAboutTheHostVaultPath(t *testing.T) {
	first := runInit(t, "501", "20", "\nOLLAMA_MODEL=llama3.2\nMEMORY_DISPLAY_ROOT=/somewhere/that/moved\n")
	assertEnvValue(t, first.env, "OLLAMA_MODEL", "llama3.2")

	second := rerunInit(t, first)
	if second.err != nil {
		t.Fatalf("second init.sh run failed: %v\n%s", second.err, second.output)
	}
	assertEnvValue(t, second.env, "OLLAMA_MODEL", "llama3.2")
	if envValue(t, second.env, "MEMORY_DISPLAY_ROOT") != envValue(t, first.env, "MEMORY_DISPLAY_ROOT") {
		t.Fatal("a second run moved the host vault path")
	}
	if strings.Count(second.env, "MEMORY_DISPLAY_ROOT=") != 1 {
		t.Fatalf("MEMORY_DISPLAY_ROOT was appended more than once:\n%s", second.env)
	}
	// And the secrets the first run minted are the same secrets. A rewrite that
	// regenerated them would lock out a client that already has the key.
	for _, name := range []string{"TURING_CLIENT_API_KEY", "TURING_RUNTIME_TOKEN", "TURING_INTEGRATION_KEY"} {
		if envValue(t, second.env, name) != envValue(t, first.env, name) {
			t.Fatalf("a second run changed %s", name)
		}
	}
}

// A checkout under a path with a space is ordinary on both desktop platforms
// this ships on. The value has to survive being written into .env and read back
// out whole; a helper that word-splits would produce a truncated path that
// looks almost right.
func TestInitHandlesAHostVaultPathWithSpaces(t *testing.T) {
	result := executeInitInDirectory(t, "501", "20", "", "My Turing Checkout")

	if result.err != nil {
		t.Fatalf("init.sh failed under a path with spaces: %v\n%s", result.err, result.output)
	}
	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	if !strings.Contains(canonical, " ") {
		t.Fatalf("this test needs a path with a space, got %q", canonical)
	}
	assertEnvValue(t, result.env, "MEMORY_DISPLAY_ROOT", canonical)
}

// The written value is a secret-free operator setting and belongs in the file
// like any other. What it must not do is arrive by being printed: init.sh's
// output is what a person pastes into an issue.
func TestInitDoesNotPrintTheHostVaultPath(t *testing.T) {
	result := runInit(t, "501", "20", "")

	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	if strings.Contains(result.output, canonical) {
		t.Fatalf("init.sh printed the vault path:\n%s", result.output)
	}
}

// The value goes into .env through a sed expression, and a checkout path is not
// a hex secret: & means "the whole matched line" in a sed replacement and |
// ends the expression. Both are legal in a directory name on every platform
// this ships on, and either one silently produces a path that looks almost
// right.
func TestInitHandlesAHostVaultPathWithSedMetacharacters(t *testing.T) {
	for _, directory := range []string{"turing & co", "turing|checkout", "turing\\checkout"} {
		result := executeInitInDirectory(t, "501", "20", "\nMEMORY_DISPLAY_ROOT=/somewhere/that/moved\n", directory)

		if result.err != nil {
			t.Fatalf("init.sh failed under %q: %v\n%s", directory, result.err, result.output)
		}
		canonical, err := filepath.EvalSymlinks(result.memory)
		if err != nil {
			t.Fatalf("resolve the provisioned vault: %v", err)
		}
		assertEnvValue(t, result.env, "MEMORY_DISPLAY_ROOT", canonical)
	}
}
