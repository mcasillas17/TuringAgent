package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
)

// conflictLegacyAt is the fixed timestamp the corrupt rows below are written
// with. It only has to be a legal persisted value; nothing here depends on when
// it is.
const conflictLegacyAt = "2026-08-12T00:00:00.000000000Z"

// dropCorrelationIndex removes one of the two uniqueness guarantees so a test
// can write the row a healthy database refuses. Both indexes arrived with the
// run-outcomes migration, so a database restored from an older backup, or
// edited by hand, reaches the history reader in exactly this shape.
func dropCorrelationIndex(t *testing.T, ctx context.Context, database *db.DB, index string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `DROP INDEX `+index); err != nil {
		t.Fatalf("drop %s: %v", index, err)
	}
}

// insertClaimantMessage writes an assistant turn that claims a run, bypassing
// every writer, because no writer would produce a second claimant.
func insertClaimantMessage(t *testing.T, ctx context.Context, database *db.DB, id, sessionID, runID string, sequence int64) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, 'assistant', 'second claimant', 'text', ?, ?)
	`, id, sessionID, runID, sequence, conflictLegacyAt); err != nil {
		t.Fatalf("insert claimant message %s: %v", id, err)
	}
}

// TestListMessagesRejectsMultipleRunsClaimingOneAssistantMessage covers the
// other direction of the circular link. Two runs naming one assistant message
// is the same ambiguity as two messages naming one run — read from the run
// side, where a primary-key join can never see it, because the page row joins
// the run it names rather than the runs that name it.
func TestListMessagesRejectsMultipleRunsClaimingOneAssistantMessage(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Two runs one answer", "the answer")

	dropCorrelationIndex(t, ctx, database, "idx_runs_assistant_message_unique")
	// A second run claiming the first run's answer. The message still names
	// only the original run, so the claimant is invisible from the message
	// row's own columns.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id,
			status, model_provider, model_name, created_at, state_version, state_updated_at, outcome_reason,
			assistant_content_sha256)
		VALUES ('run_second_owner', ?, ?, ?, 'general_assistant', 'trace_second_owner',
			'completed', 'ollama', 'llama3.2', ?, 3, ?, 'none', ?)
	`, enqueued.SessionID, enqueued.UserMessageID, enqueued.AssistantMessageID,
		conflictLegacyAt, conflictLegacyAt, emptyAssistantContentSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_run_owner_anchor', ?, 'user', 'anchor', 'text', 91, ?)
	`, enqueued.SessionID, conflictLegacyAt); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ListMessages(ctx, enqueued.SessionID, 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessages error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "run_second_owner", "trace_second_owner", "the answer")

	_, err = repo.ListMessagesBefore(ctx, enqueued.SessionID, "msg_run_owner_anchor", 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessagesBefore error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "run_second_owner", "trace_second_owner", "the answer")
}

// TestListMessagesRejectsDuplicateRunClaimantsOutsideTheReturnedPage is the
// property a page-scoped check cannot have. Whether a run's ownership is
// ambiguous is a fact about the database, so it cannot depend on how many rows
// a client asked for or where it happened to anchor: a page holding either
// claimant has to fail, including the page that holds only one of them.
func TestListMessagesRejectsDuplicateRunClaimantsOutsideTheReturnedPage(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Split ownership", "the answer")

	dropCorrelationIndex(t, ctx, database, "idx_messages_assistant_run_unique")
	// An anchor sits between the two claimants, so no single page can hold
	// both: everything before the anchor holds the original answer, and the
	// newest row is the second claimant alone.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_split_anchor', ?, 'user', 'anchor', 'text', 50, ?)
	`, enqueued.SessionID, conflictLegacyAt); err != nil {
		t.Fatal(err)
	}
	insertClaimantMessage(t, ctx, database, "msg_later_claimant", enqueued.SessionID, enqueued.RunID, 90)

	// The newest page at limit one is the second claimant with nothing to
	// compare it against.
	_, err := repo.ListMessages(ctx, enqueued.SessionID, 1)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessages at limit 1 error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_later_claimant", "second claimant", "the answer")

	// The older page holds the original answer and none of the ambiguity that
	// follows it.
	_, err = repo.ListMessagesBefore(ctx, enqueued.SessionID, "msg_split_anchor", 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessagesBefore across the split error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_later_claimant", "second claimant", "the answer")

	// And the unbounded page, which holds both, must not be the only one that
	// notices.
	if _, err := repo.ListMessages(ctx, enqueued.SessionID, 50); !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessages unbounded error = %v, want the correlation conflict sentinel", err)
	}
}

// TestListMessagesRejectsDuplicateRunClaimantsInAnotherSession is the same fact
// hidden behind the session predicate rather than behind the limit. Neither
// session's history contains both claimants, so a reader that only compares the
// rows it returned would tell both users a story the other contradicts.
func TestListMessagesRejectsDuplicateRunClaimantsInAnotherSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Owning session", "the answer")
	foreign, err := repo.CreateSession(ctx, "Foreign session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	dropCorrelationIndex(t, ctx, database, "idx_messages_assistant_run_unique")
	insertClaimantMessage(t, ctx, database, "msg_foreign_claimant", foreign.SessionID, enqueued.RunID, 1)

	_, err = repo.ListMessages(ctx, enqueued.SessionID, 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("owning session error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_foreign_claimant", "second claimant", "the answer")

	_, err = repo.ListMessages(ctx, foreign.SessionID, 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("foreign session error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_foreign_claimant", "second claimant", "the answer")
}

// TestHistoryClaimantCountsProbeIndexesRatherThanScanning pins what makes the
// two claimant counts affordable. They run once per returned message, so the
// difference between an index probe and a table scan is the difference between
// a fixed cost and one that grows with everything the user has ever said.
//
// The query-budget tests cannot see this: a scan and a probe are both one round
// trip. So the plan is asserted directly, on the exact statements production
// sends. A predicate written so the partial index no longer applies —
// lower-cased, collated, or with the role dropped — passes every other test
// here and quietly turns history into a full scan.
func TestHistoryClaimantCountsProbeIndexesRatherThanScanning(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		query string
		args  []any
	}{
		{name: "newest_page", query: newestHistoryQuery, args: historyJoinArgs("sess_plan", 50)},
		{name: "older_page", query: olderHistoryQuery, args: historyJoinArgs("sess_plan", int64(10), 50)},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, err := database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+test.query, test.args...)
			if err != nil {
				t.Fatalf("explain: %v", err)
			}
			defer func() { _ = rows.Close() }()
			var plan []string
			for rows.Next() {
				var node, parent, unused int
				var detail string
				if err := rows.Scan(&node, &parent, &unused, &detail); err != nil {
					t.Fatalf("scan plan: %v", err)
				}
				plan = append(plan, strings.TrimSpace(detail))
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("plan rows: %v", err)
			}
			joined := strings.Join(plan, "\n")
			for _, index := range []string{
				"idx_messages_assistant_run_unique", "idx_runs_assistant_message_unique",
			} {
				if !strings.Contains(joined, index) {
					t.Fatalf("plan never reaches %s:\n%s", index, joined)
				}
			}
			// The aliases are the claimant subqueries' own. A scan of either is
			// the whole table read once per returned message.
			for _, step := range plan {
				if strings.HasPrefix(step, "SCAN claimant") || strings.HasPrefix(step, "SCAN owner") {
					t.Fatalf("claimant count scans a table instead of probing an index:\n%s", joined)
				}
			}
		})
	}
}

// TestHistoryConflictDetectionKeepsItsQueryBudget pins the round-trip cost of
// detecting both directions. The obvious alternative — ask the database about
// each page row's run, or about each run's messages — is invisible in the
// returned error and shows up only as history that gets slower the longer a
// conversation is.
func TestHistoryConflictDetectionKeepsItsQueryBudget(t *testing.T) {
	counter := &countingSQLiteDriver{}
	database := openCountingTestDB(t, counter)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Budgeted conflict", "the answer")
	dropCorrelationIndex(t, ctx, database, "idx_messages_assistant_run_unique")
	insertClaimantMessage(t, ctx, database, "msg_budget_claimant", enqueued.SessionID, enqueued.RunID, 90)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_budget_anchor', ?, 'user', 'anchor', 'text', 91, ?)
	`, enqueued.SessionID, conflictLegacyAt); err != nil {
		t.Fatal(err)
	}

	counter.reset()
	if _, err := repo.ListMessages(ctx, enqueued.SessionID, 50); !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessages error = %v, want the correlation conflict sentinel", err)
	}
	if got := counter.count(); got != 1 {
		t.Fatalf("ListMessages issued %d queries, want exactly one history query", got)
	}

	counter.reset()
	if _, err := repo.ListMessagesBefore(ctx, enqueued.SessionID, "msg_budget_anchor", 50); !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessagesBefore error = %v, want the correlation conflict sentinel", err)
	}
	if got := counter.count(); got != 2 {
		t.Fatalf("ListMessagesBefore issued %d queries, want one anchor lookup and one history query", got)
	}
}
