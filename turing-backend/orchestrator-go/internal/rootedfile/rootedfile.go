// Package rootedfile reads small files that must stay inside a root the user
// edits by hand, such as skills/ and team/, without following a symlink
// planted anywhere on the way.
package rootedfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ReadBoundedRegular reads filename, which info describes from an earlier
// Lstat, through descriptors opened one component at a time from root with
// O_NOFOLLOW, so a folder or file swapped for a symlink after the Lstat is
// refused rather than followed. rootLabel names the root in errors, such as
// "skills root".
func ReadBoundedRegular(root, filename string, maximum int64, info os.FileInfo, rootLabel string) ([]byte, error) {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("file must be regular and not a symlink")
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	fd, err := openWithin(root, filename, rootLabel)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filename)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open file descriptor")
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, errors.New("file changed while it was being opened")
	}
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, io.LimitReader(file, maximum+1)); err != nil {
		return nil, err
	}
	if int64(buffer.Len()) > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	return buffer.Bytes(), nil
}

func openWithin(root, filename, rootLabel string) (int, error) {
	relative, err := filepath.Rel(root, filename)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return -1, fmt.Errorf("file must remain inside the %s", rootLabel)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return -1, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return -1, fmt.Errorf("%s must be a real directory", rootLabel)
	}
	currentFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	currentFile := os.NewFile(uintptr(currentFD), root)
	if currentFile == nil {
		_ = unix.Close(currentFD)
		return -1, fmt.Errorf("open %s descriptor", rootLabel)
	}
	openedRootInfo, err := currentFile.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		_ = currentFile.Close()
		if err != nil {
			return -1, err
		}
		return -1, fmt.Errorf("%s changed while it was being opened", rootLabel)
	}

	components := strings.Split(relative, string(filepath.Separator))
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			_ = currentFile.Close()
			return -1, openErr
		}
		nextFile := os.NewFile(uintptr(nextFD), component)
		if nextFile == nil {
			_ = unix.Close(nextFD)
			_ = currentFile.Close()
			return -1, errors.New("open directory descriptor")
		}
		_ = currentFile.Close()
		currentFD, currentFile = nextFD, nextFile
	}
	leafFD, err := unix.Openat(currentFD, components[len(components)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	_ = currentFile.Close()
	if err != nil {
		return -1, err
	}
	return leafFD, nil
}

// SplitFrontmatter separates the YAML between the opening and closing ---
// fences from the body after them. Windows line endings read as Unix ones.
// fileName names the file in errors, such as "SKILL.md".
func SplitFrontmatter(content, fileName string) (string, string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("%s must begin with YAML frontmatter", fileName)
	}
	rest := strings.TrimPrefix(content, "---\n")
	closing := strings.Index(rest, "\n---\n")
	if closing < 0 {
		if strings.HasSuffix(rest, "\n---") {
			return strings.TrimSuffix(rest, "\n---"), "", nil
		}
		return "", "", fmt.Errorf("%s frontmatter is not closed", fileName)
	}
	return rest[:closing], rest[closing+5:], nil
}
