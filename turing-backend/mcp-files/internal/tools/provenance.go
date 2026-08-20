package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// sessionsRoot is the server-managed subtree. Everything under it belongs to a
// specific session and run; everything beside it predates session ownership.
const sessionsRoot = "sessions"

// ownedPathPrefixDepth is how many components sessions/<sid>/runs/<rid>/files
// adds. A logical path is checked against the remaining budget so a write is
// refused for being too deep rather than failing halfway through the walk.
const ownedPathPrefixDepth = 5

// Provenance is the verified scope of one file tool call.
type Provenance struct {
	SessionID          string
	RunID              string
	LogicalPath        string
	DeletionGeneration int64
}

// WriteAuthorization asks the orchestrator to spend the approval and record the
// artifact in the same step.
type WriteAuthorization struct {
	ApprovalToken   string
	ProvenanceToken string
	Tool            string
	Args            map[string]any
	AgentID         string
	PhysicalPath    string
}

// Reservation is the durable manifest row the write is allowed to land in.
type Reservation struct {
	ArtifactID   string
	PhysicalPath string
	Policy       string
}

// ProvenanceGuard is the orchestrator, as far as this process is concerned: it
// says what a capability means, whether a write may happen, and it is told what
// the write did.
type ProvenanceGuard interface {
	Verify(token string, tool string, args map[string]any, agentID string) (Provenance, error)
	AuthorizeWrite(ctx context.Context, req WriteAuthorization) (Reservation, error)
	FinalizeWrite(ctx context.Context, artifactID string, provenanceToken string, committed bool) error
	// CheckSession asks the orchestrator whether the capability's session is
	// still accepting work. A capability cannot answer that about itself: it
	// was signed before the withdrawal it might be racing.
	CheckSession(ctx context.Context, provenanceToken string) error
}

// CallRequest is one agent-facing tool call with the capabilities issued for it.
type CallRequest struct {
	Name            string
	Args            map[string]any
	ApprovalToken   string
	ProvenanceToken string
	AgentID         string
}

// callScope carries the verified capability through a single call. An inactive
// scope means no guard is configured, and every path is used exactly as given —
// which is what the file-system-level tests exercise.
type callScope struct {
	guard  ProvenanceGuard
	claims Provenance
	token  string
	tool   string
	args   map[string]any
	agent  string
	active bool
}

func (f FilesTools) WithProvenanceGuard(guard ProvenanceGuard) FilesTools {
	f.provenance = guard
	return f
}

// newCallScope verifies the capability before anything touches the file system.
// With a guard configured there is no unauthenticated path: a call without a
// capability is refused, including the read-only tools.
func (f FilesTools) newCallScope(req CallRequest) (callScope, error) {
	if f.provenance == nil {
		return callScope{}, nil
	}
	if req.ProvenanceToken == "" {
		return callScope{}, invalidParams("provenance capability required")
	}
	claims, err := f.provenance.Verify(req.ProvenanceToken, req.Name, req.Args, req.AgentID)
	if err != nil {
		return callScope{}, fmt.Errorf("provenance capability rejected: %w", err)
	}
	if claims.SessionID == "" || claims.RunID == "" {
		return callScope{}, errors.New("provenance capability has no session or run scope")
	}
	return callScope{
		guard: f.provenance, claims: claims, token: req.ProvenanceToken,
		tool: req.Name, args: req.Args, agent: req.AgentID, active: true,
	}, nil
}

// requirePathScope refuses a call whose path argument is not the one the
// capability was issued for. The orchestrator signed a specific path; acting on
// any other one would make the manifest a record of something that did not
// happen.
func (s callScope) requirePathScope(clean string) error {
	if !s.active {
		return nil
	}
	// Checked before the capability's own scope, because a capability that
	// names session storage is not a licence to read it: the subtree holds
	// every session's artifacts and is reachable only through the mapping this
	// server does itself. The bare root matters as much as anything under it —
	// listing "sessions" enumerates every session that has ever written a file.
	if err := requireSessionStorageIsPrivate(clean); err != nil {
		return err
	}
	scoped := normalizeLogicalPath(s.claims.LogicalPath)
	if scoped == clean {
		return nil
	}
	// An omitted path means the sandbox root for the listing tools, which is
	// what an empty scope claim also means.
	if scoped == "." && clean == "." {
		return nil
	}
	return invalidParams("path is outside the provenance capability's scope")
}

// requireSessionStorageIsPrivate refuses any path at or inside the shared
// session subtree. The comparison is case-insensitive because the sandbox may
// sit on a case-insensitive file system, where "SESSIONS" and "sessions" are
// the same directory and only one of them would otherwise be refused.
func requireSessionStorageIsPrivate(clean string) error {
	if clean == "." {
		return nil
	}
	first, _, _ := strings.Cut(clean, "/")
	if strings.EqualFold(first, sessionsRoot) {
		return invalidParams("path is server-managed session storage")
	}
	return nil
}

// checkSessionActive is the server-side state check the read-only tools run on
// both sides of their I/O. Writes do not use it: their before-state is the
// artifact reservation and their after-state is the finalization, both of which
// are already server-side and both of which the orchestrator judges itself.
func (s callScope) checkSessionActive(ctx context.Context) error {
	if !s.active {
		return nil
	}
	return s.guard.CheckSession(ctx, s.token)
}

func (s callScope) ownedPrefix() string {
	return path.Join(sessionsRoot, s.claims.SessionID, "runs", s.claims.RunID, "files")
}

func (s callScope) sessionRunsRoot() string {
	return path.Join(sessionsRoot, s.claims.SessionID, "runs")
}

// ownedPath is where this run's copy of a logical path lives. It is the single
// definition on this side of the wire, and the orchestrator derives the same
// string independently, so a reservation and the write it authorises cannot
// disagree.
func (s callScope) ownedPath(clean string) (string, error) {
	if clean == "." {
		return s.ownedPrefix(), nil
	}
	owned := path.Join(s.ownedPrefix(), clean)
	if len(owned) > MaxSandboxPathBytes {
		return "", invalidParamsf("path exceeds %d bytes once mapped into session-owned storage", MaxSandboxPathBytes)
	}
	if len(strings.Split(clean, "/"))+ownedPathPrefixDepth > MaxSandboxPathDepth {
		return "", invalidParamsf("path exceeds %d components once mapped into session-owned storage", MaxSandboxPathDepth)
	}
	return owned, nil
}

// resolveRead finds the copy this session should see: its own run's artifact
// first, then an artifact an earlier run of the same session wrote, then the
// pre-existing sandbox-root file. It never looks inside another session.
func (f FilesTools) resolveRead(ctx context.Context, scope callScope, clean string) (string, error) {
	if !scope.active {
		return clean, nil
	}
	owned, err := scope.ownedPath(clean)
	if err != nil {
		return "", err
	}
	if f.pathExists(ctx, owned) {
		return owned, nil
	}
	fromRun, found, err := f.artifactFromAnotherRun(ctx, scope, clean)
	if err != nil {
		return "", err
	}
	if found {
		return fromRun, nil
	}
	// Unreachable through a tool call, which is refused earlier by
	// requireSessionStorageIsPrivate. Kept so this resolver is safe on its own
	// terms rather than only in the company of its current caller.
	if err := requireSessionStorageIsPrivate(clean); err != nil {
		return "", err
	}
	return clean, nil
}

// artifactFromAnotherRun looks for the same logical path under a different run
// of the SAME session, newest first. Run ids are time-ordered, so the highest
// name is the most recent write.
//
// A session with no storage yet is the ordinary case and reports "not found".
// Any other failure to enumerate is returned rather than treated as absence:
// silently falling through would serve a stale root file in place of an
// artifact this session actually wrote.
func (f FilesTools) artifactFromAnotherRun(ctx context.Context, scope callScope, clean string) (string, bool, error) {
	runs, err := f.readDirectoryNames(ctx, scope.sessionRunsRoot())
	if err != nil {
		if errors.Is(err, unix.ENOENT) || errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("inspect session artifact storage: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(runs)))
	for _, run := range runs {
		if run == scope.claims.RunID {
			continue
		}
		candidate := path.Join(scope.sessionRunsRoot(), run, "files")
		if clean != "." {
			candidate = path.Join(candidate, clean)
		}
		if f.pathExists(ctx, candidate) {
			return candidate, true, nil
		}
	}
	return "", false, nil
}

// resolveWrite decides where a mutation lands and how the manifest will type
// it. A create is always session-owned. An update follows the file it is
// actually updating: this run's artifact, an earlier run's artifact, or — only
// when neither exists — the pre-existing root file, which is recorded as
// retained so a session withdrawal does not delete something it never made.
func (f FilesTools) resolveWrite(ctx context.Context, scope callScope, clean string, isCreate bool) (string, error) {
	if !scope.active {
		return clean, nil
	}
	owned, err := scope.ownedPath(clean)
	if err != nil {
		return "", err
	}
	if isCreate {
		return owned, nil
	}
	if f.pathExists(ctx, owned) {
		return owned, nil
	}
	fromRun, found, err := f.artifactFromAnotherRun(ctx, scope, clean)
	if err != nil {
		return "", err
	}
	if found {
		return fromRun, nil
	}
	if f.pathExists(ctx, clean) {
		return clean, nil
	}
	return owned, nil
}

// authorizeWrite spends the approval and takes the reservation. It runs after
// the local preconditions and before any bytes exist, so a refusal here leaves
// the file system untouched.
func (f FilesTools) authorizeWrite(ctx context.Context, scope callScope, approvalToken string, physicalPath string) (Reservation, error) {
	reservation, err := scope.guard.AuthorizeWrite(ctx, WriteAuthorization{
		ApprovalToken:   approvalToken,
		ProvenanceToken: scope.token,
		Tool:            scope.tool,
		Args:            scope.args,
		AgentID:         scope.agent,
		PhysicalPath:    physicalPath,
	})
	if err != nil {
		return Reservation{}, err
	}
	if reservation.ArtifactID == "" {
		return Reservation{}, errors.New("write was authorized without an artifact reservation")
	}
	if reservation.PhysicalPath != physicalPath {
		return Reservation{}, fmt.Errorf("artifact reservation is for %q, not %q", reservation.PhysicalPath, physicalPath)
	}
	return reservation, nil
}

// commitWrite is the after-I/O half of the check. The capability is verified a
// second time against the same call, because a write that outlived its
// authorisation must not be reported as an authorised one, and the path it
// resolved to is re-derived so the artifact recorded is the artifact written.
//
// A failure here is returned, never swallowed: the bytes are on disk, and the
// reservation stays in its unfinished state so the session's withdrawal remains
// retryable instead of completing over a file nothing accounts for.
func (f FilesTools) commitWrite(ctx context.Context, scope callScope, reservation Reservation, physicalPath string) error {
	claims, err := scope.guard.Verify(scope.token, scope.tool, scope.args, scope.agent)
	if err != nil {
		return fmt.Errorf("provenance capability no longer valid after write to %q: %w", physicalPath, err)
	}
	if claims.SessionID != scope.claims.SessionID || claims.RunID != scope.claims.RunID ||
		normalizeLogicalPath(claims.LogicalPath) != normalizeLogicalPath(scope.claims.LogicalPath) ||
		claims.DeletionGeneration != scope.claims.DeletionGeneration {
		return fmt.Errorf("provenance capability changed during write to %q", physicalPath)
	}
	if err := scope.guard.FinalizeWrite(ctx, reservation.ArtifactID, scope.token, true); err != nil {
		return fmt.Errorf("write to %q completed but could not be recorded: %w", physicalPath, err)
	}
	return nil
}

// releaseWrite withdraws a reservation whose bytes never landed. Its own
// failure is joined onto the original error rather than replacing it, so the
// caller learns both what went wrong and that the reservation is still open.
func (f FilesTools) releaseWrite(ctx context.Context, scope callScope, reservation Reservation, cause error) error {
	if reservation.ArtifactID == "" {
		return cause
	}
	if err := scope.guard.FinalizeWrite(ctx, reservation.ArtifactID, scope.token, false); err != nil {
		return errors.Join(cause, fmt.Errorf("release artifact reservation %s: %w", reservation.ArtifactID, err))
	}
	return cause
}

func (f FilesTools) pathExists(ctx context.Context, clean string) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if clean == "." {
		root, err := f.openRoot()
		if err != nil {
			return false
		}
		_ = root.Close()
		return true
	}
	parent, leaf, _, err := f.openParentPathContext(ctx, clean, false)
	if err != nil {
		return false
	}
	defer func() { _ = parent.Close() }()
	var stat unix.Stat_t
	return unix.Fstatat(int(parent.Fd()), leaf, &stat, unix.AT_SYMLINK_NOFOLLOW) == nil
}

func (f FilesTools) readDirectoryNames(ctx context.Context, clean string) ([]string, error) {
	directory, _, err := f.openDirectoryPathContext(ctx, clean, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	var names []string
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, readErr := readDirectoryEntriesAt(ctx, directory, searchDirBatchSize)
		for _, entry := range batch {
			if entry.err == nil && !isInternalStagingName(entry.name) {
				names = append(names, entry.name)
			}
		}
		if errors.Is(readErr, io.EOF) || len(batch) == 0 {
			return names, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func normalizeLogicalPath(logicalPath string) string {
	if strings.TrimSpace(logicalPath) == "" {
		return "."
	}
	clean, _, err := normalizeSandboxPath(logicalPath)
	if err != nil {
		return logicalPath
	}
	return clean
}
