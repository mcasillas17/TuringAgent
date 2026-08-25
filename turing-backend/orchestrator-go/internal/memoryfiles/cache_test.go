package memoryfiles

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func vaultPath(t *testing.T, vault *Vault, relPath string) string {
	t.Helper()
	return filepath.Join(vault.Root(), filepath.FromSlash(relPath))
}

// rewriteKeepingMetadata replaces a file's bytes while leaving the two things
// the cache keys on — modification time and length — exactly as they were. It
// is the one edit a (mtime, size) cache cannot see, and the plan names it as an
// accepted residual rather than pretending it does not exist.
func rewriteKeepingMetadata(t *testing.T, path string, content string) {
	t.Helper()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if int64(len(content)) != before.Size() {
		t.Fatalf("this edit changes the length (%d -> %d), so it is not the residual case", before.Size(), len(content))
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("rewrite %q: %v", path, err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("restore times on %q: %v", path, err)
	}
}

func noteByPath(t *testing.T, result ScanResult, relPath string) NoteRow {
	t.Helper()
	for _, note := range result.Notes {
		if note.RelPath == relPath {
			return note
		}
	}
	t.Fatalf("%q was not scanned; got %v", relPath, scanPaths(result))
	return NoteRow{}
}

func TestScanWithCacheReusesUnchangedNotesAndNamesItsResidual(t *testing.T) {
	vault := newTestVault(t)
	original := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfirst\n"
	relPath := "beliefs/note.md"
	writeVaultFile(t, vault, relPath, original)
	cache := NewMetadataCache()

	if _, err := vault.ScanWithCache(context.Background(), cache); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if cache.Len() != 1 {
		t.Fatalf("cache holds %d entries after the first scan", cache.Len())
	}

	// Same second, same length, different words. The cache cannot tell.
	stale := strings.Replace(original, "first", "third", 1)
	rewriteKeepingMetadata(t, vaultPath(t, vault, relPath), stale)

	cached, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := noteByPath(t, cached, relPath).Content; got != original {
		t.Fatalf("the cached parse was not reused: content = %q", got)
	}

	// The residual is a cache property, not a vault property: a scan that does
	// not consult the cache reads the user's current bytes.
	fresh, err := vault.Scan(context.Background())
	if err != nil {
		t.Fatalf("uncached scan: %v", err)
	}
	if got := noteByPath(t, fresh, relPath).Content; got != stale {
		t.Fatalf("an uncached scan served %q; it must read the file", got)
	}
}

func TestScanWithCacheRereadsANoteWhoseLengthChanged(t *testing.T) {
	vault := newTestVault(t)
	relPath := "beliefs/note.md"
	writeVaultFile(t, vault, relPath, "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfirst\n")
	cache := NewMetadataCache()
	if _, err := vault.ScanWithCache(context.Background(), cache); err != nil {
		t.Fatalf("first scan: %v", err)
	}

	updated := "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\ntitle: \"Now titled\"\n---\nsecond and longer\n"
	writeVaultFile(t, vault, relPath, updated)

	result, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	note := noteByPath(t, result, relPath)
	if note.Content != updated {
		t.Fatalf("a changed note was served from the cache: %q", note.Content)
	}
	if note.Title != "Now titled" {
		t.Fatalf("the note was not reparsed: title = %q", note.Title)
	}
	if !cache.Fresh(relPath, note.ModTimeUnix, note.SizeBytes) {
		t.Fatal("the cache was not updated with what the rescan read")
	}
}

func TestScanWithCacheForgetsDeletedNotes(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/kept.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nkept\n")
	writeVaultFile(t, vault, "beliefs/gone.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAW\"\n---\ngone\n")
	cache := NewMetadataCache()
	if _, err := vault.ScanWithCache(context.Background(), cache); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if cache.Len() != 2 {
		t.Fatalf("cache holds %d entries", cache.Len())
	}

	if err := os.Remove(vaultPath(t, vault, "beliefs/gone.md")); err != nil {
		t.Fatalf("remove note: %v", err)
	}
	result, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if got := strings.Join(scanPaths(result), ","); got != "beliefs/kept.md" {
		t.Fatalf("a deleted note survived the scan: %q", got)
	}
	if _, ok := cache.Get("beliefs/gone.md"); ok {
		t.Fatal("the deleted note is still cached; a later reader would resurrect it")
	}
	if cache.Len() != 1 {
		t.Fatalf("cache holds %d entries after the deletion", cache.Len())
	}
}

// Duplicate identities are decided across the whole vault, so they cannot be
// baked into a per-file cache entry. Caching the flagged row would leave the
// survivor of a resolved duplicate permanently unindexed — the user fixes their
// vault and memory keeps refusing it.
func TestScanWithCacheKeepsDuplicateIdentitySemantics(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/one.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfirst\n")
	writeVaultFile(t, vault, "beliefs/two.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nsecond\n")
	cache := NewMetadataCache()

	first, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first.DuplicateNoteIDs) != 1 {
		t.Fatalf("duplicate ids = %v", first.DuplicateNoteIDs)
	}

	second, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second.DuplicateNoteIDs) != 1 {
		t.Fatalf("a cached rescan lost the duplicate report: %v", second.DuplicateNoteIDs)
	}
	for _, relPath := range []string{"beliefs/one.md", "beliefs/two.md"} {
		note := noteByPath(t, second, relPath)
		if note.Indexable || note.Status != NoteStatusError {
			t.Fatalf("%s was indexed from the cache despite an ambiguous identity: %+v", relPath, note)
		}
	}

	if err := os.Remove(vaultPath(t, vault, "beliefs/two.md")); err != nil {
		t.Fatalf("remove the duplicate: %v", err)
	}
	third, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	if len(third.DuplicateNoteIDs) != 0 {
		t.Fatalf("the duplicate was resolved but is still reported: %v", third.DuplicateNoteIDs)
	}
	survivor := noteByPath(t, third, "beliefs/one.md")
	if !survivor.Indexable || survivor.Status == NoteStatusError {
		t.Fatalf("the survivor stayed flagged after the duplicate was deleted: %+v", survivor)
	}
	if survivor.ParseError != "" {
		t.Fatalf("the survivor kept a stale reason: %q", survivor.ParseError)
	}
}

func TestScanWithCacheKeepsPerNoteErrorsAndLetsThemBeFixed(t *testing.T) {
	vault := newTestVault(t)
	writeVaultFile(t, vault, "beliefs/broken.md", "---\nid: \"unclosed\n")
	cache := NewMetadataCache()

	first, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if broken := noteByPath(t, first, "beliefs/broken.md"); broken.Indexable || broken.ParseError == "" {
		t.Fatalf("the broken note was indexed: %+v", broken)
	}

	cachedAgain, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if broken := noteByPath(t, cachedAgain, "beliefs/broken.md"); broken.Indexable || broken.ParseError == "" {
		t.Fatalf("a cached broken note came back indexable: %+v", broken)
	}

	writeVaultFile(t, vault, "beliefs/broken.md", "---\nid: \"01ARZ3NDEKTSV4RRFFQ69G5FAV\"\n---\nfixed by the user\n")
	fixed, err := vault.ScanWithCache(context.Background(), cache)
	if err != nil {
		t.Fatalf("third scan: %v", err)
	}
	repaired := noteByPath(t, fixed, "beliefs/broken.md")
	if !repaired.Indexable || repaired.ParseError != "" {
		t.Fatalf("the user fixed the note and the cache kept refusing it: %+v", repaired)
	}
}

func TestScanWithCacheIsSafeUnderConcurrentScans(t *testing.T) {
	const beliefs = 12
	const workers = 8

	vault := newTestVault(t)
	beliefPaths := make([]string, 0, beliefs)
	for index := 0; index < beliefs; index++ {
		relPath := fmt.Sprintf("beliefs/note-%02d.md", index)
		writeVaultFile(t, vault, relPath, fmt.Sprintf("---\nid: \"note-%02d\"\n---\nbody\n", index))
		beliefPaths = append(beliefPaths, relPath)
	}
	cache := NewMetadataCache()

	var waitGroup sync.WaitGroup
	failures := make(chan error, workers*2)
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			result, err := vault.ScanWithCache(context.Background(), cache)
			if err != nil {
				failures <- err
				return
			}
			if len(result.Notes) < beliefs {
				failures <- fmt.Errorf("worker %d saw %d notes, fewer than the %d that never change", worker, len(result.Notes), beliefs)
				return
			}
			if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
				Kind:  KindBelief,
				Title: fmt.Sprintf("worker %d", worker),
				Body:  "concurrent body",
			}); err != nil {
				failures <- err
			}
		}(worker)
	}
	waitGroup.Wait()
	close(failures)
	for err := range failures {
		t.Fatalf("concurrent cached scan failed: %v", err)
	}

	// Every scan sees every belief, so no interleaving can evict one.
	for _, relPath := range beliefPaths {
		if _, ok := cache.Get(relPath); !ok {
			t.Fatalf("%q was evicted by a concurrent scan", relPath)
		}
	}
	if cache.Len() > beliefs+workers {
		t.Fatalf("cache holds %d entries, more than the vault can contain", cache.Len())
	}
}
