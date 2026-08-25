package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func scanPaths(result ScanResult) []string {
	paths := make([]string, 0, len(result.Notes))
	for _, note := range result.Notes {
		paths = append(paths, note.RelPath)
	}
	return paths
}

func TestScanIndexesOnlyMarkdownNotes(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/real.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nmanaged: true\n---\nkept\n")
	writeVaultFile(t, vault, "beliefs/notes.txt", "not markdown")
	writeVaultFile(t, vault, "beliefs/board.canvas", "{}")
	writeVaultFile(t, vault, ".obsidian/workspace.md", "obsidian state")
	writeVaultFile(t, vault, ".trash/deleted.md", "deleted note")
	writeVaultFile(t, vault, "beliefs/note (conflicted copy 2024-05-01).md", "sync artifact")
	writeVaultFile(t, vault, "beliefs/note.sync-conflict-20240501-123456-ABCDEFG.md", "sync artifact")
	writeVaultFile(t, vault, PersonaFileName, "persona")
	writeVaultFile(t, vault, ProfileFileName, "profile")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := strings.Join(scanPaths(result), ","); got != "beliefs/real.md" {
		t.Fatalf("indexed = %q", got)
	}
}

func TestScanSkipsSymlinksIncludingInsideBeliefs(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/real.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(vault.Root(), BeliefsDirName, "link.md")); err != nil {
		t.Fatalf("symlink file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), BeliefsDirName, "linked-folder")); err != nil {
		t.Fatalf("symlink folder: %v", err)
	}

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := strings.Join(scanPaths(result), ","); got != "beliefs/real.md" {
		t.Fatalf("a symlink was followed: %q", got)
	}
	skipped := strings.Join(skippedPaths(result), ",")
	if !strings.Contains(skipped, "beliefs/link.md") || !strings.Contains(skipped, "beliefs/linked-folder") {
		t.Fatalf("symlinks were not reported as skipped: %q", skipped)
	}
}

func skippedPaths(result ScanResult) []string {
	paths := make([]string, 0, len(result.Skipped))
	for _, entry := range result.Skipped {
		paths = append(paths, entry.RelPath)
	}
	return paths
}

func TestScanSeparatesBeliefsFromInbox(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/belief.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nbelief\n")
	writeVaultFile(t, vault, "inbox/candidate.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\nkind: \"belief\"\n---\ncandidate\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	areas := map[string]VaultArea{}
	for _, note := range result.Notes {
		areas[note.RelPath] = note.Area
	}
	if areas["beliefs/belief.md"] != AreaBeliefs {
		t.Fatalf("belief area = %q", areas["beliefs/belief.md"])
	}
	if areas["inbox/candidate.md"] != AreaInbox {
		t.Fatalf("candidate area = %q", areas["inbox/candidate.md"])
	}
}

func TestScanReportsManagedAndUnmanagedStatus(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/turing.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\nmanaged: true\n---\nturing wrote this\n")
	writeVaultFile(t, vault, "beliefs/user.md", "# The user wrote this\n\nBy hand.\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	statuses := map[string]NoteStatus{}
	for _, note := range result.Notes {
		statuses[note.RelPath] = note.Status
	}
	if statuses["beliefs/turing.md"] != NoteStatusManaged {
		t.Fatalf("managed status = %q", statuses["beliefs/turing.md"])
	}
	if statuses["beliefs/user.md"] != NoteStatusUnmanaged {
		t.Fatalf("unmanaged status = %q", statuses["beliefs/user.md"])
	}
}

func TestScanReportsAMalformedNotePerNoteAndKeepsGoing(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/good.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfine\n")
	writeVaultFile(t, vault, "beliefs/broken.md", "---\nid: \"unclosed\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("one broken note must not fail the whole vault: %v", err)
	}
	byPath := map[string]NoteRow{}
	for _, note := range result.Notes {
		byPath[note.RelPath] = note
	}
	broken, ok := byPath["beliefs/broken.md"]
	if !ok {
		t.Fatal("the broken note was dropped instead of reported")
	}
	if broken.Status != NoteStatusError {
		t.Fatalf("broken status = %q", broken.Status)
	}
	if broken.ParseError == "" {
		t.Fatal("the broken note carries no reason")
	}
	if broken.Indexable {
		t.Fatal("a broken note must not be indexed")
	}
	good := byPath["beliefs/good.md"]
	if good.Status != NoteStatusUnmanaged || !good.Indexable {
		t.Fatalf("the good note was affected: %+v", good)
	}
}

func TestScanFlagsDuplicateIdentitiesAndIndexesNeither(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/one.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfirst\n")
	writeVaultFile(t, vault, "beliefs/two.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nsecond\n")
	writeVaultFile(t, vault, "beliefs/three.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\nthird\n")

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	byPath := map[string]NoteRow{}
	for _, note := range result.Notes {
		byPath[note.RelPath] = note
	}
	for _, relPath := range []string{"beliefs/one.md", "beliefs/two.md"} {
		note := byPath[relPath]
		if note.Status != NoteStatusError {
			t.Fatalf("%s status = %q, both copies of a duplicate must be flagged", relPath, note.Status)
		}
		if note.Indexable {
			t.Fatalf("%s was indexed despite an ambiguous identity", relPath)
		}
		if !strings.Contains(note.ParseError, "01ARZ3NDEKTSV4RRFFQ69G5FAV") {
			t.Fatalf("%s reason does not name the identity: %q", relPath, note.ParseError)
		}
	}
	if !byPath["beliefs/three.md"].Indexable {
		t.Fatal("an unrelated note lost its index place")
	}
	if len(result.DuplicateNoteIDs) != 1 || result.DuplicateNoteIDs[0] != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("duplicate ids = %v", result.DuplicateNoteIDs)
	}
}

func TestScanCarriesModTimeAndSizeMetadata(t *testing.T) {
	vault := newTestVault(t)
	content := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nbody\n"
	writeVaultFile(t, vault, "beliefs/note.md", content)

	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Notes) != 1 {
		t.Fatalf("notes = %d", len(result.Notes))
	}
	note := result.Notes[0]
	if note.SizeBytes != int64(len(content)) {
		t.Fatalf("size = %d, want %d", note.SizeBytes, len(content))
	}
	if note.ModTimeUnix <= 0 {
		t.Fatalf("mod time = %d", note.ModTimeUnix)
	}
	if note.ContentHash != ContentHash(content) {
		t.Fatalf("content hash = %q", note.ContentHash)
	}
}

func TestScanRefusesAVaultOverTheIndexBound(t *testing.T) {
	vault := newTestVault(t)
	beliefs := filepath.Join(vault.Root(), BeliefsDirName)
	for index := 0; index <= MaxVaultIndexedFiles; index++ {
		name := filepath.Join(beliefs, fmt.Sprintf("note-%05d.md", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("write note %d: %v", index, err)
		}
	}
	_, err := vault.Scan(context.Background())
	if !errors.Is(err, ErrVaultTooLarge) {
		t.Fatalf("expected the index bound to refuse the scan, got %v", err)
	}
	if !strings.Contains(err.Error(), "4096") {
		t.Fatalf("refusal %q does not name the bound", err.Error())
	}
}

func TestScanToleratesAMissingVaultDirectory(t *testing.T) {
	root := t.TempDir()
	vault, err := Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	result, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("an empty vault must scan cleanly: %v", err)
	}
	if len(result.Notes) != 0 {
		t.Fatalf("notes = %v", scanPaths(result))
	}
}

func TestScanIsSafeUnderConcurrentUse(t *testing.T) {
	vault := newTestVault(t)
	for index := 0; index < 12; index++ {
		writeVaultFile(t, vault, fmt.Sprintf("beliefs/note-%02d.md", index), fmt.Sprintf("---\nid: \"note-%02d\"\n---\nbody\n", index))
	}
	cache := NewMetadataCache()

	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, 32)
	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			result, err := vault.Scan(context.Background())
			if err != nil {
				errorsChannel <- err
				return
			}
			for _, note := range result.Notes {
				cache.Put(note.RelPath, NoteMetadata{
					ModTimeUnix: note.ModTimeUnix,
					SizeBytes:   note.SizeBytes,
					ContentHash: note.ContentHash,
				})
				_, _ = cache.Get(note.RelPath)
				_ = cache.Fresh(note.RelPath, note.ModTimeUnix, note.SizeBytes)
			}
			if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
				Kind:  KindBelief,
				Title: fmt.Sprintf("worker %d", worker),
				Body:  "concurrent body",
			}); err != nil {
				errorsChannel <- err
			}
		}(worker)
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatalf("concurrent vault use failed: %v", err)
	}
	if cache.Len() != 12 {
		t.Fatalf("cache holds %d entries", cache.Len())
	}
}

func TestMetadataCacheReportsFreshness(t *testing.T) {
	cache := NewMetadataCache()
	cache.Put("beliefs/note.md", NoteMetadata{ModTimeUnix: 100, SizeBytes: 42, ContentHash: "sha256:x"})
	if !cache.Fresh("beliefs/note.md", 100, 42) {
		t.Fatal("expected an unchanged entry to be fresh")
	}
	if cache.Fresh("beliefs/note.md", 101, 42) {
		t.Fatal("a newer mtime must invalidate the entry")
	}
	if cache.Fresh("beliefs/note.md", 100, 43) {
		t.Fatal("a different size must invalidate the entry")
	}
	if cache.Fresh("beliefs/missing.md", 100, 42) {
		t.Fatal("an unknown path is never fresh")
	}
}

func TestLoadPersonaReadsTheRootDocument(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, "Talk plainly.\n")
	pinned := vault.LoadPersona(context.Background())
	if !pinned.Available {
		t.Fatalf("persona unavailable: %+v", pinned)
	}
	if pinned.Content != "Talk plainly.\n" {
		t.Fatalf("persona content = %q", pinned.Content)
	}
	if pinned.Truncated {
		t.Fatal("a short persona must not report truncation")
	}
	if pinned.RelPath != PersonaFileName {
		t.Fatalf("rel path = %q", pinned.RelPath)
	}
}

func TestLoadProfileReadsTheRootDocument(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, ProfileFileName, "Goes by Miguel.\n")
	pinned := vault.LoadProfile(context.Background())
	if !pinned.Available || pinned.Content != "Goes by Miguel.\n" {
		t.Fatalf("profile = %+v", pinned)
	}
}

func TestPinnedLoadsNeverReachTheInbox(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "inbox/candidate.md", "unreviewed claim about the user")
	persona := vault.LoadPersona(context.Background())
	profile := vault.LoadProfile(context.Background())
	for _, pinned := range []PinnedDocument{persona, profile} {
		if strings.Contains(pinned.Content, "unreviewed") {
			t.Fatalf("a pinned load reached the inbox: %+v", pinned)
		}
		if pinned.RelPath != PersonaFileName && pinned.RelPath != ProfileFileName {
			t.Fatalf("pinned rel path = %q", pinned.RelPath)
		}
	}
}

func TestLoadPersonaReportsMissingAsEmptyAndUnavailable(t *testing.T) {
	vault := newTestVault(t)
	pinned := vault.LoadPersona(context.Background())
	if pinned.Available {
		t.Fatal("a missing persona must not be reported as available")
	}
	if pinned.Content != "" {
		t.Fatalf("content = %q", pinned.Content)
	}
	if pinned.Reason != UnavailableVaultMissing {
		t.Fatalf("reason = %q", pinned.Reason)
	}
}

func TestLoadProfileReportsASymlinkAsUnavailableAndPinsNothing(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.md")
	if err := os.WriteFile(outside, []byte("smuggled profile"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), ProfileFileName)); err != nil {
		t.Fatalf("symlink profile: %v", err)
	}
	pinned := vault.LoadProfile(context.Background())
	if pinned.Available {
		t.Fatal("a symlinked profile must not be pinned")
	}
	if pinned.Content != "" {
		t.Fatalf("content = %q", pinned.Content)
	}
	if pinned.Reason != UnavailableVaultUnreadable {
		t.Fatalf("reason = %q", pinned.Reason)
	}
	if pinned.Detail == "" {
		t.Fatal("an unavailable pin must carry a visible reason")
	}
}

func TestLoadPersonaReportsAnUnreadablyLargeDocumentWithoutPartialLoading(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat("a", MaxNoteFileBytes+1))
	pinned := vault.LoadPersona(context.Background())
	if pinned.Available {
		t.Fatal("an over-large persona must not be pinned")
	}
	if pinned.Content != "" {
		t.Fatalf("a partial load happened: %d bytes", len(pinned.Content))
	}
	if pinned.Reason != UnavailableContentTooLarge {
		t.Fatalf("reason = %q", pinned.Reason)
	}
}

func TestLoadPersonaTruncatesOnRuneBoundariesWithANotice(t *testing.T) {
	vault := newTestVault(t)
	// Three-byte runes, so a naive byte cut at 4096 would land mid-rune.
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat("界", 3000))
	pinned := vault.LoadPersona(context.Background())
	if !pinned.Available {
		t.Fatalf("an over-pin-limit persona is still pinnable: %+v", pinned)
	}
	if !pinned.Truncated {
		t.Fatal("expected truncation to be reported")
	}
	if !utf8.ValidString(pinned.Content) {
		t.Fatal("truncation split a rune")
	}
	if strings.ContainsRune(pinned.Content, utf8.RuneError) {
		t.Fatal("truncation produced a replacement character")
	}
	body := strings.TrimSuffix(pinned.Content, truncationNotice(PersonaFileName, MaxPersonaBytes))
	if body == pinned.Content {
		t.Fatalf("no in-context truncation notice was added: %q", pinned.Content[len(pinned.Content)-120:])
	}
	if len(body) > MaxPersonaBytes {
		t.Fatalf("pinned %d bytes, limit is %d", len(body), MaxPersonaBytes)
	}
	if len(body) < MaxPersonaBytes-utf8.UTFMax {
		t.Fatalf("truncation threw away more than one rune: %d bytes", len(body))
	}
}

func TestLoadProfileTruncatesAtItsOwnLimit(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, ProfileFileName, strings.Repeat("b", MaxProfileBytes+500))
	pinned := vault.LoadProfile(context.Background())
	if !pinned.Truncated {
		t.Fatal("expected truncation")
	}
	body := strings.TrimSuffix(pinned.Content, truncationNotice(ProfileFileName, MaxProfileBytes))
	if len(body) != MaxProfileBytes {
		t.Fatalf("pinned %d bytes, limit is %d", len(body), MaxProfileBytes)
	}
}

func TestPinnedWhitespaceOnlyAfterTruncationIsEmpty(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, strings.Repeat(" ", MaxPersonaBytes+10)+"real text")
	pinned := vault.LoadPersona(context.Background())
	if pinned.Content != "" {
		t.Fatalf("whitespace-only truncation must pin nothing, got %q", pinned.Content)
	}
	if !pinned.Truncated {
		t.Fatal("expected truncation to still be reported")
	}
}

func TestReadBeliefByIDServesFreshBytes(t *testing.T) {
	vault := newTestVault(t)
	original := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\ntitle: \"Dark mode\"\n---\nfirst\n"
	path := writeVaultFile(t, vault, "beliefs/note.md", original)
	resolve := func(string) (string, bool) { return "beliefs/note.md", true }

	if _, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", resolve); err != nil {
		t.Fatalf("read belief: %v", err)
	}
	updated := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\ntitle: \"Dark mode\"\n---\nsecond\n"
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatalf("rewrite note: %v", err)
	}
	belief, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", resolve)
	if err != nil {
		t.Fatalf("read belief: %v", err)
	}
	if belief.Content != updated {
		t.Fatalf("stale bytes were served: %q", belief.Content)
	}
	if belief.ContentHash != ContentHash(updated) {
		t.Fatalf("content hash = %q", belief.ContentHash)
	}
	if belief.Title != "Dark mode" {
		t.Fatalf("title = %q", belief.Title)
	}
}

func TestReadBeliefByIDRechecksConfinementOfTheResolvedPath(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, PersonaFileName, "persona")
	writeVaultFile(t, vault, "inbox/candidate.md", "candidate")

	hostile := append([]string{
		PersonaFileName,
		ProfileFileName,
		"inbox/candidate.md",
		"/skills/evil/SKILL.md",
		"../../etc/passwd",
		"data/turing.db",
	}, escapingRelPathValues()...)
	for _, resolved := range hostile {
		_, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", func(string) (string, bool) {
			return resolved, true
		})
		if !errors.Is(err, ErrConfinement) {
			t.Fatalf("expected a resolver returning %q to be refused, got %v", resolved, err)
		}
	}
}

func TestReadBeliefByIDRefusesASymlinkedBelief(t *testing.T) {
	vault := newTestVault(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nsecret\n"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(vault.Root(), BeliefsDirName, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", func(string) (string, bool) {
		return "beliefs/link.md", true
	})
	if !errors.Is(err, ErrConfinement) {
		t.Fatalf("expected a symlinked belief to be refused, got %v", err)
	}
}

func TestReadBeliefByIDRefusesAnIdentityMismatch(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/note.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\nbody\n")
	_, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", func(string) (string, bool) {
		return "beliefs/note.md", true
	})
	if err == nil {
		t.Fatal("expected a file whose identity does not match to be refused")
	}
}

func TestReadBeliefByIDRefusesAnUnresolvableIdentity(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", func(string) (string, bool) {
		return "", false
	}); err == nil {
		t.Fatal("expected an unresolvable identity to be refused")
	}
	if _, err := vault.ReadBeliefByID(context.Background(), "", func(string) (string, bool) {
		return "beliefs/note.md", true
	}); err == nil {
		t.Fatal("expected an empty identity to be refused")
	}
	if _, err := vault.ReadBeliefByID(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", nil); err == nil {
		t.Fatal("expected a missing resolver to be refused")
	}
}
