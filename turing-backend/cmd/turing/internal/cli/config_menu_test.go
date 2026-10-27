package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

// menuSection is what to type to pick section in the menu.
func menuSection(t *testing.T, section string) string {
	t.Helper()
	at := slices.Index(registry.Sections, section)
	if at < 0 {
		t.Fatalf("no section %q", section)
	}
	return itoa(at + 1)
}

// menuSetting is what to type to pick key in its section's list.
func menuSetting(t *testing.T, key string) string {
	t.Helper()
	wanted, ok := registry.Lookup(key)
	if !ok {
		t.Fatalf("no key %q", key)
	}
	n := 0
	for _, k := range registry.Editable() {
		if k.Section != wanted.Section {
			continue
		}
		n++
		if k.Name == key {
			return itoa(n)
		}
	}
	t.Fatalf("%s is not editable", key)
	return ""
}

func answers(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestConfigMenuSavesASettingChosenFromASection(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.Stdin = answers(menuSection(t, "Local engine"), menuSetting(t, "TURING_LOCAL_MODEL"), "llama3.2", "n", "q", "q")

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}

	if got, _ := h.envGet("TURING_LOCAL_MODEL"); got != "llama3.2" {
		t.Fatalf("TURING_LOCAL_MODEL = %q", got)
	}
	calls := h.checkCalls()
	if len(calls) != 2 || !slices.Contains(calls[0].Env, "TURING_LOCAL_MODEL=llama3.2") {
		t.Fatalf("check-config calls = %v", calls)
	}
	out := h.out.String()
	for _, want := range []string{
		"Local engine",
		"The model the local engine runs, named the way that engine names it.",
		"Now: qwen2.5:7b; shipped value: qwen2.5:7b",
		"to clear a setting, run ./turing config unset KEY",
		applyQuestion,
		savedNotApplied,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
	if h.ranCommand("compose up") {
		t.Fatalf("ran %q after the apply was declined", h.runner.commands())
	}
}

func TestConfigMenuQuitsOrKeepsAValueWithoutChanges(t *testing.T) {
	for name, stdin := range map[string]string{
		"q at the sections":     "q\n",
		"back, then q":          menuSection(t, "Local engine") + "\nq\nq\n",
		"an empty answer keeps": menuSection(t, "Local engine") + "\n" + menuSetting(t, "TURING_LOCAL_MODEL") + "\n\nq\nq\n",
		"the end of input":      menuSection(t, "Local engine") + "\n",
		"a wrong number first":  "99\nzero\nq\n",
	} {
		t.Run(name, func(t *testing.T) {
			h := newConfigHarness(t)
			h.IsTerminal = true
			h.Stdin = strings.NewReader(stdin)
			before := h.envBytes()

			if code := h.config(); code != 0 {
				t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
			}
			if len(h.runner.calls) != 0 {
				t.Fatalf("ran %q", h.runner.commands())
			}
			h.unchanged(before)
		})
	}
}

func TestConfigMenuSaysWhichNumbersItTakes(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.Stdin = answers("99", "q")

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if want := "Type a number from 1 to " + itoa(len(registry.Sections)) + ", or q."; !strings.Contains(h.out.String(), want) {
		t.Fatalf("stdout = %q, want %q", h.out, want)
	}
}

func TestConfigMenuNeedsATerminal(t *testing.T) {
	h := newConfigHarness(t)

	if code := h.config(); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if got := h.errOut.String(); !strings.Contains(got, "./turing config show") || !strings.Contains(got, "./turing config set") {
		t.Fatalf("stderr = %q", got)
	}
	if len(h.runner.calls) != 0 || h.out.Len() != 0 {
		t.Fatalf("ran %q, printed %q", h.runner.commands(), h.out)
	}
}

func TestConfigMenuNeedsADotEnv(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.removeEnv()

	if code := h.config(); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(h.errOut.String(), "./scripts/init.sh") {
		t.Fatalf("stderr = %q", h.errOut)
	}
}

func TestConfigMenuMasksSecretsInTheList(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.Stdin = answers(menuSection(t, "Remote model"), "q", "q")

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := h.out.String()
	for _, want := range []string{"OPENAI_API_KEY", "Now: set", "The key for the remote model.", "OPENAI_MODEL", "Now: gpt-4o-mini; shipped value: gpt-4o-mini"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
	h.noOutputContains("CANARY")
}

func TestConfigMenuReadsASecretWithEchoOff(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	var prompts []string
	h.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "sk-CANARY-NEW", nil
	}
	h.Stdin = answers(menuSection(t, "Remote model"), menuSetting(t, "OPENAI_API_KEY"), "n", "q", "q")

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "OPENAI_API_KEY") {
		t.Fatalf("ReadSecret prompts = %q", prompts)
	}
	if got, _ := h.envGet("OPENAI_API_KEY"); got != "sk-CANARY-NEW" {
		t.Fatalf("OPENAI_API_KEY = %q", got)
	}
	h.noOutputContains("CANARY")
}

func TestConfigMenuKeepsASecretWhenNothingIsTyped(t *testing.T) {
	h := newConfigHarness(t)
	h.IsTerminal = true
	h.ReadSecret = func(string) (string, error) { return "", nil }
	h.Stdin = answers(menuSection(t, "Remote model"), menuSetting(t, "OPENAI_API_KEY"), "q", "q")
	before := h.envBytes()

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if len(h.runner.calls) != 0 {
		t.Fatalf("ran %q", h.runner.commands())
	}
	h.unchanged(before)
}

func TestConfigMenuEditsAgentKeysOneNameAtATime(t *testing.T) {
	h := newConfigHarness(t)
	h.writeEnv(fullEnv(`TURING_AGENT_API_KEYS={"openai":"sk-CANARY-O"}`))
	h.IsTerminal = true
	var prompts []string
	h.ReadSecret = func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "sk-CANARY-C", nil
	}
	agents, mapKey := menuSection(t, "External agents"), menuSetting(t, "TURING_AGENT_API_KEYS")
	h.Stdin = answers(
		agents, mapKey,
		"1", "bad name", // refused, and asked again
		"1", "claude", "n", // added; the apply is declined
		mapKey, "2", "openai", "n", // removed
		"q", "q",
	)

	if code := h.config(); code != 0 {
		t.Fatalf("exit = %d; stderr: %s", code, h.errOut)
	}
	if raw, _ := h.envGet("TURING_AGENT_API_KEYS"); raw != `{"claude":"sk-CANARY-C"}` {
		t.Fatalf("TURING_AGENT_API_KEYS = %s", raw)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "claude") {
		t.Fatalf("ReadSecret prompts = %q", prompts)
	}
	out := h.out.String()
	for _, want := range []string{"TURING_AGENT_API_KEYS: openai", "Add or replace a name", "Remove a name", "TURING_AGENT_API_KEYS: claude, openai"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, out)
		}
	}
	if !strings.Contains(h.errOut.String(), "not a valid agent name") {
		t.Fatalf("stderr = %q", h.errOut)
	}
	h.noOutputContains("CANARY")
}
