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

	"golang.org/x/sys/unix"
)

func newTestVaultRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{InboxDirName, BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	return root
}

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	return openTestVault(t, newTestVaultRoot(t), realSyncHooks())
}

// openTestVault is the only way a test injects durability hooks. They are
// constructor arguments rather than fields a test may assign, so production
// keeps the property that a live Vault's fsync discipline never changes.
func openTestVault(t *testing.T, root string, hooks syncHooks) *Vault {
	t.Helper()
	vault, err := openVault(root, hooks)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return vault
}

// syncRecorder remembers which inodes were fsynced, so a durability test can
// name the actual file and the actual directories that had to reach the disk
// instead of counting calls and hoping they were the right ones.
type syncRecorder struct {
	mutex       sync.Mutex
	files       []uint64
	directories []uint64
	// fileError, when set, fails every file fsync, which is how a test proves
	// a half-written note never appears under its final name.
	fileError error
	// failDirectorySyncNumber, when positive, fails that directory fsync and
	// only that one, counting from 1. It lets a test fail a specific step of a
	// multi-step mutation instead of all of them.
	failDirectorySyncNumber int
	// beforeDirectorySync runs before each directory fsync. It is the seam a
	// test uses to change the vault underneath an in-flight mutation.
	beforeDirectorySync func()
}

// setBeforeDirectorySync installs the interference hook after any setup writes
// have already happened, so a test only disturbs the operation it is about.
func (r *syncRecorder) setBeforeDirectorySync(hook func()) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.beforeDirectorySync = hook
}

// setFailDirectorySyncNumber arms the failure after setup, for the same reason.
func (r *syncRecorder) setFailDirectorySyncNumber(number int) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.failDirectorySyncNumber = number
	r.directories = nil
}

func (r *syncRecorder) hooks() syncHooks {
	return syncHooks{
		file: func(file *os.File) error {
			r.record(&r.files, file)
			if r.fileError != nil {
				return r.fileError
			}
			return file.Sync()
		},
		directory: func(directory *os.File) error {
			r.mutex.Lock()
			hook := r.beforeDirectorySync
			failAt := r.failDirectorySyncNumber
			number := len(r.directories) + 1
			r.mutex.Unlock()
			if hook != nil {
				hook()
			}
			r.record(&r.directories, directory)
			if failAt > 0 && number == failAt {
				return fmt.Errorf("simulated directory fsync failure on call %d", number)
			}
			return directory.Sync()
		},
	}
}

func (r *syncRecorder) record(into *[]uint64, file *os.File) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	*into = append(*into, uint64(stat.Ino))
}

func (r *syncRecorder) syncedFile(inode uint64) bool { return r.saw(&r.files, inode) }

func (r *syncRecorder) syncedDirectory(inode uint64) bool { return r.saw(&r.directories, inode) }

func (r *syncRecorder) saw(from *[]uint64, inode uint64) bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, seen := range *from {
		if seen == inode {
			return true
		}
	}
	return false
}

func TestOpenRejectsRelativeRoot(t *testing.T) {
	if _, err := Open("relative/vault"); err == nil {
		t.Fatal("expected a relative vault root to be refused")
	}
}

func TestOpenRejectsSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatalf("make real dir: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("expected a symlinked vault root to be refused")
	}
}

func TestOpenReportsCleanAbsoluteRoot(t *testing.T) {
	root := t.TempDir()
	vault, err := Open(root + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if !filepath.IsAbs(vault.Root()) {
		t.Fatalf("root %q is not absolute", vault.Root())
	}
	if vault.Root() != filepath.Clean(vault.Root()) {
		t.Fatalf("root %q is not clean", vault.Root())
	}
}

// The lock key is the vault root and the clean path, exactly as safe_fs keys
// its own table. Nothing else is folded in: the key has to be derivable from
// the path this package already normalised, with no dependency of its own.
func TestPathLockKeyIsTheRootAndTheCleanPath(t *testing.T) {
	vault := newTestVault(t)
	for _, clean := range []string{"inbox/Note.md", "beliefs/People/Miguel.md", ProfileFileName} {
		want := vault.Root() + "\x00" + clean
		if got := vault.pathLockKey(clean); got != want {
			t.Fatalf("lock key for %q = %q, want %q", clean, got, want)
		}
	}
}

func TestPathLockKeysSeparateVaultsAndPaths(t *testing.T) {
	first := newTestVault(t)
	second := newTestVault(t)
	if first.pathLockKey("inbox/a.md") == first.pathLockKey("inbox/b.md") {
		t.Fatal("two paths share one lock key")
	}
	if first.pathLockKey("inbox/a.md") == second.pathLockKey("inbox/a.md") {
		t.Fatal("two vaults share one lock key")
	}
}

var escapingRelPaths = []struct {
	name string
	path string
}{
	{"traversal", "../persona.md"},
	{"nested traversal", "inbox/../persona.md"},
	{"deep traversal", "inbox/../../../etc/passwd"},
	{"absolute", "/etc/passwd"},
	{"absolute skills", "/skills/evil.md"},
	{"database", "data/turing.db"},
	{"empty", ""},
	{"dot", "."},
	{"root persona", PersonaFileName},
	{"root profile", ProfileFileName},
	{"nul byte", "inbox/bad\x00name.md"},
}

func TestRequireInboxRelPathRefusesEscapes(t *testing.T) {
	for _, testCase := range escapingRelPaths {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := requireInboxRelPath(testCase.path); err == nil {
				t.Fatalf("expected %q to be refused as an inbox path", testCase.path)
			}
		})
	}
	for _, path := range []string{"beliefs/note.md", "inbox", "inboxer/note.md"} {
		if _, err := requireInboxRelPath(path); err == nil {
			t.Fatalf("expected %q to be refused as an inbox path", path)
		}
	}
	if _, err := requireInboxRelPath("inbox/note.md"); err != nil {
		t.Fatalf("expected inbox/note.md to be accepted: %v", err)
	}
	if _, err := requireInboxRelPath("inbox/sub/note.md"); err != nil {
		t.Fatalf("expected a nested inbox path to be accepted: %v", err)
	}
}

func TestRequireBeliefsRelPathRefusesEscapes(t *testing.T) {
	for _, testCase := range escapingRelPaths {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := requireBeliefsRelPath(testCase.path); err == nil {
				t.Fatalf("expected %q to be refused as a beliefs path", testCase.path)
			}
		})
	}
	if _, err := requireBeliefsRelPath("inbox/note.md"); err == nil {
		t.Fatal("expected an inbox path to be refused as a beliefs path")
	}
	if _, err := requireBeliefsRelPath("beliefs/note.md"); err != nil {
		t.Fatalf("expected beliefs/note.md to be accepted: %v", err)
	}
}

func TestRequireProfileRelPathAcceptsOnlyProfile(t *testing.T) {
	if _, err := requireProfileRelPath(ProfileFileName); err != nil {
		t.Fatalf("expected profile.md to be accepted: %v", err)
	}
	for _, path := range []string{PersonaFileName, "inbox/profile.md", "beliefs/profile.md", "../profile.md", "/profile.md", "Profile.md"} {
		if _, err := requireProfileRelPath(path); err == nil {
			t.Fatalf("expected %q to be refused as the profile path", path)
		}
	}
}

func TestRelPathRefusesOverlongComponent(t *testing.T) {
	long := strings.Repeat("a", MaxVaultPathComponentBytes+1)
	if _, err := requireInboxRelPath("inbox/" + long + ".md"); err == nil {
		t.Fatal("expected an overlong path component to be refused")
	}
}

func TestRelPathRefusesTooManyComponents(t *testing.T) {
	deep := InboxDirName
	for i := 0; i < MaxVaultPathDepth+1; i++ {
		deep += "/x"
	}
	if _, err := requireInboxRelPath(deep + ".md"); err == nil {
		t.Fatal("expected an over-deep path to be refused")
	}
}

func TestConfinementErrorsAreTyped(t *testing.T) {
	_, err := requireInboxRelPath("../persona.md")
	if !errors.Is(err, ErrConfinement) {
		t.Fatalf("expected a confinement error, got %v", err)
	}
}

func TestCreateInboxNoteWritesUnderInbox(t *testing.T) {
	vault := newTestVault(t)
	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "Prefers dark mode",
		Body:  "The user said they prefer dark mode.",
	})
	if err != nil {
		t.Fatalf("create inbox note: %v", err)
	}
	if !strings.HasPrefix(note.RelPath, InboxDirName+"/") {
		t.Fatalf("note path %q is not under the inbox", note.RelPath)
	}
	if !strings.HasSuffix(note.RelPath, ".md") {
		t.Fatalf("note path %q is not a markdown file", note.RelPath)
	}
	if note.NoteID == "" {
		t.Fatal("expected a frontmatter identity")
	}
	if !strings.Contains(note.RelPath, note.NoteID) {
		t.Fatalf("note path %q does not carry its identity %q", note.RelPath, note.NoteID)
	}
	onDisk, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(note.RelPath)))
	if err != nil {
		t.Fatalf("read created note: %v", err)
	}
	if string(onDisk) != note.Content {
		t.Fatalf("on-disk content does not match the reported content")
	}
	if !strings.Contains(string(onDisk), `id: "`+note.NoteID+`"`) {
		t.Fatalf("frontmatter is missing the note identity:\n%s", onDisk)
	}
	if !strings.Contains(string(onDisk), `kind: "belief"`) {
		t.Fatalf("frontmatter is missing the note kind:\n%s", onDisk)
	}
	if !strings.Contains(string(onDisk), "The user said they prefer dark mode.") {
		t.Fatalf("body was not preserved:\n%s", onDisk)
	}
}

func TestCreateInboxNoteRefusesUnknownKind(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind: NoteKind("belief; drop table"),
		Body: "x",
	}); err == nil {
		t.Fatal("expected an unknown candidate kind to be refused")
	}
}

func TestCreateInboxNoteRefusesEmptyBody(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind: KindBelief,
		Body: "   \n\t ",
	}); err == nil {
		t.Fatal("expected an empty candidate body to be refused")
	}
}

func TestCreateInboxNoteRefusesOverLimitBodyWithoutTruncating(t *testing.T) {
	vault := newTestVault(t)
	body := strings.Repeat("a", MaxCandidateBodyBytes+1)
	_, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "too big",
		Body:  body,
	})
	if err == nil {
		t.Fatal("expected an over-limit body to be refused")
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected a typed too-large error, got %v", err)
	}
	if !strings.Contains(err.Error(), "16384") {
		t.Fatalf("refusal %q does not name the byte limit", err.Error())
	}
	entries, readErr := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
	if readErr != nil {
		t.Fatalf("read inbox: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no file to be written, found %d", len(entries))
	}
}

func TestCreateInboxNoteAcceptsBodyExactlyAtLimit(t *testing.T) {
	vault := newTestVault(t)
	body := strings.Repeat("a", MaxCandidateBodyBytes)
	note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "at limit",
		Body:  body,
	})
	if err != nil {
		t.Fatalf("expected a body exactly at the limit to be accepted: %v", err)
	}
	if !strings.Contains(note.Content, body) {
		t.Fatal("body at the limit was not preserved whole")
	}
}

func TestCreateInboxNoteNeverLetsATitleControlThePath(t *testing.T) {
	vault := newTestVault(t)
	hostileTitles := []string{
		"../../persona",
		"/etc/passwd",
		"..",
		PersonaFileName,
		ProfileFileName,
		"beliefs/mine",
		strings.Repeat("z", 4096),
		"inbox\x00null",
	}
	for _, title := range hostileTitles {
		note, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
			Kind:  KindBelief,
			Title: title,
			Body:  "body",
		})
		if err != nil {
			t.Fatalf("title %q should be sanitized, not fatal: %v", title, err)
		}
		if !strings.HasPrefix(note.RelPath, InboxDirName+"/") {
			t.Fatalf("title %q escaped the inbox as %q", title, note.RelPath)
		}
		if strings.Count(note.RelPath, "/") != 1 {
			t.Fatalf("title %q produced a nested path %q", title, note.RelPath)
		}
		if _, err := os.Lstat(filepath.Join(vault.Root(), filepath.FromSlash(note.RelPath))); err != nil {
			t.Fatalf("title %q did not produce a readable file: %v", title, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), PersonaFileName)); !os.IsNotExist(err) {
		t.Fatal("a hostile title reached persona.md")
	}
}

func TestCreateInboxNoteAtRefusesPathsOutsideInbox(t *testing.T) {
	vault := newTestVault(t)
	for _, path := range append([]string{"beliefs/note.md"}, escapingRelPathValues()...) {
		if _, err := vault.createInboxNoteAt(context.Background(), path, "content"); err == nil {
			t.Fatalf("expected createInboxNote to refuse %q", path)
		}
	}
}

func escapingRelPathValues() []string {
	values := make([]string, 0, len(escapingRelPaths))
	for _, testCase := range escapingRelPaths {
		values = append(values, testCase.path)
	}
	return values
}

func TestCreateInboxNoteRefusesSymlinkedInbox(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("make outside dir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, InboxDirName)); err != nil {
		t.Fatalf("symlink inbox: %v", err)
	}
	vault, err := Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	if _, err := vault.CreateInboxNote(context.Background(), CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "escape",
		Body:  "body",
	}); err == nil {
		t.Fatal("expected a symlinked inbox to refuse the write")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read outside dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("write escaped the vault into %d entries", len(entries))
	}
}

func TestCreateInboxNoteRefusesSymlinkedFinalName(t *testing.T) {
	vault := newTestVault(t)
	target := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(vault.Root(), InboxDirName, "note.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink note: %v", err)
	}
	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "replacement"); err == nil {
		t.Fatal("expected a symlinked final name to refuse the write")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(content) != "original" {
		t.Fatalf("symlink target was written through: %q", content)
	}
}

func TestCreateInboxNoteIsExclusive(t *testing.T) {
	vault := newTestVault(t)
	existing := filepath.Join(vault.Root(), InboxDirName, "note.md")
	if err := os.WriteFile(existing, []byte("first"), 0o600); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	_, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "second")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected an exclusivity refusal, got %v", err)
	}
	content, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatalf("read existing: %v", readErr)
	}
	if string(content) != "first" {
		t.Fatalf("existing content was replaced: %q", content)
	}
}

func TestCreateInboxNoteLeavesNoPartialFinalName(t *testing.T) {
	recorder := &syncRecorder{fileError: errors.New("simulated fsync failure")}
	vault := openTestVault(t, newTestVaultRoot(t), recorder.hooks())
	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/note.md", "content"); err == nil {
		t.Fatal("expected the staged write to fail")
	}
	if _, err := os.Lstat(filepath.Join(vault.Root(), InboxDirName, "note.md")); !os.IsNotExist(err) {
		t.Fatalf("a partial file was installed under its final name: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(vault.Root(), InboxDirName))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging residue left behind: %v", entries)
	}
}

func TestCreateInboxNoteRefusesReservedStagingNames(t *testing.T) {
	vault := newTestVault(t)
	if _, err := vault.createInboxNoteAt(context.Background(), "inbox/"+stagingPrefix+"abc", "content"); err == nil {
		t.Fatal("expected a reserved staging name to be refused")
	}
}

func TestCreateInboxNoteHonoursContextCancellation(t *testing.T) {
	vault := newTestVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.CreateInboxNote(ctx, CreateInboxNoteRequest{
		Kind:  KindBelief,
		Title: "cancelled",
		Body:  "body",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

// The three path bounds are separate rules and each one has to hold on its own.
// A path can sit well inside the depth limit with every component well inside
// the component limit and still be far past the 4096-byte ceiling: 20 folders
// of 250 bytes is a legal shape and an illegal length. Without this leg the
// byte bound could be deleted outright and the depth and component checks would
// keep every existing test green.
func TestRelPathRefusesAnOverlongPathThatPassesEveryOtherBound(t *testing.T) {
	component := strings.Repeat("a", MaxVaultPathComponentBytes-5)
	components := make([]string, 0, 20)
	for range 20 {
		components = append(components, component)
	}
	deep := strings.Join(components, "/")
	if len(components)+1 >= MaxVaultPathDepth {
		t.Fatalf("the fixture is %d components deep, which the depth bound would refuse first", len(components)+1)
	}
	for _, name := range components {
		if len(name) > MaxVaultPathComponentBytes {
			t.Fatalf("the fixture has a %d-byte component, which the component bound would refuse first", len(name))
		}
	}

	for _, gate := range []struct {
		name  string
		check func(string) (string, error)
		path  string
	}{
		{"inbox", requireInboxRelPath, InboxDirName + "/" + deep + "/note.md"},
		{"beliefs", requireBeliefsRelPath, BeliefsDirName + "/" + deep + "/note.md"},
	} {
		t.Run(gate.name, func(t *testing.T) {
			if len(gate.path) <= MaxVaultPathBytes {
				t.Fatalf("the fixture is %d bytes, which is inside the %d-byte bound", len(gate.path), MaxVaultPathBytes)
			}
			_, err := gate.check(gate.path)
			if !errors.Is(err, ErrConfinement) {
				t.Fatalf("a %d-byte path was not refused: %v", len(gate.path), err)
			}
			if !strings.Contains(err.Error(), "4096-byte limit") {
				t.Fatalf("the refusal does not say which bound was hit: %v", err)
			}
		})
	}
}
