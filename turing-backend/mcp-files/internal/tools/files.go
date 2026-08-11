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

	"golang.org/x/sys/unix"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MaxMutationContentBytes = 512 * 1024
	MaxReadBytes            = 64 * 1024

	defaultReadBytes      = 64 * 1024
	maxReadBytes          = MaxReadBytes
	maxSearchFileBytes    = 512 * 1024
	defaultListEntries    = 200
	maxListEntries        = 1000
	maxListEntriesScanned = 4 * maxListEntries
	defaultSearchResults  = 50
	maxSearchResults      = 200
	maxSearchEntries      = 2000
	maxSearchFiles        = 1000
	maxSearchBytes        = 8 * 1024 * 1024
	searchDirBatchSize    = 64
	maxSearchErrorDetails = 20

	maxCollectionResultJSONBytes = 384 * 1024
)

type ApprovalValidator interface {
	ValidateContext(ctx context.Context, token string, tool string, args map[string]any, agentID string) error
}

type FilesTools struct {
	root          string
	validator     ApprovalValidator
	locks         *pathLockTable
	syncFile      func(*os.File) error
	syncDirectory func(*os.File) error
}

func NewFilesTools(root string) FilesTools {
	abs, _ := filepath.Abs(root)
	// Canonicalize the configured root once so descriptor-relative opens start
	// from the same directory on systems where a parent path is a symlink.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return FilesTools{
		root:          abs,
		locks:         processPathLocks,
		syncFile:      func(file *os.File) error { return file.Sync() },
		syncDirectory: func(directory *os.File) error { return directory.Sync() },
	}
}

func (f FilesTools) WithApprovalValidator(validator ApprovalValidator) FilesTools {
	f.validator = validator
	return f
}

func (f FilesTools) pathLockKey(clean string) string {
	return f.root + "\x00" + norm.NFC.String(cases.Fold().String(clean))
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
	clean, _, err := normalizeSandboxPath(pathValue)
	if err != nil {
		return nil, err
	}
	unlock, err := f.locks.lockContext(ctx, f.pathLockKey(clean))
	if err != nil {
		return nil, err
	}
	defer unlock()
	file, _, _, err := f.openRegularFileContext(ctx, clean)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, bytesRead, _, err := readBoundedContext(ctx, file, maxReadBytes+1)
	if err != nil {
		return nil, err
	}
	if len(content) > maxReadBytes {
		return nil, errors.New("file too large")
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
	return map[string]any{"path": pathValue, "content": string(content), "truncated": truncated, "bytesRead": bytesRead}, nil
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
	directory, _, err := f.openDirectoryPathContext(ctx, pathValue, false)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries := make([]safeDirectoryEntry, 0, limit+1)
	entriesScanned := 0
	exhausted := false
	for len(entries) <= limit && entriesScanned < maxListEntriesScanned {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batchSize := searchDirBatchSize
		if remaining := maxListEntriesScanned - entriesScanned; remaining < batchSize {
			batchSize = remaining
		}
		batch, readErr := readDirectoryEntriesAt(ctx, directory, batchSize)
		entriesScanned += len(batch)
		for _, entry := range batch {
			if entry.err != nil {
				return nil, fmt.Errorf("inspect directory entry %q: %w", entry.name, entry.err)
			}
			if !isInternalStagingName(entry.name) {
				entries = append(entries, entry)
			}
			if len(entries) > limit {
				break
			}
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if errors.Is(readErr, io.EOF) {
			exhausted = true
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	truncated := len(entries) > limit || !exhausted
	if len(entries) > limit {
		entries = entries[:limit]
	}
	items := make([]map[string]any, 0, len(entries))
	resultJSONBytes := 0
	for _, entry := range entries {
		item := map[string]any{
			"name":  entry.name,
			"isDir": entry.mode&unix.S_IFMT == unix.S_IFDIR,
		}
		if !reserveCollectionResultJSON(&resultJSONBytes, item) {
			truncated = true
			break
		}
		items = append(items, item)
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
	clean, _, err := normalizeSandboxPath(pathValue)
	if err != nil {
		return nil, err
	}
	type directoryTask struct {
		path      string
		directory *os.File
	}
	directories := []directoryTask{}
	defer func() {
		for _, task := range directories {
			if task.directory != nil {
				_ = task.directory.Close()
			}
		}
	}()
	matches := []map[string]any{}
	errorDetails := []map[string]any{}
	entriesVisited := 0
	directoryEntriesRead := 0
	directoriesScanned := 0
	filesScanned := 0
	skippedFiles := 0
	bytesScanned := int64(0)
	errorCount := 0
	resultJSONBytes := 0
	truncated := false
	stop := false
	recordFailure := func(path, operation string, failure error) {
		errorCount++
		if len(errorDetails) < maxSearchErrorDetails {
			detail := map[string]any{
				"path":      filepath.ToSlash(path),
				"operation": operation,
				"error":     failure.Error(),
			}
			if reserveCollectionResultJSON(&resultJSONBytes, detail) {
				errorDetails = append(errorDetails, detail)
			}
		}
	}
	scanFile := func(file *os.File, rel string) {
		if len(matches) >= limit || filesScanned >= maxSearchFiles {
			_ = file.Close()
			truncated = true
			stop = true
			return
		}
		filesScanned++
		remainingBytes := int64(maxSearchBytes) - bytesScanned
		if remainingBytes <= 0 {
			_ = file.Close()
			truncated = true
			stop = true
			return
		}
		readLimit := int64(maxSearchFileBytes + 1)
		aggregateLimited := remainingBytes < readLimit
		if aggregateLimited {
			readLimit = remainingBytes
		}
		content, actualBytes, reachedLimit, readErr := readBoundedContext(ctx, file, readLimit)
		closeErr := file.Close()
		bytesScanned += actualBytes
		if readErr != nil {
			recordFailure(rel, "read file", readErr)
			return
		}
		if closeErr != nil {
			recordFailure(rel, "close file", closeErr)
			return
		}
		if len(content) > maxSearchFileBytes {
			recordFailure(rel, "read file", errors.New("file exceeds per-file byte limit"))
			return
		}
		if aggregateLimited && reachedLimit {
			truncated = true
			stop = true
			return
		}
		if !utf8.Valid(content) {
			skippedFiles++
			return
		}
		text := string(content)
		if strings.Contains(text, query) {
			match := map[string]any{
				"path":    filepath.ToSlash(rel),
				"snippet": firstSnippet(text, query),
			}
			if !reserveCollectionResultJSON(&resultJSONBytes, match) {
				truncated = true
				stop = true
				return
			}
			matches = append(matches, match)
		}
	}
	openAndScanFile := func(rel string) {
		file, _, _, openErr := f.openRegularFileContext(ctx, rel)
		if openErr != nil {
			recordFailure(rel, "open file", openErr)
			return
		}
		scanFile(file, rel)
	}
	initial, _, openErr := f.openPathContext(ctx, clean, unix.O_RDONLY|unix.O_NONBLOCK)
	if openErr != nil {
		return nil, openErr
	}
	var initialStat unix.Stat_t
	if err := unix.Fstat(int(initial.Fd()), &initialStat); err != nil {
		_ = initial.Close()
		return nil, err
	}
	switch initialStat.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		entriesVisited = 1
		scanFile(initial, clean)
	case unix.S_IFDIR:
		entriesVisited = 1
		directories = append(directories, directoryTask{path: clean, directory: initial})
	default:
		_ = initial.Close()
		recordFailure(clean, "inspect path", errors.New("unsupported file type"))
	}
	for len(directories) > 0 && !stop {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := directories[0]
		directories = directories[1:]
		directory := task.directory
		if directory == nil {
			directory, _, openErr = f.openDirectoryPathContext(ctx, task.path, false)
			if openErr != nil {
				recordFailure(task.path, "open directory", openErr)
				continue
			}
		}
		directoriesScanned++
		for !stop {
			if err := ctx.Err(); err != nil {
				_ = directory.Close()
				return nil, err
			}
			remainingEntries := maxSearchEntries - entriesVisited
			if remainingEntries <= 0 {
				truncated = true
				stop = true
				break
			}
			batchSize := searchDirBatchSize
			if remainingEntries < batchSize {
				batchSize = remainingEntries
			}
			entries, readErr := readDirectoryEntriesAt(ctx, directory, batchSize)
			directoryEntriesRead += len(entries)
			sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					_ = directory.Close()
					return nil, err
				}
				entriesVisited++
				if isInternalStagingName(entry.name) {
					continue
				}
				rel := entry.name
				if task.path != "." {
					rel = filepath.Join(task.path, entry.name)
				}
				if entry.err != nil {
					recordFailure(rel, "inspect entry", entry.err)
					continue
				}
				if entry.mode&unix.S_IFMT == unix.S_IFLNK {
					skippedFiles++
					continue
				}
				if entry.mode&unix.S_IFMT == unix.S_IFDIR {
					directories = append(directories, directoryTask{path: rel})
					continue
				}
				openAndScanFile(rel)
				if stop {
					break
				}
			}
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				recordFailure(task.path, "read directory", readErr)
				break
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if entriesVisited >= maxSearchEntries {
				truncated = true
				stop = true
			}
		}
		if closeErr := directory.Close(); closeErr != nil {
			recordFailure(task.path, "close directory", closeErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"matches":               matches,
		"truncated":             truncated,
		"incomplete":            errorCount > 0,
		"errors":                errorDetails,
		"errorCount":            errorCount,
		"errorDetailsTruncated": errorCount > len(errorDetails),
		"entriesVisited":        entriesVisited,
		"directoryEntriesRead":  directoryEntriesRead,
		"directoriesScanned":    directoriesScanned,
		"filesScanned":          filesScanned,
		"skippedFiles":          skippedFiles,
		"bytesScanned":          bytesScanned,
	}, nil
}

func (f FilesTools) Create(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	return f.CreateContext(context.Background(), args, approvalToken, agentID)
}

func (f FilesTools) CreateContext(ctx context.Context, args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	pathValue, content, err := validateCreateArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, _, err := normalizeSandboxPath(pathValue)
	if err != nil {
		return nil, err
	}
	unlock, err := f.locks.lockContext(ctx, f.pathLockKey(clean))
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.checkCreatePreconditions(ctx, clean); err != nil {
		return nil, err
	}
	if err := f.validateApproval(ctx, "files.create", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, leaf, _, err := f.openParentPathContext(ctx, clean, true)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	temporary, temporaryName, err := createTemporaryFile(parent, createStagingPrefix, 0600)
	if err != nil {
		return nil, fmt.Errorf("stage create %q: %w", pathValue, err)
	}
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = unix.Unlinkat(int(parent.Fd()), temporaryName, 0)
		}
	}()
	if err := writeAllContext(ctx, temporary, []byte(content)); err != nil {
		return nil, err
	}
	if err := f.syncFile(temporary); err != nil {
		return nil, err
	}
	if err := temporary.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := unix.Linkat(int(parent.Fd()), temporaryName, int(parent.Fd()), leaf, 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return nil, errors.New("file already exists")
		}
		return nil, fmt.Errorf("install create target %q: %w", pathValue, err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), temporaryName, 0); err != nil {
		return nil, fmt.Errorf("remove create staging file: %w", err)
	}
	removeTemporary = false
	if err := f.syncDirectory(parent); err != nil {
		return nil, fmt.Errorf("sync directory after create %q: %w", pathValue, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.syncCreateAncestorsContext(ctx, clean); err != nil {
		return nil, fmt.Errorf("sync directory hierarchy after create %q: %w", pathValue, err)
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func (f FilesTools) checkCreatePreconditions(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := f.openRoot()
	if err != nil {
		return err
	}
	if err := root.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, leaf, _, err := f.openParentPathContext(ctx, path, false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	var stat unix.Stat_t
	err = unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect create target %q: %w", path, err)
	}
	return errors.New("file already exists")
}

func (f FilesTools) Update(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	return f.UpdateContext(context.Background(), args, approvalToken, agentID)
}

func (f FilesTools) UpdateContext(ctx context.Context, args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	pathValue, content, expectedHash, err := validateUpdateArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, _, err := normalizeSandboxPath(pathValue)
	if err != nil {
		return nil, err
	}
	unlock, err := f.locks.lockContext(ctx, f.pathLockKey(clean))
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent, leaf, _, err := f.openParentPathContext(ctx, clean, false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	openFlags := unix.O_WRONLY
	if expectedHash != "" {
		openFlags = unix.O_RDWR
	}
	fd, err := unix.Openat(
		int(parent.Fd()),
		leaf,
		openFlags|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", pathValue, err)
	}
	currentFile := os.NewFile(uintptr(fd), leaf)
	if currentFile == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open existing file: invalid descriptor")
	}
	defer currentFile.Close()
	var originalStat unix.Stat_t
	if err := unix.Fstat(fd, &originalStat); err != nil {
		return nil, err
	}
	if originalStat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("update target is not a regular file")
	}
	if err := requireUpdateWriteBits(uint32(originalStat.Mode)); err != nil {
		return nil, err
	}
	if expectedHash != "" {
		current, _, _, err := readBoundedContext(ctx, currentFile, MaxMutationContentBytes+1)
		if err != nil {
			return nil, err
		}
		if len(current) > MaxMutationContentBytes {
			return nil, fmt.Errorf("existing file is too large to hash (limit %d bytes)", MaxMutationContentBytes)
		}
		if contentHash(string(current)) != expectedHash {
			return nil, errors.New("expectedHash mismatch")
		}
	}
	if err := f.validateApproval(ctx, "files.update", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	temporary, temporaryName, err := createTemporaryFile(parent, updateStagingPrefix, uint32(originalStat.Mode&0777))
	if err != nil {
		return nil, err
	}
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = unix.Unlinkat(int(parent.Fd()), temporaryName, 0)
		}
	}()
	if err := writeAllContext(ctx, temporary, []byte(content)); err != nil {
		return nil, err
	}
	if err := unix.Fchmod(int(temporary.Fd()), uint32(originalStat.Mode&0777)); err != nil {
		return nil, err
	}
	if err := f.syncFile(temporary); err != nil {
		return nil, err
	}
	if err := temporary.Close(); err != nil {
		return nil, err
	}
	if expectedHash != "" {
		if _, err := currentFile.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		current, _, _, err := readBoundedContext(ctx, currentFile, MaxMutationContentBytes+1)
		if err != nil {
			return nil, err
		}
		if len(current) > MaxMutationContentBytes || contentHash(string(current)) != expectedHash {
			return nil, errors.New("update target changed during operation")
		}
	}
	var finalStat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), leaf, &finalStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, fmt.Errorf("verify update target: %w", err)
	}
	if finalStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		finalStat.Dev != originalStat.Dev ||
		finalStat.Ino != originalStat.Ino {
		return nil, errors.New("update target changed during operation")
	}
	if err := requireUpdateWriteBits(uint32(finalStat.Mode)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := unix.Renameat(int(parent.Fd()), temporaryName, int(parent.Fd()), leaf); err != nil {
		return nil, fmt.Errorf("replace update target: %w", err)
	}
	removeTemporary = false
	if err := f.syncDirectory(parent); err != nil {
		return nil, fmt.Errorf("sync directory after update %q: %w", pathValue, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func requireUpdateWriteBits(mode uint32) error {
	if mode&0222 == 0 {
		return errors.New("update target has no write permission bits")
	}
	return nil
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
		return f.CreateContext(ctx, args, approvalToken, agentID)
	case "files.update":
		return f.UpdateContext(ctx, args, approvalToken, agentID)
	case "files.delete", "files.move":
		return nil, errors.New("tool disabled")
	default:
		return nil, invalidParams("unknown tool")
	}
}

func (f FilesTools) validateApproval(ctx context.Context, tool string, args map[string]any, approvalToken string, agentID string) error {
	if f.validator == nil {
		return errors.New("approval validator not configured")
	}
	if approvalToken == "" {
		return errors.New("approval token required")
	}
	return f.validator.ValidateContext(ctx, approvalToken, tool, args, agentID)
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
	if err != nil {
		return "", "", err
	}
	if len(content) > MaxMutationContentBytes {
		return "", "", invalidParamsf("content exceeds %d byte limit", MaxMutationContentBytes)
	}
	return pathValue, content, nil
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
	if len(content) > MaxMutationContentBytes {
		return "", "", "", invalidParamsf("content exceeds %d byte limit", MaxMutationContentBytes)
	}
	expectedHash := ""
	if value, present := args["expectedHash"]; present {
		var valid bool
		expectedHash, valid = value.(string)
		if !valid || !validContentHash(expectedHash) {
			return "", "", "", invalidParams("expectedHash must be a sha256: prefixed SHA-256 hex digest")
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
			return invalidParamsf("unknown argument %q", name)
		}
	}
	return nil
}

func requiredString(args map[string]any, name string, allowEmpty bool) (string, error) {
	value, present := args[name]
	stringValue, valid := value.(string)
	if !present || !valid || (!allowEmpty && strings.TrimSpace(stringValue) == "") {
		return "", invalidParamsf("%s is required and must be a %sstring", name, map[bool]string{true: "", false: "non-empty "}[allowEmpty])
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
		return 0, invalidParamsf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number ||
		number < float64(minimum) || number > float64(maximum) {
		return 0, invalidParamsf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return int(number), nil
}

func validContentHash(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256.Size*2 {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func reserveCollectionResultJSON(used *int, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	required := len(encoded)
	if *used > 0 {
		required++
	}
	if required > maxCollectionResultJSONBytes-*used {
		return false
	}
	*used += required
	return true
}

func readBoundedContext(ctx context.Context, reader io.Reader, limit int64) ([]byte, int64, bool, error) {
	if limit < 0 {
		return nil, 0, false, errors.New("read limit must not be negative")
	}
	const chunkSize = 32 * 1024
	var content bytes.Buffer
	var bytesRead int64
	for bytesRead < limit {
		if err := ctx.Err(); err != nil {
			return nil, bytesRead, false, err
		}
		size := int64(chunkSize)
		if remaining := limit - bytesRead; remaining < size {
			size = remaining
		}
		buffer := make([]byte, int(size))
		count, err := reader.Read(buffer)
		if count > 0 {
			_, _ = content.Write(buffer[:count])
			bytesRead += int64(count)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return content.Bytes(), bytesRead, false, nil
			}
			return nil, bytesRead, false, err
		}
		if count == 0 {
			return nil, bytesRead, false, io.ErrNoProgress
		}
	}
	return content.Bytes(), bytesRead, true, nil
}

func writeAllContext(ctx context.Context, writer io.Writer, content []byte) error {
	const chunkSize = 32 * 1024
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := chunkSize
		if len(content) < size {
			size = len(content)
		}
		written, err := writer.Write(content[:size])
		if err != nil {
			return err
		}
		if written <= 0 || written > size {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return ctx.Err()
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
	for start < index && !utf8.RuneStart(text[start]) {
		start++
	}
	end := index + len(query) + 40
	if end > len(text) {
		end = len(text)
	}
	for end > index+len(query) && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
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
