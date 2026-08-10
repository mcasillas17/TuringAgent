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
)

type pathLockEntry struct {
	token chan struct{}
	refs  int
}

type pathLockTable struct {
	mutex sync.Mutex
	locks map[string]*pathLockEntry
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
	trimmed := strings.TrimLeft(input, string(filepath.Separator))
	clean := filepath.Clean(trimmed)
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
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.IndexByte(component, 0) >= 0 {
			return "", nil, invalidParams("path escapes sandbox")
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
	clean, components, err := normalizeSandboxPath(input)
	if err != nil {
		return nil, "", err
	}
	current, err := f.openRoot()
	if err != nil {
		return nil, "", err
	}
	for _, component := range components {
		created := false
		fd, openErr := unix.Openat(
			int(current.Fd()),
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil && create && errors.Is(openErr, unix.ENOENT) {
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
		_ = current.Close()
		current = next
	}
	return current, clean, nil
}

func (f FilesTools) openParentPath(input string, create bool) (*os.File, string, string, error) {
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
	parent, _, err := f.openDirectoryPath(parentPath, create)
	if err != nil {
		return nil, "", "", err
	}
	return parent, components[len(components)-1], clean, nil
}

func (f FilesTools) openPath(input string, flags int) (*os.File, string, error) {
	clean, components, err := normalizeSandboxPath(input)
	if err != nil {
		return nil, "", err
	}
	if len(components) == 0 {
		root, rootErr := f.openRoot()
		return root, clean, rootErr
	}
	parent, leaf, _, err := f.openParentPath(clean, false)
	if err != nil {
		return nil, "", err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), leaf, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open %q: %w", clean, err)
	}
	file := os.NewFile(uintptr(fd), clean)
	if file == nil {
		_ = unix.Close(fd)
		return nil, "", fmt.Errorf("open %q: invalid descriptor", clean)
	}
	return file, clean, nil
}

func (f FilesTools) openRegularFile(input string) (*os.File, string, *unix.Stat_t, error) {
	file, clean, err := f.openPath(input, unix.O_RDONLY|unix.O_NONBLOCK)
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
	return file, clean, &stat, nil
}

func (f FilesTools) syncCreateAncestors(input string) error {
	_, components, err := normalizeSandboxPath(input)
	if err != nil {
		return err
	}
	if len(components) <= 1 {
		return nil
	}
	root, err := f.openRoot()
	if err != nil {
		return err
	}
	directories := []*os.File{root}
	defer func() {
		for _, directory := range directories {
			_ = directory.Close()
		}
	}()
	current := root
	for _, component := range components[:len(components)-2] {
		fd, openErr := unix.Openat(
			int(current.Fd()),
			component,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if openErr != nil {
			return fmt.Errorf("open create ancestor %q: %w", component, openErr)
		}
		next := os.NewFile(uintptr(fd), component)
		if next == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("open create ancestor %q: invalid descriptor", component)
		}
		directories = append(directories, next)
		current = next
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if syncErr := f.syncDirectory(directories[index]); syncErr != nil {
			return fmt.Errorf("sync create ancestor %q: %w", directories[index].Name(), syncErr)
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
