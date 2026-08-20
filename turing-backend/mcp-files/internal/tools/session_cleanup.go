package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// SessionCleanupTool is the non-advertised name the orchestrator uses to remove
// a withdrawn session's namespace. It is deliberately absent from tools/list
// and refused by the ordinary tool dispatch: only the internal caller that
// authenticates with the internal token can reach it.
const SessionCleanupTool = "files.session_cleanup"

const (
	// MaxSessionCleanupEntries bounds the work one cleanup may do.
	//
	// Exceeding it fails the call rather than continuing, so a namespace this
	// server cannot finish is never reported as clean. Entries removed before
	// the budget ran out stay removed: the caller retries, and each retry has
	// less left to do, which is why the operation is idempotent by design.
	MaxSessionCleanupEntries = 10000

	// maxSessionCleanupDepth matches the sandbox's own path depth limit: no
	// artifact this server wrote can be deeper than a path it would accept.
	maxSessionCleanupDepth = MaxSandboxPathDepth

	// sessionCleanupBatchSize is how many entries one pass reads before it
	// starts unlinking and reopens for the next pass.
	sessionCleanupBatchSize = searchDirBatchSize

	maxSessionIDBytes   = 128
	maxLifecycleVersion = math.MaxInt32
)

// sessionCleanupCounts is what a cleanup reports back. It is deliberately only
// counts: the caller already knows which session it asked about, and anything
// else — names, paths, content — would turn an operational acknowledgement into
// a disclosure channel for a session that was just withdrawn.
type sessionCleanupCounts struct {
	files       int
	directories int
}

func (f FilesTools) SessionCleanup(args map[string]any) (map[string]any, error) {
	return f.SessionCleanupContext(context.Background(), args)
}

// SessionCleanupContext removes exactly one session namespace,
// <sandbox>/sessions/<sessionId>, and nothing else.
//
// The identifier is validated as a single safe path component before it is used
// at all, and the walk is descriptor-relative with O_NOFOLLOW throughout, so a
// symlink planted inside the namespace is unlinked as a link rather than
// followed to whatever it points at. A namespace that is already gone is a
// success: cleanup is retried after partial failures, and a caller that has to
// distinguish "removed" from "was already removed" reads the counts.
func (f FilesTools) SessionCleanupContext(ctx context.Context, args map[string]any) (map[string]any, error) {
	sessionID, lifecycleVersion, err := validateSessionCleanupArgs(args)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Serialises cleanups of the same namespace with each other. Writes into
	// the namespace lock their own leaf paths, so this is not mutual exclusion
	// against an in-flight write — the orchestrator only cleans up a session
	// whose runs have already stopped and whose new reservations are refused.
	unlock, err := f.locks.lockContext(ctx, f.pathLockKey(path.Join(sessionsRoot, sessionID)))
	if err != nil {
		return nil, err
	}
	defer unlock()

	sessions, err := f.openSandboxDirectory(ctx, sessionsRoot)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return sessionCleanupResult(false, sessionCleanupCounts{}, lifecycleVersion), nil
		}
		return nil, err
	}
	defer func() { _ = sessions.Close() }()

	// Opened once up front purely to answer "is there a namespace here, and is
	// it a directory?" before any removal starts, so a symlink standing where a
	// namespace should be is refused rather than walked.
	namespace, err := openChildDirectory(sessions, sessionID)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return sessionCleanupResult(false, sessionCleanupCounts{}, lifecycleVersion), nil
		}
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, errors.New("session namespace is not a directory")
		}
		return nil, fmt.Errorf("open session namespace: %w", err)
	}
	if err := namespace.Close(); err != nil {
		return nil, fmt.Errorf("close session namespace: %w", err)
	}

	budget := MaxSessionCleanupEntries
	counts, err := f.removeDirectoryTree(ctx, sessions, sessionID, maxSessionCleanupDepth, &budget)
	if err != nil {
		return nil, err
	}
	if err := f.syncDirectory(sessions); err != nil {
		return nil, fmt.Errorf("sync session storage after cleanup: %w", err)
	}
	return sessionCleanupResult(counts.directories > 0 || counts.files > 0, counts, lifecycleVersion), nil
}

func sessionCleanupResult(removed bool, counts sessionCleanupCounts, lifecycleVersion int64) map[string]any {
	return map[string]any{
		"namespaceRemoved":   removed,
		"removedFiles":       counts.files,
		"removedDirectories": counts.directories,
		"lifecycleVersion":   lifecycleVersion,
	}
}

// removeDirectoryTree removes everything under (parent, name) and then the
// directory itself.
//
// It works in bounded batches, reopening the directory between them rather than
// unlinking while a single read is in flight: mutating a directory underneath
// its own reader can make the reader skip entries, which would leave files
// behind and report a namespace as clean when it is not.
//
// The budget is spent per entry ATTEMPTED, not per entry removed, so a
// directory whose entries keep reappearing cannot hold this loop open. That
// also means every call makes real progress before it gives up, which is what
// lets a namespace too large for one call be finished by retrying.
func (f FilesTools) removeDirectoryTree(ctx context.Context, parent *os.File, name string, depth int, budget *int) (sessionCleanupCounts, error) {
	var counts sessionCleanupCounts
	if depth <= 0 {
		return counts, errors.New("session namespace is nested too deeply to remove")
	}
	for {
		if err := ctx.Err(); err != nil {
			return counts, err
		}
		directory, err := openChildDirectory(parent, name)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return counts, nil
			}
			if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return counts, errors.New("session namespace entry is not a directory")
			}
			return counts, fmt.Errorf("open session namespace directory: %w", err)
		}
		entries, readErr := readDirectoryEntriesAt(ctx, directory, sessionCleanupBatchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = directory.Close()
			return counts, fmt.Errorf("read session namespace: %w", readErr)
		}
		if len(entries) == 0 {
			if err := directory.Close(); err != nil {
				return counts, fmt.Errorf("close session namespace directory: %w", err)
			}
			break
		}
		batchCounts, batchErr := f.removeEntries(ctx, directory, entries, depth, budget)
		counts.files += batchCounts.files
		counts.directories += batchCounts.directories
		closeErr := directory.Close()
		if batchErr != nil {
			return counts, batchErr
		}
		if closeErr != nil {
			return counts, fmt.Errorf("close session namespace directory: %w", closeErr)
		}
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return counts, nil
		}
		return counts, fmt.Errorf("remove session namespace directory: %w", err)
	}
	counts.directories++
	return counts, nil
}

func (f FilesTools) removeEntries(ctx context.Context, directory *os.File, entries []safeDirectoryEntry, depth int, budget *int) (sessionCleanupCounts, error) {
	var counts sessionCleanupCounts
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return counts, err
		}
		if entry.err != nil {
			// The entry name is deliberately absent from every message here:
			// this answer travels back to a caller entitled only to counts.
			return counts, fmt.Errorf("inspect session namespace entry: %w", entry.err)
		}
		if *budget <= 0 {
			return counts, fmt.Errorf("session namespace exceeds the %d entry cleanup limit", MaxSessionCleanupEntries)
		}
		*budget--
		if entry.mode&unix.S_IFMT == unix.S_IFDIR {
			childCounts, err := f.removeDirectoryTree(ctx, directory, entry.name, depth-1, budget)
			counts.files += childCounts.files
			counts.directories += childCounts.directories
			if err != nil {
				return counts, err
			}
			continue
		}
		// Every non-directory, including a symlink, is unlinked as an entry of
		// this directory. unlinkat never follows, so a link out of the sandbox
		// loses the link and keeps its target.
		if err := unix.Unlinkat(int(directory.Fd()), entry.name, 0); err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return counts, fmt.Errorf("remove session namespace entry: %w", err)
		}
		counts.files++
	}
	return counts, nil
}

func (f FilesTools) openSandboxDirectory(ctx context.Context, name string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := f.openRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return openChildDirectory(root, name)
}

func openChildDirectory(parent *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(
		int(parent.Fd()),
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open directory: invalid descriptor")
	}
	return file, nil
}

func validateSessionCleanupArgs(args map[string]any) (string, int64, error) {
	if err := rejectUnknownArgs(args, "sessionId", "lifecycleVersion"); err != nil {
		return "", 0, err
	}
	rawSessionID, present := args["sessionId"]
	if !present {
		return "", 0, invalidParams("sessionId is required")
	}
	sessionID, isString := rawSessionID.(string)
	if !isString {
		return "", 0, invalidParams("sessionId must be a string")
	}
	if err := requireSafeSessionID(sessionID); err != nil {
		return "", 0, err
	}
	rawVersion, present := args["lifecycleVersion"]
	if !present {
		return "", 0, invalidParams("lifecycleVersion is required")
	}
	version, isNumber := rawVersion.(float64)
	if !isNumber {
		return "", 0, invalidParams("lifecycleVersion must be a number")
	}
	if version != math.Trunc(version) || version < 1 || version > maxLifecycleVersion {
		return "", 0, invalidParamsf("lifecycleVersion must be a whole number between 1 and %d", int64(maxLifecycleVersion))
	}
	return sessionID, int64(version), nil
}

// requireSafeSessionID accepts only a single, self-evidently safe path
// component. It is an allowlist rather than a traversal blocklist: the argument
// is concatenated onto a directory this server then deletes, so "not obviously
// dangerous" is not a strong enough answer.
func requireSafeSessionID(sessionID string) error {
	if sessionID == "" {
		return invalidParams("sessionId must not be empty")
	}
	if len(sessionID) > maxSessionIDBytes {
		return invalidParamsf("sessionId exceeds %d bytes", maxSessionIDBytes)
	}
	for index, character := range sessionID {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9':
		case (character == '_' || character == '-') && index > 0:
		default:
			return invalidParams("sessionId must be alphanumeric with underscores or hyphens")
		}
	}
	if strings.HasPrefix(sessionID, ".") {
		return invalidParams("sessionId must not start with a dot")
	}
	return nil
}
