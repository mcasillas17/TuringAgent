package memoryfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// MaxVaultIndexedFiles bounds how many notes one scan will index. It is a
// refusal rather than a truncation: a vault silently indexed halfway is a
// search that quietly lies about what the user has written.
//
// The bound applies to scanning, searching and reconciliation only. Reading a
// belief by its pinned identity never consults it, so a large vault degrades
// discovery without breaking retrieval of memories already known.
const MaxVaultIndexedFiles = 4096

// noteFileExtension is the only extension this package indexes. Canvases,
// attachments and everything else Obsidian keeps in a vault are the user's,
// not Turing's.
const noteFileExtension = ".md"

// ErrVaultTooLarge marks a vault past the index bound.
var ErrVaultTooLarge = errors.New("vault holds more notes than memory indexing will scan")

// VaultArea says which part of the vault a note came from. Callers must keep
// them apart: beliefs are accepted memory and may be indexed for search, while
// inbox candidates are unreviewed model output about the user and must never
// turn up in a search over their memory.
type VaultArea string

const (
	AreaBeliefs VaultArea = "beliefs"
	AreaInbox   VaultArea = "inbox"
	AreaOther   VaultArea = "other"
)

// NoteStatus mirrors the managed/unmanaged distinction the client renders, plus
// the per-note error state a broken or ambiguous file lands in.
type NoteStatus string

const (
	NoteStatusManaged   NoteStatus = "managed"
	NoteStatusUnmanaged NoteStatus = "unmanaged"
	NoteStatusError     NoteStatus = "error"
)

// NoteRow is one scanned note. Content, RawFrontmatter and Body are the file's
// own bytes so a later writer can splice rather than re-encode.
type NoteRow struct {
	RelPath        string
	Area           VaultArea
	NoteID         string
	Kind           NoteKind
	Title          string
	Content        string
	ContentHash    string
	RawFrontmatter string
	Body           string
	EvidenceRefs   []string
	Status         NoteStatus
	ParseError     string
	// Indexable is false for every note the caller must not project into
	// search: broken frontmatter, or an identity two files both claim.
	Indexable   bool
	ModTimeUnix int64
	SizeBytes   int64
}

// SkippedEntry records something the walk deliberately did not index, so the
// user can be told why their file is not showing up instead of guessing.
type SkippedEntry struct {
	RelPath string
	Reason  string
}

// ScanResult is one whole-vault pass.
type ScanResult struct {
	Notes            []NoteRow
	Skipped          []SkippedEntry
	DuplicateNoteIDs []string
}

// NoteMetadata is the cheap identity of a file's contents.
//
// Accepted residual: two writes in the same second that leave the file exactly
// the same length look identical here, so a cache keyed on it can serve one
// stale search result. Reads by belief identity always go to disk, so the stale
// window is confined to discovery.
type NoteMetadata struct {
	ModTimeUnix int64
	SizeBytes   int64
	ContentHash string
}

// MetadataCache lets a caller keep what one scan read and hand it back to the
// next one. It is safe for concurrent use: two scans of the same vault share
// one cache, and the app scans on a timer while the user is editing.
//
// Entries hold the note exactly as it parsed, before anything the vault decides
// across files. Nothing whose answer depends on another note is stored here.
type MetadataCache struct {
	mutex   sync.RWMutex
	entries map[string]cacheEntry
}

// cacheEntry keeps the cheap identity beside the parse it belongs to. A caller
// that supplies metadata by hand gets an entry with no note attached, which is
// a miss for reuse and a hit for freshness questions.
type cacheEntry struct {
	metadata NoteMetadata
	note     NoteRow
	hasNote  bool
}

func NewMetadataCache() *MetadataCache {
	return &MetadataCache{entries: make(map[string]cacheEntry)}
}

func (c *MetadataCache) Put(relPath string, metadata NoteMetadata) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries[relPath] = cacheEntry{metadata: metadata}
}

func (c *MetadataCache) Get(relPath string) (NoteMetadata, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	entry, ok := c.entries[relPath]
	return entry.metadata, ok
}

// Fresh reports whether a cached entry still matches what the filesystem says.
func (c *MetadataCache) Fresh(relPath string, modTimeUnix int64, sizeBytes int64) bool {
	metadata, ok := c.Get(relPath)
	return ok && metadata.ModTimeUnix == modTimeUnix && metadata.SizeBytes == sizeBytes
}

func (c *MetadataCache) Len() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.entries)
}

// Drop forgets one path, for use when a note is removed.
func (c *MetadataCache) Drop(relPath string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.entries, relPath)
}

// The three methods below are what a scan uses. Each tolerates a nil cache, so
// scanning without one is the same code path with the caching turned off
// rather than a second implementation that can drift from this one.

func (c *MetadataCache) reusableRow(relPath string, modTimeUnix int64, sizeBytes int64) (NoteRow, bool) {
	if c == nil {
		return NoteRow{}, false
	}
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	entry, ok := c.entries[relPath]
	if !ok || !entry.hasNote {
		return NoteRow{}, false
	}
	if entry.metadata.ModTimeUnix != modTimeUnix || entry.metadata.SizeBytes != sizeBytes {
		return NoteRow{}, false
	}
	return entry.note, true
}

func (c *MetadataCache) putScanned(relPath string, note NoteRow) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries[relPath] = cacheEntry{
		metadata: NoteMetadata{
			ModTimeUnix: note.ModTimeUnix,
			SizeBytes:   note.SizeBytes,
			ContentHash: note.ContentHash,
		},
		note:    note,
		hasNote: true,
	}
}

// retain drops every path this pass did not see. A note the user deleted in
// Obsidian has to leave the cache with it, or a later reader is holding the
// only copy of a memory they asked to be rid of.
func (c *MetadataCache) retain(seen map[string]struct{}) {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for relPath := range c.entries {
		if _, ok := seen[relPath]; !ok {
			delete(c.entries, relPath)
		}
	}
}

type scanCandidate struct {
	relPath     string
	area        VaultArea
	modTimeUnix int64
	sizeBytes   int64
}

// Scan walks the vault with Lstat at every step, never Stat, so a symlink is
// seen as a symlink and skipped instead of being resolved into whatever it
// points at. Directories the vault does not own — every dot folder, so
// .obsidian and .trash included — are skipped whole.
func (v *Vault) Scan(ctx context.Context) (ScanResult, error) {
	return v.ScanWithCache(ctx, nil)
}

// ScanWithCache is the same pass with a cache the caller keeps between runs. A
// note whose modification time and length are unchanged is not re-read or
// re-parsed; it is served from what the last pass already read.
//
// Only a verdict the pass reached about a file's own bytes is cached. A read
// that failed — a cancelled context, an entry that vanished or could not be
// opened — decided nothing about the note, so it is never stored: a cancelled
// pass returns its context error and keeps nothing, and any other failure
// leaves a visible row this pass only, read again on the next one even though
// the file has not changed.
//
// The cache is deliberately not consulted for anything decided across the whole
// vault. Duplicate identities are recomputed on every pass, because that answer
// depends on the other files, and a cached verdict would leave the survivor of
// a resolved duplicate refused forever.
//
// Paths that no longer exist are dropped at the end of the pass, so a deleted
// note cannot be served out of the cache by a later reader.
func (v *Vault) ScanWithCache(ctx context.Context, cache *MetadataCache) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{}
	candidates, err := v.walkVault(ctx, &result)
	if err != nil {
		return ScanResult{}, err
	}
	notes := make([]NoteRow, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, err
		}
		seen[candidate.relPath] = struct{}{}
		row, reused := cache.reusableRow(candidate.relPath, candidate.modTimeUnix, candidate.sizeBytes)
		if !reused {
			cacheable := false
			row, cacheable, err = v.scanNoteRow(ctx, candidate)
			if err != nil {
				return ScanResult{}, err
			}
			if cacheable {
				cache.putScanned(candidate.relPath, row)
			}
		}
		notes = append(notes, row)
	}
	cache.retain(seen)
	sort.Slice(notes, func(first int, second int) bool {
		return notes[first].RelPath < notes[second].RelPath
	})
	result.Notes = notes
	result.DuplicateNoteIDs = flagDuplicateIdentities(result.Notes)
	sort.Slice(result.Skipped, func(first int, second int) bool {
		return result.Skipped[first].RelPath < result.Skipped[second].RelPath
	})
	return result, nil
}

// walkVault lists the vault breadth-first and refuses the moment it has seen
// one indexable note more than the bound allows. Stopping there rather than
// after the fact is the difference between a bounded walk and an unbounded one
// that reports a bound: a vault with a million notes must not be pulled into
// memory to be told it is too large.
func (v *Vault) walkVault(ctx context.Context, result *ScanResult) ([]scanCandidate, error) {
	var candidates []scanCandidate
	queue := []string{""}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relDirectory := queue[0]
		queue = queue[1:]

		components := []string(nil)
		if relDirectory != "" {
			components = strings.Split(relDirectory, "/")
		}
		directory, err := v.openDirectory(ctx, components, false)
		if err != nil {
			if relDirectory == "" && (errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist)) {
				return nil, nil
			}
			result.Skipped = append(result.Skipped, SkippedEntry{
				RelPath: relDirectory,
				Reason:  fmt.Sprintf("directory could not be opened: %v", err),
			})
			continue
		}
		names, readErr := directory.Readdirnames(-1)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				_ = directory.Close()
				return nil, err
			}
			relPath := name
			if relDirectory != "" {
				relPath = relDirectory + "/" + name
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, unix.ENOENT) {
					continue
				}
				result.Skipped = append(result.Skipped, SkippedEntry{RelPath: relPath, Reason: fmt.Sprintf("entry could not be inspected: %v", err)})
				continue
			}
			if reason := skipReason(name, relPath, stat); reason != "" {
				result.Skipped = append(result.Skipped, SkippedEntry{RelPath: relPath, Reason: reason})
				continue
			}
			if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
				if reason := descentRefusal(relPath); reason != "" {
					result.Skipped = append(result.Skipped, SkippedEntry{RelPath: relPath, Reason: reason})
					continue
				}
				queue = append(queue, relPath)
				continue
			}
			candidates = append(candidates, scanCandidate{
				relPath:     relPath,
				area:        areaOf(relPath),
				modTimeUnix: stat.Mtim.Sec,
				sizeBytes:   stat.Size,
			})
			if len(candidates) > MaxVaultIndexedFiles {
				_ = directory.Close()
				return nil, vaultTooLargeError()
			}
		}
		_ = directory.Close()
		if readErr != nil {
			result.Skipped = append(result.Skipped, SkippedEntry{
				RelPath: relDirectory,
				Reason:  fmt.Sprintf("directory listing was incomplete: %v", readErr),
			})
		}
	}
	sort.Slice(candidates, func(first int, second int) bool {
		return candidates[first].relPath < candidates[second].relPath
	})
	return candidates, nil
}

func vaultTooLargeError() error {
	return fmt.Errorf(
		"the vault holds more than %d indexable notes, past the %d-note scan bound; memory search and reconciliation are bounded so a large vault cannot stall the app: %w",
		MaxVaultIndexedFiles, MaxVaultIndexedFiles, ErrVaultTooLarge,
	)
}

// descentRefusal says why the walk will not go into a directory. The bound is
// the one every write gate already applies: a note deeper than
// MaxVaultPathDepth is a note no primitive in this package could write or
// rewrite, so indexing it would offer the user a memory nothing can maintain.
// An ordinary Obsidian tree is nowhere near it.
func descentRefusal(relPath string) string {
	if len(relPath) > MaxVaultPathBytes {
		return fmt.Sprintf("folder path exceeds the %d-byte vault path limit and is not walked", MaxVaultPathBytes)
	}
	if strings.Count(relPath, "/")+1 >= MaxVaultPathDepth {
		return fmt.Sprintf(
			"folder is %d levels deep, at the %d-level vault path limit; notes below it could not be written or rewritten, so they are not indexed",
			strings.Count(relPath, "/")+1, MaxVaultPathDepth,
		)
	}
	return ""
}

// skipReason returns why an entry is not indexed, or "" when it should be.
func skipReason(name string, relPath string, stat unix.Stat_t) string {
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return "entry is a symlink; the vault walk never follows links"
	}
	if strings.HasPrefix(name, ".") {
		return "dot entries belong to the editor, not to memory"
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
		return ""
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return "entry is not a regular file"
	}
	if isSyncConflictArtifact(name) {
		return "entry is a sync conflict artifact, not a note the user wrote"
	}
	if !strings.EqualFold(path.Ext(name), noteFileExtension) {
		return "only " + noteFileExtension + " files are indexed"
	}
	if relPath == PersonaFileName || relPath == ProfileFileName {
		return "pinned document, loaded separately and never indexed as a note"
	}
	return ""
}

// isSyncConflictArtifact matches the duplicate files Obsidian Sync, Dropbox,
// iCloud and Syncthing leave behind. Indexing them would double every note the
// user has ever had a conflict on.
func isSyncConflictArtifact(name string) bool {
	folded := strings.ToLower(name)
	return strings.Contains(folded, "conflicted copy") ||
		strings.Contains(folded, "sync-conflict") ||
		strings.Contains(folded, "conflicted version")
}

func areaOf(relPath string) VaultArea {
	switch {
	case strings.HasPrefix(relPath, BeliefsDirName+"/"):
		return AreaBeliefs
	case strings.HasPrefix(relPath, InboxDirName+"/"):
		return AreaInbox
	default:
		return AreaOther
	}
}

// scanNoteRow produces the row one candidate contributes to this pass and says
// whether that row may be cached. The distinction is the whole point: a row
// derived from bytes this pass read is a verdict about the file and keeps until
// the file changes, while a read that never got its bytes has decided nothing
// and must be tried again.
func (v *Vault) scanNoteRow(ctx context.Context, candidate scanCandidate) (NoteRow, bool, error) {
	row, err := v.readNoteRow(ctx, candidate)
	if err == nil {
		return row, true, nil
	}
	// A read that failed because the caller hung up decided nothing about this
	// note, so the pass reports the cancellation rather than inventing a note
	// error out of it — and keeps nothing. The context is the whole test: a
	// read that reports cancellation observed this same context, so ctx.Err()
	// is already set by the time its error arrives here.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return NoteRow{}, false, ctxErr
	}
	return unreadableNoteRow(candidate, err), false, nil
}

// unreadableNoteRow is what a pass shows for a note it could not read at all.
// The user is told which file and why — silence is the one behaviour the plan
// forbids — and the row is deliberately never cached, so the next pass reads
// the file again even though its modification time and length are unchanged. A
// note that is genuinely unreadable keeps its row on every pass; a note that
// was briefly unavailable comes back the moment it can be read.
func unreadableNoteRow(candidate scanCandidate, err error) NoteRow {
	return NoteRow{
		RelPath:     candidate.relPath,
		Area:        candidate.area,
		Status:      NoteStatusError,
		ModTimeUnix: candidate.modTimeUnix,
		SizeBytes:   candidate.sizeBytes,
		ParseError: fmt.Sprintf(
			"the note could not be read on this pass and will be read again on the next one: %v", err,
		),
	}
}

// readNoteRow reads one note and parses what it read. The error it returns is
// reserved for the read itself failing — a cancelled pass, an entry that could
// not be opened, a descriptor that gave out mid-file — and the caller decides
// what a failed read means for the pass. Content that was read and could not be
// parsed is not an error here: it is the per-note error row the plan asks for,
// and the only kind of refusal this package caches.
func (v *Vault) readNoteRow(ctx context.Context, candidate scanCandidate) (NoteRow, error) {
	row := NoteRow{
		RelPath:     candidate.relPath,
		Area:        candidate.area,
		Status:      NoteStatusError,
		ModTimeUnix: candidate.modTimeUnix,
		SizeBytes:   candidate.sizeBytes,
	}
	content, stat, err := v.readScannedNote(ctx, candidate.relPath)
	if err != nil {
		var overLimit *LimitError
		if !errors.As(err, &overLimit) {
			return NoteRow{}, err
		}
		// The one refusal the read reaches by counting bytes it did read, so it
		// is a statement about the file rather than about the read. Caching it
		// is what keeps a half-megabyte note from being pulled off the disk on
		// every timer tick, and its length is part of the cache key, so the
		// moment the user trims the note it is read again.
		row.ModTimeUnix = stat.Mtim.Sec
		row.SizeBytes = stat.Size
		row.ParseError = err.Error()
		return row, nil
	}
	row.Content = content
	row.ContentHash = ContentHash(content)
	row.ModTimeUnix = stat.Mtim.Sec
	row.SizeBytes = stat.Size

	parsed, err := ParseNote(candidate.relPath, content)
	if err != nil {
		row.ParseError = err.Error()
		return row, nil
	}
	row.NoteID = parsed.ID
	row.Kind = parsed.Kind
	row.Title = parsed.Title
	row.RawFrontmatter = parsed.RawFrontmatter
	row.Body = parsed.Body
	row.EvidenceRefs = parsed.Refs
	row.Indexable = true
	if parsed.Managed {
		row.Status = NoteStatusManaged
	} else {
		row.Status = NoteStatusUnmanaged
	}
	return row, nil
}

// readScannedNote is the scan's one read of a note's bytes, routed through the
// test seam when one is installed and straight to the confined read otherwise.
func (v *Vault) readScannedNote(ctx context.Context, relPath string) (string, unix.Stat_t, error) {
	if v.scanRead == nil {
		return v.readConfinedFile(ctx, relPath, MaxNoteBytes)
	}
	return v.scanRead(ctx, relPath, func() (string, unix.Stat_t, error) {
		return v.readConfinedFile(ctx, relPath, MaxNoteBytes)
	})
}

// flagDuplicateIdentities marks every copy of a shared identity as an error and
// indexes none of them. Picking a winner would silently drop one of the user's
// files from their own memory; saying so out loud lets them fix it.
func flagDuplicateIdentities(notes []NoteRow) []string {
	owners := map[string][]int{}
	for index, note := range notes {
		if note.NoteID == "" || !note.Indexable {
			continue
		}
		owners[note.NoteID] = append(owners[note.NoteID], index)
	}
	var duplicates []string
	for noteID, indexes := range owners {
		if len(indexes) < 2 {
			continue
		}
		duplicates = append(duplicates, noteID)
		paths := make([]string, 0, len(indexes))
		for _, index := range indexes {
			paths = append(paths, notes[index].RelPath)
		}
		for _, index := range indexes {
			notes[index].Status = NoteStatusError
			notes[index].Indexable = false
			notes[index].ParseError = fmt.Sprintf(
				"identity %q is claimed by %s; none of them are indexed until one is renamed or given its own id",
				noteID, strings.Join(paths, " and "),
			)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}
