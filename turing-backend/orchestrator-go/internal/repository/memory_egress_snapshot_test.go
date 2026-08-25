package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
)

func writePin(t *testing.T, vault *memoryfiles.Vault, name string, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vault.Root(), name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestEgressMemorySnapshotPinsBothDocuments(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	if snapshot.Persona.Content != "Speak plainly." || !snapshot.Persona.Available {
		t.Fatalf("persona = %+v", snapshot.Persona)
	}
	if snapshot.Profile.Content != "The user keeps chickens." || !snapshot.Profile.Available {
		t.Fatalf("profile = %+v", snapshot.Profile)
	}
	if !snapshot.Enabled {
		t.Fatal("memory reads as off on a fresh database")
	}
	if snapshot.Persona.RelPath != memoryfiles.PersonaFileName ||
		snapshot.Profile.RelPath != memoryfiles.ProfileFileName {
		t.Fatalf("pinned paths = %q / %q", snapshot.Persona.RelPath, snapshot.Profile.RelPath)
	}
	preimage := snapshot.Preimage(nil)
	if preimage.PersonaBody != "Speak plainly." || preimage.ProfileBody != "The user keeps chickens." {
		t.Fatalf("preimage = %+v", preimage)
	}
	if preimage.PersonaWithheld || preimage.ProfileWithheld || preimage.MemoryToolsSelected {
		t.Fatalf("preimage flags = %+v", preimage)
	}
}

// The budget bounds the pin, not the file: past 4096 bytes the pin is cut on a
// rune boundary and carries a notice, and the snapshot is the cut bytes with
// the notice attached — the exact thing a model would be shown.
func TestEgressMemorySnapshotKeepsPostTruncationBytes(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, strings.Repeat("é", memoryfiles.MaxPersonaBytes))

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	if !snapshot.Persona.Truncated {
		t.Fatal("an over-budget persona did not report truncation")
	}
	if !strings.Contains(snapshot.Persona.Content, "are pinned") {
		t.Fatalf("persona carries no truncation notice: %q", tail(snapshot.Persona.Content))
	}
	if snapshot.Persona.ContentHash != memoryfiles.ContentHash(snapshot.Persona.Content) {
		t.Fatal("the pinned hash does not cover the pinned bytes")
	}
}

// A pin nobody can read is a visible unavailable row, never an empty document
// pretending to be a healthy one, and it must not put memory on a disclosure.
func TestEgressMemorySnapshotReportsUnreadablePins(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(vault.Root(), memoryfiles.PersonaFileName)); err != nil {
		t.Fatalf("symlink persona: %v", err)
	}

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	if snapshot.Persona.Available {
		t.Fatal("a symlinked persona reads as available")
	}
	if snapshot.Persona.Content != "" {
		t.Fatalf("a symlinked persona pinned %q", snapshot.Persona.Content)
	}
	if snapshot.Persona.Reason == "" {
		t.Fatal("an unavailable persona carries no typed reason")
	}
	if snapshot.Preimage(nil).HasPinnedContent() {
		t.Fatal("an unreadable pin claims pinned content")
	}
	if !snapshot.Preimage(nil).PersonaWithheld {
		t.Fatal("an unreadable pin is not marked withheld")
	}
}

// Turning memory off is not the same as an empty vault: nothing is pinned, and
// both tiers say so through withheld rather than through an empty body.
func TestEgressMemorySnapshotPinsNothingWhenMemoryIsOff(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "Speak plainly.")
	writePin(t, vault, memoryfiles.ProfileFileName, "The user keeps chickens.")
	if _, err := repo.SetMemoryEnabled(ctx(), false); err != nil {
		t.Fatalf("SetMemoryEnabled: %v", err)
	}

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	if snapshot.Enabled {
		t.Fatal("the snapshot reports memory on after it was turned off")
	}
	preimage := snapshot.Preimage(nil)
	if preimage.PersonaBody != "" || preimage.ProfileBody != "" {
		t.Fatalf("memory is off but something was pinned: %+v", preimage)
	}
	if !preimage.PersonaWithheld || !preimage.ProfileWithheld {
		t.Fatalf("memory is off but the tiers are not withheld: %+v", preimage)
	}
	if preimage.HasPinnedContent() {
		t.Fatal("a disabled snapshot claims pinned content")
	}
}

// Whitespace that survives truncation is not content, and the trim decision has
// to be the same one the runtime makes.
func TestEgressMemorySnapshotTreatsWhitespacePinsAsEmpty(t *testing.T) {
	repo, vault, _ := newMemoryTestRepo(t)
	writePin(t, vault, memoryfiles.PersonaFileName, "   \n\t  ")
	writePin(t, vault, memoryfiles.ProfileFileName, "\n\n")

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	if snapshot.Preimage(nil).HasPinnedContent() {
		t.Fatal("whitespace-only pins claim pinned content")
	}
	if !snapshot.Persona.Available || !snapshot.Profile.Available {
		t.Fatal("a readable whitespace pin is reported as unavailable")
	}
}

// The tool half of the preimage comes from the frozen selected tools, so a run
// with memory tools and empty pins still hashes differently from one without.
func TestEgressMemorySnapshotPreimageCarriesSelectedMemoryTools(t *testing.T) {
	repo, _, _ := newMemoryTestRepo(t)

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	without, err := backendegress.MemorySnapshotFingerprint(snapshot.Preimage([]string{"files/read"}))
	if err != nil {
		t.Fatal(err)
	}
	with, err := backendegress.MemorySnapshotFingerprint(
		snapshot.Preimage([]string{"files/read", "memory/memory.search"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if without == with {
		t.Fatal("selecting a memory tool did not change the memory fingerprint")
	}
}

// A vault the orchestrator could not open is not an excuse to send nothing
// quietly: the tiers come back withheld and the run carries no memory claim.
func TestEgressMemorySnapshotWithoutAVault(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)

	snapshot, err := repo.EgressMemorySnapshot(ctx())
	if err != nil {
		t.Fatalf("EgressMemorySnapshot: %v", err)
	}
	preimage := snapshot.Preimage(nil)
	if !preimage.PersonaWithheld || !preimage.ProfileWithheld || preimage.HasPinnedContent() {
		t.Fatalf("a vault-less snapshot is not withheld: %+v", preimage)
	}
	if snapshot.Persona.Reason == "" || snapshot.Profile.Reason == "" {
		t.Fatalf("a vault-less snapshot carries no typed reason: %+v", snapshot)
	}
}

func tail(value string) string {
	if len(value) <= 120 {
		return value
	}
	return value[len(value)-120:]
}
