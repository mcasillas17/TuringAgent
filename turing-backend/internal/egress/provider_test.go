package egress

import (
	"strings"
	"testing"
)

// An engine label is shown to the user verbatim and never routed on, so it is
// short, printable text: no control, format or line-separator characters that
// could break a line or reorder what is shown around it.
func TestValidateEngineLabel(t *testing.T) {
	for _, label := range []string{"ds4", "llama.cpp", "LM Studio", "Ollama · Metal", strings.Repeat("a", 64)} {
		if err := ValidateEngineLabel(label); err != nil {
			t.Fatalf("%q: %v", label, err)
		}
	}
	for name, label := range map[string]string{
		"empty": "", "newline": "a\nb", "65 bytes": strings.Repeat("a", 65), "invalid UTF-8": "\xff",
		"escape": "a\x1b[31m", "line separator": "a\u2028b", "paragraph separator": "a\u2029b",
		"bidi override": "a\u202eb", "zero-width space": "a\u200bb",
	} {
		if err := ValidateEngineLabel(label); err == nil {
			t.Fatalf("%s: accepted %q", name, label)
		}
	}
}
