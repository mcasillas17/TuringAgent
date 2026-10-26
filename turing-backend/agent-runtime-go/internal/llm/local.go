package llm

import (
	"errors"
	"unicode"
	"unicode/utf8"
)

// The local route's engines, by the protocol the runtime speaks to them. Which
// product serves the protocol (ds4, llama.cpp, LM Studio, …) is the operator's
// business and shows only in the engine label.
const (
	LocalEngineOllama = "ollama"
	LocalEngineOpenAI = "openai"
)

// maxEngineLabelBytes bounds a label a client renders verbatim.
const maxEngineLabelBytes = 64

// ValidateEngineLabel accepts a display label for the local engine: 1–64
// bytes of valid UTF-8 with no control characters. It is shown to the user and
// never routed on.
func ValidateEngineLabel(label string) error {
	if label == "" || len(label) > maxEngineLabelBytes || !utf8.ValidString(label) {
		return errors.New("must be 1 to 64 bytes of valid UTF-8")
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	return nil
}
