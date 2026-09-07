package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

type ApprovalPreviewRecord struct {
	ApprovalID   string
	Hash         string
	SnapshotJSON string
	Generation   int64
}

// ApprovalPreviewContext reads no raw argument bodies. The exact recorded
// tool-call identity is checked before any private file-server read.
type ApprovalPreviewContext struct {
	SessionID  string
	Generation int64
	Lifecycle  string
	ToolName   string
	ArgsHash   string
	AgentID    string
}

func (r *Repository) GetApprovalPreviewContext(ctx context.Context, id string) (ApprovalPreviewContext, error) {
	var out ApprovalPreviewContext
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, COALESCE((SELECT lifecycle_version FROM session_deletions WHERE session_id=s.id),0), r.status, tc.tool_name, tc.args_hash, tc.agent_id
		FROM approvals a JOIN agent_runs r ON r.id=a.run_id
		JOIN sessions s ON s.id=r.session_id
		JOIN tool_calls tc ON tc.id=a.tool_call_id AND tc.run_id=a.run_id
		WHERE a.id=? AND s.deletion_state='active'
	`, id).Scan(&out.SessionID, &out.Generation, &out.Lifecycle, &out.ToolName, &out.ArgsHash, &out.AgentID)
	return out, err
}

func (r *Repository) GetApprovalPreview(ctx context.Context, id string) (ApprovalPreviewRecord, error) {
	var p ApprovalPreviewRecord
	err := r.db.QueryRowContext(ctx, `SELECT p.approval_id,p.preview_hash,
		CASE WHEN length(CAST(p.snapshot_json AS BLOB))<=? THEN p.snapshot_json ELSE '' END,p.deletion_generation
		FROM approval_previews p JOIN approvals a ON a.id=p.approval_id
		JOIN agent_runs r ON r.id=a.run_id JOIN sessions s ON s.id=r.session_id
		WHERE p.approval_id=? AND s.deletion_state='active' AND COALESCE((SELECT lifecycle_version FROM session_deletions WHERE session_id=s.id),0)=p.deletion_generation`,
		approvalpreview.MaxSnapshotBytes, id).Scan(&p.ApprovalID, &p.Hash, &p.SnapshotJSON, &p.Generation)
	return p, err
}

// StoreApprovalPreview serializes refresh against decisions. A first read
// racing another first read retains the first snapshot, not last-writer-wins.
func (r *Repository) StoreApprovalPreview(ctx context.Context, p ApprovalPreviewRecord, refresh bool) error {
	if len(p.SnapshotJSON) > approvalpreview.MaxSnapshotBytes || p.Hash == "" {
		return errors.New("invalid preview")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	a, err := approvalByID(ctx, tx, p.ApprovalID)
	if err != nil {
		return err
	}
	if approvalExpiredAtDecision(a.ExpiresAt, now()) {
		return ErrApprovalExpired
	}
	var pending int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM approvals a JOIN agent_runs r ON r.id=a.run_id
		JOIN sessions s ON s.id=r.session_id WHERE a.id=? AND a.status='pending'
		AND r.status IN ('running','waiting_approval') AND s.deletion_state='active'
		AND COALESCE((SELECT lifecycle_version FROM session_deletions WHERE session_id=s.id),0)=?`, p.ApprovalID, p.Generation).Scan(&pending)
	if err != nil {
		return err
	}
	statement := `INSERT INTO approval_previews(approval_id,preview_hash,snapshot_json,deletion_generation) VALUES(?,?,?,?) ON CONFLICT(approval_id) DO NOTHING`
	if refresh {
		statement = `INSERT INTO approval_previews(approval_id,preview_hash,snapshot_json,deletion_generation) VALUES(?,?,?,?) ON CONFLICT(approval_id) DO UPDATE SET preview_hash=excluded.preview_hash,snapshot_json=excluded.snapshot_json,deletion_generation=excluded.deletion_generation`
	}
	if _, err = tx.ExecContext(ctx, statement, p.ApprovalID, p.Hash, p.SnapshotJSON, p.Generation); err != nil {
		return err
	}
	return tx.Commit()
}

func checkPreviewDecision(ctx context.Context, tx *sql.Tx, id, hash string) error {
	var match int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM approval_previews p JOIN approvals a ON a.id=p.approval_id
		JOIN agent_runs r ON r.id=a.run_id JOIN sessions s ON s.id=r.session_id
		WHERE p.approval_id=? AND p.preview_hash=? AND s.deletion_state='active'
		AND COALESCE((SELECT lifecycle_version FROM session_deletions WHERE session_id=s.id),0)=p.deletion_generation AND r.status IN ('running','waiting_approval')`, id, hash).Scan(&match)
	if err != nil {
		return errors.New("preview changed or unavailable")
	}
	return nil
}
