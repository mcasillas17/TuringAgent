package cli

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/envfile"
	"github.com/mcasillas17/TuringAgent/turing-backend/cmd/turing/internal/registry"
)

const menuIntro = "Pick a section, then a setting. Enter keeps a value; to clear a setting, run ./turing config unset KEY."

// configMenu walks a person through the sections and settings at a
// terminal, and saves each new value with saveSettings. It returns the exit
// code of the last save, or 0 when nothing was saved.
func (e Env) configMenu(ctx context.Context) int {
	if !e.IsTerminal {
		return e.usageError("the config menu needs a terminal; run ./turing config show to list the settings and ./turing config set KEY VALUE to change one")
	}
	if _, err := e.readDotEnv(); err != nil {
		return e.fail("%v", err)
	}
	example := e.readExample()
	e.printf("%s\n", menuIntro)
	code := 0
	for {
		e.printf("\nSections:\n")
		for i, section := range registry.Sections {
			e.printf("  %d. %s\n", i+1, section)
		}
		choice, ok := e.choose("Choose a section", len(registry.Sections), "quit")
		if !ok {
			return code
		}
		section := registry.Sections[choice-1]
		keys := sectionKeys(section)
		for {
			// Read again each time: the last save may have changed it.
			file, err := e.readDotEnv()
			if err != nil {
				return e.fail("%v", err)
			}
			e.listSettings(section, keys, file, example)
			pick, ok := e.choose("Choose a setting", len(keys), "go back")
			if !ok {
				break
			}
			if result, saved := e.editSetting(ctx, keys[pick-1], file); saved {
				code = result
			}
		}
	}
}

func (e Env) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(e.Stdout, format, args...)
}

// choose asks for a number from 1 to n until it gets one. It reports false
// for q or the end of input.
func (e Env) choose(prompt string, n int, quit string) (int, bool) {
	for {
		e.printf("%s (1-%d, or q to %s): ", prompt, n, quit)
		line, err := e.readLine()
		answer := strings.TrimSpace(line)
		if number, convErr := strconv.Atoi(answer); convErr == nil && number >= 1 && number <= n {
			return number, true
		}
		if err != nil {
			e.printf("\n")
			return 0, false
		}
		if strings.EqualFold(answer, "q") {
			return 0, false
		}
		e.printf("Type a number from 1 to %d, or q.\n", n)
	}
}

// listSettings shows a section's settings, each with its description from
// .env.example, its value, masked for a secret, and the value .env.example
// ships.
func (e Env) listSettings(section string, keys []registry.Key, file *envfile.File, example []byte) {
	e.printf("\n%s:\n", section)
	for i, key := range keys {
		e.printf("  %d. %s\n", i+1, key.Name)
		if description := registry.Describe(example, key.Name); description != "" {
			for _, line := range strings.Split(description, "\n") {
				e.printf("     %s\n", line)
			}
		}
		current := currentValue(key, file)
		if key.IsSecret() {
			e.printf("     Now: %s\n", current)
			continue
		}
		e.printf("     Now: %s; shipped value: %s\n", cmp.Or(current, "(empty)"), cmp.Or(registry.Shipped(example, key.Name), "(empty)"))
	}
}

// editSetting asks for a new value and saves it. An empty answer keeps the
// value. It reports whether a save ran, and its exit code.
func (e Env) editSetting(ctx context.Context, key registry.Key, file *envfile.File) (int, bool) {
	if key.Kind == registry.SecretMap {
		return e.editAgentKeys(ctx, file)
	}
	var value string
	if key.IsSecret() {
		typed, err := e.ReadSecret(fmt.Sprintf("New value for %s (typing is hidden; Enter keeps it): ", key.Name))
		if err != nil {
			_, _ = fmt.Fprintf(e.Stderr, "could not read the value: %v\n", err)
			return 0, false
		}
		value = typed
	} else {
		e.printf("New value for %s%s (Enter keeps it): ", key.Name, choicesHint(key))
		typed, err := e.readLine()
		if err != nil && typed == "" {
			e.printf("\n")
			return 0, false
		}
		value = typed
	}
	if value == "" {
		e.printf("Kept.\n")
		return 0, false
	}
	return e.saveSettings(ctx, map[string]*string{key.Name: &value}, saveOptions{}), true
}

func choicesHint(key registry.Key) string {
	switch {
	case len(key.Choices) > 0:
		return " (" + strings.Join(key.Choices, " or ") + ")"
	case key.Kind == registry.Boolean:
		return " (true or false)"
	}
	return ""
}

// editAgentKeys changes the agent key map one name at a time, so nobody
// writes JSON.
func (e Env) editAgentKeys(ctx context.Context, file *envfile.File) (int, bool) {
	key, _ := registry.Lookup(agentKeysName)
	for {
		e.printf("\n%s: %s\n", agentKeysName, currentValue(key, file))
		e.printf("  1. Add or replace a name\n  2. Remove a name\n")
		choice, ok := e.choose("Choose", 2, "go back")
		if !ok {
			return 0, false
		}
		e.printf("Agent name: ")
		line, err := e.readLine()
		name := strings.TrimSpace(line)
		if err != nil && name == "" {
			e.printf("\n")
			return 0, false
		}
		if !validAgentName(name) {
			_, _ = fmt.Fprintln(e.Stderr, invalidAgentName(name))
			continue
		}
		if choice == 2 {
			return e.saveEdit(ctx, agentKeyEdit(name, nil), saveOptions{}), true
		}
		secret, err := e.ReadSecret(fmt.Sprintf("Key for %s (typing is hidden; Enter leaves it unchanged): ", name))
		if err != nil {
			_, _ = fmt.Fprintf(e.Stderr, "could not read the key: %v\n", err)
			continue
		}
		if secret == "" {
			e.printf("Kept.\n")
			continue
		}
		return e.saveEdit(ctx, agentKeyEdit(name, &secret), saveOptions{}), true
	}
}
