package machine

import "slices"

// Recommendation is the engine suited to a machine, from the table in the
// local inference engines design (section 5.4).
type Recommendation struct {
	// Engine is the TURING_LOCAL_ENGINE value: "ollama" or "openai".
	Engine string
	// Label is what the owner is told to run: "Ollama" or "ds4".
	Label string
	// Backend is the GPU API ds4 runs on: "Metal", "ROCm" or "CUDA". Ollama
	// has none to name.
	Backend string
	// ModelHint is the model the table names for this machine.
	ModelHint string
	// PreferredModel is the name Turing would serve it under: Ollama's
	// default, or the non-thinking alias ds4 serves.
	PreferredModel string
	// Reason names the table row in words.
	Reason string
}

// The memory classes, in GiB. The tolerance below the nominal 64 and 96 GB
// covers what firmware and the kernel keep for themselves.
const (
	gib             = uint64(1) << 30
	class64Floor    = 58 * gib
	class96Floor    = 88 * gib
	ollamaModel     = "qwen2.5:7b"
	ds4ServedModel  = "deepseek-chat"
	ollamaLabel     = "Ollama"
	ds4Label        = "ds4"
	engineOllama    = "ollama"
	engineOpenAI    = "openai"
	reasonNoRowFits = "no larger-model row of the table fits this machine, so Ollama with its default model is the safe choice"
)

func ollama(reason string) Recommendation {
	return Recommendation{Engine: engineOllama, Label: ollamaLabel, ModelHint: ollamaModel, PreferredModel: ollamaModel, Reason: reason}
}

func ds4(backend, modelHint, reason string) Recommendation {
	return Recommendation{Engine: engineOpenAI, Label: ds4Label, Backend: backend, ModelHint: modelHint, PreferredModel: ds4ServedModel, Reason: reason}
}

// Recommend picks the engine for the machine. Memory it could not read is 0,
// which recommends Ollama: the table is about large-memory machines, and a
// wrong guess toward ds4 costs more than a wrong guess toward Ollama.
func Recommend(f Facts) Recommendation {
	if f.MemoryBytes == 0 {
		return ollama("this machine's memory could not be read, so Turing recommends Ollama, which fits any machine")
	}
	switch {
	case f.AppleSilicon && f.MemoryBytes < class64Floor:
		return ollama("Apple Silicon with less memory than the 64 GB class (under 58 GiB)")
	case f.AppleSilicon && f.MemoryBytes < class96Floor:
		return ds4("Metal", "Qwen3.8 Q2, about 42 GiB resident", "Apple Silicon in the 64 GB class (58 to under 88 GiB)")
	case f.AppleSilicon:
		return ds4("Metal", "DeepSeek V4 Flash Q2", "Apple Silicon in the 96-128 GB class (88 GiB or more)")
	case f.StrixHalo && f.MemoryBytes >= class96Floor:
		return ds4("ROCm", "DeepSeek V4 Flash Q2 or GLM 5.3 Q2; not Qwen3.8",
			"an AMD Strix Halo in the 96 GB+ class (88 GiB or more, counting the memory the GPU reserves)")
	case slices.Contains(f.GPUs, "nvidia") && f.MemoryBytes >= class96Floor:
		return ds4("CUDA", "DeepSeek V4 Flash or GLM 5.3 Q2", "Linux with an NVIDIA GPU in the 96 GB+ class (88 GiB or more)")
	default:
		return ollama(reasonNoRowFits)
	}
}
