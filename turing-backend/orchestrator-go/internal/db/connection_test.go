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

func TestOpenMemoryDoesNotCreateFilesystemArtifact(t *testing.T) {
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := os.Lstat(":memory:"); !os.IsNotExist(err) {
		t.Fatalf("in-memory database created filesystem artifact: %v", err)
	}
}

func TestOpenKeepsFileBackedSQLiteTemporaryStorageInMemory(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	var tempStore int
	if err := database.QueryRow(`PRAGMA temp_store`).Scan(&tempStore); err != nil {
		t.Fatal(err)
	}
	if tempStore != 2 {
		t.Fatalf("PRAGMA temp_store = %d, want 2 (MEMORY)", tempStore)
	}

	if _, err := database.Exec(`
		CREATE TEMP TABLE temporary_payload (payload BLOB);
		WITH RECURSIVE sequence(value) AS (
			VALUES (1)
			UNION ALL
			SELECT value + 1 FROM sequence WHERE value < 128
		)
		INSERT INTO temporary_payload(payload)
		SELECT randomblob(1024) FROM sequence AS first CROSS JOIN sequence AS second;
	`); err != nil {
		t.Fatalf("exercise file-backed SQLite temporary storage: %v", err)
	}
	var rows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM temporary_payload`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 128*128 {
		t.Fatalf("temporary row count = %d, want %d", rows, 128*128)
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
