package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestCancelledApprovalResumeRequiresCompleteAuthorizedIdentity(t *testing.T) {
	for _, change := range []string{"matching", "worker", "run", "approval", "attempt", "old-version", "future-version", "absent-version", "pending", "missing-expiry-proof", "wrong-duplicate-approval", "forged-waiting-event"} {
		t.Run(change, func(t *testing.T) {
			repo := New(openTestDB(t))
			ctx := context.Background()
			fixture := newApprovalResumeFixture(t, repo, "worker-cancel-ready")
			approvalID := fixture.approve(t, "call-cancel-ready", "ready.txt")
			input := ResumeApprovedRunInput{
				RunID: fixture.runID, ApprovalID: approvalID, WorkerID: fixture.workerID,
				AssignmentAttemptID: fixture.attempt, ExpectedStateVersion: fixture.waiting.StateVersion,
			}
			if change == "pending" {
				input.ApprovalID = fixture.request(t, "call-pending", "pending.txt")
				input.ExpectedStateVersion = fixture.waiting.StateVersion
			}
			if change == "wrong-duplicate-approval" {
				input.ApprovalID = fixture.approve(t, "call-other-approved", "other.txt")
				input.ExpectedStateVersion = fixture.waiting.StateVersion
			}
			if change == "wrong-duplicate-approval" || change == "forged-waiting-event" {
				if _, err := fixture.resume(approvalID); err != nil {
					t.Fatal(err)
				}
				if change == "forged-waiting-event" {
					input.ExpectedStateVersion = fixture.state(t).StateVersion
				}
			}
			run, err := repo.GetRun(ctx, fixture.runID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CancelUserRun(ctx, run.SessionID, run.RunID, "cancel"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "worker":
				input.WorkerID = "different-worker"
			case "run":
				input.RunID = "different-run"
			case "approval":
				input.ApprovalID = "different-approval"
			case "attempt":
				input.AssignmentAttemptID = "different-attempt"
			case "old-version":
				input.ExpectedStateVersion--
			case "future-version":
				input.ExpectedStateVersion += 2
			case "absent-version":
				input.ExpectedStateVersion = 0
			case "missing-expiry-proof":
				if _, err := repo.db.Exec(`DELETE FROM events WHERE run_id=? AND type='approval.expired'`, run.RunID); err != nil {
					t.Fatal(err)
				}
			case "forged-waiting-event":
				payload := fmt.Sprintf(`{"runState":{"stateVersion":%d,"lifecycle":"waiting_approval"}}`, input.ExpectedStateVersion)
				if _, err := repo.db.Exec(`UPDATE events SET type='message.delta', payload_json=? WHERE run_id=? AND type='agent.run.state_changed'`, payload, run.RunID); err != nil {
					t.Fatal(err)
				}
			}
			before := fixture.state(t)
			events := countRunEvents(t, repo, run.RunID)
			matched, err := repo.ApprovalResumeLostToUserCancellation(ctx, input)
			if err != nil || matched != (change == "matching") {
				t.Fatalf("match(%s)=%v, %v", change, matched, err)
			}
			if after := fixture.state(t); after != before {
				t.Fatalf("classification changed state: %+v -> %+v", before, after)
			}
			if countRunEvents(t, repo, run.RunID) != events {
				t.Fatal("classification appended an event")
			}
			var approvalStatus string
			var token sql.NullString
			if err := repo.db.QueryRow(`SELECT status,approval_token FROM approvals WHERE id=?`, approvalID).Scan(&approvalStatus, &token); err != nil {
				t.Fatal(err)
			}
			if approvalStatus != "expired" || token.Valid {
				t.Fatalf("classification restored authorization: %s %v", approvalStatus, token)
			}
		})
	}
}
