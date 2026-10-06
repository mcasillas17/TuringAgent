package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// seedChildSession is a hidden child of parentSessionID with one queued run,
// as delegation leaves it.
func seedChildSession(t *testing.T, ctx context.Context, repo *Repository, parentSessionID string) (string, string) {
	t.Helper()
	child := seedSession(t, ctx, repo, "Child", "")
	queued, err := repo.EnqueueUserMessage(ctx, localTurn(child, "the brief"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE sessions SET kind = 'delegation', parent_session_id = ? WHERE id = ?`, parentSessionID, child); err != nil {
		t.Fatal(err)
	}
	return child, queued.RunID
}

func receiptParent(t *testing.T, ctx context.Context, repo *Repository, sessionID string) string {
	t.Helper()
	var parent string
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COALESCE(parent_session_id, '') FROM session_deletions WHERE session_id = ?`, sessionID).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	return parent
}

func sessionExists(t *testing.T, ctx context.Context, repo *Repository, sessionID string) bool {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

// A child is withdrawn by the same begin as a chat, with its runs cancelled,
// and its receipt names the parent. The child begin refuses a chat session,
// and the public begin still refuses a child.
func TestAChildSessionDeletionRecordsItsParent(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	child, childRun := seedChildSession(t, ctx, repo, parent)

	if _, err := repo.BeginDelegationSessionDeletion(ctx, parent); !errors.Is(err, ErrNotADelegationSession) {
		t.Fatalf("child begin on a chat = %v, want ErrNotADelegationSession", err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, child); !errors.Is(err, ErrDelegationSessionReadOnly) {
		t.Fatalf("public begin on a child = %v, want ErrDelegationSessionReadOnly", err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, parent); err != nil {
		t.Fatal(err)
	}
	receipt, err := repo.BeginDelegationSessionDeletion(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SessionID != child || receipt.State != "quiescing" || receipt.RunCount != 1 {
		t.Fatalf("child receipt = %+v", receipt)
	}
	if got := receiptParent(t, ctx, repo, child); got != parent {
		t.Fatalf("child receipt parent = %q, want %q", got, parent)
	}
	if got := receiptParent(t, ctx, repo, parent); got != "" {
		t.Fatalf("chat receipt parent = %q, want none", got)
	}
	var status string
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id = ?`, childRun).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("child run status = %q, want cancelled", status)
	}
	again, err := repo.BeginDelegationSessionDeletion(ctx, child)
	if err != nil || again != receipt {
		t.Fatalf("a repeated child begin = %+v, %v; want the same receipt", again, err)
	}
}

// The parent's row stays until every child is withdrawn: a child row, or a
// child receipt that is not completed after its row is gone, holds the parent
// at child_deletion_pending ahead of its own artifact count.
func TestAParentDeletionWaitsForItsChildren(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	child, _ := seedChildSession(t, ctx, repo, parent)
	parentTurn, err := repo.EnqueueUserMessage(ctx, localTurn(parent, "delegate this"))
	if err != nil {
		t.Fatal(err)
	}
	seedSandboxArtifact(t, repo, parent, parentTurn.RunID, "sandbox_parent")
	if _, err := repo.BeginSessionDeletion(ctx, parent); err != nil {
		t.Fatal(err)
	}

	assertWaiting := func(stage string) {
		t.Helper()
		receipt, err := repo.AdvanceSessionDeletion(ctx, parent, nil)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.State != "failed_external" || !receipt.Retryable || receipt.ErrorCode != SessionDeletionChildPending {
			t.Fatalf("%s: parent receipt = %+v, want child_deletion_pending", stage, receipt)
		}
		if !sessionExists(t, ctx, repo, parent) {
			t.Fatalf("%s: the parent row was deleted before its children", stage)
		}
	}
	assertWaiting("child not begun")

	if _, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil {
		t.Fatal(err)
	}
	failing := func(context.Context) error { return errors.New("vault unavailable") }
	if receipt, err := repo.AdvanceSessionDeletion(ctx, child, failing); err != nil || receipt.ErrorCode != SessionDeletionMemoryReconcileFailed {
		t.Fatalf("child advance = %+v, %v; want its completion to fail", receipt, err)
	}
	if sessionExists(t, ctx, repo, child) {
		t.Fatal("the child row survived its own advance")
	}
	assertWaiting("child row gone, receipt unfinished")

	if receipt, err := repo.AdvanceSessionDeletion(ctx, child, nil); err != nil || receipt.State != "completed" {
		t.Fatalf("child retry = %+v, %v; want completed", receipt, err)
	}
	receipt, err := repo.AdvanceSessionDeletion(ctx, parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ErrorCode != SessionDeletionArtifactCleanupPending {
		t.Fatalf("parent receipt = %+v, want its own artifact count once its children are gone", receipt)
	}
}

// The client never sees a child's receipt; the reconciler sees every receipt
// with its parent.
func TestChildDeletionReceiptsAreInternal(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	child, _ := seedChildSession(t, ctx, repo, parent)
	if _, err := repo.BeginSessionDeletion(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx, child, func(context.Context) error { return errors.New("vault unavailable") }); err != nil {
		t.Fatal(err)
	}

	public, err := repo.PendingSessionDeletionReceipts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(public) != 1 || public[0].SessionID != parent {
		t.Fatalf("public receipts = %+v, want only the parent's", public)
	}
	pending, err := repo.PendingSessionDeletions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{parent: "", child: parent}
	if len(pending) != len(want) {
		t.Fatalf("pending = %+v, want %v", pending, want)
	}
	for _, deletion := range pending {
		if parentID, ok := want[deletion.SessionID]; !ok || parentID != deletion.ParentSessionID {
			t.Fatalf("pending = %+v, want %v", pending, want)
		}
	}
}

// A parent's children are its child rows in id order, then every unfinished
// child receipt whose row is already gone; a finished child and another
// parent's child are not among them.
func TestDelegationChildSessionIDsFindsRowsAndUnfinishedReceipts(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	other := seedSession(t, ctx, repo, "Other", "")
	first, _ := seedChildSession(t, ctx, repo, parent)
	second, _ := seedChildSession(t, ctx, repo, parent)
	gone, _ := seedChildSession(t, ctx, repo, parent)
	finished, _ := seedChildSession(t, ctx, repo, parent)
	seedChildSession(t, ctx, repo, other)
	if _, err := repo.BeginSessionDeletion(ctx, parent); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{second, gone, finished} {
		if _, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.AdvanceSessionDeletion(ctx, gone, func(context.Context) error { return errors.New("vault unavailable") }); err != nil {
		t.Fatal(err)
	}
	if receipt, err := repo.AdvanceSessionDeletion(ctx, finished, nil); err != nil || receipt.State != "completed" {
		t.Fatalf("finished child = %+v, %v", receipt, err)
	}

	got, err := repo.DelegationChildSessionIDs(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	rows := []string{first, second}
	slices.Sort(rows)
	if want := append(rows, gone); !slices.Equal(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
}

// Each begin keeps refusing the other kind after the session's row is gone:
// the receipt says whether it withdrew a child, through the parent it names.
func TestTheBeginsKeepTheirKindAfterTheRowIsGone(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	child, _ := seedChildSession(t, ctx, repo, parent)
	chat := seedSession(t, ctx, repo, "Chat", "")
	if _, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdvanceSessionDeletion(ctx, child, func(context.Context) error { return errors.New("vault unavailable") }); err != nil {
		t.Fatal(err)
	}
	if sessionExists(t, ctx, repo, child) {
		t.Fatal("the child's row survived its advance")
	}
	if _, err := repo.BeginSessionDeletion(ctx, child); !errors.Is(err, ErrDelegationSessionReadOnly) {
		t.Fatalf("public begin on an unfinished child receipt = %v, want ErrDelegationSessionReadOnly", err)
	}
	if receipt, err := repo.AdvanceSessionDeletion(ctx, child, nil); err != nil || receipt.State != "completed" {
		t.Fatalf("child retry = %+v, %v", receipt, err)
	}
	if _, err := repo.BeginSessionDeletion(ctx, child); !errors.Is(err, ErrDelegationSessionReadOnly) {
		t.Fatalf("public begin on a completed child receipt = %v, want ErrDelegationSessionReadOnly", err)
	}
	if again, err := repo.BeginDelegationSessionDeletion(ctx, child); err != nil || again.State != "completed" {
		t.Fatalf("child begin on its completed receipt = %+v, %v", again, err)
	}

	if _, err := repo.BeginSessionDeletion(ctx, chat); err != nil {
		t.Fatal(err)
	}
	if receipt, err := repo.AdvanceSessionDeletion(ctx, chat, nil); err != nil || receipt.State != "completed" {
		t.Fatalf("chat deletion = %+v, %v", receipt, err)
	}
	if _, err := repo.BeginDelegationSessionDeletion(ctx, chat); !errors.Is(err, ErrNotADelegationSession) {
		t.Fatalf("child begin on a deleted chat's receipt = %v, want ErrNotADelegationSession", err)
	}
	if again, err := repo.BeginSessionDeletion(ctx, chat); err != nil || again.State != "completed" {
		t.Fatalf("public begin on a deleted chat's receipt = %+v, %v", again, err)
	}
}

// The child gate comes after the parent's own quiesce wait: a parent whose run
// is still executing reports quiescing, not child_deletion_pending, and moves
// to the child gate once its execution exits.
func TestAParentQuiescesBeforeItWaitsForItsChildren(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	parent := seedSession(t, ctx, repo, "Turing", "")
	parentTurn, err := repo.EnqueueUserMessage(ctx, localTurn(parent, "delegate this"))
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker", 0, 0, nil, nil); err != nil || claimed.RunID != parentTurn.RunID {
		t.Fatalf("claim = %+v, %v; want the parent's run", claimed, err)
	}
	seedChildSession(t, ctx, repo, parent)
	if _, err := repo.BeginSessionDeletion(ctx, parent); err != nil {
		t.Fatal(err)
	}

	receipt, err := repo.AdvanceSessionDeletion(ctx, parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "quiescing" || receipt.ErrorCode != "" {
		t.Fatalf("parent receipt while executing = %+v, want quiescing", receipt)
	}
	if err := repo.AcknowledgeExecutionExit(ctx, parentTurn.RunID); err != nil {
		t.Fatal(err)
	}
	receipt, err = repo.AdvanceSessionDeletion(ctx, parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ErrorCode != SessionDeletionChildPending {
		t.Fatalf("parent receipt after its exit = %+v, want child_deletion_pending", receipt)
	}
}
