package egress

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSkillSnapshotFingerprintIsDeterministicAndContentBound(t *testing.T) {
	first := []SkillSnapshot{{
		SkillID: "writing/tone", Name: "Tone", Description: "Brief",
		Category: "writing", Instructions: "Use short sentences.",
		References:          map[string]string{"b.md": "B", "a.md": "A"},
		MissingCapabilities: []string{"files.read"},
	}}
	second := []SkillSnapshot{{
		SkillID: "writing/tone", Name: "Tone", Description: "Brief",
		Category: "writing", Instructions: "Use short sentences.",
		References:          map[string]string{"a.md": "A", "b.md": "B"},
		MissingCapabilities: []string{"files.read"},
	}}

	firstHash, err := SkillSnapshotFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := SkillSnapshotFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == "" || firstHash != secondHash {
		t.Fatalf("fingerprints = %q / %q", firstHash, secondHash)
	}
	second[0].Instructions = "Use long sentences."
	changed, err := SkillSnapshotFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if changed == firstHash {
		t.Fatal("skill content change did not change fingerprint")
	}
}

func TestSkillSnapshotFingerprintBindsEveryDisplayedField(t *testing.T) {
	baseline := SkillSnapshot{
		SkillID:  "writing/tone",
		Name:     "Tone",
		Withheld: true,
	}
	baselineHash, err := SkillSnapshotFingerprint([]SkillSnapshot{baseline})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*SkillSnapshot){
		"skill id": func(snapshot *SkillSnapshot) { snapshot.SkillID = "writing/style" },
		"name":     func(snapshot *SkillSnapshot) { snapshot.Name = "Style" },
		"withheld": func(snapshot *SkillSnapshot) { snapshot.Withheld = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := baseline
			mutate(&changed)
			changedHash, err := SkillSnapshotFingerprint([]SkillSnapshot{changed})
			if err != nil {
				t.Fatal(err)
			}
			if changedHash == baselineHash {
				t.Fatalf("mutating %s did not change the fingerprint", name)
			}
		})
	}
}

func TestSkillSnapshotFingerprintBindsSetMembership(t *testing.T) {
	one := []SkillSnapshot{{SkillID: "writing/tone", Name: "Tone"}}
	two := append(append([]SkillSnapshot(nil), one...), SkillSnapshot{
		SkillID: "research/sources",
		Name:    "Sources",
	})
	oneHash, err := SkillSnapshotFingerprint(one)
	if err != nil {
		t.Fatal(err)
	}
	twoHash, err := SkillSnapshotFingerprint(two)
	if err != nil {
		t.Fatal(err)
	}
	if oneHash == twoHash {
		t.Fatal("adding a skill did not change the fingerprint")
	}
}

func TestSanitizeSkillDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		displayName string
		skillID     string
		want        string
	}{
		{
			name:        "flattens whitespace and strips format and control runes",
			displayName: "  Line\n\tOne \u202e hidden \u2066 text\u2069 \x00  ",
			skillID:     "writing/tone",
			want:        "Line One hidden text",
		},
		{
			name:        "falls back to sanitized skill id",
			displayName: "\u200b\u202e\u2066",
			skillID:     "writing/\u200btone",
			want:        "writing/tone",
		},
		{
			name:        "treats separators-only display name as empty before id fallback",
			displayName: " / ",
			skillID:     "writing/tone",
			want:        "writing/tone",
		},
		{
			name:        "treats separators mixed with spaces as empty before id fallback",
			displayName: "/ /",
			skillID:     "writing/tone",
			want:        "writing/tone",
		},
		{
			name:        "uses unnamed when both inputs become separators only",
			displayName: "\u200b\u202e",
			skillID:     "\u200b/\u2066",
			want:        "(unnamed)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SanitizeSkillDisplayName(test.displayName, test.skillID); got != test.want {
				t.Fatalf("SanitizeSkillDisplayName(%q, %q) = %q, want %q", test.displayName, test.skillID, got, test.want)
			}
		})
	}
}

func TestSanitizeSkillDisplayNameCapsOnRuneBoundary(t *testing.T) {
	got := SanitizeSkillDisplayName(strings.Repeat("界", 300), "writing/tone")
	if !utf8.ValidString(got) {
		t.Fatalf("result is invalid UTF-8: %q", got)
	}
	if runeCount := utf8.RuneCountInString(got); runeCount != maxSkillDisplayNameRunes {
		t.Fatalf("rune count = %d, want %d", runeCount, maxSkillDisplayNameRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("result %q does not end with an ellipsis", got)
	}
}
