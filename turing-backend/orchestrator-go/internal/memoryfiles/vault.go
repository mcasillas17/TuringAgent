// Package memoryfiles is the structural confinement layer for the memory
// vault: an Obsidian-readable folder of Markdown the user owns and can open in
// their own editor. Every mutation here is descriptor-relative and refuses to
// follow a symlink at the root, at any component, or at the final entry, so a
// link planted inside the vault cannot turn a memory write into a write
// anywhere else on the user's disk.
//
// The security model is deliberately a reimplementation of the sandbox model in
// turing-backend/mcp-files/internal/tools: that module is a separate Go module
// with its own approval-token contract and cannot be imported from the
// orchestrator. The shape is the same on purpose — resolve the configured root
// once, walk it with openat and O_NOFOLLOW, take a per-path lock, stage every
// create under a random name and link it into place, and fsync the file and its
// parents — so the two layers fail the same way for the same reasons.
package memoryfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	// PersonaFileName and ProfileFileName are the two pinned documents. They
	// live at the vault root and are never reachable through a path argument
	// that names an inbox or beliefs entry.
	PersonaFileName = "persona.md"
	ProfileFileName = "profile.md"

	// InboxDirName holds unreviewed candidates; BeliefsDirName holds accepted
	// notes. No primitive may cross between them implicitly.
	InboxDirName   = "inbox"
	BeliefsDirName = "beliefs"

	MaxVaultPathBytes          = 4096
	MaxVaultPathComponentBytes = 255
	MaxVaultPathDepth          = 64

	// stagingPrefix names the half-written file a create holds before it is
	// linked into place. It begins with a dot so the vault walk skips it and so
	// the reserved-name check below refuses it as a caller-supplied component.
	stagingPrefix = ".turing-memory-"
)

var (
	// ErrConfinement is the single typed answer to "this path is not somewhere
	// this operation is allowed to touch". Callers match on it rather than on
	// message text.
	ErrConfinement = errors.New("path is outside the part of the vault this operation may touch")

	// ErrKind refuses a candidate whose kind does not match the primitive.
	ErrKind = errors.New("candidate kind is not allowed for this operation")

	// ErrUnboundDecision refuses a mutation that says neither which bytes it is
	// acting on nor, by name, why it cannot.
	//
	// Every decision primitive here reads the candidate under its own path lock
	// and then moves, applies or deletes it. The caller's earlier read released
	// that lock, so the compare-and-set has to be made against the read this
	// package did — and a caller that supplies no token is asking for the
	// mutation to be applied to whatever happens to be on disk. That is refused
	// rather than defaulted, and the two cases where no token can exist have
	// modes of their own.
	ErrUnboundDecision = errors.New("this mutation is not bound to the bytes it acts on")

	// ErrTooLarge refuses over-limit content instead of silently truncating it.
	ErrTooLarge = errors.New("content exceeds its limit")

	// ErrAlreadyExists is real exclusivity: the final name was already taken.
	ErrAlreadyExists = errors.New("file already exists")
)

// ConfinementError names the offending path and the rule it broke, so a refusal
// read by a human says which boundary was crossed.
type ConfinementError struct {
	Path   string
	Reason string
}

func (e *ConfinementError) Error() string {
	return fmt.Sprintf("%q is refused: %s", e.Path, e.Reason)
}

func (e *ConfinementError) Unwrap() error { return ErrConfinement }

func confinementError(path string, reason string) error {
	return &ConfinementError{Path: path, Reason: reason}
}

// LimitError reports which limit was exceeded and by how much, in bytes.
type LimitError struct {
	What  string
	Limit int
	Got   int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s is %d bytes, which exceeds the %d-byte limit", e.What, e.Got, e.Limit)
}

func (e *LimitError) Unwrap() error { return ErrTooLarge }

// Vault is a resolved vault root plus the fsync and locking discipline every
// mutation runs through. The zero value is unusable; construct one with Open.
type Vault struct {
	root  string
	locks *pathLockTable
	hooks syncHooks
	// scanRead wraps the read a scan performs on one note. Production installs
	// none, so the scan calls straight through. It is a constructor argument
	// for the same reason the sync hooks are: a Vault is shared across
	// goroutines, and a seam assignable on a live value would be a data race on
	// the pass that reads the user's memory.
	scanRead scanReadHook
	// readDirNames wraps one bounded batch of a directory listing, and is a
	// constructor argument for the same reason. Production installs none. A
	// test installs one to prove the walk never asks for an unbounded listing.
	readDirNames readDirNamesHook
}

// readDirNamesHook stands between the walk and one batch of directory entries.
// count is always positive: the walk's whole discipline is that it consults its
// bounds between batches, which it cannot do if the listing arrives all at once.
type readDirNamesHook func(directory *os.File, count int) ([]string, error)

// scanReadHook stands between a scan and one note's bytes. read is the real
// confined read, so a hook may fail, delay or delegate without reimplementing
// the descriptor walk.
type scanReadHook func(ctx context.Context, relPath string, read func() (string, unix.Stat_t, error)) (string, unix.Stat_t, error)

// syncHooks are the two durability calls every mutation runs through. They are
// fixed when the Vault is constructed and never reassigned afterwards: a Vault
// is shared across goroutines, and a hook swapped on a live value is a data
// race on the one code path whose whole job is to be trustworthy.
type syncHooks struct {
	file      func(*os.File) error
	directory func(*os.File) error
}

// realSyncHooks is the only set production ever uses.
func realSyncHooks() syncHooks {
	return syncHooks{
		file:      func(file *os.File) error { return file.Sync() },
		directory: func(directory *os.File) error { return directory.Sync() },
	}
}

func (v *Vault) syncFile(file *os.File) error { return v.hooks.file(file) }

func (v *Vault) syncDirectory(directory *os.File) error { return v.hooks.directory(directory) }

// Open resolves the configured vault root exactly once. Ancestors of the
// configured path may be symlinks — that is ordinary on macOS, where /var is a
// link — but the vault directory itself must be a real directory, and every
// path walked inside it afterwards refuses links outright.
func Open(root string) (*Vault, error) {
	return openVault(root, realSyncHooks())
}

// openVault is the one constructor. Hooks are a parameter rather than a field a
// caller may assign later, so the durability discipline of a live Vault cannot
// change underneath a goroutine already inside a write.
func openVault(root string, hooks syncHooks) (*Vault, error) {
	return openVaultWith(root, hooks, nil)
}

// openVaultWith adds the scan read seam, which only a test supplies.
func openVaultWith(root string, hooks syncHooks, scanRead scanReadHook) (*Vault, error) {
	return openVaultWithListing(root, hooks, scanRead, nil)
}

// openVaultWithListing adds the directory-listing seam alongside it. Both are
// nil in production, where the walk lists directories itself.
func openVaultWithListing(root string, hooks syncHooks, scanRead scanReadHook, readDirNames readDirNamesHook) (*Vault, error) {
	if hooks.file == nil || hooks.directory == nil {
		return nil, errors.New("vault sync hooks must both be set")
	}
	if root == "" {
		return nil, errors.New("vault root must be a non-empty absolute path")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("vault root %q must be an absolute path", root)
	}
	clean := filepath.Clean(root)
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, fmt.Errorf("inspect vault root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("vault root %q must be a real directory, not a symlink", clean)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault root %q must be a directory", clean)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	return &Vault{
		root:         filepath.Clean(resolved),
		locks:        processPathLocks,
		hooks:        hooks,
		scanRead:     scanRead,
		readDirNames: readDirNames,
	}, nil
}

// Root is the resolved absolute vault directory.
func (v *Vault) Root() string { return v.root }

func (v *Vault) pathLockKey(clean string) string {
	// The vault root plus the clean path, which is how safe_fs keys the table
	// this one is modelled on. The path has already been through
	// normalizeVaultPath, so the key is derived from bytes this package
	// controls rather than from a caller's spelling.
	//
	// Named residual: on a case-insensitive filesystem two spellings of one
	// name — inbox/Note.md and inbox/note.md — are one file with two keys, so
	// they do not exclude each other. Every path this package acts on is either
	// server-generated (a ULID and a slug, one canonical spelling) or a
	// fixed name, so the two-spelling case needs a caller to invent it; and the
	// confinement, the O_EXCL create and the fd-verified compare-and-set are
	// what actually keep a write honest. The lock is contention control, not a
	// safety boundary, and it does not carry a Unicode dependency for that.
	return v.root + "\x00" + clean
}

type pathLockEntry struct {
	token chan struct{}
	refs  int
}

type pathLockTable struct {
	mutex sync.Mutex
	locks map[string]*pathLockEntry
}

// Process-wide on purpose: two Vault values for the same root must contend on
// the same key, the way two FilesTools values do in the sandbox.
var processPathLocks = newPathLockTable()

func newPathLockTable() *pathLockTable {
	return &pathLockTable{locks: make(map[string]*pathLockEntry)}
}

func (t *pathLockTable) lockContext(ctx context.Context, key string) (func(), error) {
	t.mutex.Lock()
	entry := t.locks[key]
	if entry == nil {
		entry = &pathLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		t.locks[key] = entry
	}
	entry.refs++
	t.mutex.Unlock()

	releaseReference := func() {
		t.mutex.Lock()
		entry.refs--
		if entry.refs == 0 && t.locks[key] == entry {
			delete(t.locks, key)
		}
		t.mutex.Unlock()
	}
	if err := ctx.Err(); err != nil {
		releaseReference()
		return nil, err
	}
	select {
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			releaseReference()
			return nil, err
		}
		var once sync.Once
		return func() {
			once.Do(func() {
				entry.token <- struct{}{}
				releaseReference()
			})
		}, nil
	case <-ctx.Done():
		releaseReference()
		return nil, ctx.Err()
	}
}

// normalizeVaultPath turns a caller-supplied vault-relative path into its clean
// slash form and components, refusing anything that could leave the vault or
// name something the vault reserves.
func normalizeVaultPath(input string) (string, []string, error) {
	if input == "" {
		return "", nil, confinementError(input, "path is empty")
	}
	if len(input) > MaxVaultPathBytes {
		return "", nil, confinementError(input, fmt.Sprintf("path exceeds the %d-byte limit", MaxVaultPathBytes))
	}
	if strings.IndexByte(input, 0) >= 0 {
		return "", nil, confinementError(input, "path contains a NUL byte")
	}
	if strings.HasPrefix(input, "/") || filepath.IsAbs(input) || filepath.VolumeName(input) != "" {
		return "", nil, confinementError(input, "path must be relative to the vault root")
	}
	clean := path.Clean(filepath.ToSlash(input))
	if clean == "." || clean == ".." || clean == "/" || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
		return "", nil, confinementError(input, "path must name an entry inside the vault")
	}
	components := strings.Split(clean, "/")
	if len(components) > MaxVaultPathDepth {
		return "", nil, confinementError(input, fmt.Sprintf("path exceeds %d components", MaxVaultPathDepth))
	}
	for _, component := range components {
		switch {
		case component == "" || component == "." || component == "..":
			return "", nil, confinementError(input, "path contains an empty or traversing component")
		case len(component) > MaxVaultPathComponentBytes:
			return "", nil, confinementError(input, fmt.Sprintf("path component exceeds the %d-byte limit", MaxVaultPathComponentBytes))
		case strings.HasPrefix(component, "."):
			return "", nil, confinementError(input, "path components may not begin with '.': dot folders and Turing staging names are reserved")
		}
	}
	return clean, components, nil
}

func requireRelPathUnder(directory string, input string) (string, error) {
	clean, components, err := normalizeVaultPath(input)
	if err != nil {
		return "", err
	}
	if len(components) < 2 || components[0] != directory {
		return "", confinementError(input, "must name a file under "+directory+"/")
	}
	return clean, nil
}

// requireInboxRelPath is the inbox gate. Every primitive that touches a
// candidate calls it for itself.
func requireInboxRelPath(input string) (string, error) {
	return requireRelPathUnder(InboxDirName, input)
}

// requireBeliefsRelPath is the beliefs gate, called independently by every
// primitive that may read or write an accepted note.
func requireBeliefsRelPath(input string) (string, error) {
	return requireRelPathUnder(BeliefsDirName, input)
}

// requireProfileRelPath accepts profile.md and nothing else — not persona.md,
// not a same-named file parked under inbox/ or beliefs/.
func requireProfileRelPath(input string) (string, error) {
	clean, components, err := normalizeVaultPath(input)
	if err != nil {
		return "", err
	}
	if len(components) != 1 || components[0] != ProfileFileName {
		return "", confinementError(input, "only "+ProfileFileName+" may be edited here")
	}
	return clean, nil
}

// requirePersonaRelPath accepts persona.md and nothing else. Persona is
// read-only to every primitive an agent can reach; the only writer in this
// package is SavePersona, which the user drives directly.
func requirePersonaRelPath(input string) (string, error) {
	clean, components, err := normalizeVaultPath(input)
	if err != nil {
		return "", err
	}
	if len(components) != 1 || components[0] != PersonaFileName {
		return "", confinementError(input, "only "+PersonaFileName+" may be read or saved here")
	}
	return clean, nil
}

func (v *Vault) openRoot() (*os.File, error) {
	fd, err := unix.Open(v.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open vault root: %w", err)
	}
	file := os.NewFile(uintptr(fd), v.root)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open vault root: invalid descriptor")
	}
	return file, nil
}

// openDirectory walks the vault one component at a time from a descriptor on
// the root, refusing symlinks at every step. Nothing here ever resolves a path
// through the kernel's name lookup in one shot.
func (v *Vault) openDirectory(ctx context.Context, components []string, create bool) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := v.openRoot()
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		if err := ctx.Err(); err != nil {
			_ = current.Close()
			return nil, err
		}
		created := false
		fd, openErr := unix.Openat(
			int(current.Fd()),
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil && create && errors.Is(openErr, unix.ENOENT) {
			mkdirErr := unix.Mkdirat(int(current.Fd()), component, 0o700)
			switch {
			case mkdirErr == nil:
				created = true
			case !errors.Is(mkdirErr, unix.EEXIST):
				_ = current.Close()
				return nil, fmt.Errorf("create vault directory %q: %w", component, mkdirErr)
			}
			fd, openErr = unix.Openat(
				int(current.Fd()),
				component,
				unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
				0,
			)
		}
		if openErr != nil {
			_ = current.Close()
			return nil, fmt.Errorf("open vault directory %q: %w", component, openErr)
		}
		next := os.NewFile(uintptr(fd), component)
		if next == nil {
			_ = unix.Close(fd)
			_ = current.Close()
			return nil, fmt.Errorf("open vault directory %q: invalid descriptor", component)
		}
		if created {
			if syncErr := v.syncDirectory(next); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, fmt.Errorf("sync new vault directory %q: %w", component, syncErr)
			}
			if syncErr := v.syncDirectory(current); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, fmt.Errorf("sync parent of vault directory %q: %w", component, syncErr)
			}
		}
		_ = current.Close()
		current = next
	}
	return current, nil
}

// openParent returns a descriptor on the directory holding clean's final
// component, plus that component's name.
func (v *Vault) openParent(ctx context.Context, clean string, create bool) (*os.File, string, error) {
	components := strings.Split(clean, "/")
	if len(components) == 0 {
		return nil, "", confinementError(clean, "path must name a file")
	}
	parent, err := v.openDirectory(ctx, components[:len(components)-1], create)
	if err != nil {
		return nil, "", err
	}
	return parent, components[len(components)-1], nil
}

// syncAncestors flushes every directory above the immediate parent, so a create
// that added directories is durable all the way back to the vault root.
func (v *Vault) syncAncestors(ctx context.Context, clean string) error {
	components := strings.Split(clean, "/")
	for depth := len(components) - 2; depth >= 0; depth-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		directory, err := v.openDirectory(ctx, components[:depth], false)
		if err != nil {
			return err
		}
		syncErr := v.syncDirectory(directory)
		_ = directory.Close()
		if syncErr != nil {
			return syncErr
		}
	}
	return nil
}

func createStagingFile(parent *os.File, mode uint32) (*os.File, string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		name := stagingPrefix + hex.EncodeToString(random)
		fd, err := unix.Openat(
			int(parent.Fd()),
			name,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			mode,
		)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(int(parent.Fd()), name, 0)
			return nil, "", errors.New("stage vault file: invalid descriptor")
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate a vault staging name")
}

// installStagedFile writes content into a staging file next to its final name,
// fsyncs it, and only then links the final name into place. A crash at any
// point before the link leaves nothing but an unreferenced staging file, so
// partial content is never visible under the name a reader would open, and an
// EEXIST from the link is real exclusivity rather than a truncation race.
//
// Its contract is all-or-nothing: when it returns an error, the final name was
// not installed by this call. That is why a failed parent fsync unlinks the
// entry it just made — the name would be visible but uncommitted, while the
// caller has just been told the write did not happen.
func (v *Vault) installStagedFile(ctx context.Context, parent *os.File, leaf string, content string) error {
	staging, stagingName, err := createStagingFile(parent, 0o600)
	if err != nil {
		return fmt.Errorf("stage vault write: %w", err)
	}
	removeStaging := true
	defer func() {
		_ = staging.Close()
		if removeStaging {
			_ = unix.Unlinkat(int(parent.Fd()), stagingName, 0)
		}
	}()
	if err := writeAll(ctx, staging, []byte(content)); err != nil {
		return err
	}
	if err := v.syncFile(staging); err != nil {
		return fmt.Errorf("sync staged vault write: %w", err)
	}
	// Taken while the descriptor is still open, so the undo below can prove the
	// entry it removes is the one this call linked and not a file that arrived
	// under the same name afterwards.
	var staged unix.Stat_t
	if statErr := unix.Fstat(int(staging.Fd()), &staged); statErr != nil {
		return fmt.Errorf("inspect staged vault write: %w", statErr)
	}
	if err := staging.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Linkat(int(parent.Fd()), stagingName, int(parent.Fd()), leaf, 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("install %q: %w", leaf, ErrAlreadyExists)
		}
		return fmt.Errorf("install %q: %w", leaf, err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), stagingName, 0); err != nil {
		removeStaging = false
		v.undoInstall(parent, leaf, staged)
		return fmt.Errorf("remove vault staging file: %w", err)
	}
	removeStaging = false
	if err := v.syncDirectory(parent); err != nil {
		v.undoInstall(parent, leaf, staged)
		return fmt.Errorf("sync vault directory after install: %w", err)
	}
	if err := ctx.Err(); err != nil {
		v.undoInstall(parent, leaf, staged)
		return err
	}
	return nil
}

// undoInstall removes a name this call linked, and only if it is still that
// exact inode. It is best-effort by design: the error the caller is already
// returning is the one worth reporting, and a failure to undo leaves a visible
// file rather than a silent one.
func (v *Vault) undoInstall(parent *os.File, leaf string, installed unix.Stat_t) {
	var current unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return
	}
	if current.Dev != installed.Dev || current.Ino != installed.Ino {
		return
	}
	_ = unix.Unlinkat(int(parent.Fd()), leaf, 0)
	_ = v.syncDirectory(parent)
}

func writeAll(ctx context.Context, writer *os.File, content []byte) error {
	const chunkSize = 32 * 1024
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := chunkSize
		if len(content) < size {
			size = len(content)
		}
		written, err := writer.Write(content[:size])
		if err != nil {
			return err
		}
		if written <= 0 || written > size {
			return errors.New("short write to vault staging file")
		}
		content = content[written:]
	}
	return ctx.Err()
}
