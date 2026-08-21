package egress

import "testing"

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
