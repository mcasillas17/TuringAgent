package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The vault path is the one value in .env that is not a hex secret. It is a
// filesystem path, and a filesystem path can legally contain every character
// Compose's dotenv reader treats as syntax: `$` and `${...}` are interpolated,
// an unquoted `#` starts a comment, and leading or trailing spaces are trimmed.
//
// A checkout under ~/Projects/$HOME notes/ is unusual and entirely legal. Left
// bare in .env it would reach the orchestrator as some *other* string — one
// with an environment variable spliced into it, or truncated at a hash — and
// what the client would then show the user is a folder that does not exist. A
// checkout path that happened to spell ${TURING_INTEGRATION_KEY} would have the
// key itself interpolated into the display path, which puts a secret on a card
// in the UI.
//
// So the value is written as a Compose single-quoted literal, where nothing is
// interpolated and the only escape is \' for an apostrophe.

// composeLiteral is the raw .env line one setting is written as, quotes and all.
func composeLiteralLine(t *testing.T, env string, name string) string {
	t.Helper()
	prefix := name + "="
	for _, line := range strings.Split(env, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("%s missing from .env:\n%s", name, env)
	return ""
}

// composeLiteralValue decodes one setting the way Compose's dotenv reader
// would: a single-quoted value is literal apart from \' for an apostrophe, and
// a bare value is itself.
func composeLiteralValue(t *testing.T, env string, name string) string {
	t.Helper()
	raw := composeLiteralLine(t, env, name)
	if len(raw) >= 2 && strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") {
		return strings.ReplaceAll(raw[1:len(raw)-1], `\'`, "'")
	}
	return raw
}

// assertEnvLiteral is assertEnvValue for a setting written as a literal: what
// it compares is the value that reaches the stack, not the bytes on the line.
func assertEnvLiteral(t *testing.T, env, name, want string) {
	t.Helper()
	if got := composeLiteralValue(t, env, name); got != want {
		t.Fatalf("%s = %q, want %q\n.env:\n%s", name, got, want, env)
	}
}

// TestInitWritesTheHostVaultPathAsAComposeLiteral holds the serialisation to
// the exact form Compose reads back unchanged.
func TestInitWritesTheHostVaultPathAsAComposeLiteral(t *testing.T) {
	result := executeInitInDirectory(t, "501", "20", "", hostileCheckoutName)
	if result.err != nil {
		t.Fatalf("init.sh failed under a hostile checkout name: %v\n%s", result.err, result.output)
	}
	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	want := "'" + strings.ReplaceAll(canonical, "'", `\'`) + "'"
	if got := composeLiteralLine(t, result.env, "MEMORY_DISPLAY_ROOT"); got != want {
		t.Fatalf("MEMORY_DISPLAY_ROOT line = %s\nwant %s", got, want)
	}
	// The characters that make this necessary are still in the path, unchanged.
	for _, fragment := range []string{"$HOME", "${TURING_INTEGRATION_KEY}", "#", " ", "'"} {
		if !strings.Contains(canonical, fragment) {
			t.Fatalf("this test needs a path containing %q, got %q", fragment, canonical)
		}
	}
}

// Secrets are hex and need no quoting; quoting them anyway would change bytes
// that other tools read out of this file, so the literal helper is scoped to
// the one setting that needs it.
func TestInitLeavesSecretsUnquoted(t *testing.T) {
	result := runInit(t, "501", "20", "")

	for _, name := range []string{
		"TURING_CLIENT_API_KEY",
		"TURING_RUNTIME_TOKEN",
		"TURING_INTEGRATION_KEY",
		"HOST_UID",
		"HOST_GID",
	} {
		value := composeLiteralLine(t, result.env, name)
		if strings.HasPrefix(value, "'") || strings.HasPrefix(value, `"`) {
			t.Fatalf("%s = %s, want an unquoted value", name, value)
		}
	}
}

// And the proof that the serialisation is right is Compose reading it: the
// value that reaches the orchestrator has to be the path on disk, character for
// character, with nothing substituted into it.
func TestComposeReadsTheHostVaultPathBackUnchanged(t *testing.T) {
	requireDockerCompose(t)

	result := executeInitInDirectory(t, "501", "20", "", hostileCheckoutName)
	if result.err != nil {
		t.Fatalf("init.sh failed under a hostile checkout name: %v\n%s", result.err, result.output)
	}
	canonical, err := filepath.EvalSymlinks(result.memory)
	if err != nil {
		t.Fatalf("resolve the provisioned vault: %v", err)
	}
	infra := filepath.Join(result.root, "infra")
	if err := os.MkdirAll(infra, 0o700); err != nil {
		t.Fatal(err)
	}
	compose, err := os.ReadFile("../infra/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(infra, "docker-compose.yml"), compose, 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("docker", "compose",
		"--env-file", ".env", "-f", "infra/docker-compose.yml", "config", "--format", "json")
	command.Dir = result.root
	command.Env = append(cleanComposeEnvironment(), "HOST_UID=501", "HOST_GID=20")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("docker compose config: %v", err)
	}
	// `config` prints a compose file, and a literal $ is spelled $$ in one, so
	// the printed form is undone before it is compared with the path on disk.
	// The doubling is itself the evidence: a $ that had been interpolated would
	// not be there to escape.
	resolved := strings.ReplaceAll(
		composeConfiguredValue(t, string(output), "MEMORY_DISPLAY_ROOT"), "$$", "$")
	if resolved != canonical {
		t.Fatalf("compose resolved MEMORY_DISPLAY_ROOT to %q, want the canonical path %q", resolved, canonical)
	}
	// Nothing of the file's own secrets may have been spliced into it by the
	// path pretending to name one.
	key := envValue(t, result.env, "TURING_INTEGRATION_KEY")
	if key == "" || strings.Contains(resolved, key) {
		t.Fatalf("the display path carries the integration key: %q", resolved)
	}
	if home := os.Getenv("HOME"); home != "" && strings.Contains(resolved, home+" notes") {
		t.Fatalf("the display path had $HOME interpolated into it: %q", resolved)
	}
}

// hostileCheckoutName is a directory name containing every character that makes
// dotenv serialisation a decision rather than a formality: interpolation in two
// spellings, a comment marker, a space and an apostrophe.
const hostileCheckoutName = "turing $HOME ${TURING_INTEGRATION_KEY} #1 o'brien"

// composeConfiguredValue pulls one resolved environment value out of the JSON
// `docker compose config` prints, without pulling a YAML or JSON dependency
// into a package that has none.
func composeConfiguredValue(t *testing.T, configured string, name string) string {
	t.Helper()
	marker := `"` + name + `":`
	index := strings.Index(configured, marker)
	if index < 0 {
		t.Fatalf("%s missing from the resolved compose configuration", name)
	}
	rest := configured[index+len(marker):]
	rest = strings.TrimLeft(rest, " ")
	if !strings.HasPrefix(rest, `"`) {
		t.Fatalf("%s is not a string in the resolved compose configuration: %.80s", name, rest)
	}
	rest = rest[1:]
	var value strings.Builder
	for index := 0; index < len(rest); index++ {
		switch rest[index] {
		case '\\':
			index++
			if index >= len(rest) {
				t.Fatalf("%s ends in an unfinished escape", name)
			}
			value.WriteByte(rest[index])
		case '"':
			return value.String()
		default:
			value.WriteByte(rest[index])
		}
	}
	t.Fatalf("%s is unterminated in the resolved compose configuration", name)
	return ""
}
