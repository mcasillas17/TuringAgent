package repository

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

// truncationNoticeMarker is where the notice starts inside a pin.
const truncationNoticeMarker = "\n\n[Only the first "

// expectedTruncationNotice is the notice a pinned document over the budget
// carries, written out here rather than borrowed from the package that produces
// it.
//
// Duplicating it is the point. These bytes are inside the preimage a consent is
// bound to, so they are part of the contract and not an implementation detail:
// change the wording, the spacing, or the number it reports and every consent
// granted over the old text stops matching what would be sent. A test that
// asked memoryfiles what the notice says would agree with whatever it answered.
func expectedTruncationNotice(relPath string, retainedBytes int) string {
	return fmt.Sprintf(
		"\n\n[Only the first %d bytes of %s are pinned. Open the vault to read the rest.]\n",
		retainedBytes, relPath,
	)
}

// expectedPin derives the pin from the raw file the way the plan describes it:
// cut at the last rune boundary at or below the budget, then say so.
func expectedPin(t *testing.T, raw string, relPath string, budget int) string {
	t.Helper()
	if len(raw) <= budget {
		t.Fatalf("this fixture is not over the %d byte budget: %d bytes", budget, len(raw))
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	return raw[:cut] + expectedTruncationNotice(relPath, cut)
}

// splitPin separates the retained text from the notice appended to it, so a
// test can vary one without touching the other.
func splitPin(t *testing.T, pinned string) (string, string) {
	t.Helper()
	index := strings.LastIndex(pinned, truncationNoticeMarker)
	if index < 0 {
		t.Fatalf("this pin carries no truncation notice to reason about: %q", tail(pinned))
	}
	return pinned[:index], pinned[index:]
}

// The fingerprint is the one-way binding between the user's consent and the
// pinned material it was granted over. For a document past the 4096-byte
// budget, that material is not the file: it is the rune-safe cut plus the
// notice saying it is a cut. Those exact bytes are what a model is shown, so
// those exact bytes are what the binding has to be over.
//
// The whole preimage is derived here from the raw file independently — cut,
// notice, hash — and compared against the fingerprint the send path produces.
// Hash the file's own bytes instead, pin the cut without its notice, or report
// a retained count that is really the budget, and the equality fails: the
// consent would be bound to something no model ever saw.
func TestEgressMemoryFingerprintIsExactlyTheTruncatedPinsPreimage(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	// Three-byte runes, and two-byte runes behind a one-byte head, so neither
	// budget lands on a rune boundary and both cuts have to come out below it.
	personaRaw := strings.Repeat("界", 2000)
	profileRaw := "a" + strings.Repeat("é", 3000)
	writePin(t, vault, memoryfiles.PersonaFileName, personaRaw)
	writePin(t, vault, memoryfiles.ProfileFileName, profileRaw)

	persona := expectedPin(t, personaRaw, memoryfiles.PersonaFileName, memoryfiles.MaxPersonaBytes)
	profile := expectedPin(t, profileRaw, memoryfiles.ProfileFileName, memoryfiles.MaxProfileBytes)

	fingerprint, snapshot, err := repo.EgressMemorySnapshotFingerprint(ctx(), nil)
	if err != nil {
		t.Fatalf("EgressMemorySnapshotFingerprint: %v", err)
	}
	if !snapshot.Persona.Truncated || !snapshot.Profile.Truncated {
		t.Fatalf("an over-budget pin did not report truncation: %+v / %+v", snapshot.Persona, snapshot.Profile)
	}
	if snapshot.Persona.Content != persona {
		t.Fatalf("persona pinned %d bytes ending %q, want %d ending %q",
			len(snapshot.Persona.Content), tail(snapshot.Persona.Content), len(persona), tail(persona))
	}
	if snapshot.Profile.Content != profile {
		t.Fatalf("profile pinned %d bytes ending %q, want %d ending %q",
			len(snapshot.Profile.Content), tail(snapshot.Profile.Content), len(profile), tail(profile))
	}

	// The cut is rune-safe and lands below the budget, which is what makes the
	// number in the notice a real count rather than the budget written twice.
	cuts := []struct {
		name    string
		pinned  string
		relPath string
		budget  int
	}{
		{"persona", persona, memoryfiles.PersonaFileName, memoryfiles.MaxPersonaBytes},
		{"profile", profile, memoryfiles.ProfileFileName, memoryfiles.MaxProfileBytes},
	}
	for _, cut := range cuts {
		retained, notice := splitPin(t, cut.pinned)
		if !utf8.ValidString(retained) {
			t.Fatalf("%s: truncation split a rune", cut.name)
		}
		if len(retained) >= cut.budget {
			t.Fatalf("%s: kept %d bytes, want a rune-safe cut below %d", cut.name, len(retained), cut.budget)
		}
		if notice != expectedTruncationNotice(cut.relPath, len(retained)) {
			t.Fatalf("%s: notice = %q, want it to report the %d bytes kept", cut.name, notice, len(retained))
		}
	}

	want, err := backendegress.MemorySnapshotFingerprint(backendegress.MemorySnapshot{
		PersonaID:          memoryfiles.PersonaFileName,
		PersonaDisplayName: memoryfiles.PersonaFileName,
		PersonaBody:        persona,
		PersonaContentHash: memoryfiles.ContentHash(persona),
		ProfileID:          memoryfiles.ProfileFileName,
		ProfileBody:        profile,
		ProfileContentHash: memoryfiles.ContentHash(profile),
	})
	if err != nil {
		t.Fatalf("expected fingerprint: %v", err)
	}
	if fingerprint != want {
		t.Fatalf("fingerprint = %s, want the fingerprint of the post-truncation pins %s", fingerprint, want)
	}

	// The way this goes wrong, named rather than implied: binding the whole
	// file, which is the document the user has and the model does not.
	raw, err := backendegress.MemorySnapshotFingerprint(backendegress.MemorySnapshot{
		PersonaID:          memoryfiles.PersonaFileName,
		PersonaDisplayName: memoryfiles.PersonaFileName,
		PersonaBody:        personaRaw,
		PersonaContentHash: memoryfiles.ContentHash(personaRaw),
		ProfileID:          memoryfiles.ProfileFileName,
		ProfileBody:        profileRaw,
		ProfileContentHash: memoryfiles.ContentHash(profileRaw),
	})
	if err != nil {
		t.Fatalf("raw fingerprint: %v", err)
	}
	if fingerprint == raw {
		t.Fatal("the fingerprint binds the whole file rather than the bytes that were pinned")
	}
}

// Every byte of the pin is inside the binding: the prose that survived the cut,
// and the notice appended to it. Move either — one character of the user's
// text, or the retained count the notice reports — and the fingerprint has to
// move with it, or a run could be consented to over one fragment and sent with
// another.
func TestEgressMemoryFingerprintMovesWithRetainedAndNoticeBytes(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	raw := strings.Repeat("界", 2000)
	writePin(t, vault, memoryfiles.PersonaFileName, raw)

	before, snapshot, err := repo.EgressMemorySnapshotFingerprint(ctx(), nil)
	if err != nil {
		t.Fatalf("EgressMemorySnapshotFingerprint: %v", err)
	}
	pinned := snapshot.Persona.Content
	retained, notice := splitPin(t, pinned)

	// One retained byte changes: same length, same notice, one different
	// character inside the fragment the model is shown.
	writePin(t, vault, memoryfiles.PersonaFileName, "海"+raw[len("界"):])
	afterEdit, editedSnapshot, err := repo.EgressMemorySnapshotFingerprint(ctx(), nil)
	if err != nil {
		t.Fatalf("EgressMemorySnapshotFingerprint after the edit: %v", err)
	}
	editedRetained, editedNotice := splitPin(t, editedSnapshot.Persona.Content)
	if len(editedRetained) != len(retained) || editedNotice != notice {
		t.Fatalf("the edit moved the cut or the notice (%d/%q -> %d/%q); it no longer isolates the retained bytes",
			len(retained), notice, len(editedRetained), editedNotice)
	}
	if afterEdit == before {
		t.Fatal("editing a retained byte left the fingerprint unchanged")
	}

	// Only the notice changes: the same retained text, with a notice claiming
	// the budget instead of what was actually kept — the exact error a notice
	// built from the wrong number would make.
	misreported := retained + expectedTruncationNotice(memoryfiles.PersonaFileName, memoryfiles.MaxPersonaBytes)
	if misreported == pinned {
		t.Fatal("the notice already reports the budget, so this comparison proves nothing")
	}
	if personaOnlyFingerprint(t, pinned) == personaOnlyFingerprint(t, misreported) {
		t.Fatal("changing the truncation notice left the fingerprint unchanged")
	}
}

// personaOnlyFingerprint hashes a persona pin on its own, so a comparison is
// about the bytes handed in and nothing else.
func personaOnlyFingerprint(t *testing.T, pinned string) string {
	t.Helper()
	fingerprint, err := backendegress.MemorySnapshotFingerprint(backendegress.MemorySnapshot{
		PersonaID:          memoryfiles.PersonaFileName,
		PersonaDisplayName: memoryfiles.PersonaFileName,
		PersonaBody:        pinned,
		PersonaContentHash: memoryfiles.ContentHash(pinned),
		ProfileID:          memoryfiles.ProfileFileName,
		ProfileWithheld:    true,
	})
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	return fingerprint
}
