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

// MetadataCache is a concurrency-safe (mtime, size) map a later scan or search
// can consult before re-reading a file.
type MetadataCache struct {
	mutex   sync.RWMutex
	entries map[string]NoteMetadata
}

func NewMetadataCache() *MetadataCache {
	return &MetadataCache{entries: make(map[string]NoteMetadata)}
}

func (c *MetadataCache) Put(relPath string, metadata NoteMetadata) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.entries[relPath] = metadata
}

func (c *MetadataCache) Get(relPath string) (NoteMetadata, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	metadata, ok := c.entries[relPath]
	return metadata, ok
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
	if err := ctx.Err(); err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{}
	candidates, err := v.walkVault(ctx, &result)
	if err != nil {
		return ScanResult{}, err
	}
	if len(candidates) > MaxVaultIndexedFiles {
		return ScanResult{}, fmt.Errorf(
			"the vault holds %d notes, past the %d-note scan bound; memory search and reconciliation are bounded so a large vault cannot stall the app: %w",
			len(candidates), MaxVaultIndexedFiles, ErrVaultTooLarge,
		)
	}
	notes := make([]NoteRow, 0, len(candidates))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, err
		}
		notes = append(notes, v.readNoteRow(ctx, candidate))
	}
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
				queue = append(queue, relPath)
				continue
			}
			candidates = append(candidates, scanCandidate{
				relPath:     relPath,
				area:        areaOf(relPath),
				modTimeUnix: stat.Mtim.Sec,
				sizeBytes:   stat.Size,
			})
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

func (v *Vault) readNoteRow(ctx context.Context, candidate scanCandidate) NoteRow {
	row := NoteRow{
		RelPath:     candidate.relPath,
		Area:        candidate.area,
		Status:      NoteStatusError,
		ModTimeUnix: candidate.modTimeUnix,
		SizeBytes:   candidate.sizeBytes,
	}
	content, stat, err := v.readConfinedFile(ctx, candidate.relPath, MaxNoteFileBytes)
	if err != nil {
		row.ParseError = err.Error()
		return row
	}
	row.Content = content
	row.ContentHash = ContentHash(content)
	row.ModTimeUnix = stat.Mtim.Sec
	row.SizeBytes = stat.Size

	parsed, err := ParseNote(candidate.relPath, content)
	if err != nil {
		row.ParseError = err.Error()
		return row
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
	return row
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
