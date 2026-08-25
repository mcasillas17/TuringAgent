package egress

import (
	"encoding/json"
	"strings"
	"testing"
)

func populatedMemorySnapshot() MemorySnapshot {
	return MemorySnapshot{
		PersonaID:           "persona.md",
		PersonaDisplayName:  "persona.md",
		PersonaBody:         "Speak plainly.",
		PersonaContentHash:  "persona-hash",
		PersonaWithheld:     false,
		ProfileID:           "profile.md",
		ProfileBody:         "The user keeps chickens.",
		ProfileContentHash:  "profile-hash",
		ProfileWithheld:     false,
		MemoryToolsSelected: true,
	}
}

func TestMemorySnapshotFingerprintIsDeterministicAndContentBound(t *testing.T) {
	first, err := MemorySnapshotFingerprint(populatedMemorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	second, err := MemorySnapshotFingerprint(populatedMemorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("fingerprints = %q / %q", first, second)
	}
}

// Every field of the preimage has to move the fingerprint. A field that the
// hash ignored would be a claim the enqueue and the runtime could disagree
// about without either noticing.
func TestMemorySnapshotFingerprintCoversEveryField(t *testing.T) {
	base := populatedMemorySnapshot()
	baseline, err := MemorySnapshotFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*MemorySnapshot){
		"PersonaID":           func(s *MemorySnapshot) { s.PersonaID = "other.md" },
		"PersonaDisplayName":  func(s *MemorySnapshot) { s.PersonaDisplayName = "Other" },
		"PersonaBody":         func(s *MemorySnapshot) { s.PersonaBody = "Speak grandly." },
		"PersonaContentHash":  func(s *MemorySnapshot) { s.PersonaContentHash = "changed" },
		"PersonaWithheld":     func(s *MemorySnapshot) { s.PersonaWithheld = true },
		"ProfileID":           func(s *MemorySnapshot) { s.ProfileID = "other.md" },
		"ProfileBody":         func(s *MemorySnapshot) { s.ProfileBody = "The user keeps bees." },
		"ProfileContentHash":  func(s *MemorySnapshot) { s.ProfileContentHash = "changed" },
		"ProfileWithheld":     func(s *MemorySnapshot) { s.ProfileWithheld = true },
		"MemoryToolsSelected": func(s *MemorySnapshot) { s.MemoryToolsSelected = false },
	}
	fields := memorySnapshotJSONFieldNames(t)
	if len(fields) != len(mutations) {
		t.Fatalf("preimage has %d fields but %d are exercised: %v", len(fields), len(mutations), fields)
	}
	for name, mutate := range mutations {
		mutated := base
		mutate(&mutated)
		changed, err := MemorySnapshotFingerprint(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if changed == baseline {
			t.Fatalf("changing %s did not change the fingerprint", name)
		}
	}
}

// A truncation notice is part of what a model was shown, so two documents that
// differ only by the notice must not share a fingerprint.
func TestMemorySnapshotFingerprintSeesTheTruncationNotice(t *testing.T) {
	withoutNotice := populatedMemorySnapshot()
	withNotice := populatedMemorySnapshot()
	withNotice.PersonaBody += "\n\n[Only the first 14 bytes of persona.md are pinned. Open the vault to read the rest.]\n"

	plain, err := MemorySnapshotFingerprint(withoutNotice)
	if err != nil {
		t.Fatal(err)
	}
	noticed, err := MemorySnapshotFingerprint(withNotice)
	if err != nil {
		t.Fatal(err)
	}
	if plain == noticed {
		t.Fatal("the truncation notice did not reach the fingerprint")
	}
}

// omitempty would let an empty persona and an absent one hash the same, which
// is the exact confusion the withheld flag exists to prevent.
func TestMemorySnapshotPreimageOmitsNothing(t *testing.T) {
	encoded, err := json.Marshal(MemorySnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	fields := memorySnapshotJSONFieldNames(t)
	for _, field := range fields {
		if !strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("zero-valued %s is missing from the preimage: %s", field, encoded)
		}
	}
}

func memorySnapshotJSONFieldNames(t *testing.T) []string {
	t.Helper()
	encoded, err := json.Marshal(MemorySnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(decoded))
	for name := range decoded {
		names = append(names, name)
	}
	return names
}

// The trim rule the applicability decision uses lives here so the orchestrator
// and the runtime cannot drift into two different ideas of "empty".
func TestMemorySnapshotHasPinnedContent(t *testing.T) {
	if (MemorySnapshot{}).HasPinnedContent() {
		t.Fatal("an empty snapshot claims pinned content")
	}
	whitespace := MemorySnapshot{PersonaBody: "  \n\t ", ProfileBody: "\u00a0\n"}
	if whitespace.HasPinnedContent() {
		t.Fatal("whitespace-only pins claim pinned content")
	}
	if !(MemorySnapshot{ProfileBody: " x "}).HasPinnedContent() {
		t.Fatal("a profile with content does not claim pinned content")
	}
	if !(MemorySnapshot{PersonaBody: "x"}).HasPinnedContent() {
		t.Fatal("a persona with content does not claim pinned content")
	}
}

func TestIsMemoryToolName(t *testing.T) {
	for _, name := range []string{"memory/memory.search", "memory/memory.read", "memory/memory.remember"} {
		if !IsMemoryToolName(name) {
			t.Fatalf("%q is not recognised as a memory tool", name)
		}
	}
	for _, name := range []string{"", "memory", "memoryx/tool", "files/read", "skills/skill_view"} {
		if IsMemoryToolName(name) {
			t.Fatalf("%q was wrongly recognised as a memory tool", name)
		}
	}
}
