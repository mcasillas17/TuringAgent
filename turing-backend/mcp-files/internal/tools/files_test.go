package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if result["content"] != "abc" || result["truncated"] != true {
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

func TestCreateAndUpdateRequireValidatedApproval(t *testing.T) {
	root := t.TempDir()
	validator := fakeApprovalValidator{valid: true}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	if _, err := files.Create(map[string]any{"path": "note.txt", "content": "hello"}, "approval-token", "general_assistant"); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := files.Update(map[string]any{"path": "note.txt", "content": "updated"}, "approval-token-2", "general_assistant"); err != nil {
		t.Fatalf("update failed: %v", err)
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
		if validator.calls != 0 {
			t.Fatalf("Create(%#v) approval validations = %d, want 0", args, validator.calls)
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
		if validator.calls != 0 {
			t.Fatalf("Update(%#v) approval validations = %d, want 0", args, validator.calls)
		}
		content, readErr := os.ReadFile(note)
		if readErr != nil || string(content) != "original" {
			t.Fatalf("Update(%#v) content = %q, %v; want original", args, content, readErr)
		}
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
	if validator.calls != 0 {
		t.Fatalf("approval validations = %d, want 0", validator.calls)
	}
	if _, statErr := os.Stat(filepath.Join(root, "note.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("canceled call mutated note.txt: %v", statErr)
	}
}

type fakeApprovalValidator struct {
	valid bool
}

type recordingApprovalValidator struct {
	calls int
}

func (v *recordingApprovalValidator) Validate(string, string, map[string]any, string) error {
	v.calls++
	return nil
}

func (f fakeApprovalValidator) Validate(token string, tool string, args map[string]any, agentID string) error {
	if f.valid {
		return nil
	}
	return os.ErrPermission
}
