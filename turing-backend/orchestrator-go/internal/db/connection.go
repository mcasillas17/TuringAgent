package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/mattn/go-sqlite3"
	"golang.org/x/sys/unix"
)

const sqliteDriverName = "turing_sqlite3"

type DB struct {
	*sql.DB
}

func init() {
	sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{
		ConnectHook: configureSQLiteConnection,
	})
}

func configureSQLiteConnection(connection *sqlite3.SQLiteConn) error {
	// The orchestrator runs with a read-only root filesystem. Keep SQLite sort
	// and transient b-tree storage off /tmp on every pooled connection.
	if _, err := connection.Exec(`PRAGMA temp_store = MEMORY`, nil); err != nil {
		return fmt.Errorf("configure SQLite temporary storage: %w", err)
	}
	return nil
}

func Open(path string) (*DB, error) {
	inMemory := path == ":memory:"
	dsn := "file::memory:?_foreign_keys=on"
	if !inMemory {
		if err := secureSQLiteFile(path, true); err != nil {
			return nil, err
		}
		dsn = fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", path)
	}
	database, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	if !inMemory {
		if err := secureSQLiteArtifacts(path); err != nil {
			_ = database.Close()
			return nil, err
		}
	}
	return &DB{DB: database}, nil
}

func secureSQLiteArtifacts(path string) error {
	for _, candidate := range []string{path, path + "-journal", path + "-shm", path + "-wal"} {
		if err := secureSQLiteFile(candidate, false); err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
	}
	return nil
}

func secureSQLiteFile(path string, create bool) error {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return fmt.Errorf("open SQLite file securely: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect SQLite file: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("SQLite path must be a regular file")
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("SQLite file must be owned by the current user")
	}
	if err := unix.Fchmod(fd, 0600); err != nil {
		return fmt.Errorf("set SQLite file mode: %w", err)
	}
	return nil
}
