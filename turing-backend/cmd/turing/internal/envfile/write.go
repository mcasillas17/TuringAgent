package envfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// ErrLocked reports that another turing command holds the .env lock.
var ErrLocked = errors.New("another turing command is changing .env")

// renameFile is a seam for the one step a test cannot otherwise make fail.
var renameFile = os.Rename

// WriteAtomic replaces path with data at mode 0600 so that an interruption
// leaves either the old file or the new one, never a partial one. It writes a
// temporary file in the same directory, syncs it, renames it over path and
// syncs the directory. It refuses a path that is a symlink or not a regular
// file.
func WriteAtomic(path string, data []byte) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if err := requireRegular(path, info); err != nil {
			return err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := fillTemporary(temporary, data); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := renameFile(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("%s was replaced, but its directory could not be synced: %w", path, err)
	}
	return nil
}

// fillTemporary writes, syncs and closes the temporary file. CreateTemp opens
// it with O_EXCL at 0600, and the chmod keeps a restrictive umask from
// lowering that.
func fillTemporary(file *os.File, data []byte) error {
	_, err := file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if err == nil {
		err = file.Chmod(0o600)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func syncDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = handle.Sync()
	if closeErr := handle.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Lock takes an exclusive advisory lock on path, creating it at 0600 when
// absent, so two commands cannot change .env at once. It does not wait: a held
// lock is ErrLocked. The returned unlock is safe to call more than once.
func Lock(path string) (unlock func(), err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}
