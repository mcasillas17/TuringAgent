package teamfiles

import (
	"path/filepath"
	"slices"
	"testing"
)

// init.sh copies these tracked templates into team/ on a fresh install, so a
// template the loader rejects would ship a broken specialist to every user.
func TestShippedSeedTemplatesLoadAsSpecified(t *testing.T) {
	root := filepath.Join("..", "..", "..", "scripts", "team-templates")
	profiles, err := New(root).Scan()
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		tools        []string
		requires     []string
		memory       string
		maxToolCalls int
	}
	expected := map[string]want{
		"dev": {
			tools:  []string{"files.*", "github.*", "system.*"},
			memory: MemoryNone,
		},
		"inbox": {
			tools:    []string{"gmail.*"},
			requires: []string{"gmail.*"},
			memory:   MemoryRead,
		},
		"research": {
			tools:        []string{"files.*", "memory.read", "memory.search", "skill_view", "skills_list", "system.time"},
			memory:       MemoryRead,
			maxToolCalls: 12,
		},
	}
	if len(profiles) != len(expected) {
		t.Fatalf("seed profiles = %d, want %d", len(profiles), len(expected))
	}
	for _, profile := range profiles {
		if profile.ParseError != "" {
			t.Fatalf("seed %s does not load: %s", profile.ID, profile.ParseError)
		}
		w, ok := expected[profile.ID]
		if !ok {
			t.Fatalf("unexpected seed %q", profile.ID)
		}
		if !slices.Equal(profile.Tools, w.tools) || !slices.Equal(profile.Requires, w.requires) ||
			profile.Memory != w.memory || profile.MaxToolCalls != w.maxToolCalls || profile.Model != "" ||
			len(profile.Skills) != 0 || profile.Emoji == "" || profile.Instructions == "" {
			t.Fatalf("seed %s = %+v", profile.ID, profile)
		}
	}
}
