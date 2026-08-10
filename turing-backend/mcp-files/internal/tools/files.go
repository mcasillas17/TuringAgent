package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	defaultReadBytes     = 64 * 1024
	maxReadBytes         = 512 * 1024
	defaultListEntries   = 200
	maxListEntries       = 1000
	defaultSearchResults = 50
	maxSearchResults     = 200
	maxSearchEntries     = 2000
	maxSearchFiles       = 1000
	maxSearchBytes       = 8 * 1024 * 1024
)

type ApprovalValidator interface {
	Validate(token string, tool string, args map[string]any, agentID string) error
}

type FilesTools struct {
	root      string
	validator ApprovalValidator
}

func NewFilesTools(root string) FilesTools {
	abs, _ := filepath.Abs(root)
	// Canonicalize the sandbox root so that EvalSymlinks-based path checks
	// compare like-for-like on systems where the tmp/parent path traverses a
	// symlink (e.g. macOS, where /var is a symlink to /private/var).
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return FilesTools{root: abs}
}

func (f FilesTools) WithApprovalValidator(validator ApprovalValidator) FilesTools {
	f.validator = validator
	return f
}

func (f FilesTools) resolve(input string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(input, "/"))
	full := filepath.Join(f.root, clean)
	existing := full
	missing := []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("path escapes sandbox")
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(append([]string{resolvedExisting}, missing...)...)
	rel, err := filepath.Rel(f.root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("path escapes sandbox")
	}
	return resolved, nil
}

func (f FilesTools) Read(args map[string]any) (map[string]any, error) {
	return f.ReadContext(context.Background(), args)
}

func (f FilesTools) ReadContext(ctx context.Context, args map[string]any) (map[string]any, error) {
	pathValue, limit, err := validateReadArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxReadBytes {
		return nil, errors.New("file too large")
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(content) {
		return nil, errors.New("unsupported media type")
	}
	truncated := len(content) > limit
	if truncated {
		content = content[:limit]
		for !utf8.Valid(content) && len(content) > 0 {
			content = content[:len(content)-1]
		}
	}
	return map[string]any{"path": pathValue, "content": string(content), "truncated": truncated}, nil
}

func (f FilesTools) List(args map[string]any) (map[string]any, error) {
	return f.ListContext(context.Background(), args)
}

func (f FilesTools) ListContext(ctx context.Context, args map[string]any) (map[string]any, error) {
	pathValue, limit, err := validateListArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	directory, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	truncated := len(entries) > limit
	if truncated {
		entries = entries[:limit]
	}
	items := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		items = append(items, map[string]any{"name": entry.Name(), "isDir": entry.IsDir()})
	}
	return map[string]any{"items": items, "truncated": truncated}, nil
}

func (f FilesTools) Search(args map[string]any) (map[string]any, error) {
	return f.SearchContext(context.Background(), args)
}

func (f FilesTools) SearchContext(ctx context.Context, args map[string]any) (map[string]any, error) {
	pathValue, query, limit, err := validateSearchArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	matches := []map[string]any{}
	entriesVisited := 0
	filesScanned := 0
	bytesScanned := int64(0)
	truncated := false
	err = filepath.WalkDir(full, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entriesVisited >= maxSearchEntries {
			truncated = true
			return filepath.SkipAll
		}
		entriesVisited++
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(matches) >= limit {
			truncated = true
			return filepath.SkipAll
		}
		if filesScanned >= maxSearchFiles {
			truncated = true
			return filepath.SkipAll
		}
		filesScanned++
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(f.root, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxReadBytes {
			return nil
		}
		if info.Size() > int64(maxSearchBytes)-bytesScanned {
			truncated = true
			return filepath.SkipAll
		}
		bytesScanned += info.Size()
		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !utf8.Valid(content) {
			return nil
		}
		text := string(content)
		if strings.Contains(text, query) {
			matches = append(matches, map[string]any{"path": rel, "snippet": firstSnippet(text, query)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"matches":        matches,
		"truncated":      truncated,
		"entriesVisited": entriesVisited,
		"filesScanned":   filesScanned,
		"bytesScanned":   bytesScanned,
	}, nil
}

func (f FilesTools) Create(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	pathValue, content, err := validateCreateArgs(args)
	if err != nil {
		return nil, err
	}
	if err := f.validateApproval("files.create", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(full); err == nil {
		return nil, errors.New("file already exists")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func (f FilesTools) Update(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	pathValue, content, expectedHash, err := validateUpdateArgs(args)
	if err != nil {
		return nil, err
	}
	if err := f.validateApproval("files.update", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	if expectedHash != "" {
		current, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		if contentHash(string(current)) != expectedHash {
			return nil, errors.New("expectedHash mismatch")
		}
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func (f FilesTools) Call(name string, args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	return f.CallContext(context.Background(), name, args, approvalToken, agentID)
}

func (f FilesTools) CallContext(ctx context.Context, name string, args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch name {
	case "files.list":
		return f.ListContext(ctx, args)
	case "files.search":
		return f.SearchContext(ctx, args)
	case "files.read":
		return f.ReadContext(ctx, args)
	case "files.create":
		return f.Create(args, approvalToken, agentID)
	case "files.update":
		return f.Update(args, approvalToken, agentID)
	case "files.delete", "files.move":
		return nil, errors.New("tool disabled")
	default:
		return nil, errors.New("unknown tool")
	}
}

func (f FilesTools) validateApproval(tool string, args map[string]any, approvalToken string, agentID string) error {
	if f.validator == nil {
		return errors.New("approval validator not configured")
	}
	if approvalToken == "" {
		return errors.New("approval token required")
	}
	return f.validator.Validate(approvalToken, tool, args, agentID)
}

func validateReadArgs(args map[string]any) (string, int, error) {
	if err := rejectUnknownArgs(args, "path", "maxBytes"); err != nil {
		return "", 0, err
	}
	pathValue, err := requiredString(args, "path", false)
	if err != nil {
		return "", 0, err
	}
	limit, err := optionalInteger(args, "maxBytes", defaultReadBytes, 1, maxReadBytes)
	return pathValue, limit, err
}

func validateListArgs(args map[string]any) (string, int, error) {
	if err := rejectUnknownArgs(args, "path", "limit"); err != nil {
		return "", 0, err
	}
	pathValue, err := optionalPath(args)
	if err != nil {
		return "", 0, err
	}
	limit, err := optionalInteger(args, "limit", defaultListEntries, 1, maxListEntries)
	return pathValue, limit, err
}

func validateSearchArgs(args map[string]any) (string, string, int, error) {
	if err := rejectUnknownArgs(args, "path", "query", "limit"); err != nil {
		return "", "", 0, err
	}
	pathValue, err := optionalPath(args)
	if err != nil {
		return "", "", 0, err
	}
	query, err := requiredString(args, "query", false)
	if err != nil {
		return "", "", 0, err
	}
	limit, err := optionalInteger(args, "limit", defaultSearchResults, 1, maxSearchResults)
	return pathValue, query, limit, err
}

func validateCreateArgs(args map[string]any) (string, string, error) {
	if err := rejectUnknownArgs(args, "path", "content"); err != nil {
		return "", "", err
	}
	pathValue, err := requiredString(args, "path", false)
	if err != nil {
		return "", "", err
	}
	content, err := requiredString(args, "content", true)
	return pathValue, content, err
}

func validateUpdateArgs(args map[string]any) (string, string, string, error) {
	if err := rejectUnknownArgs(args, "path", "content", "expectedHash"); err != nil {
		return "", "", "", err
	}
	pathValue, err := requiredString(args, "path", false)
	if err != nil {
		return "", "", "", err
	}
	content, err := requiredString(args, "content", true)
	if err != nil {
		return "", "", "", err
	}
	expectedHash := ""
	if value, present := args["expectedHash"]; present {
		var valid bool
		expectedHash, valid = value.(string)
		if !valid || !validContentHash(expectedHash) {
			return "", "", "", errors.New("expectedHash must be a sha256: prefixed SHA-256 hex digest")
		}
	}
	return pathValue, content, expectedHash, nil
}

func rejectUnknownArgs(args map[string]any, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for name := range args {
		if _, ok := allowedSet[name]; !ok {
			return fmt.Errorf("unknown argument %q", name)
		}
	}
	return nil
}

func requiredString(args map[string]any, name string, allowEmpty bool) (string, error) {
	value, present := args[name]
	stringValue, valid := value.(string)
	if !present || !valid || (!allowEmpty && strings.TrimSpace(stringValue) == "") {
		return "", fmt.Errorf("%s is required and must be a %sstring", name, map[bool]string{true: "", false: "non-empty "}[allowEmpty])
	}
	return stringValue, nil
}

func optionalPath(args map[string]any) (string, error) {
	if _, present := args["path"]; !present {
		return ".", nil
	}
	return requiredString(args, "path", false)
}

func optionalInteger(args map[string]any, name string, defaultValue, minimum, maximum int) (int, error) {
	value, present := args[name]
	if !present {
		return defaultValue, nil
	}
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case int:
		number = float64(value)
	default:
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		number < float64(minimum) || number > float64(maximum) {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return int(number), nil
}

func validContentHash(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

func firstSnippet(text string, query string) string {
	index := strings.Index(text, query)
	if index < 0 {
		return ""
	}
	start := index - 40
	if start < 0 {
		start = 0
	}
	end := index + len(query) + 40
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func CanonicalArgsHash(args map[string]any) (string, error) {
	canonical, err := canonicalJSON(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(args map[string]any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(args); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
