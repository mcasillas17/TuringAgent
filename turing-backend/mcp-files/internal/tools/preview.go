package tools

import (
	"context"
	"errors"
	"os"
	"strings"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"golang.org/x/sys/unix"
)

// PreparePreview uses the real argument validators and physical-path resolver.
// It never consumes approval, reserves an artifact, or creates directories.
func (f FilesTools) PreparePreview(ctx context.Context, req CallRequest) (approvalpreview.Snapshot, error) {
	safeArgs, state := approvalpreview.SanitizeArguments(req.Args)
	p := approvalpreview.Snapshot{ArgumentsJSON: safeArgs, State: state}
	if req.Name == "files.create" || req.Name == "files.update" {
		// Optional display arguments are not execution arguments. Only a ready
		// File preview should retain mutation content, never a failed preview.
		p.ArgumentsJSON = "{}"
	}
	scope, err := f.newCallScope(req)
	if err != nil {
		return p, err
	}
	if err := scope.checkSessionActive(ctx); err != nil {
		return p, err
	}
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY {
		return p, nil
	}
	var logical, after, expected string
	switch req.Name {
	case "files.create":
		logical, after, err = validateCreateArgs(req.Args)
	case "files.update":
		logical, after, expected, err = validateUpdateArgs(req.Args)
	default:
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNSUPPORTED
		return p, nil
	}

	if err != nil {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
		return p, nil
	}
	if len(after) > approvalpreview.MaxTextBytes {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED
		return p, nil
	}
	clean, _, err := normalizeSandboxPath(logical)
	if err != nil {
		return p, err
	}
	if err := scope.requirePathScope(clean); err != nil {
		return p, err
	}
	physical, err := f.resolveWrite(ctx, scope, clean, req.Name == "files.create")
	if err != nil {
		return p, err
	}
	unlock, err := f.locks.lockContext(ctx, f.pathLockKey(physical))
	if err != nil {
		return p, err
	}
	defer unlock()
	file := &turingv1.FileMutationPreview{LogicalPath: clean, PhysicalPath: physical, Operation: strings.TrimPrefix(req.Name, "files."), AfterText: after, AfterHash: contentHash(after)}
	if req.Name == "files.create" {
		if err := f.checkCreatePreconditions(ctx, physical); err != nil {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			if errors.Is(err, errCreateTargetExists) {
				p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
			}
			return p, nil
		}
	} else {
		current, _, stat, err := f.openRegularFileContext(ctx, physical)
		if err != nil {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			return p, nil
		}
		defer func() { _ = current.Close() }()
		if stat.Mode&0444 == 0 || requireUpdateWriteBits(uint32(stat.Mode)) != nil {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			return p, nil
		}
		content, _, _, err := readBoundedContext(ctx, current, approvalpreview.MaxTextBytes+1)
		if err != nil {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			return p, nil
		}
		if len(content) > approvalpreview.MaxTextBytes {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED
			return p, nil
		}
		file.BeforeExists = true
		file.BeforeText = string(content)
		file.BeforeHash = contentHash(file.BeforeText)
		if expected != "" && expected != file.BeforeHash {
			p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
			return p, nil
		}
	}
	if approvalpreview.Binary(file.BeforeText) || approvalpreview.Binary(after) {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY
		return p, nil
	}
	if approvalpreview.UnsafeText(file.BeforeText) || approvalpreview.UnsafeText(after) || approvalpreview.SensitivePath(clean) {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED
		return p, nil
	}
	file.UnifiedDiff = approvalpreview.Diff(file.BeforeText, after)
	if len(file.UnifiedDiff) > approvalpreview.MaxDiffBytes {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED
		return p, nil
	}
	// Re-resolve to detect a run-scoped file shadowing the target during I/O.
	resolved, err := f.resolveWrite(ctx, scope, clean, req.Name == "files.create")
	if err != nil {
		return p, err
	}
	if resolved != physical {
		p.State = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
		return p, nil
	}
	if err := scope.checkSessionActive(ctx); err != nil {
		return p, err
	}
	p.File = file
	return p, nil
}

func (f FilesTools) writeBinding(scope callScope, token, physical, content string) (approvalpreview.Binding, error) {
	if !scope.active {
		return approvalpreview.Binding{}, nil
	}
	b, err := scope.guard.VerifyWrite(WriteAuthorization{ApprovalToken: token, ProvenanceToken: scope.token, Tool: scope.tool,
		Args: scope.args, AgentID: scope.agent, PhysicalPath: physical})
	if err != nil {
		return b, err
	}
	if b.PreviewHash == "" || b.PhysicalPath != physical || b.AfterHash != contentHash(content) ||
		b.SessionID != scope.claims.SessionID || b.RunID != scope.claims.RunID || b.Generation != scope.claims.DeletionGeneration ||
		(scope.tool == "files.create" && (b.BeforeExists || b.BeforeHash != "")) ||
		(scope.tool == "files.update" && (!b.BeforeExists || b.BeforeHash == "")) {
		return b, errors.New("reviewed mutation precondition mismatch")
	}
	return b, nil
}

// checkWriteBoundary keeps the cooperating-writer lock held through re-resolve,
// session liveness, and final descriptor identity checks. Privileged external
// writers ignoring that lock can still race the final POSIX compare/rename.
func (f FilesTools) checkWriteBoundary(ctx context.Context, scope callScope, logical, physical string, parent *os.File, isCreate bool) error {
	if !scope.active {
		return ctx.Err()
	}
	if err := scope.checkSessionActive(ctx); err != nil {
		return err
	}
	resolved, err := f.resolveWrite(ctx, scope, logical, isCreate)
	if err != nil {
		return err
	}
	if resolved != physical {
		return errors.New("reviewed physical target changed")
	}
	current, _, _, err := f.openParentPathContext(ctx, physical, false)
	if err != nil {
		return err
	}
	defer func() { _ = current.Close() }()
	var originalStat, currentStat unix.Stat_t
	if err := unix.Fstat(int(parent.Fd()), &originalStat); err != nil {
		return err
	}
	if err := unix.Fstat(int(current.Fd()), &currentStat); err != nil {
		return err
	}
	if originalStat.Dev != currentStat.Dev || originalStat.Ino != currentStat.Ino {
		return errors.New("reviewed target directory changed")
	}
	return nil
}
