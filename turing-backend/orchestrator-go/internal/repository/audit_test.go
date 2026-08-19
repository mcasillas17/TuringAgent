package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// insertAuditRow writes a row directly (bypassing RecordAudit's now()) so
// tests can pin exact created_at values, including two rows sharing the same
// timestamp — the only way to exercise the rowid tie-breaker deterministically.
func insertAuditRow(t *testing.T, database *db.DB, id, correlationID, actorType, actorID, action, target, payloadJSON, createdAt string) int64 {
	t.Helper()
	result, err := database.ExecContext(context.Background(), `
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullableText(correlationID), actorType, nullableText(actorID), action, nullableText(target), nullableText(payloadJSON), createdAt)
	if err != nil {
		t.Fatalf("insert audit row %s: %v", id, err)
	}
	rowID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id for %s: %v", id, err)
	}
	return rowID
}

func TestListAuditRecordsFiltersExactCorrelationAndActionWithTimeBounds(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	insertAuditRow(t, database, "audit_1", "run_1", "runtime", "actor_1", "tool.call.before", "call_1", `{"toolName":"files.update"}`, "2026-01-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_2", "run_2", "runtime", "actor_1", "tool.call.before", "call_2", `{"toolName":"files.update"}`, "2026-01-01T00:00:01.000000000Z")
	insertAuditRow(t, database, "audit_3", "run_1", "runtime", "actor_1", "tool.call.after", "call_3", `{"toolName":"files.update"}`, "2026-01-01T00:00:02.000000000Z")
	insertAuditRow(t, database, "audit_4", "run_1", "runtime", "actor_1", "tool.call.before", "call_4", `{"toolName":"files.update"}`, "2026-01-01T00:00:03.000000000Z")
	// Outside the time window below; must not appear.
	insertAuditRow(t, database, "audit_5", "run_1", "runtime", "actor_1", "tool.call.before", "call_5", `{"toolName":"files.update"}`, "2026-01-01T00:00:04.000000000Z")

	records, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID:  sql.NullString{String: "run_1", Valid: true},
		Action:         sql.NullString{String: "tool.call.before", Valid: true},
		CreatedAtStart: sql.NullString{String: "2026-01-01T00:00:00.000000000Z", Valid: true},
		CreatedAtEnd:   sql.NullString{String: "2026-01-01T00:00:04.000000000Z", Valid: true},
		Order:          AuditOrderAscending,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords: %v", err)
	}
	var gotIDs []string
	for _, record := range records {
		gotIDs = append(gotIDs, record.AuditID)
	}
	want := []string{"audit_1", "audit_4"}
	if len(gotIDs) != len(want) || gotIDs[0] != want[0] || gotIDs[1] != want[1] {
		t.Fatalf("filtered IDs = %v, want %v (start is inclusive, end is exclusive, action/correlation are exact)", gotIDs, want)
	}
}

func TestListAuditRecordsOrdersByCreatedAtThenRowIDBothDirections(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	// Three rows share one timestamp; insertion order fixes their rowid order.
	insertAuditRow(t, database, "audit_a", "run_tie", "runtime", "", "tied.action", "", "", "2026-02-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_b", "run_tie", "runtime", "", "tied.action", "", "", "2026-02-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_c", "run_tie", "runtime", "", "tied.action", "", "", "2026-02-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_d", "run_tie", "runtime", "", "tied.action", "", "", "2026-02-01T00:00:01.000000000Z")

	descending, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_tie", Valid: true},
		Order:         AuditOrderDescending,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords descending: %v", err)
	}
	wantDesc := []string{"audit_d", "audit_c", "audit_b", "audit_a"}
	if !auditIDsEqual(descending, wantDesc) {
		t.Fatalf("descending IDs = %v, want %v", auditIDList(descending), wantDesc)
	}

	ascending, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_tie", Valid: true},
		Order:         AuditOrderAscending,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords ascending: %v", err)
	}
	wantAsc := []string{"audit_a", "audit_b", "audit_c", "audit_d"}
	if !auditIDsEqual(ascending, wantAsc) {
		t.Fatalf("ascending IDs = %v, want %v", auditIDList(ascending), wantAsc)
	}
	for i := range wantAsc {
		if wantAsc[i] != wantDesc[len(wantDesc)-1-i] {
			t.Fatalf("ascending is not the exact reverse of descending: asc=%v desc=%v", wantAsc, wantDesc)
		}
	}
}

func TestListAuditRecordsPaginatesWithoutDuplicatesOrGapsWhenTimestampsMatch(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	ids := []string{"audit_p1", "audit_p2", "audit_p3", "audit_p4", "audit_p5"}
	for _, id := range ids {
		// Every row shares the same timestamp, so only the rowid tie-breaker
		// can keep pagination stable.
		insertAuditRow(t, database, id, "run_page", "runtime", "", "paged.action", "", "", "2026-03-01T00:00:00.000000000Z")
	}

	firstPage, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_page", Valid: true},
		Order:         AuditOrderDescending,
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords first page: %v", err)
	}
	// Limit+1 over-fetch: 2 requested, 3 returned, so the caller can tell
	// there is a next page without a second round trip.
	if len(firstPage) != 3 {
		t.Fatalf("first page returned %d rows, want limit+1 = 3", len(firstPage))
	}
	firstPageIDs := auditIDList(firstPage[:2])
	if !equalStrings(firstPageIDs, []string{"audit_p5", "audit_p4"}) {
		t.Fatalf("first page IDs = %v, want [audit_p5 audit_p4]", firstPageIDs)
	}

	lastOfFirstPage := firstPage[1]
	secondPage, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_page", Valid: true},
		Order:         AuditOrderDescending,
		Limit:         2,
		After: &AuditCursor{
			CreatedAt: lastOfFirstPage.CreatedAt,
			RowID:     lastOfFirstPage.RowID,
		},
	})
	if err != nil {
		t.Fatalf("ListAuditRecords second page: %v", err)
	}
	if len(secondPage) != 3 {
		t.Fatalf("second page returned %d rows, want limit+1 = 3 (one more page remains after it)", len(secondPage))
	}
	secondPageIDs := auditIDList(secondPage[:2])
	if !equalStrings(secondPageIDs, []string{"audit_p3", "audit_p2"}) {
		t.Fatalf("second page IDs = %v, want [audit_p3 audit_p2]", secondPageIDs)
	}

	lastOfSecondPage := secondPage[1]
	thirdPage, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_page", Valid: true},
		Order:         AuditOrderDescending,
		Limit:         2,
		After: &AuditCursor{
			CreatedAt: lastOfSecondPage.CreatedAt,
			RowID:     lastOfSecondPage.RowID,
		},
	})
	if err != nil {
		t.Fatalf("ListAuditRecords third page: %v", err)
	}
	// Only one row remains, so the over-fetch has nothing extra to find: this
	// is the signal a caller uses to know there is no further page.
	if len(thirdPage) != 1 {
		t.Fatalf("third page returned %d rows, want 1 (no more pages beyond)", len(thirdPage))
	}
	thirdPageIDs := auditIDList(thirdPage)
	if !equalStrings(thirdPageIDs, []string{"audit_p1"}) {
		t.Fatalf("third page IDs = %v, want [audit_p1] (no duplicates, no gaps)", thirdPageIDs)
	}

	all := append(append([]string{}, firstPageIDs...), secondPageIDs...)
	all = append(all, thirdPageIDs...)
	if !equalStrings(all, []string{"audit_p5", "audit_p4", "audit_p3", "audit_p2", "audit_p1"}) {
		t.Fatalf("stitched pages = %v, want every row exactly once in order", all)
	}
}

func TestListAuditRecordsReportsAbsentScrubbedNormalAndOversizedPayloads(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	oversized := strings.Repeat("a", 16*1024+1)
	insertAuditRow(t, database, "audit_absent", "run_payload", "runtime", "", "payload.absent", "", "", "2026-04-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_scrubbed", "run_payload", "runtime", "", "payload.scrubbed", "", `{"scrubbed":true}`, "2026-04-01T00:00:01.000000000Z")
	insertAuditRow(t, database, "audit_normal", "run_payload", "runtime", "", "payload.normal", "", `{"toolName":"files.update"}`, "2026-04-01T00:00:02.000000000Z")
	insertAuditRow(t, database, "audit_oversized", "run_payload", "runtime", "", "payload.oversized", "", oversized, "2026-04-01T00:00:03.000000000Z")

	records, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_payload", Valid: true},
		Order:         AuditOrderAscending,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}

	absent := records[0]
	if absent.PayloadPresent || absent.PayloadScrubbed || absent.PayloadJSON.Valid {
		t.Fatalf("absent payload record = %+v, want present=false scrubbed=false json=invalid", absent)
	}

	scrubbed := records[1]
	if !scrubbed.PayloadPresent || !scrubbed.PayloadScrubbed {
		t.Fatalf("scrubbed payload record = %+v, want present=true scrubbed=true", scrubbed)
	}
	if !scrubbed.PayloadJSON.Valid || scrubbed.PayloadJSON.String != `{"scrubbed":true}` {
		t.Fatalf("scrubbed payload JSON = %+v, want the exact tombstone (it fits under the byte bound)", scrubbed.PayloadJSON)
	}

	normal := records[2]
	if !normal.PayloadPresent || normal.PayloadScrubbed {
		t.Fatalf("normal payload record = %+v, want present=true scrubbed=false", normal)
	}
	if !normal.PayloadJSON.Valid || normal.PayloadJSON.String != `{"toolName":"files.update"}` {
		t.Fatalf("normal payload JSON = %+v, want the stored payload verbatim", normal.PayloadJSON)
	}

	overBound := records[3]
	if !overBound.PayloadPresent {
		t.Fatalf("oversized payload record = %+v, want present=true (it exists, it's just not readable)", overBound)
	}
	if overBound.PayloadScrubbed {
		t.Fatalf("oversized payload record = %+v, want scrubbed=false (it isn't the tombstone)", overBound)
	}
	if overBound.PayloadJSON.Valid {
		t.Fatalf("oversized payload JSON = %+v, want invalid/null because it exceeds the 16 KiB bound", overBound.PayloadJSON)
	}
}

func TestListAuditRecordsRejectsInvalidLimitAndOrder(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	if _, err := repo.ListAuditRecords(ctx, AuditQuery{Order: AuditOrderDescending, Limit: 0}); err != ErrAuditInvalidLimit {
		t.Fatalf("Limit=0: err = %v, want ErrAuditInvalidLimit", err)
	}
	if _, err := repo.ListAuditRecords(ctx, AuditQuery{Order: AuditOrderDescending, Limit: -1}); err != ErrAuditInvalidLimit {
		t.Fatalf("Limit=-1: err = %v, want ErrAuditInvalidLimit", err)
	}
	if _, err := repo.ListAuditRecords(ctx, AuditQuery{Order: AuditOrderDescending, Limit: maxAuditRecordsLimit + 1}); err != ErrAuditInvalidLimit {
		t.Fatalf("Limit=%d: err = %v, want ErrAuditInvalidLimit", maxAuditRecordsLimit+1, err)
	}
	if _, err := repo.ListAuditRecords(ctx, AuditQuery{Order: AuditOrder(99), Limit: 10}); err != ErrAuditInvalidOrder {
		t.Fatalf("Order=99: err = %v, want ErrAuditInvalidOrder", err)
	}
}

// TestListAuditRecordsRejectsRunawayLimitsWithoutOverflowingOrPanicking pins
// the failure mode from a client (or a bug upstream of validation) passing an
// enormous Limit: math.MaxInt overflows to a negative number at Limit+1, and
// that negative number was previously fed straight into make()'s capacity
// argument, which panics. Both math.MaxInt and one-above-the-repository-max
// must be rejected by validation before any arithmetic or allocation runs.
func TestListAuditRecordsRejectsRunawayLimitsWithoutOverflowingOrPanicking(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	cases := []struct {
		name  string
		limit int
	}{
		{"MaxInt", math.MaxInt},
		{"OneAboveRepositoryMax", 101},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ListAuditRecords panicked for Limit=%d: %v", tc.limit, r)
				}
			}()
			_, err := repo.ListAuditRecords(ctx, AuditQuery{Order: AuditOrderDescending, Limit: tc.limit})
			if !errors.Is(err, ErrAuditInvalidLimit) {
				t.Fatalf("Limit=%d: err = %v, want ErrAuditInvalidLimit", tc.limit, err)
			}
		})
	}
}

// TestAuditPayloadBoundedProjectionSQLScansOnlyBoundedPrefix locks the SQL
// projection used to null out oversized payloads. It must inspect only the
// first maxAuditPayloadReadBytes+1 bytes via substr(...) rather than
// length()ing the entire stored payload_json column, or a large stored
// payload turns every read of its row into a full-payload scan even though
// the bytes returned are still bounded.
func TestAuditPayloadBoundedProjectionSQLScansOnlyBoundedPrefix(t *testing.T) {
	want := "CASE WHEN length(substr(CAST(payload_json AS BLOB), 1, ?)) <= ? THEN payload_json END"
	if auditPayloadBoundedProjectionSQL != want {
		t.Fatalf("auditPayloadBoundedProjectionSQL = %q, want %q (bounded substr prefix, not length() over the full column)", auditPayloadBoundedProjectionSQL, want)
	}
}

// TestListAuditRecordsBoundsOversizedStructuralMetadata proves stored
// structural metadata is treated as untrusted at the repository boundary: a row
// whose id/actor_type/action (required) and correlation_id/actor_id/target
// (optional) all exceed the read bound must still return, with required fields
// projected empty and optional fields projected NULL, so a single hostile row
// can never make one read hand back an unbounded amount of column bytes.
func TestListAuditRecordsBoundsOversizedStructuralMetadata(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	oversized := strings.Repeat("m", maxAuditMetadataReadBytes+1)
	// actor_type is constrained by a CHECK to a fixed enum, so it can't hold an
	// oversized value; every other structural column is free-form TEXT. Bounding
	// actor_type in the query is still defense-in-depth (the read path must not
	// rely on a write-time CHECK), locked by the SQL-expression guard test.
	insertAuditRow(t, database, oversized, oversized, "runtime", oversized, oversized, oversized, `{"toolName":"files.update"}`, "2026-06-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_small", "run_small", "runtime", "actor_small", "small.action", "target_small", "", "2026-06-01T00:00:01.000000000Z")

	records, err := repo.ListAuditRecords(ctx, AuditQuery{
		CreatedAtStart: sql.NullString{String: "2026-06-01T00:00:00.000000000Z", Valid: true},
		CreatedAtEnd:   sql.NullString{String: "2026-06-01T00:00:02.000000000Z", Valid: true},
		Order:          AuditOrderAscending,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (the oversized row must still return)", len(records))
	}

	over := records[0]
	if over.AuditID != "" {
		t.Fatalf("oversized id = %q, want empty (required over the bound projects '')", over.AuditID)
	}
	if over.ActorType != "runtime" {
		t.Fatalf("actor_type = %q, want runtime (a within-bound required field is untouched)", over.ActorType)
	}
	if over.Action != "" {
		t.Fatalf("oversized action = %q, want empty", over.Action)
	}
	if over.CorrelationID.Valid {
		t.Fatalf("oversized correlation_id valid=%v value=%q, want NULL", over.CorrelationID.Valid, over.CorrelationID.String)
	}
	if over.ActorID.Valid {
		t.Fatalf("oversized actor_id valid=%v value=%q, want NULL", over.ActorID.Valid, over.ActorID.String)
	}
	if over.Target.Valid {
		t.Fatalf("oversized target valid=%v value=%q, want NULL", over.Target.Valid, over.Target.String)
	}
	// The row still comes back — a readable payload is proof it wasn't dropped.
	if !over.PayloadPresent || !over.PayloadJSON.Valid {
		t.Fatalf("oversized row present=%v json.valid=%v, want the row to return with a readable payload", over.PayloadPresent, over.PayloadJSON.Valid)
	}

	small := records[1]
	if small.AuditID != "audit_small" || small.ActorType != "runtime" || small.Action != "small.action" {
		t.Fatalf("small row required = (%q,%q,%q), want unchanged", small.AuditID, small.ActorType, small.Action)
	}
	if small.CorrelationID.String != "run_small" || small.ActorID.String != "actor_small" || small.Target.String != "target_small" {
		t.Fatalf("small row optionals changed, want unchanged (bounding only affects over-bound rows)")
	}
}

// TestAuditMetadataBoundedProjectionSQLScansOnlyBoundedPrefix locks the SQL
// projection used to bound each structural metadata column. Like the payload
// projection it must inspect only the first maxAuditMetadataReadBytes+1 bytes
// via substr(...) rather than length()ing the whole column, and it must project
// ” for required columns (COALESCE) versus NULL for optional ones.
func TestAuditMetadataBoundedProjectionSQLScansOnlyBoundedPrefix(t *testing.T) {
	gotRequired := auditMetadataBoundedProjectionSQL("id", true)
	wantRequired := "COALESCE(CASE WHEN COALESCE(length(substr(CAST(id AS BLOB), 1, ?)), 0) <= ? THEN id END, '')"
	if gotRequired != wantRequired {
		t.Fatalf("required projection = %q, want %q (bounded substr prefix + COALESCE to '')", gotRequired, wantRequired)
	}
	gotOptional := auditMetadataBoundedProjectionSQL("correlation_id", false)
	wantOptional := "CASE WHEN COALESCE(length(substr(CAST(correlation_id AS BLOB), 1, ?)), 0) <= ? THEN correlation_id END"
	if gotOptional != wantOptional {
		t.Fatalf("optional projection = %q, want %q (bounded substr prefix, NULL when over-bound)", gotOptional, wantOptional)
	}
}

// TestListAuditRecordsClassifiesApprovalPrefixIndependentOfActionBound proves
// the repository derives ActionHasApprovalPrefix from the *original* action —
// only its first 9 bytes — not from the bounded Action column the same query
// projects. An approval.* action longer than the 512-byte metadata read bound
// still collapses Action to ” (so a reader gets no unbounded action bytes),
// yet ActionHasApprovalPrefix stays true so the service can still fail closed
// and drop the target (the approval JWT jti). A non-approval oversized action,
// and within-bound rows, classify honestly too.
func TestListAuditRecordsClassifiesApprovalPrefixIndependentOfActionBound(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	// Over the 512-byte read bound, so the bounded projection collapses Action
	// to '' — but the approval. prefix lives in the first 9 bytes either way.
	oversizedApproval := "approval.approved" + strings.Repeat("a", maxAuditMetadataReadBytes)
	oversizedOther := "tool.call.before" + strings.Repeat("b", maxAuditMetadataReadBytes)
	const jti = "appr_01JJTISENTINEL"

	insertAuditRow(t, database, "audit_over_appr", "run_pref", "runtime", "", oversizedApproval, jti, "", "2026-07-01T00:00:00.000000000Z")
	insertAuditRow(t, database, "audit_over_other", "run_pref", "runtime", "", oversizedOther, "call_1", "", "2026-07-01T00:00:01.000000000Z")
	insertAuditRow(t, database, "audit_small_appr", "run_pref", "runtime", "", "approval.denied", jti, "", "2026-07-01T00:00:02.000000000Z")
	insertAuditRow(t, database, "audit_small_other", "run_pref", "runtime", "", "tool.call.after", "call_2", "", "2026-07-01T00:00:03.000000000Z")

	records, err := repo.ListAuditRecords(ctx, AuditQuery{
		CorrelationID: sql.NullString{String: "run_pref", Valid: true},
		Order:         AuditOrderAscending,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}

	overAppr := records[0]
	if overAppr.Action != "" {
		t.Fatalf("oversized approval Action = %q, want empty (bounded to '')", overAppr.Action)
	}
	if !overAppr.ActionHasApprovalPrefix {
		t.Fatalf("oversized approval ActionHasApprovalPrefix = false, want true (derived from the first 9 bytes, not the emptied Action column)")
	}

	overOther := records[1]
	if overOther.Action != "" {
		t.Fatalf("oversized non-approval Action = %q, want empty", overOther.Action)
	}
	if overOther.ActionHasApprovalPrefix {
		t.Fatalf("oversized non-approval ActionHasApprovalPrefix = true, want false")
	}

	smallAppr := records[2]
	if smallAppr.Action != "approval.denied" || !smallAppr.ActionHasApprovalPrefix {
		t.Fatalf("within-bound approval = (action=%q, prefix=%v), want (approval.denied, true)", smallAppr.Action, smallAppr.ActionHasApprovalPrefix)
	}

	smallOther := records[3]
	if smallOther.Action != "tool.call.after" || smallOther.ActionHasApprovalPrefix {
		t.Fatalf("within-bound non-approval = (action=%q, prefix=%v), want (tool.call.after, false)", smallOther.Action, smallOther.ActionHasApprovalPrefix)
	}
}

// TestAuditActionApprovalPrefixSQLReadsOnlyBoundedPrefix locks the SQL that
// classifies whether a row's action begins with "approval.". It must compare
// only the first 9 bytes (the length of "approval.") of the action column —
// substr(CAST(action AS BLOB), 1, 9) — to the "approval." blob, never
// length()ing or otherwise scanning the whole action, and it must be NULL-safe
// (COALESCE to 0). This bounded classification is what lets the service keep
// dropping an approval row's jti target even after the bounded action
// projection has collapsed an oversized action to ”.
func TestAuditActionApprovalPrefixSQLReadsOnlyBoundedPrefix(t *testing.T) {
	want := `COALESCE(substr(CAST(action AS BLOB), 1, 9) = CAST('approval.' AS BLOB), 0)`
	if auditActionApprovalPrefixSQL != want {
		t.Fatalf("auditActionApprovalPrefixSQL = %q, want %q (bounded 9-byte prefix compare, NULL-safe)", auditActionApprovalPrefixSQL, want)
	}
}

func auditIDList(records []AuditRecord) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.AuditID)
	}
	return ids
}

func auditIDsEqual(records []AuditRecord, want []string) bool {
	return equalStrings(auditIDList(records), want)
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
