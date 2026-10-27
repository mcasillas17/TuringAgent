package envfile

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The strongest check that Set writes what it means is Compose itself reading
// the file back: the value that reaches a service has to be the value that was
// given, byte for byte, with nothing interpolated into it or cut off at a
// comment marker.
func TestComposeReadsWhatSetWrote(t *testing.T) {
	requireDockerCompose(t)

	// NAME is defined in the file so that a value written without protection
	// would be visibly rewritten rather than quietly empty.
	file := mustParse(t, "# existing settings\nNAME=interpolated\nV_DOLLAR=old\nV_DOLLAR=older\n")
	values := []struct {
		key   string
		value string
	}{
		{"V_DOLLAR", "$NAME"},
		{"V_BRACE", "${NAME}"},
		{"V_HASH", "value #x"},
		{"V_HASH_BARE", "a#b"},
		{"V_LEADING", "  leading and trailing  "},
		{"V_APOSTROPHE", "it's"},
		{"V_JSON", `{"agent":"key with space","n":["$NAME"]}`},
		{"V_URL", "http://x:1/v1"},
		{"V_EMPTY", ""},
		{"V_BACKSLASH", `C:\dir\file`},
		{"V_DOLLARS", "a$$b"},
		{"V_QUOTES", `say "hi"`},
		{"V_UNICODE", "héllo ☃"},
	}
	for _, entry := range values {
		if err := file.Set(entry.key, entry.value); err != nil {
			t.Fatalf("Set(%s): %v", entry.key, err)
		}
	}

	keys := make([]string, len(values))
	for i, entry := range values {
		keys[i] = entry.key
	}
	resolved := composeResolve(t, file.Bytes(), keys)

	for _, entry := range values {
		got, ok := resolved[entry.key]
		if !ok {
			t.Fatalf("%s missing from the resolved configuration", entry.key)
		}
		if got != entry.value {
			t.Errorf("Compose read %s as %q, Set wrote %q", entry.key, got, entry.value)
		}
		if reread, ok := mustParse(t, string(file.Bytes())).Get(entry.key); !ok || reread != entry.value {
			t.Errorf("Get(%s) = (%q, %v), want %q", entry.key, reread, ok, entry.value)
		}
	}
}

// Get must agree with Compose on lines nobody generated: hand-edited quoting,
// comments, duplicates and spacing. Double-quoted interpolation is left out on
// purpose, since Get does not evaluate it.
func TestGetAgreesWithComposeOnHandWrittenLines(t *testing.T) {
	requireDockerCompose(t)

	content := "V_COMMENT=a #b #c\n" +
		"V_SPACE_HASH= #x\n" +
		"V_BARE_HASH=a#b\n" +
		"V_PADDED=   padded   \n" +
		"V_DQ_ESCAPES=\"a\\\\b\\nc\\td\\\"e\"\n" +
		"V_DQ_BACKSLASH=\"a\\\\\" # tail\n" +
		"V_DQ_UNKNOWN=\"a\\qb\"\n" +
		"V_SQ_ESCAPED='it\\'s' # tail\n" +
		"V_SQ_LITERAL='a \\n \\\\ \"b\"'\n" +
		"V_SQ_TRAILING='x' trailing\n" +
		"V_MULTILINE=\"one\ntwo\"\n" +
		"V_SPACED_EQUALS = spaced\n" +
		"  export\tV_EXPORT=exported\n" +
		"V_DUP=1\nV_DUP=\"2\"\nV_DUP='3'\n" +
		"V_EMPTY=\n" +
		"V_EMPTY_QUOTED=''\n"
	file := mustParse(t, content)
	keys := file.Keys()
	resolved := composeResolve(t, []byte(content), keys)

	if len(keys) != 16 {
		t.Fatalf("Keys() = %v, want the 16 distinct keys written above", keys)
	}
	for _, key := range keys {
		want, ok := resolved[key]
		if !ok {
			t.Fatalf("%s missing from the resolved configuration", key)
		}
		if got, found := file.Get(key); !found || got != want {
			t.Errorf("Get(%s) = (%q, %v), Compose read %q", key, got, found, want)
		}
	}
}

// composeResolve has Compose read an .env and returns what each of keys
// resolves to in a service's environment, with the $$ that `config` prints for
// a literal $ undone. A $ that had been interpolated would not be there to
// escape.
func composeResolve(t *testing.T, env []byte, keys []string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), env, 0o600); err != nil {
		t.Fatal(err)
	}
	var compose strings.Builder
	compose.WriteString("services:\n  probe:\n    image: busybox\n    environment:\n")
	for _, key := range keys {
		compose.WriteString("      " + key + ": ${" + key + "}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "c.yml"), []byte(compose.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command("docker", "compose", "--env-file", ".env", "-f", "c.yml", "config", "--format", "json")
	command.Dir = dir
	command.Env = cleanComposeEnvironment()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("docker compose config: %v\n%s", err, output)
	}
	var config struct {
		Services struct {
			Probe struct {
				Environment map[string]string `json:"environment"`
			} `json:"probe"`
		} `json:"services"`
	}
	if err := json.Unmarshal(output, &config); err != nil {
		t.Fatalf("docker compose config printed no JSON: %v\n%s", err, output)
	}
	resolved := make(map[string]string, len(config.Services.Probe.Environment))
	for key, value := range config.Services.Probe.Environment {
		resolved[key] = strings.ReplaceAll(value, "$$", "$")
	}
	return resolved
}

// requireDockerCompose skips a test that needs the real interpolator. `config`
// needs no daemon — it resolves variables and stops — but it does need the
// plugin to be installed.
func requireDockerCompose(t *testing.T) {
	t.Helper()
	command := exec.Command("docker", "compose", "version")
	command.Env = cleanComposeEnvironment()
	if err := command.Run(); err != nil {
		t.Skipf("docker compose is not available here: %v", err)
	}
}

// cleanComposeEnvironment drops every variable that could change what Compose
// reads from the .env, since an exported variable is preferred to the file and
// the developer running the test may well have one.
func cleanComposeEnvironment() []string {
	filtered := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found || name == "NAME" || strings.HasPrefix(name, "V_") || strings.HasPrefix(name, "COMPOSE_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
