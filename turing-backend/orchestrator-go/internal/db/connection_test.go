package db

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestOpenCreatesSQLiteFilesWithPrivateModesRegardlessOfUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	path := filepath.Join(t.TempDir(), "turing.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.Exec(`CREATE TABLE private_mode_test (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		assertPrivateRegularFile(t, candidate)
	}
}

func TestOpenRestrictsExistingSQLiteFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	assertPrivateRegularFile(t, path)
}

func TestOpenRejectsDatabaseSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("database no-follow semantics are Unix-specific")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.db")
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "turing.db")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}

	if database, err := Open(path); err == nil {
		_ = database.Close()
		t.Fatal("Open accepted a symlink database path")
	}
}

func assertPrivateRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s mode = %v, want a regular file", path, info.Mode())
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("%s permissions = %04o, want 0600", path, got)
	}
}
