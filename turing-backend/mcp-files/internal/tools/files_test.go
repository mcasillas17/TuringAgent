package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

const mutationContentByteLimit = 512 * 1024

func TestReadRejectsUnixAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("sandbox content"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/note.txt", "//note.txt"} {
		t.Run(path, func(t *testing.T) {
			if _, err := NewFilesTools(root).Read(map[string]any{"path": path}); err == nil {
				t.Fatalf("Read accepted absolute path %q", path)
			}
		})
	}
}

func TestReadRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := NewFilesTools(root).Read(map[string]any{"path": "../outside.txt"})
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
}

func TestReadInsideSandbox(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Read(map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if result["content"] != "hello" {
		t.Fatalf("unexpected content: %#v", result)
	}
}

func TestReadRequiresNonEmptyStringPath(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"path": ""},
		{"path": 123},
	} {
		_, err := NewFilesTools(t.TempDir()).Read(args)
		if err == nil || !strings.Contains(err.Error(), "path is required") {
			t.Fatalf("Read(%#v) error = %v, want path is required", args, err)
		}
	}
}

func TestReadRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "link.txt"}); err == nil {
		t.Fatalf("expected symlink escape rejection")
	}
}

func TestReadRejectsSymlinkEvenWhenTargetRemainsInsideSandbox(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFilesTools(root).Read(map[string]any{"path": "link.txt"}); err == nil {
		t.Fatal("Read followed a symlink")
	}
}

func TestReadRejectsFileTooLarge(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", maxReadBytes+1)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "large.txt"}); err == nil {
		t.Fatalf("expected max file size rejection")
	}
}

func TestReadHonorsMaxBytesWithTruncation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Read(map[string]any{"path": "note.txt", "maxBytes": float64(3)})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if result["content"] != "abc" || result["truncated"] != true || result["bytesRead"] != int64(6) {
		t.Fatalf("expected truncated content, got %#v", result)
	}
}

func TestReadRejectsBinaryContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0xff, 0xfe, 0xfd}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "binary.bin"}); err == nil {
		t.Fatalf("expected binary/invalid UTF-8 rejection")
	}
}

func TestReadBoundedContextStopsAtActualByteLimit(t *testing.T) {
	reader := &countingReader{remaining: 4096}

	content, bytesRead, reachedLimit, err := readBoundedContext(context.Background(), reader, 1024)

	if err != nil {
		t.Fatalf("readBoundedContext returned error: %v", err)
	}
	if len(content) != 1024 || bytesRead != 1024 || reader.read != 1024 {
		t.Fatalf("bounded read = len %d, bytesRead %d, source reads %d; want 1024", len(content), bytesRead, reader.read)
	}
	if !reachedLimit {
		t.Fatal("bounded read did not report reaching its limit")
	}
}

func TestReadBoundedContextChecksCancellationBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterReadReader{cancel: cancel}

	_, _, _, err := readBoundedContext(ctx, reader, 128*1024)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readBoundedContext error = %v, want context.Canceled", err)
	}
	if reader.reads != 1 {
		t.Fatalf("source reads = %d, want 1 before cancellation", reader.reads)
	}
}

func TestSearchInsideSandbox(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha beta"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("gamma"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "alpha"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || !strings.Contains(matches[0]["path"].(string), "note.txt") {
		t.Fatalf("unexpected matches: %#v", matches)
	}
	if result["entriesVisited"] != 3 {
		t.Fatalf("entriesVisited = %#v, want root plus two files", result["entriesVisited"])
	}
}

func TestSearchAcceptsFileAsStartingPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha beta"), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := NewFilesTools(root).Search(map[string]any{"path": "note.txt", "query": "alpha"})

	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["path"] != "note.txt" {
		t.Fatalf("matches = %#v, want note.txt", matches)
	}
	if result["incomplete"] != false {
		t.Fatalf("Search result = %#v, want complete file search", result)
	}
}

func TestSearchReportsTraversalAndReadFailuresAsIncomplete(t *testing.T) {
	root, err := os.MkdirTemp(".", ".search-errors-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socketPath := filepath.Join(root, "unreadable.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "missing"})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if result["incomplete"] != true || result["errorCount"] != 1 {
		t.Fatalf("Search result = %#v, want incomplete=true and errorCount=1", result)
	}
	details, ok := result["errors"].([]map[string]any)
	if !ok || len(details) != 1 || !strings.Contains(details[0]["path"].(string), "unreadable.sock") {
		t.Fatalf("Search errors = %#v, want socket path detail", result["errors"])
	}
}

func TestSearchReportsBoundedDirectoryWork(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < maxSearchEntries+100; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.txt", index)), []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "missing"})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	entriesRead, ok := result["directoryEntriesRead"].(int)
	if !ok || entriesRead > maxSearchEntries {
		t.Fatalf("directoryEntriesRead = %#v, want at most %d", result["directoryEntriesRead"], maxSearchEntries)
	}
	if result["truncated"] != true || result["directoriesScanned"] != 1 {
		t.Fatalf("Search result = %#v, want bounded single-directory truncation", result)
	}
}

func TestSearchSnippetAlwaysHasValidUTF8Boundaries(t *testing.T) {
	root := t.TempDir()
	text := strings.Repeat("界", 20) + "needle" + strings.Repeat("界", 20)
	if err := os.WriteFile(filepath.Join(root, "unicode.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "needle"})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("matches = %#v, want one", matches)
	}
	snippet := matches[0]["snippet"].(string)
	if !utf8.ValidString(snippet) {
		t.Fatalf("snippet is invalid UTF-8: %q", snippet)
	}
}

func TestListHonorsLimitAndReportsTruncation(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 3; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%d.txt", index)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewFilesTools(root).List(map[string]any{"path": ".", "limit": float64(2)})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	items := result["items"].([]map[string]any)
	if len(items) != 2 || result["truncated"] != true {
		t.Fatalf("List result = %#v, want two items and truncated=true", result)
	}
}

func TestListBoundsScanningWhenDirectoryContainsOnlyInternalStagingNames(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < maxListEntriesScanned; index++ {
		name := fmt.Sprintf("%s%04d", createStagingPrefix, index)
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewFilesTools(root).List(map[string]any{"limit": 10})

	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if items := result["items"].([]map[string]any); len(items) != 0 {
		t.Fatalf("List exposed internal staging entries: %#v", items)
	}
	if result["truncated"] != true {
		t.Fatalf("truncated = %#v, want true at scan bound", result["truncated"])
	}
}

func TestListRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFilesTools(root).List(map[string]any{"path": "link"}); err == nil {
		t.Fatal("List followed a symlinked directory")
	}
}

func TestSearchStopsAtTraversalBudget(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 1001; index++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.txt", index)), []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "missing"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if result["truncated"] != true {
		t.Fatalf("Search result = %#v, want truncated=true at traversal budget", result)
	}
}

func TestSearchStopsAtDirectoryTraversalBudget(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 2001; index++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("%04d", index)), 0700); err != nil {
			t.Fatal(err)
		}
	}

	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "missing"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if result["truncated"] != true {
		t.Fatalf("Search result = %#v, want truncated=true at directory traversal budget", result)
	}
}

func TestSearchRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("alpha secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "alpha"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result["matches"].([]map[string]any)) != 0 {
		t.Fatalf("expected symlink escape to be skipped, got %#v", result)
	}
}

func TestSearchRejectsSymlinkedStartingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "target", "note.txt"), []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	if _, err := NewFilesTools(root).Search(map[string]any{"path": "link", "query": "alpha"}); err == nil {
		t.Fatal("Search followed a symlinked starting directory")
	}
}

func TestCreateAndUpdateRequireValidatedApproval(t *testing.T) {
	root := t.TempDir()
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	if _, err := files.Create(map[string]any{"path": "note.txt", "content": "hello"}, "approval-token", "general_assistant"); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if validator.callCount() != 1 {
		t.Fatalf("approval validations after create = %d, want 1", validator.callCount())
	}
	if _, err := files.Update(map[string]any{"path": "note.txt", "content": "updated"}, "approval-token-2", "general_assistant"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if validator.callCount() != 2 {
		t.Fatalf("approval validations after update = %d, want 2", validator.callCount())
	}
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "updated" {
		t.Fatalf("expected updated content, got %q", string(content))
	}
}

func TestCreateRejectsApprovalForDifferentArgs(t *testing.T) {
	root := t.TempDir()
	validator := fakeApprovalValidator{valid: false}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	if _, err := files.Create(map[string]any{"path": "note.txt", "content": "hello"}, "bad-token", "general_assistant"); err == nil {
		t.Fatalf("expected approval validation failure")
	}
}

func TestCreateEnforcesContentByteLimitBeforeApproval(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "ASCII boundary", content: strings.Repeat("x", mutationContentByteLimit)},
		{name: "multibyte boundary", content: strings.Repeat("é", mutationContentByteLimit/2)},
		{name: "one byte over", content: strings.Repeat("x", mutationContentByteLimit+1), wantErr: true},
		{name: "multibyte over", content: strings.Repeat("é", mutationContentByteLimit/2+1), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			validator := &recordingApprovalValidator{}
			files := NewFilesTools(root).WithApprovalValidator(validator)

			_, err := files.Create(map[string]any{"path": "note.txt", "content": test.content}, "approval-token", "general_assistant")

			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "content exceeds") {
					t.Fatalf("Create error = %v, want content byte limit error", err)
				}
				if validator.callCount() != 0 {
					t.Fatalf("approval validations = %d, want 0", validator.callCount())
				}
				return
			}
			if err != nil {
				t.Fatalf("Create returned error at boundary: %v", err)
			}
			if validator.callCount() != 1 {
				t.Fatalf("approval validations = %d, want 1", validator.callCount())
			}
		})
	}
}

func TestUpdateEnforcesContentByteLimitBeforeApprovalOrMutation(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	_, err := files.Update(map[string]any{
		"path": "note.txt", "content": strings.Repeat("x", mutationContentByteLimit+1),
	}, "approval-token", "general_assistant")

	if err == nil || !strings.Contains(err.Error(), "content exceeds") {
		t.Fatalf("Update error = %v, want content byte limit error", err)
	}
	if validator.callCount() != 0 {
		t.Fatalf("approval validations = %d, want 0", validator.callCount())
	}
	content, readErr := os.ReadFile(note)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("content = %q, %v; want original", content, readErr)
	}
}

func TestCreateRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	_, err := files.Create(map[string]any{"path": "link/note.txt", "content": "hello"}, "approval-token", "general_assistant")

	if err == nil {
		t.Fatal("Create followed a symlinked parent")
	}
	if _, statErr := os.Stat(filepath.Join(root, "target", "note.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("Create mutated symlink target: %v", statErr)
	}
}

func TestUpdateRejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	_, err := files.Update(map[string]any{"path": "link.txt", "content": "updated"}, "approval-token", "general_assistant")

	if err == nil {
		t.Fatal("Update followed a symlink")
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("symlink target content = %q, %v; want original", content, readErr)
	}
}

func TestCreateIsExclusiveUnderConcurrency(t *testing.T) {
	const callers = 32
	root := t.TempDir()
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	start := make(chan struct{})
	results := make(chan error, callers)
	var workers sync.WaitGroup
	for index := 0; index < callers; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, err := files.Create(map[string]any{
				"path":    "note.txt",
				"content": strings.Repeat(fmt.Sprintf("%02d", index), mutationContentByteLimit/2),
			}, "approval-token", "general_assistant")
			results <- err
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful creates = %d, want exactly 1", successes)
	}
	if validator.callCount() != 1 {
		t.Fatalf("approval validations = %d, want only the successful create", validator.callCount())
	}
}

func TestUpdateExpectedHashIsCompareAndSwapUnderConcurrency(t *testing.T) {
	const callers = 16
	root := t.TempDir()
	original := strings.Repeat("a", mutationContentByteLimit)
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	validator := &recordingApprovalValidator{}
	instances := []FilesTools{
		NewFilesTools(root).WithApprovalValidator(validator),
		NewFilesTools(root).WithApprovalValidator(validator),
	}
	start := make(chan struct{})
	results := make(chan error, callers)
	var workers sync.WaitGroup
	for index := 0; index < callers; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			_, err := instances[index%len(instances)].Update(map[string]any{
				"path":         "note.txt",
				"content":      fmt.Sprintf("updated-%02d", index),
				"expectedHash": contentHash(original),
			}, "approval-token", "general_assistant")
			results <- err
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful compare-and-swap updates = %d, want exactly 1", successes)
	}
	if validator.callCount() != 1 {
		t.Fatalf("approval validations = %d, want only the successful update", validator.callCount())
	}
}

func TestCreatePublishesOnlyFsyncedContentAndSerializesRead(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("complete-content-", 16*1024)
	staged := make(chan struct{})
	release := make(chan struct{})
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	syncFile := files.syncFile
	files.syncFile = func(file *os.File) error {
		if err := syncFile(file); err != nil {
			return err
		}
		if strings.HasPrefix(file.Name(), ".turing-create-") {
			close(staged)
			<-release
		}
		return nil
	}

	createResult := make(chan error, 1)
	go func() {
		_, err := files.Create(map[string]any{
			"path": "note.txt", "content": content,
		}, "approval-token", "general_assistant")
		createResult <- err
	}()
	select {
	case <-staged:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not reach the fsynced staging point")
	}
	if _, err := os.Lstat(filepath.Join(root, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("final path became visible before staging completed: %v", err)
	}

	readResult := make(chan struct {
		result map[string]any
		err    error
	}, 1)
	go func() {
		result, err := files.Read(map[string]any{"path": "note.txt", "maxBytes": len(content)})
		readResult <- struct {
			result map[string]any
			err    error
		}{result: result, err: err}
	}()
	waitForPathLockRefs(t, files.pathLockKey("note.txt"), 2)
	select {
	case result := <-readResult:
		t.Fatalf("read completed while create held the path lock: %#v, %v", result.result, result.err)
	default:
	}

	close(release)
	if err := <-createResult; err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	read := <-readResult
	if read.err != nil {
		t.Fatalf("Read returned error: %v", read.err)
	}
	if read.result["content"] != content {
		t.Fatalf("Read returned %d bytes, want complete %d-byte content", len(read.result["content"].(string)), len(content))
	}
}

func TestCreateSerializesUpdateUntilAtomicPublication(t *testing.T) {
	root := t.TempDir()
	staged := make(chan struct{})
	release := make(chan struct{})
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	syncFile := files.syncFile
	files.syncFile = func(file *os.File) error {
		if err := syncFile(file); err != nil {
			return err
		}
		if strings.HasPrefix(file.Name(), ".turing-create-") {
			close(staged)
			<-release
		}
		return nil
	}

	createResult := make(chan error, 1)
	go func() {
		_, err := files.Create(map[string]any{
			"path": "note.txt", "content": "created",
		}, "create-approval", "general_assistant")
		createResult <- err
	}()
	select {
	case <-staged:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not reach the fsynced staging point")
	}

	updateResult := make(chan error, 1)
	go func() {
		_, err := files.Update(map[string]any{
			"path": "note.txt", "content": "updated",
		}, "update-approval", "general_assistant")
		updateResult <- err
	}()
	waitForPathLockRefs(t, files.pathLockKey("note.txt"), 2)
	if validator.callCount() != 1 {
		t.Fatalf("approval validations before create publication = %d, want only create", validator.callCount())
	}

	close(release)
	if err := <-createResult; err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := <-updateResult; err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if validator.callCount() != 2 {
		t.Fatalf("approval validations = %d, want one per successful mutation", validator.callCount())
	}
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(content) != "updated" {
		t.Fatalf("final content = %q, %v; want updated", content, err)
	}
}

func TestCreateAtomicInstallDoesNotReplaceConcurrentTarget(t *testing.T) {
	root := t.TempDir()
	staged := make(chan struct{})
	release := make(chan struct{})
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	syncFile := files.syncFile
	files.syncFile = func(file *os.File) error {
		if err := syncFile(file); err != nil {
			return err
		}
		if strings.HasPrefix(file.Name(), ".turing-create-") {
			close(staged)
			<-release
		}
		return nil
	}

	result := make(chan error, 1)
	go func() {
		_, err := files.Create(map[string]any{
			"path": "note.txt", "content": "approved content",
		}, "approval-token", "general_assistant")
		result <- err
	}()
	select {
	case <-staged:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not reach the fsynced staging point")
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("concurrent content"), 0600); err != nil {
		t.Fatal(err)
	}
	close(release)

	if err := <-result; err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Create error = %v, want file already exists", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(content) != "concurrent content" {
		t.Fatalf("final content = %q, %v; want concurrent content", content, err)
	}
}

func TestCreateStagingFilesAreInaccessibleToConcurrentTools(t *testing.T) {
	root := t.TempDir()
	const approvedContent = "approved content"
	staged := make(chan struct{})
	release := make(chan struct{})
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	syncFile := files.syncFile
	files.syncFile = func(file *os.File) error {
		if err := syncFile(file); err != nil {
			return err
		}
		if strings.HasPrefix(file.Name(), ".turing-create-") {
			close(staged)
			<-release
		}
		return nil
	}

	createResult := make(chan error, 1)
	go func() {
		_, err := files.Create(map[string]any{
			"path": "note.txt", "content": approvedContent,
		}, "create-approval", "general_assistant")
		createResult <- err
	}()
	select {
	case <-staged:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not reach the fsynced staging point")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	stagingName := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".turing-create-") {
			stagingName = entry.Name()
			break
		}
	}
	if stagingName == "" {
		close(release)
		t.Fatal("could not find create staging file")
	}

	listResult, listErr := files.List(map[string]any{})
	searchResult, searchErr := files.Search(map[string]any{"query": approvedContent})
	readResult, readErr := files.Read(map[string]any{"path": stagingName})
	_, updateErr := files.Update(map[string]any{
		"path": stagingName, "content": "substituted content",
	}, "update-approval", "general_assistant")
	close(release)
	createErr := <-createResult

	if listErr != nil {
		t.Fatalf("List returned error: %v", listErr)
	}
	for _, item := range listResult["items"].([]map[string]any) {
		if item["name"] == stagingName {
			t.Errorf("List exposed internal staging file %q", stagingName)
		}
	}
	if searchErr != nil {
		t.Fatalf("Search returned error: %v", searchErr)
	}
	for _, match := range searchResult["matches"].([]map[string]any) {
		if match["path"] == stagingName {
			t.Errorf("Search exposed internal staging file %q", stagingName)
		}
	}
	for _, detail := range searchResult["errors"].([]map[string]any) {
		if detail["path"] == stagingName {
			t.Errorf("Search exposed internal staging file %q as an error", stagingName)
		}
	}
	if searchResult["incomplete"] == true {
		t.Errorf("Search treated an internal staging file as a failure: %#v", searchResult["errors"])
	}
	if readErr == nil {
		t.Errorf("Read exposed internal staging content: %#v", readResult)
	}
	if updateErr == nil {
		t.Error("Update replaced an internal staging file")
	}
	if createErr != nil {
		t.Fatalf("Create returned error: %v", createErr)
	}
	if validator.callCount() != 1 {
		t.Fatalf("approval validations = %d, want only create", validator.callCount())
	}
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(content) != approvedContent {
		t.Fatalf("final content = %q, %v; want approved content", content, err)
	}
}

func TestMutationsFsyncContainingDirectoryBeforeSuccess(t *testing.T) {
	for _, mutation := range []string{"create", "update"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			if mutation == "update" {
				if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
			syncDirectory := files.syncDirectory
			var directorySyncs int
			files.syncDirectory = func(directory *os.File) error {
				directorySyncs++
				return syncDirectory(directory)
			}

			var err error
			if mutation == "create" {
				_, err = files.Create(map[string]any{
					"path": "note.txt", "content": "created",
				}, "approval-token", "general_assistant")
			} else {
				_, err = files.Update(map[string]any{
					"path": "note.txt", "content": "updated",
				}, "approval-token", "general_assistant")
			}

			if err != nil {
				t.Fatalf("%s returned error: %v", mutation, err)
			}
			if directorySyncs == 0 {
				t.Fatalf("%s acknowledged without syncing the containing directory", mutation)
			}
		})
	}
}

func TestConcurrentCreateFsyncsSharedAncestorsBeforeAcknowledgement(t *testing.T) {
	root := t.TempDir()
	first := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	second := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	firstRootSync := make(chan struct{})
	releaseFirst := make(chan struct{})
	var pauseFirstRootSync sync.Once
	firstSyncDirectory := first.syncDirectory
	first.syncDirectory = func(directory *os.File) error {
		if directory.Name() == first.root {
			pauseFirstRootSync.Do(func() {
				close(firstRootSync)
				<-releaseFirst
			})
		}
		return firstSyncDirectory(directory)
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := first.Create(map[string]any{
			"path": "shared/first.txt", "content": "first",
		}, "first-approval", "general_assistant")
		firstResult <- err
	}()
	select {
	case <-firstRootSync:
	case <-time.After(5 * time.Second):
		t.Fatal("first create did not pause before syncing the root")
	}

	secondSyncDirectory := second.syncDirectory
	var secondRootSyncs int
	second.syncDirectory = func(directory *os.File) error {
		if directory.Name() == second.root {
			secondRootSyncs++
		}
		return secondSyncDirectory(directory)
	}
	_, secondErr := second.Create(map[string]any{
		"path": "shared/second.txt", "content": "second",
	}, "second-approval", "general_assistant")
	close(releaseFirst)
	firstErr := <-firstResult

	if secondErr != nil {
		t.Fatalf("second Create returned error: %v", secondErr)
	}
	if secondRootSyncs == 0 {
		t.Fatal("second Create acknowledged without independently syncing the shared ancestor")
	}
	if firstErr != nil {
		t.Fatalf("first Create returned error: %v", firstErr)
	}
}

func TestCreateFsyncsNewDirectoryHierarchy(t *testing.T) {
	root := t.TempDir()
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	syncDirectory := files.syncDirectory
	synced := map[string]int{}
	files.syncDirectory = func(directory *os.File) error {
		synced[directory.Name()]++
		return syncDirectory(directory)
	}

	_, err := files.Create(map[string]any{
		"path": "nested/deeper/note.txt", "content": "created",
	}, "approval-token", "general_assistant")

	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	for _, directory := range []string{files.root, "nested", "deeper"} {
		if synced[directory] == 0 {
			t.Errorf("directory %q was not synced; calls = %#v", directory, synced)
		}
	}
}

func TestMutationsDoNotAcknowledgeDirectorySyncFailure(t *testing.T) {
	syncFailure := errors.New("directory sync failed")
	for _, mutation := range []string{"create", "update"} {
		t.Run(mutation, func(t *testing.T) {
			root := t.TempDir()
			if mutation == "update" {
				if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
			files.syncDirectory = func(*os.File) error { return syncFailure }

			var err error
			if mutation == "create" {
				_, err = files.Create(map[string]any{
					"path": "note.txt", "content": "created",
				}, "approval-token", "general_assistant")
			} else {
				_, err = files.Update(map[string]any{
					"path": "note.txt", "content": "updated",
				}, "approval-token", "general_assistant")
			}

			if !errors.Is(err, syncFailure) {
				t.Fatalf("%s error = %v, want directory sync failure", mutation, err)
			}
		})
	}
}

func TestUpdateWithoutExpectedHashHoldsPathLockThroughReplacement(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	const original = "original"
	if err := os.WriteFile(note, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	plain := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	compareAndSwap := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	lockKey := plain.pathLockKey("note.txt")
	unlock, err := processPathLocks.lockContext(context.Background(), lockKey)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	plainResult := make(chan error, 1)
	go func() {
		_, updateErr := plain.Update(map[string]any{
			"path": "note.txt", "content": "plain-update",
		}, "approval-token", "general_assistant")
		plainResult <- updateErr
	}()
	waitForPathLockRefs(t, lockKey, 2)

	compareAndSwapResult := make(chan error, 1)
	go func() {
		_, updateErr := compareAndSwap.Update(map[string]any{
			"path":         "note.txt",
			"content":      "compare-and-swap-update",
			"expectedHash": contentHash(original),
		}, "approval-token", "general_assistant")
		compareAndSwapResult <- updateErr
	}()
	waitForPathLockRefs(t, lockKey, 3)
	unlock()

	if err := <-plainResult; err != nil {
		t.Fatalf("plain update failed: %v", err)
	}
	if err := <-compareAndSwapResult; err == nil || !strings.Contains(err.Error(), "expectedHash mismatch") {
		t.Fatalf("compare-and-swap error = %v, want expectedHash mismatch", err)
	}
	content, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "plain-update" {
		t.Fatalf("final content = %q, want plain-update", content)
	}
}

func TestUpdateExpectedHashRejectsOversizedExistingFileWithoutUnboundedRead(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte(strings.Repeat("x", mutationContentByteLimit+1)), 0640); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	_, err := files.Update(map[string]any{
		"path":         "note.txt",
		"content":      "updated",
		"expectedHash": "sha256:" + strings.Repeat("0", sha256.Size*2),
	}, "approval-token", "general_assistant")

	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Update error = %v, want bounded existing-file error", err)
	}
	content, readErr := os.ReadFile(note)
	if readErr != nil || len(content) != mutationContentByteLimit+1 {
		t.Fatalf("existing file changed: len=%d, err=%v", len(content), readErr)
	}
}

func waitForPathLockRefs(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		processPathLocks.mutex.Lock()
		entry := processPathLocks.locks[path]
		got := 0
		if entry != nil {
			got = entry.refs
		}
		processPathLocks.mutex.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("path lock references = %d, want %d", got, want)
		}
		runtime.Gosched()
	}
}

func TestUpdatePreservesExistingPermissions(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(note, 0666); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	if _, err := files.Update(map[string]any{"path": "note.txt", "content": "updated"}, "approval-token", "general_assistant"); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	info, err := os.Stat(note)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0666 {
		t.Fatalf("updated mode = %04o, want 0666", info.Mode().Perm())
	}
}

func TestUpdateStripsSpecialPermissionBits(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	specialMode := os.FileMode(0755) | os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	if err := os.Chmod(note, specialMode); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(note)
	if err != nil {
		t.Fatal(err)
	}
	specialBits := os.ModeSetuid | os.ModeSetgid | os.ModeSticky
	if before.Mode()&specialBits != specialBits {
		t.Skipf("filesystem did not retain requested special bits: mode %v", before.Mode())
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	if _, err := files.Update(map[string]any{"path": "note.txt", "content": "updated"}, "approval-token", "general_assistant"); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	after, err := os.Stat(note)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != 0755 {
		t.Fatalf("updated permissions = %04o, want 0755", after.Mode().Perm())
	}
	if got := after.Mode() & specialBits; got != 0 {
		t.Fatalf("updated special bits = %v, want none", got)
	}
}

func TestUpdateTargetModeRequiresWriteBit(t *testing.T) {
	for _, mode := range []os.FileMode{0400, 0444} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			if err := requireUpdateWriteBits(uint32(mode)); err == nil {
				t.Fatalf("requireUpdateWriteBits(%04o) succeeded; want rejection", mode)
			}
		})
	}
}

func TestUpdateWithoutExpectedHashSupportsWriteOnlyFile(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(note, 0200); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})

	_, updateErr := files.Update(map[string]any{
		"path": "note.txt", "content": "updated",
	}, "approval-token", "general_assistant")

	if updateErr != nil {
		t.Fatalf("Update returned error for write-only file: %v", updateErr)
	}
	if err := os.Chmod(note, 0600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(note)
	if err != nil || string(content) != "updated" {
		t.Fatalf("updated content = %q, %v; want updated", content, err)
	}
}

func TestReadAndUpdateStayConfinedDuringParentRenameSymlinkRace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	insideDirectory := filepath.Join(root, "swap")
	parkedDirectory := filepath.Join(root, "parked")
	if err := os.Mkdir(insideDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(insideDirectory, "note.txt"), []byte("inside"), 0640); err != nil {
		t.Fatal(err)
	}
	outsideNote := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(outsideNote, []byte("outside"), 0640); err != nil {
		t.Fatal(err)
	}
	files := NewFilesTools(root).WithApprovalValidator(fakeApprovalValidator{valid: true})
	stop := make(chan struct{})
	var flipper sync.WaitGroup
	flipper.Add(1)
	go func() {
		defer flipper.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.Rename(insideDirectory, parkedDirectory); err != nil {
				runtime.Gosched()
				continue
			}
			if err := os.Symlink(outside, insideDirectory); err == nil {
				runtime.Gosched()
				_ = os.Remove(insideDirectory)
			}
			_ = os.Rename(parkedDirectory, insideDirectory)
		}
	}()

	for index := 0; index < 500; index++ {
		if result, err := files.Read(map[string]any{"path": "swap/note.txt"}); err == nil {
			content := result["content"].(string)
			if content != "inside" && content != "updated" {
				close(stop)
				flipper.Wait()
				t.Fatalf("Read escaped sandbox and returned %q", content)
			}
		}
		if index%10 == 0 {
			_, _ = files.Update(map[string]any{
				"path": "swap/note.txt", "content": "updated",
			}, "approval-token", "general_assistant")
		}
	}
	close(stop)
	flipper.Wait()

	outsideContent, err := os.ReadFile(outsideNote)
	if err != nil {
		t.Fatal(err)
	}
	if string(outsideContent) != "outside" {
		t.Fatalf("outside file was mutated to %q", outsideContent)
	}
}

func TestPathLockWaitHonorsCancellation(t *testing.T) {
	locks := newPathLockTable()
	unlock, err := locks.lockContext(context.Background(), "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	secondUnlock, err := locks.lockContext(ctx, "note.txt")

	if secondUnlock != nil {
		secondUnlock()
		t.Fatal("canceled lock wait acquired the lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lockContext error = %v, want context.Canceled", err)
	}
}

func TestPathLockKeyConservativelyFoldsCaseAliases(t *testing.T) {
	files := NewFilesTools(t.TempDir())

	aliases := []string{
		"directory/café.txt",
		"Directory/CAFÉ.txt",
		"directory/cafe\u0301.txt",
		"directory/CAFE\u0301.TXT",
	}
	want := files.pathLockKey(aliases[0])
	for _, alias := range aliases[1:] {
		if got := files.pathLockKey(alias); got != want {
			t.Fatalf("alias lock key %q = %q, want %q", alias, got, want)
		}
	}
}

func TestPathLockKeyUsesFullUnicodeCaseFolding(t *testing.T) {
	files := NewFilesTools(t.TempDir())

	sigma := files.pathLockKey("directory/Σ.txt")
	finalSigma := files.pathLockKey("directory/ς.txt")

	if sigma != finalSigma {
		t.Fatalf("Unicode case-alias lock keys differ: %q != %q", sigma, finalSigma)
	}
}

func TestReservedStagingNamesAreCaseInsensitive(t *testing.T) {
	for _, name := range []string{
		".turing-create-token",
		".TURING-CREATE-token",
		".Turing-Update-token",
	} {
		if !isInternalStagingName(name) {
			t.Errorf("isInternalStagingName(%q) = false, want true", name)
		}
	}
}

func TestCreateValidatesArgumentsBeforeApproval(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"path": "", "content": "hello"},
		{"path": "note.txt"},
		{"path": "note.txt", "content": 123},
		{"path": "note.txt", "content": "hello", "unexpected": true},
	} {
		root := t.TempDir()
		validator := &recordingApprovalValidator{}
		files := NewFilesTools(root).WithApprovalValidator(validator)

		_, err := files.Create(args, "approval-token", "general_assistant")

		if err == nil {
			t.Fatalf("Create(%#v) returned nil error", args)
		}
		if validator.callCount() != 0 {
			t.Fatalf("Create(%#v) approval validations = %d, want 0", args, validator.callCount())
		}
		if _, statErr := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("Create(%#v) mutated note.txt: %v", args, statErr)
		}
	}
}

func TestUpdateValidatesArgumentsBeforeApprovalAndMutation(t *testing.T) {
	for _, args := range []map[string]any{
		{},
		{"path": "", "content": "updated"},
		{"path": "note.txt"},
		{"path": "note.txt", "content": 123},
		{"path": "note.txt", "content": "updated", "expectedHash": "not-a-sha256"},
		{"path": "note.txt", "content": "updated", "unexpected": true},
	} {
		root := t.TempDir()
		note := filepath.Join(root, "note.txt")
		if err := os.WriteFile(note, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		validator := &recordingApprovalValidator{}
		files := NewFilesTools(root).WithApprovalValidator(validator)

		_, err := files.Update(args, "approval-token", "general_assistant")

		if err == nil {
			t.Fatalf("Update(%#v) returned nil error", args)
		}
		if validator.callCount() != 0 {
			t.Fatalf("Update(%#v) approval validations = %d, want 0", args, validator.callCount())
		}
		content, readErr := os.ReadFile(note)
		if readErr != nil || string(content) != "original" {
			t.Fatalf("Update(%#v) content = %q, %v; want original", args, content, readErr)
		}
	}
}

func TestCreateLocalPreconditionFailuresDoNotConsumeApproval(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		setup func(t *testing.T, root string)
	}{
		{
			name: "target exists",
			path: "note.txt",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "parent is symlink",
			path: "link/note.txt",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "target"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "path escapes sandbox",
			path: "../outside.txt",
			setup: func(*testing.T, string) {
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)
			validator := &recordingApprovalValidator{}
			files := NewFilesTools(root).WithApprovalValidator(validator)

			_, err := files.Create(map[string]any{
				"path": test.path, "content": "created",
			}, "approval-token", "general_assistant")

			if err == nil {
				t.Fatal("Create returned nil error")
			}
			if validator.callCount() != 0 {
				t.Fatalf("approval validations = %d, want 0", validator.callCount())
			}
		})
	}
}

func TestUpdateLocalPreconditionFailuresDoNotConsumeApproval(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		args  map[string]any
		setup func(t *testing.T, root string)
	}{
		{
			name: "target missing",
			path: "missing.txt",
			setup: func(*testing.T, string) {
			},
		},
		{
			name: "target is directory",
			path: "directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "target is symlink",
			path: "link.txt",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "expected hash mismatch",
			path: "note.txt",
			args: map[string]any{"expectedHash": "sha256:" + strings.Repeat("0", sha256.Size*2)},
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "path escapes sandbox",
			path: "../outside.txt",
			setup: func(*testing.T, string) {
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			test.setup(t, root)
			validator := &recordingApprovalValidator{}
			files := NewFilesTools(root).WithApprovalValidator(validator)
			args := map[string]any{"path": test.path, "content": "updated"}
			for key, value := range test.args {
				args[key] = value
			}

			_, err := files.Update(args, "approval-token", "general_assistant")

			if err == nil {
				t.Fatal("Update returned nil error")
			}
			if validator.callCount() != 0 {
				t.Fatalf("approval validations = %d, want 0", validator.callCount())
			}
		})
	}
}

func TestDeleteAndMoveDisabled(t *testing.T) {
	files := NewFilesTools(t.TempDir())
	if _, err := files.Call("files.delete", map[string]any{}, "", "general_assistant"); err == nil {
		t.Fatalf("expected delete to be disabled")
	}
	if _, err := files.Call("files.move", map[string]any{}, "", "general_assistant"); err == nil {
		t.Fatalf("expected move to be disabled")
	}
}

func TestCallContextRejectsCanceledSideEffectBeforeApprovalOrMutation(t *testing.T) {
	root := t.TempDir()
	validator := &recordingApprovalValidator{}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := files.CallContext(ctx, "files.create", map[string]any{
		"path": "note.txt", "content": "hello",
	}, "approval-token", "general_assistant")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallContext error = %v, want context.Canceled", err)
	}
	if validator.callCount() != 0 {
		t.Fatalf("approval validations = %d, want 0", validator.callCount())
	}
	if _, statErr := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("canceled call mutated note.txt: %v", statErr)
	}
}

func TestCallContextCancelsBlockingApprovalWithoutMutation(t *testing.T) {
	root := t.TempDir()
	validator := &blockingApprovalValidator{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := files.CallContext(ctx, "files.create", map[string]any{
			"path": "note.txt", "content": "hello",
		}, "approval-token", "general_assistant")
		result <- err
	}()
	select {
	case <-validator.entered:
	case <-time.After(time.Second):
		t.Fatal("approval validation did not start")
	}

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CallContext error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		close(validator.release)
		t.Fatal("CallContext did not stop after cancellation")
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("canceled call mutated note.txt: %v", err)
	}
}

func TestCallContextChecksCancellationAfterApproval(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	validator := cancelingApprovalValidator{cancel: cancel}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	_, err := files.CallContext(ctx, "files.create", map[string]any{
		"path": "note.txt", "content": "hello",
	}, "approval-token", "general_assistant")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallContext error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("canceled call mutated note.txt: %v", statErr)
	}
}

func TestCallContextDoesNotUpdateWhenApprovalCancelsContext(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	validator := cancelingApprovalValidator{cancel: cancel}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	_, err := files.CallContext(ctx, "files.update", map[string]any{
		"path": "note.txt", "content": "updated",
	}, "approval-token", "general_assistant")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CallContext error = %v, want context.Canceled", err)
	}
	content, readErr := os.ReadFile(note)
	if readErr != nil || string(content) != "original" {
		t.Fatalf("canceled call content = %q, %v; want original", content, readErr)
	}
}

type fakeApprovalValidator struct {
	valid bool
}

type recordingApprovalValidator struct {
	calls atomic.Int32
}

func (v *recordingApprovalValidator) ValidateContext(context.Context, string, string, map[string]any, string) error {
	v.calls.Add(1)
	return nil
}

func (v *recordingApprovalValidator) callCount() int {
	return int(v.calls.Load())
}

func (f fakeApprovalValidator) ValidateContext(_ context.Context, token string, tool string, args map[string]any, agentID string) error {
	if f.valid {
		return nil
	}
	return os.ErrPermission
}

type blockingApprovalValidator struct {
	entered chan struct{}
	release chan struct{}
}

func (v *blockingApprovalValidator) Validate(string, string, map[string]any, string) error {
	close(v.entered)
	<-v.release
	return nil
}

func (v *blockingApprovalValidator) ValidateContext(ctx context.Context, _ string, _ string, _ map[string]any, _ string) error {
	close(v.entered)
	<-ctx.Done()
	return ctx.Err()
}

type cancelingApprovalValidator struct {
	cancel context.CancelFunc
}

func (v cancelingApprovalValidator) Validate(string, string, map[string]any, string) error {
	v.cancel()
	return nil
}

func (v cancelingApprovalValidator) ValidateContext(context.Context, string, string, map[string]any, string) error {
	v.cancel()
	return nil
}

type countingReader struct {
	remaining int
	read      int
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	count := len(buffer)
	if count > r.remaining {
		count = r.remaining
	}
	for index := 0; index < count; index++ {
		buffer[index] = 'x'
	}
	r.remaining -= count
	r.read += count
	return count, nil
}

type cancelAfterReadReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelAfterReadReader) Read(buffer []byte) (int, error) {
	r.reads++
	for index := range buffer {
		buffer[index] = 'x'
	}
	r.cancel()
	return len(buffer), nil
}
