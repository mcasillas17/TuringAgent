package rootedfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBoundedRegularReadsAFileInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "FILE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ReadBoundedRegular(root, path, 16, info, "test root")
	if err != nil || string(data) != "hello" {
		t.Fatalf("data, err = %q, %v", data, err)
	}
	if _, err := ReadBoundedRegular(root, path, 4, info, "test root"); err == nil {
		t.Fatal("read a file over the bound")
	}
}

// The caller's earlier Lstat is the identity being read: a different file
// at the path, or the same file grown past the bound, is refused.
func TestReadBoundedRegularRefusesAFileChangedSinceItWasInspected(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "FILE.md")
	other := filepath.Join(root, "OTHER.md")
	for _, name := range []string{path, other} {
		if err := os.WriteFile(name, []byte("hi"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	otherInfo, err := os.Lstat(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBoundedRegular(root, path, 16, otherInfo, "test root"); err == nil ||
		!strings.Contains(err.Error(), "changed while it was being opened") {
		t.Fatalf("read with another file's identity: err = %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadBoundedRegular(root, path, 16, info, "test root"); err == nil ||
		!strings.Contains(err.Error(), "exceeds 16 bytes") {
		t.Fatalf("read a file grown past the bound since its Lstat: data %d bytes, err = %v", len(data), err)
	}
}

// A folder swapped for a symlink after the Lstat is refused, not followed.
func TestReadBoundedRegularRefusesASymlinkOnTheWay(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "FILE.md"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(outside, "FILE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadBoundedRegular(root, filepath.Join(root, "a", "FILE.md"), 64, info, "test root"); err == nil {
		t.Fatalf("followed a symlinked folder and read %q", data)
	}
	if _, err := ReadBoundedRegular(root, filepath.Join(outside, "FILE.md"), 64, info, "test root"); err == nil ||
		!strings.Contains(err.Error(), "inside the test root") {
		t.Fatalf("err = %v, want the root named", err)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	yaml, body, err := SplitFrontmatter("---\r\nname: x\r\n---\r\nbody\r\n", "FILE.md")
	if err != nil || yaml != "name: x" || body != "body\n" {
		t.Fatalf("yaml %q body %q err %v", yaml, body, err)
	}
	if yaml, body, err := SplitFrontmatter("---\nname: x\n---", "FILE.md"); err != nil || yaml != "name: x" || body != "" {
		t.Fatalf("closed at end: yaml %q body %q err %v", yaml, body, err)
	}
	for _, content := range []string{"name: x\n", "---\nname: x\n"} {
		if _, _, err := SplitFrontmatter(content, "FILE.md"); err == nil || !strings.Contains(err.Error(), "FILE.md") {
			t.Fatalf("SplitFrontmatter(%q) err = %v, want one naming the file", content, err)
		}
	}
}
