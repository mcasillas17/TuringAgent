package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	createStagingPrefix = ".turing-create-"
	updateStagingPrefix = ".turing-update-"

	MaxSandboxPathBytes      = 4096
	MaxSandboxComponentBytes = 255
	MaxSandboxPathDepth      = 64
)

type pathLockEntry struct {
	token chan struct{}
	refs  int
}

type pathLockTable struct {
	mutex sync.Mutex
	locks map[string]*pathLockEntry
}

type safeDirectoryEntry struct {
	name string
	mode uint32
	err  error
}

var processPathLocks = newPathLockTable()

func newPathLockTable() *pathLockTable {
	return &pathLockTable{locks: make(map[string]*pathLockEntry)}
}

func (t *pathLockTable) lockContext(ctx context.Context, path string) (func(), error) {
	t.mutex.Lock()
	entry := t.locks[path]
	if entry == nil {
		entry = &pathLockEntry{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		t.locks[path] = entry
	}
	entry.refs++
	t.mutex.Unlock()

	releaseReference := func() {
		t.mutex.Lock()
		entry.refs--
		if entry.refs == 0 && t.locks[path] == entry {
			delete(t.locks, path)
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

func normalizeSandboxPath(input string) (string, []string, error) {
	if len(input) > MaxSandboxPathBytes {
		return "", nil, invalidParamsf("path exceeds %d-byte limit", MaxSandboxPathBytes)
	}
	if filepath.IsAbs(input) || filepath.VolumeName(input) != "" {
		return "", nil, invalidParams("path escapes sandbox")
	}
	clean := filepath.Clean(input)
	if clean == "" {
		clean = "."
	}
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", nil, invalidParams("path escapes sandbox")
	}
	if clean == "." {
		return clean, nil, nil
	}
	components := strings.Split(clean, string(filepath.Separator))
	if len(components) > MaxSandboxPathDepth {
		return "", nil, invalidParamsf("path exceeds %d components", MaxSandboxPathDepth)
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.IndexByte(component, 0) >= 0 {
			return "", nil, invalidParams("path escapes sandbox")
		}
		if len(component) > MaxSandboxComponentBytes {
			return "", nil, invalidParamsf("path component exceeds %d-byte limit", MaxSandboxComponentBytes)
		}
		if isInternalStagingName(component) {
			return "", nil, invalidParams("path uses a reserved internal name")
		}
	}
	return clean, components, nil
}

func isInternalStagingName(name string) bool {
	folded := strings.ToLower(name)
	return strings.HasPrefix(folded, createStagingPrefix) || strings.HasPrefix(folded, updateStagingPrefix)
}

func (f FilesTools) openRoot() (*os.File, error) {
	fd, err := unix.Open(f.root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open sandbox root: %w", err)
	}
	file := os.NewFile(uintptr(fd), f.root)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open sandbox root: invalid descriptor")
	}
	return file, nil
}

func (f FilesTools) openDirectoryPath(input string, create bool) (*os.File, string, error) {
	return f.openDirectoryPathContext(context.Background(), input, create)
}

func (f FilesTools) openDirectoryPathContext(ctx context.Context, input string, create bool) (*os.File, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	clean, components, err := normalizeSandboxPath(input)
	if err != nil {
		return nil, "", err
	}
	current, err := f.openRoot()
	if err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		_ = current.Close()
		return nil, "", err
	}
	for _, component := range components {
		if err := ctx.Err(); err != nil {
			_ = current.Close()
			return nil, "", err
		}
		created := false
		fd, openErr := unix.Openat(
			int(current.Fd()),
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil && create && errors.Is(openErr, unix.ENOENT) {
			if err := ctx.Err(); err != nil {
				_ = current.Close()
				return nil, "", err
			}
			mkdirErr := unix.Mkdirat(int(current.Fd()), component, 0700)
			switch {
			case mkdirErr == nil:
				created = true
			case !errors.Is(mkdirErr, unix.EEXIST):
				_ = current.Close()
				return nil, "", fmt.Errorf("create directory %q: %w", component, mkdirErr)
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
			return nil, "", fmt.Errorf("open directory %q: %w", component, openErr)
		}
		next := os.NewFile(uintptr(fd), component)
		if next == nil {
			_ = unix.Close(fd)
			_ = current.Close()
			return nil, "", fmt.Errorf("open directory %q: invalid descriptor", component)
		}
		if created {
			if syncErr := f.syncDirectory(next); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, "", fmt.Errorf("sync new directory %q: %w", component, syncErr)
			}
			if syncErr := f.syncDirectory(current); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, "", fmt.Errorf("sync parent for directory %q: %w", component, syncErr)
			}
		}
		if err := ctx.Err(); err != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, "", err
		}
		_ = current.Close()
		current = next
	}
	return current, clean, nil
}

func (f FilesTools) openParentPathContext(ctx context.Context, input string, create bool) (*os.File, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", err
	}
	clean, components, err := normalizeSandboxPath(input)
	if err != nil {
		return nil, "", "", err
	}
	if len(components) == 0 {
		return nil, "", "", invalidParams("path must name a file")
	}
	parentPath := "."
	if len(components) > 1 {
		parentPath = filepath.Join(components[:len(components)-1]...)
	}
	parent, _, err := f.openDirectoryPathContext(ctx, parentPath, create)
	if err != nil {
		return nil, "", "", err
	}
	return parent, components[len(components)-1], clean, nil
}

func (f FilesTools) openPathContext(ctx context.Context, input string, flags int) (*os.File, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	clean, components, err := normalizeSandboxPath(input)
	if err != nil {
		return nil, "", err
	}
	if len(components) == 0 {
		root, rootErr := f.openRoot()
		if rootErr == nil {
			if err := ctx.Err(); err != nil {
				_ = root.Close()
				return nil, "", err
			}
		}
		return root, clean, rootErr
	}
	parent, leaf, _, err := f.openParentPathContext(ctx, clean, false)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = parent.Close() }()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	fd, err := unix.Openat(int(parent.Fd()), leaf, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open %q: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		_ = unix.Close(fd)
		return nil, "", fmt.Errorf("open %q: invalid descriptor", clean)
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, "", err
	}
	return file, clean, nil
}

func (f FilesTools) openRegularFileContext(ctx context.Context, input string) (*os.File, string, *unix.Stat_t, error) {
	file, clean, err := f.openPathContext(ctx, input, unix.O_RDONLY|unix.O_NONBLOCK)
	if err != nil {
		return nil, "", nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		_ = file.Close()
		return nil, "", nil, fmt.Errorf("inspect %q: %w", clean, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = file.Close()
		return nil, "", nil, fmt.Errorf("inspect %q: unsupported file type", clean)
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, "", nil, err
	}
	return file, clean, &stat, nil
}

func readDirectoryEntriesAt(ctx context.Context, directory *os.File, limit int) ([]safeDirectoryEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, readErr := directory.Readdirnames(limit)
	entries := make([]safeDirectoryEntry, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var stat unix.Stat_t
		err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		entries = append(entries, safeDirectoryEntry{name: name, mode: uint32(stat.Mode), err: err})
	}
	return entries, readErr
}

func (f FilesTools) syncCreateAncestors(input string) error {
	return f.syncCreateAncestorsContext(context.Background(), input)
}

func (f FilesTools) syncCreateAncestorsContext(ctx context.Context, input string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, components, err := normalizeSandboxPath(input)
	if err != nil {
		return err
	}
	if len(components) <= 1 {
		return nil
	}
	for componentCount := len(components) - 2; componentCount >= 0; componentCount-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := "."
		if componentCount > 0 {
			path = filepath.Join(components[:componentCount]...)
		}
		directory, _, openErr := f.openDirectoryPathContext(ctx, path, false)
		if openErr != nil {
			return fmt.Errorf("open create ancestor %q: %w", path, openErr)
		}
		syncErr := f.syncDirectory(directory)
		_ = directory.Close()
		if syncErr != nil {
			return fmt.Errorf("sync create ancestor %q: %w", directory.Name(), syncErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func createTemporaryFile(parent *os.File, prefix string, mode uint32) (*os.File, string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		random := make([]byte, 12)
		if _, err := rand.Read(random); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(random)
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
			return nil, "", errors.New("create temporary file: invalid descriptor")
		}
		return file, name, nil
	}
	return nil, "", errors.New("could not allocate temporary file name")
}
