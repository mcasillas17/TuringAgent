package llm

import (
	"strings"
	"testing"
)

// An engine label is shown to the user and nothing else, so it must be short,
// printable text.
func TestValidateEngineLabel(t *testing.T) {
	if err := ValidateEngineLabel("ds4"); err != nil {
		t.Fatalf("ds4: %v", err)
	}
	if err := ValidateEngineLabel(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("64 bytes: %v", err)
	}
	for name, label := range map[string]string{
		"empty": "", "newline": "a\nb", "65 bytes": strings.Repeat("a", 65), "invalid UTF-8": "\xff", "escape": "a\x1b[31m",
	} {
		if err := ValidateEngineLabel(label); err == nil {
			t.Fatalf("%s: accepted %q", name, label)
		}
	}
}
