package machine

import (
	"strings"
	"testing"
)

// gibibytes is a memory size in GiB that need not be whole, as the class
// boundaries are tested at 57.9 and 87.9.
func gibibytes(n float64) uint64 { return uint64(n * float64(gib)) }

func TestRecommendFollowsTheTableOneRowAtATime(t *testing.T) {
	tests := []struct {
		name  string
		facts Facts
		want  Recommendation
	}{
		{
			name:  "Apple Silicon under the 64 GB class, the 24 GB Mac mini",
			facts: Facts{OS: "darwin", Arch: "arm64", AppleSilicon: true, MemoryBytes: 24 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", Backend: "", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "Apple Silicon in the 64 GB class",
			facts: Facts{OS: "darwin", Arch: "arm64", AppleSilicon: true, MemoryBytes: 64 * gib},
			want:  Recommendation{Engine: "openai", Label: "ds4", Backend: "Metal", ModelHint: "Qwen3.8 Q2, about 42 GiB resident", PreferredModel: "deepseek-chat"},
		},
		{
			name:  "Apple Silicon in the 96-128 GB class",
			facts: Facts{OS: "darwin", Arch: "arm64", AppleSilicon: true, MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "openai", Label: "ds4", Backend: "Metal", ModelHint: "DeepSeek V4 Flash Q2", PreferredModel: "deepseek-chat"},
		},
		{
			name:  "AMD Strix Halo in the 96 GB+ class, the Framework Desktop",
			facts: Facts{OS: "linux", Arch: "amd64", StrixHalo: true, GPUs: []string{"amd"}, MemoryBytes: 127 * gib},
			want:  Recommendation{Engine: "openai", Label: "ds4", Backend: "ROCm", ModelHint: "DeepSeek V4 Flash Q2 or GLM 5.3 Q2; not Qwen3.8", PreferredModel: "deepseek-chat"},
		},
		{
			name:  "Linux with an NVIDIA GPU in the 96 GB+ class",
			facts: Facts{OS: "linux", Arch: "amd64", GPUs: []string{"nvidia"}, MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "openai", Label: "ds4", Backend: "CUDA", ModelHint: "DeepSeek V4 Flash or GLM 5.3 Q2", PreferredModel: "deepseek-chat"},
		},
		{
			name:  "an Intel Mac, even with 128 GiB",
			facts: Facts{OS: "darwin", Arch: "x86_64", MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "Linux with no GPU and 128 GiB",
			facts: Facts{OS: "linux", Arch: "amd64", MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "a Strix Halo below the 96 GB+ class",
			facts: Facts{OS: "linux", Arch: "amd64", StrixHalo: true, GPUs: []string{"amd"}, MemoryBytes: 64 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "an NVIDIA box below the 96 GB+ class",
			facts: Facts{OS: "linux", Arch: "amd64", GPUs: []string{"nvidia"}, MemoryBytes: 24 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "AMD without Strix Halo, an ordinary Radeon",
			facts: Facts{OS: "linux", Arch: "amd64", GPUs: []string{"amd"}, MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "ollama", Label: "Ollama", ModelHint: "qwen2.5:7b", PreferredModel: "qwen2.5:7b"},
		},
		{
			name:  "a Strix Halo that also has an NVIDIA GPU is a Strix Halo",
			facts: Facts{OS: "linux", Arch: "amd64", StrixHalo: true, GPUs: []string{"amd", "nvidia"}, MemoryBytes: 128 * gib},
			want:  Recommendation{Engine: "openai", Label: "ds4", Backend: "ROCm", ModelHint: "DeepSeek V4 Flash Q2 or GLM 5.3 Q2; not Qwen3.8", PreferredModel: "deepseek-chat"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Recommend(tt.facts)

			if got.Reason == "" {
				t.Fatal("the recommendation gives no reason")
			}
			got.Reason = ""
			if got != tt.want {
				t.Fatalf("Recommend(%+v) =\n  %+v\nwant\n  %+v", tt.facts, got, tt.want)
			}
		})
	}
}

func TestRecommendClassBoundariesOnAppleSilicon(t *testing.T) {
	tests := []struct {
		gib         float64
		wantEngine  string
		wantBackend string
		wantHint    string
	}{
		{57.9, "ollama", "", "qwen2.5:7b"},
		{58, "openai", "Metal", "Qwen3.8 Q2, about 42 GiB resident"},
		{87.9, "openai", "Metal", "Qwen3.8 Q2, about 42 GiB resident"},
		{88, "openai", "Metal", "DeepSeek V4 Flash Q2"},
	}
	for _, tt := range tests {
		got := Recommend(Facts{OS: "darwin", Arch: "arm64", AppleSilicon: true, MemoryBytes: gibibytes(tt.gib)})

		if got.Engine != tt.wantEngine || got.Backend != tt.wantBackend || got.ModelHint != tt.wantHint {
			t.Errorf("%v GiB: %+v, want %s %q %q", tt.gib, got, tt.wantEngine, tt.wantBackend, tt.wantHint)
		}
	}
}

func TestRecommendClassBoundariesForTheLargeClassEngines(t *testing.T) {
	for name, facts := range map[string]Facts{
		"strix halo": {OS: "linux", StrixHalo: true, GPUs: []string{"amd"}},
		"nvidia":     {OS: "linux", GPUs: []string{"nvidia"}},
	} {
		for _, tt := range []struct {
			gib    float64
			ollama bool
		}{{57.9, true}, {58, true}, {87.9, true}, {88, false}} {
			facts.MemoryBytes = gibibytes(tt.gib)

			got := Recommend(facts)

			if (got.Engine == "ollama") != tt.ollama {
				t.Errorf("%s at %v GiB: %+v, want Ollama=%v", name, tt.gib, got, tt.ollama)
			}
		}
	}
}

func TestRecommendWithNoMemoryReadRecommendsOllamaAndSaysWhy(t *testing.T) {
	for _, facts := range []Facts{
		{OS: "darwin", Arch: "arm64", AppleSilicon: true},
		{OS: "linux", StrixHalo: true, GPUs: []string{"amd"}},
		{OS: "linux", GPUs: []string{"nvidia"}},
		{},
	} {
		got := Recommend(facts)

		if got.Engine != "ollama" || got.Label != "Ollama" || got.PreferredModel != "qwen2.5:7b" {
			t.Errorf("Recommend(%+v) = %+v, want Ollama", facts, got)
		}
		if !strings.Contains(got.Reason, "memory") {
			t.Errorf("Recommend(%+v).Reason = %q, want it to say the memory was not read", facts, got.Reason)
		}
	}
}

func TestEveryRecommendationNamesItsTableRow(t *testing.T) {
	seen := make(map[string]bool)
	for _, facts := range []Facts{
		{AppleSilicon: true, MemoryBytes: 24 * gib},
		{AppleSilicon: true, MemoryBytes: 64 * gib},
		{AppleSilicon: true, MemoryBytes: 128 * gib},
		{StrixHalo: true, MemoryBytes: 128 * gib},
		{GPUs: []string{"nvidia"}, MemoryBytes: 128 * gib},
		{MemoryBytes: 128 * gib},
		{},
	} {
		reason := Recommend(facts).Reason
		if seen[reason] {
			t.Errorf("reason %q is shared by two table rows", reason)
		}
		seen[reason] = true
	}
}

func TestPreferredModelIsWhatEachLabelServesTuringBest(t *testing.T) {
	for label, want := range map[string]string{"ds4": "deepseek-chat", "Ollama": "qwen2.5:7b", "OpenAI-compatible": "", "": ""} {
		if got := PreferredModel(label); got != want {
			t.Errorf("PreferredModel(%q) = %q, want %q", label, got, want)
		}
	}
	for _, facts := range []Facts{{AppleSilicon: true, MemoryBytes: 24 * gib}, {AppleSilicon: true, MemoryBytes: 128 * gib}, {StrixHalo: true, MemoryBytes: 128 * gib}} {
		if got := Recommend(facts); got.PreferredModel != PreferredModel(got.Label) {
			t.Errorf("Recommend(%+v).PreferredModel = %q, want PreferredModel(%q)", facts, got.PreferredModel, got.Label)
		}
	}
}
