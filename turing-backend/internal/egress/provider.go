package egress

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

// LocalProvider is the stored and wire identity of the local route: a model
// running on this machine, so a send needs no remote-egress consent. Any
// local engine runs under it, Ollama or an OpenAI-compatible server. It is
// spelled "ollama" because it was named before other engines existed, and the
// stored rows, job payloads and egress decisions that carry it cannot change.
const LocalProvider = "ollama"

// maxEngineLabelBytes bounds an engine label a client renders verbatim.
const maxEngineLabelBytes = 64

// ValidateEngineLabel accepts a display label for the local engine: 1–64
// bytes of valid UTF-8, every rune printable (letters, marks, numbers,
// punctuation, symbols and the ASCII space), so nothing in it breaks a line or
// reorders the text around it. The runtime checks it when configured and the
// orchestrator again when a worker advertises it; it is never routed on.
func ValidateEngineLabel(label string) error {
	if label == "" || len(label) > maxEngineLabelBytes || !utf8.ValidString(label) {
		return errors.New("must be 1 to 64 bytes of valid UTF-8")
	}
	for _, r := range label {
		if !unicode.IsPrint(r) {
			return errors.New("must contain only printable characters")
		}
	}
	return nil
}
