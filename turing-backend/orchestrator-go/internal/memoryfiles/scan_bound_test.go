package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedIndexableNotes(t *testing.T, vault *Vault, count int) {
	t.Helper()
	beliefs := filepath.Join(vault.Root(), BeliefsDirName)
	for index := 0; index < count; index++ {
		name := filepath.Join(beliefs, fmt.Sprintf("note-%05d.md", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("write note %d: %v", index, err)
		}
	}
}

// The index bound is a number the plan states, so the boundary itself is the
// contract: a vault holding exactly the bound is a vault that works. Testing
// only the over-limit side would let the comparison drift to >= and quietly
// refuse the last vault that was supposed to be fine.
func TestScanIndexBoundIsExactAndTheWalkStopsAtTheFirstFileOverIt(t *testing.T) {
	vault := newTestVault(t)
	seedIndexableNotes(t, vault, MaxVaultIndexedFiles)

	t.Run("exactly at the bound is indexed", func(t *testing.T) {
		result, err := vault.Scan(context.Background())
		if err != nil {
			t.Fatalf("a vault holding exactly %d notes must scan: %v", MaxVaultIndexedFiles, err)
		}
		if len(result.Notes) != MaxVaultIndexedFiles {
			t.Fatalf("indexed %d notes, want %d", len(result.Notes), MaxVaultIndexedFiles)
		}
	})

	if err := os.WriteFile(filepath.Join(vault.Root(), BeliefsDirName, "note-over.md"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write the note that goes over the bound: %v", err)
	}

	t.Run("one over the bound is refused legibly", func(t *testing.T) {
		_, err := vault.Scan(context.Background())
		if !errors.Is(err, ErrVaultTooLarge) {
			t.Fatalf("expected the index bound to refuse the scan, got %v", err)
		}
		if !strings.Contains(err.Error(), fmt.Sprint(MaxVaultIndexedFiles)) {
			t.Fatalf("refusal %q does not name the bound", err.Error())
		}
	})

	t.Run("the walk stops instead of accumulating the vault", func(t *testing.T) {
		result := ScanResult{}
		candidates, err := vault.walkVault(context.Background(), &result)
		if !errors.Is(err, ErrVaultTooLarge) {
			t.Fatalf("the walk itself must refuse, got %v", err)
		}
		if len(candidates) != 0 {
			t.Fatalf(
				"the walk returned %d candidates; it read the whole vault into memory before refusing it",
				len(candidates),
			)
		}
	})
}

// A vault is a folder on the user's disk, and a folder can be nested as deeply
// as the filesystem allows. The walk stops at the same depth every write gate
// stops at, so the scan can never index a note at a path the rest of this
// package would refuse to write.
func TestScanRefusesToDescendPastTheVaultPathDepth(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/people/projects/decisions/shallow.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nnormal\n")

	deep := BeliefsDirName
	for depth := 0; depth < MaxVaultPathDepth; depth++ {
		deep += "/d"
	}
	writeVaultFile(t, vault, deep+"/buried.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\nburied\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("a deep folder must not fail the whole scan: %v", err)
	}
	indexed := strings.Join(scanPaths(result), ",")
	if !strings.Contains(indexed, "beliefs/people/projects/decisions/shallow.md") {
		t.Fatalf("an ordinary Obsidian tree stopped being indexed: %q", indexed)
	}
	if strings.Contains(indexed, "buried.md") {
		t.Fatal("a note past the vault path depth was indexed")
	}
	var reason string
	for _, entry := range result.Skipped {
		if strings.HasPrefix(entry.RelPath, BeliefsDirName+"/d") {
			reason = entry.Reason
			break
		}
	}
	if reason == "" {
		t.Fatalf("the walk stopped silently; skipped = %v", result.Skipped)
	}
	if !strings.Contains(reason, fmt.Sprint(MaxVaultPathDepth)) {
		t.Fatalf("refusal %q does not name the depth bound", reason)
	}
}

// The depth bound is one of two reasons the walk refuses a folder; the other is
// the total path length, which a deep-but-narrow tree can hit long before the
// depth limit. It is checked here directly because building a 4 KiB path on
// disk runs into the operating system's own limits before it reaches ours.
func TestDescentRefusalCoversLengthAsWellAsDepth(t *testing.T) {
	if reason := descentRefusal("beliefs/people/miguel"); reason != "" {
		t.Fatalf("an ordinary folder was refused: %q", reason)
	}

	deep := BeliefsDirName + strings.Repeat("/d", MaxVaultPathDepth)
	reason := descentRefusal(deep)
	if reason == "" {
		t.Fatal("a folder at the depth limit must not be walked")
	}
	if !strings.Contains(reason, fmt.Sprint(MaxVaultPathDepth)) {
		t.Fatalf("depth refusal %q does not name the bound", reason)
	}

	// Few components, but a path no vault primitive could name.
	long := BeliefsDirName + "/" + strings.Repeat("a", MaxVaultPathBytes)
	reason = descentRefusal(long)
	if reason == "" {
		t.Fatal("a folder past the vault path byte limit must not be walked")
	}
	if !strings.Contains(reason, fmt.Sprint(MaxVaultPathBytes)) {
		t.Fatalf("length refusal %q does not name the bound", reason)
	}
}
