package envfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteAtomicCreatesTheFileAtMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := WriteAtomic(path, []byte("A=1\n")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "A=1\n", 0o600)
}

func TestWriteAtomicReplacesTheContentAndTightensTheMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("OLD=1\nOLD=2\nOLD=3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("NEW=1\n")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "NEW=1\n", 0o600)
}

func TestWriteAtomicIsNotAffectedByAPermissiveUmask(t *testing.T) {
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	path := filepath.Join(t.TempDir(), ".env")
	if err := WriteAtomic(path, []byte("A=1\n")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, path, "A=1\n", 0o600)
}

func TestWriteAtomicLeavesNoTemporaryFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := WriteAtomic(path, []byte("A=1\n")); err != nil {
		t.Fatal(err)
	}
	assertOnlyFiles(t, dir, ".env")
}

func TestWriteAtomicRefusesASymlinkAndLeavesItUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.WriteFile(target, []byte("TARGET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	err := WriteAtomic(link, []byte("EVIL=1\n"))
	if err == nil {
		t.Fatal("WriteAtomic wrote through a symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("the error does not say what is wrong with the path: %v", err)
	}
	assertFile(t, target, "TARGET=1\n", 0o600)
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v, %v", info, err)
	}
	assertOnlyFiles(t, dir, ".env", "real")
}

func TestWriteAtomicRefusesADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(link, []byte("A=1\n")); err == nil {
		t.Fatal("WriteAtomic replaced a dangling symlink")
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the symlink's target was created: %v", err)
	}
}

func TestWriteAtomicRefusesAPathThatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(path, []byte("A=1\n"))
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("WriteAtomic over a directory = %v", err)
	}
	assertOnlyFiles(t, dir, ".env")
}

func TestWriteAtomicLeavesNoTemporaryFileInAReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := WriteAtomic(path, []byte("B=2\n")); err == nil {
		t.Fatal("WriteAtomic succeeded in a read-only directory")
	}
	assertFile(t, path, "A=1\n", 0o600)
	assertOnlyFiles(t, dir, ".env")
}

func TestWriteAtomicRemovesTheTemporaryFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	renameErr := errors.New("rename refused")
	original := renameFile
	renameFile = func(string, string) error { return renameErr }
	t.Cleanup(func() { renameFile = original })

	if err := WriteAtomic(path, []byte("B=2\n")); !errors.Is(err, renameErr) {
		t.Fatalf("WriteAtomic = %v, want the rename error", err)
	}
	assertFile(t, path, "A=1\n", 0o600)
	assertOnlyFiles(t, dir, ".env")
}

func TestLockRefusesASecondHolderUntilTheFirstReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.lock")

	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock file = %v, %v, want mode 0600", info, err)
	}

	_, err = Lock(path)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock = %v, want ErrLocked", err)
	}
	if got, want := err.Error(), "another turing command is changing .env"; got != want {
		t.Fatalf("ErrLocked message = %q, want %q", got, want)
	}

	unlock()
	second, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock after unlock = %v", err)
	}
	second()
}

func TestUnlockMayBeCalledTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.lock")
	first, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	first()
	second, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second()

	// A second call of the first unlock must not release the lock the
	// second holder now owns.
	first()
	if _, err := Lock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("Lock = %v, want ErrLocked while the second holder is alive", err)
	}
}

func TestLockRefusesASymlinkedLockFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if unlock, err := Lock(link); err == nil {
		unlock()
		t.Fatal("Lock followed a symlink")
	}
}

func TestLockFailsWhenTheDirectoryDoesNotExist(t *testing.T) {
	unlock, err := Lock(filepath.Join(t.TempDir(), "missing", ".env.lock"))
	if err == nil {
		unlock()
		t.Fatal("Lock succeeded in a missing directory")
	}
	if errors.Is(err, ErrLocked) {
		t.Fatalf("a missing directory was reported as contention: %v", err)
	}
}

func assertFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("%s = %q, want %q", path, got, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode = %v, want %v", path, info.Mode().Perm(), mode)
	}
}

func assertOnlyFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("%s holds %v, want exactly %v", dir, got, names)
	}
}
