package attention

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	projection "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/attention"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository/repotest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

const emptyContentSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type recordingHints struct {
	mu    sync.Mutex
	count int
}

func (h *recordingHints) PublishAttentionChanged() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
}

func (h *recordingHints) published() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count
}

type fixture struct {
	t      *testing.T
	ctx    context.Context
	db     *db.DB
	repo   *repository.Repository
	server *Server
	hints  *recordingHints
	// base is an hour ago: every seeded occurrence is before the first page.
	base time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/turing.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	hints := &recordingHints{}
	return &fixture{
		t: t, ctx: context.Background(), db: database, repo: repo, server: New(repo, hints), hints: hints,
		base: time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond),
	}
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) session(id, title string) {
	f.t.Helper()
	f.exec(`INSERT INTO sessions (id, title, title_origin, status, created_at, updated_at)
		VALUES (?, ?, 'explicit', 'active', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`, id, title)
}

func (f *fixture) run(sessionID, runID, status string, at time.Time) {
	f.t.Helper()
	stamp := repository.FormatTimestamp(at)
	f.exec(`INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, 'user', 'SENTINEL_MESSAGE_CONTENT', 'text',
			(SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?), ?)`,
		"msg_"+runID, sessionID, runID, sessionID, stamp)
	var finished any
	outcome := "none"
	switch status {
	case "failed":
		finished, outcome = stamp, "internal_failure"
	case "completed":
		finished, outcome = stamp, "completed_no_content"
	}
	f.exec(`INSERT INTO agent_runs (id, session_id, user_message_id, agent_id, trace_id, status, model_provider, model_name,
			created_at, started_at, finished_at, state_version, state_updated_at, outcome_reason, assistant_content_sha256)
		VALUES (?, ?, ?, 'general_assistant', 'trace', ?, 'ollama', 'qwen2.5:7b', ?, ?, ?, 1, ?, ?, ?)`,
		runID, sessionID, "msg_"+runID, status, stamp, stamp, finished, stamp, outcome, emptyContentSHA256)
}

// chat seeds a conversation whose run waits on approvals.
func (f *fixture) chat(sessionID string) string {
	f.t.Helper()
	f.session(sessionID, "Chat "+sessionID)
	runID := "run_" + sessionID
	f.run(sessionID, runID, "waiting_approval", f.base)
	return runID
}

func (f *fixture) approval(id, runID string, at time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO approvals (id, run_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at)
		VALUES (?, ?, 'general_assistant', 'files.create', '{"path":"SENTINEL_ARGUMENT"}', 'sha256:x', 'pending', ?, ?)`,
		id, runID, repository.FormatTimestamp(time.Now().Add(time.Hour)), repository.FormatTimestamp(at))
}

func (f *fixture) consent(id, parentSession, parentRun string, at time.Time) {
	f.t.Helper()
	child := "sess_child_" + id
	f.exec(`INSERT INTO sessions (id, kind, parent_session_id, status, created_at, updated_at)
		VALUES (?, 'delegation', ?, 'active', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`, child, parentSession)
	f.run(child, "run_child_"+id, "queued", at)
	f.exec(`INSERT INTO delegations (id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, task, created_at, egress_tools_json, egress_consent)
		VALUES (?, ?, ?, ?, ?, ?, 'research', 'rev-1', 'sha256:x', 'SENTINEL_TASK', ?, '["integrations/github.list_issues"]', 'pending')`,
		id, parentSession, parentRun, "call_"+id, child, "run_child_"+id, repository.FormatTimestamp(at))
}

// failedAutomation seeds an enabled automation whose last run failed at at,
// and returns it with that run, its occurrence key.
func (f *fixture) failedAutomation(name string, at time.Time) (string, string) {
	f.t.Helper()
	automation, err := f.repo.CreateAutomation(f.ctx, repository.AutomationInput{
		Name: name, Prompt: "SENTINEL_PROMPT", Enabled: true,
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return automation.AutomationID, f.failRun(automation.AutomationID, name, at)
}

// failRun gives an automation a new failed run at at, its latest occurrence.
func (f *fixture) failRun(automationID, name string, at time.Time) string {
	f.t.Helper()
	sessionID := "sess_" + automationID
	var exists int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		f.t.Fatal(err)
	}
	if exists == 0 {
		f.session(sessionID, name)
	}
	runID := fmt.Sprintf("run_%s_%d", automationID, at.UnixNano())
	f.run(sessionID, runID, "failed", at)
	f.exec(`UPDATE agent_runs SET error_code = 'automation_tool_not_allowlisted' WHERE id = ?`, runID)
	f.exec(`UPDATE automations SET last_run_id = ?, last_run_at = ?, session_id = ? WHERE id = ?`,
		runID, repository.FormatTimestamp(at), sessionID, automationID)
	return runID
}

func (f *fixture) deletion(sessionID string, began time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO session_deletions (session_id, lifecycle_version, state, quiesce_deadline_at, terminal_sequence,
			retryable, error_code) VALUES (?, 1, 'failed_external', ?, 1, 1, 'artifact_cleanup_pending')`,
		sessionID, repository.FormatTimestamp(began.Add(5*time.Minute)))
}

func (f *fixture) list(cursor string, limit int32) *turingv1.ListAttentionResponse {
	f.t.Helper()
	response, err := f.server.ListAttention(f.ctx, &turingv1.ListAttentionRequest{
		Page: &turingv1.PageRequest{Cursor: cursor, Limit: limit},
	})
	if err != nil {
		f.t.Fatalf("ListAttention: %v", err)
	}
	return response
}

// pass reads every page from a fresh first page, calling between after each
// page that has a next one.
func (f *fixture) pass(limit int32, between func(page int)) [][]*turingv1.AttentionRow {
	f.t.Helper()
	pageLimit := int(limit)
	if pageLimit == 0 {
		pageLimit = 100
	}
	var pages [][]*turingv1.AttentionRow
	cursor := ""
	for {
		response := f.list(cursor, limit)
		if len(response.GetRows()) > pageLimit {
			f.t.Fatalf("page %d holds %d rows, over its limit %d", len(pages)+1, len(response.GetRows()), pageLimit)
		}
		pages = append(pages, response.GetRows())
		cursor = response.GetPage().GetNextCursor()
		if cursor == "" {
			return pages
		}
		if len(response.GetRows()) != pageLimit {
			f.t.Fatalf("page %d holds %d rows with a next page, want a full page", len(pages), len(response.GetRows()))
		}
		if between != nil {
			between(len(pages))
		}
		if len(pages) > 1000 {
			f.t.Fatal("paging did not end")
		}
	}
}

func rowKey(row *turingv1.AttentionRow) string {
	switch row.GetKind() {
	case turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_APPROVAL:
		return row.GetApprovalId()
	case turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_DELEGATION:
		return row.GetDelegationId()
	case turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_AUTOMATION:
		return row.GetAutomationId()
	default:
		return row.GetSessionId()
	}
}

func keys(rows []*turingv1.AttentionRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, rowKey(row))
	}
	return out
}

func flatten(pages [][]*turingv1.AttentionRow) []*turingv1.AttentionRow {
	var rows []*turingv1.AttentionRow
	for _, page := range pages {
		rows = append(rows, page...)
	}
	return rows
}

// assertTotalOrder checks rows follow the queue's order: stage, since
// descending, kind, id.
func assertTotalOrder(t *testing.T, rows []*turingv1.AttentionRow) {
	t.Helper()
	for index := 1; index < len(rows); index++ {
		a, b := rows[index-1], rows[index]
		ka := []int64{int64(a.GetStage()), -a.GetSince().AsTime().UnixNano(), int64(a.GetKind())}
		kb := []int64{int64(b.GetStage()), -b.GetSince().AsTime().UnixNano(), int64(b.GetKind())}
		if c := slices.Compare(ka, kb); c > 0 || c == 0 && rowKey(a) >= rowKey(b) {
			t.Fatalf("row %d (%s) sorts before row %d (%s)", index, rowKey(b), index-1, rowKey(a))
		}
	}
}

func assertNoDuplicates(t *testing.T, rows []*turingv1.AttentionRow) {
	t.Helper()
	seen := map[string]bool{}
	for _, row := range rows {
		key := row.GetKind().String() + "/" + rowKey(row)
		if seen[key] {
			t.Fatalf("row %s shown twice in one pass", key)
		}
		seen[key] = true
	}
}

// A page holds at most its limit, a next page is reported exactly when one
// exists, and a pass visits every row once, in order, across every stage.
func TestPagingVisitsEveryRowExactlyOnceInOrder(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	var want []string
	for index := range 120 {
		id := fmt.Sprintf("appr_%03d", index)
		f.approval(id, runID, f.base.Add(-time.Duration(index)*time.Second))
		want = append(want, id)
	}
	for index := range 40 {
		id := fmt.Sprintf("dlg_%03d", index)
		f.consent(id, "sess_a", runID, f.base.Add(-time.Duration(index)*time.Second))
		want = append(want, id)
	}
	for index := range 20 {
		automationID, _ := f.failedAutomation(fmt.Sprintf("Auto %03d", index), f.base.Add(-time.Duration(index)*time.Second))
		want = append(want, automationID)
	}
	for index := range 10 {
		id := fmt.Sprintf("sess_gone_%03d", index)
		f.deletion(id, f.base.Add(-time.Duration(index)*time.Second))
		want = append(want, id)
	}

	pages := f.pass(0, nil)
	if len(pages) != 2 || len(pages[0]) != 100 || len(pages[1]) != 90 {
		t.Fatalf("pages = %d, want 100 rows then 90", len(pages))
	}
	rows := flatten(pages)
	assertNoDuplicates(t, rows)
	assertTotalOrder(t, rows)
	if got := keys(rows); !slices.Equal(got, want) {
		t.Fatalf("pass = %v\nwant %v", got, want)
	}

	if got := keys(flatten(f.pass(7, nil))); !slices.Equal(got, want) {
		t.Fatalf("pass at 7 a page = %v\nwant %v", got, want)
	}
}

// Two rows of one source with the same since are ordered by source id, and a
// page boundary between them neither repeats nor skips one.
func TestEqualSinceRowsAreOrderedBySourceID(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	for _, id := range []string{"appr_c", "appr_a", "appr_b"} {
		f.approval(id, runID, f.base)
	}
	automationB, _ := f.failedAutomation("B", f.base)
	automationA, _ := f.failedAutomation("A", f.base)
	automations := []string{automationA, automationB}
	slices.Sort(automations)
	want := append([]string{"appr_a", "appr_b", "appr_c"}, automations...)
	for _, limit := range []int32{1, 2, 100} {
		if got := keys(flatten(f.pass(limit, nil))); !slices.Equal(got, want) {
			t.Fatalf("pass at %d a page = %v, want %v", limit, got, want)
		}
	}
}

// 250 rows; between pages a new approval is inserted, a listed approval and
// an unlisted one resolved, and a listed automation row and an unlisted one
// dismissed. The pass shows no row twice, skips nothing that qualified at
// the watermark and still qualifies, and leaves the new approval to the next
// first page.
func TestAPassIsStableUnderChangesBetweenPages(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	var approvals, automations []string
	keysOf := map[string]string{}
	for index := range 150 {
		id := fmt.Sprintf("appr_%03d", index)
		f.approval(id, runID, f.base.Add(-time.Duration(index)*time.Second))
		approvals = append(approvals, id)
	}
	for index := range 40 {
		f.consent(fmt.Sprintf("dlg_%03d", index), "sess_a", runID, f.base.Add(-time.Duration(index)*time.Second))
	}
	for index := range 40 {
		id, key := f.failedAutomation(fmt.Sprintf("Auto %03d", index), f.base.Add(-time.Duration(index)*time.Second))
		automations = append(automations, id)
		keysOf[id] = key
	}
	for index := range 20 {
		f.deletion(fmt.Sprintf("sess_gone_%03d", index), f.base.Add(-time.Duration(index)*time.Second))
	}
	before := keys(flatten(f.pass(0, nil)))
	if len(before) != 250 {
		t.Fatalf("rows = %d, want 250", len(before))
	}

	removed := map[string]bool{}
	pages := f.pass(0, func(page int) {
		switch page {
		case 1:
			// Both are after the watermark. The approval sorts before the
			// cursor; the consent is in a later stage, which only the
			// watermark keeps out of this pass.
			f.approval("appr_new", runID, time.Now().Add(time.Millisecond))
			f.consent("dlg_new", "sess_a", runID, time.Now().Add(time.Millisecond))
			f.exec(`UPDATE approvals SET status = 'approved' WHERE id IN (?, ?)`, approvals[3], approvals[140])
			removed[approvals[140]] = true
		case 2:
			// Page 2 ends inside stage 3, after the first eleven automations.
			for _, id := range []string{automations[2], automations[30]} {
				if _, err := f.server.DismissAttention(f.ctx, &turingv1.DismissAttentionRequest{
					AutomationId: id, OccurrenceKey: keysOf[id],
				}); err != nil {
					t.Fatal(err)
				}
			}
			removed[automations[30]] = true
		}
	})
	if len(pages) != 3 {
		t.Fatalf("pages = %d, want 3", len(pages))
	}
	rows := flatten(pages)
	assertNoDuplicates(t, rows)
	assertTotalOrder(t, rows)
	var want []string
	for _, key := range before {
		if !removed[key] {
			want = append(want, key)
		}
	}
	if got := keys(rows); !slices.Equal(got, want) {
		t.Fatalf("pass = %v\nwant %v", got, want)
	}
	if !slices.Contains(keys(pages[1]), automations[2]) || slices.Contains(keys(pages[1]), automations[30]) {
		t.Fatalf("page 2 = %v, want %s listed and %s not yet", keys(pages[1]), automations[2], automations[30])
	}
	if slices.Contains(keys(rows), "dlg_new") {
		t.Fatal("a consent whose since is after the watermark was shown mid-pass")
	}
	next := keys(flatten(f.pass(0, nil)))
	if next[0] != "appr_new" || !slices.Contains(next, "dlg_new") {
		t.Fatalf("next pass = %v, want the new approval first and the new consent listed", next)
	}
}

// A late commit (created_at taken before BeginTx, as CreateApprovalWithEvents
// does) at or before the watermark shows on a later page when it sorts after
// the cursor, and on no page of the pass when it sorts before; never twice.
func TestALateCommitBeforeTheWatermarkIsShownAtMostOnce(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	for index := range 6 {
		f.approval(fmt.Sprintf("appr_%d", index), runID, f.base.Add(-time.Duration(index)*time.Minute))
	}
	pages := f.pass(2, func(page int) {
		if page == 1 {
			// Page 1 showed appr_0 and appr_1. One late commit sorts before
			// that cursor, one after it.
			f.approval("appr_late_before", runID, f.base.Add(-30*time.Second))
			f.approval("appr_late_after", runID, f.base.Add(-150*time.Second))
		}
	})
	want := []string{"appr_0", "appr_1", "appr_2", "appr_late_after", "appr_3", "appr_4", "appr_5"}
	if got := keys(flatten(pages)); !slices.Equal(got, want) {
		t.Fatalf("pass = %v, want %v", got, want)
	}
	if first := keys(f.list("", 0).GetRows()); !slices.Contains(first, "appr_late_before") {
		t.Fatalf("next first page = %v, want the late commit listed", first)
	}
}

// With 101 approvals older than a pending consent and a blocked automation,
// page 1 ends inside stage 1 and page 2 shows the 101st approval and then the
// consent and the automation, though each is newer than the cursor.
func TestALaterStageIsReadWhateverItsSince(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	for index := range 101 {
		f.approval(fmt.Sprintf("appr_%03d", index), runID, f.base.Add(-time.Hour-time.Duration(index)*time.Second))
	}
	f.consent("dlg_new", "sess_a", runID, f.base.Add(time.Minute))
	automationID, _ := f.failedAutomation("Nightly", f.base.Add(2*time.Minute))

	first := f.list("", 0)
	if len(first.GetRows()) != 100 || first.GetPage().GetNextCursor() == "" {
		t.Fatalf("page 1 = %d rows, next %q; want 100 and a next page", len(first.GetRows()), first.GetPage().GetNextCursor())
	}
	if last := first.GetRows()[99]; last.GetStage() != turingv1.AttentionStage_ATTENTION_STAGE_PERSON_APPROVAL {
		t.Fatalf("page 1 ends at %s, want inside stage 1", last.GetStage())
	}
	second := f.list(first.GetPage().GetNextCursor(), 0)
	if got := keys(second.GetRows()); !slices.Equal(got, []string{"appr_100", "dlg_new", automationID}) {
		t.Fatalf("page 2 = %v, want the 101st approval, then the consent and the automation", got)
	}
	if second.GetPage().GetNextCursor() != "" {
		t.Fatal("page 2 reported a next page")
	}
}

// A token that does not decode, or names an unknown stage or kind, is
// refused; so is a limit outside 1 to 100.
func TestAMalformedPageIsRefused(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	f.approval("appr_a", runID, f.base)
	f.approval("appr_b", runID, f.base.Add(-time.Second))
	valid := f.list("", 1).GetPage().GetNextCursor()
	if valid == "" {
		t.Fatal("no cursor to tamper with")
	}
	encode := func(body string) string { return base64.RawURLEncoding.EncodeToString([]byte(body)) }
	for name, cursor := range map[string]string{
		"not base64":         "!!!",
		"not JSON":           encode("nope"),
		"trailing data":      encode(`{"v":1,"w":5,"r":1,"s":1,"k":1,"i":"appr_a"}{}`),
		"an unknown field":   encode(`{"v":1,"w":5,"r":1,"s":1,"k":1,"i":"appr_a","x":1}`),
		"another version":    encode(`{"v":2,"w":5,"r":1,"s":1,"k":1,"i":"appr_a"}`),
		"an unknown kind":    encode(`{"v":1,"w":5,"r":1,"s":1,"k":7,"i":"appr_a"}`),
		"an unknown stage":   encode(`{"v":1,"w":5,"r":6,"s":1,"k":1,"i":"appr_a"}`),
		"a mismatched stage": encode(`{"v":1,"w":5,"r":2,"s":1,"k":1,"i":"appr_a"}`),
		"no id":              encode(`{"v":1,"w":5,"r":1,"s":1,"k":1,"i":""}`),
		"no watermark":       encode(`{"v":1,"r":1,"s":1,"k":1,"i":"appr_a"}`),
		"over long":          valid + strings.Repeat("A", 1024),
	} {
		_, err := f.server.ListAttention(f.ctx, &turingv1.ListAttentionRequest{Page: &turingv1.PageRequest{Cursor: cursor}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: err = %v, want InvalidArgument", name, err)
		}
	}
	for _, limit := range []int32{-1, 101} {
		_, err := f.server.ListAttention(f.ctx, &turingv1.ListAttentionRequest{Page: &turingv1.PageRequest{Limit: limit}})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("limit %d: err = %v, want InvalidArgument", limit, err)
		}
	}
	if _, err := f.server.ListAttention(f.ctx, &turingv1.ListAttentionRequest{
		Page: &turingv1.PageRequest{Cursor: valid},
	}); err != nil {
		t.Fatalf("the untampered cursor: %v", err)
	}
}

// presentRowFields is the closed set of string fields a row sets, so the test
// can assert exactly which ones each kind carries.
func presentRowFields(row *turingv1.AttentionRow) []string {
	var set []string
	for name, value := range map[string]string{
		"session_id": row.GetSessionId(), "session_title": row.GetSessionTitle(), "approval_id": row.GetApprovalId(),
		"run_id": row.GetRunId(), "delegation_id": row.GetDelegationId(), "automation_id": row.GetAutomationId(),
		"automation_name": row.GetAutomationName(), "occurrence_key": row.GetOccurrenceKey(), "cause": row.GetCause(),
	} {
		if value != "" {
			set = append(set, name)
		}
	}
	slices.Sort(set)
	return set
}

// The response holds no tool arguments, message content, task text, prompt
// or audit payload beyond its closed code: each kind sets exactly its
// allowlisted fields.
func TestTheResponseCarriesOnlyAllowlistedFields(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	f.approval("appr_a", runID, f.base)
	f.consent("dlg_a", "sess_a", runID, f.base)
	f.failedAutomation("Nightly", f.base)
	remote, err := f.repo.CreateAutomation(f.ctx, repository.AutomationInput{
		Name: "Remote", Prompt: "SENTINEL_PROMPT", Enabled: true,
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO audit_logs (id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES ('audit_block', 'automation', ?, 'automation.remote_egress_blocked', ?,
			'{"code":"remote_egress_requires_interactive_consent","endpointHost":"SENTINEL_HOST","prompt":"SENTINEL_PROMPT"}', ?)`,
		remote.AutomationID, remote.AutomationID, repository.FormatTimestamp(f.base))
	f.deletion("sess_gone", f.base)

	response := f.list("", 0)
	encoded, err := protojson.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "SENTINEL") {
		t.Fatalf("response carries content: %s", encoded)
	}
	want := map[turingv1.AttentionSourceKind][][]string{
		turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_APPROVAL:   {{"approval_id", "run_id", "session_id", "session_title"}},
		turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_DELEGATION: {{"delegation_id", "run_id", "session_id", "session_title"}},
		turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_AUTOMATION: {
			{"automation_id", "automation_name", "cause", "occurrence_key", "run_id", "session_id", "session_title"},
			{"automation_id", "automation_name", "cause", "occurrence_key"},
		},
		turingv1.AttentionSourceKind_ATTENTION_SOURCE_KIND_DELETION: {{"cause", "session_id"}},
	}
	if len(response.GetRows()) != 5 {
		t.Fatalf("rows = %d, want 5", len(response.GetRows()))
	}
	for _, row := range response.GetRows() {
		got := presentRowFields(row)
		if !slices.ContainsFunc(want[row.GetKind()], func(fields []string) bool { return slices.Equal(fields, got) }) {
			t.Fatalf("%s row sets %v, want one of %v", row.GetKind(), got, want[row.GetKind()])
		}
		if row.GetSince() == nil || row.GetStage() == turingv1.AttentionStage_ATTENTION_STAGE_UNSPECIFIED {
			t.Fatalf("%s row has no stage or since", row.GetKind())
		}
	}
}

// A stored code that is not a closed code is left out rather than passed on.
func TestACauseThatIsNotAClosedCodeIsLeftOut(t *testing.T) {
	f := newFixture(t)
	_, runID := f.failedAutomation("Nightly", f.base)
	f.exec(`UPDATE agent_runs SET error_code = 'Provider said: SENTINEL message' WHERE id = ?`, runID)
	rows := f.list("", 0).GetRows()
	if len(rows) != 1 || rows[0].GetCause() != "" {
		t.Fatalf("rows = %+v, want one row with no cause", rows)
	}
}

// A row the projection cannot classify is listed as unavailable in its
// source's place, never dropped.
func TestAnUnclassifiableRowIsListedAsUnavailable(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	f.approval("appr_ok", runID, f.base)
	f.approval("appr_bad", runID, f.base.Add(-time.Minute))
	f.exec(`UPDATE approvals SET expires_at = 'garbage' WHERE id = 'appr_bad'`)
	rows := f.list("", 0).GetRows()
	if len(rows) != 2 || rows[0].GetStage() != turingv1.AttentionStage_ATTENTION_STAGE_PERSON_APPROVAL ||
		rows[1].GetStage() != turingv1.AttentionStage_ATTENTION_STAGE_UNAVAILABLE || rows[1].GetApprovalId() != "appr_bad" {
		t.Fatalf("rows = %+v, want the readable approval then the unreadable one as unavailable", rows)
	}
}

// Dismissing a failing automation hides its row and lowers the badge (the
// first page's count) by one; its next failing run raises it again. A
// dismissal hints the change.
func TestDismissalLowersTheBadgeUntilTheNextFailure(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	f.approval("appr_a", runID, f.base)
	automationID, _ := f.failedAutomation("Nightly", f.base)
	f.failedAutomation("Other", f.base)
	first := f.list("", 0).GetRows()
	if len(first) != 3 {
		t.Fatalf("badge = %d, want 3", len(first))
	}
	var key string
	for _, row := range first {
		if row.GetAutomationId() == automationID {
			key = row.GetOccurrenceKey()
		}
	}
	if key == "" {
		t.Fatal("the automation row carries no occurrence key")
	}
	if _, err := f.server.DismissAttention(f.ctx, &turingv1.DismissAttentionRequest{AutomationId: automationID, OccurrenceKey: key}); err != nil {
		t.Fatal(err)
	}
	if f.hints.published() != 1 {
		t.Fatalf("hints = %d, want 1", f.hints.published())
	}
	after := f.list("", 0).GetRows()
	if len(after) != 2 || slices.ContainsFunc(after, func(row *turingv1.AttentionRow) bool { return row.GetAutomationId() == automationID }) {
		t.Fatalf("rows after dismissal = %v, want two without %s", keys(after), automationID)
	}
	f.failRun(automationID, "Nightly", f.base.Add(time.Minute))
	if again := f.list("", 0).GetRows(); len(again) != 3 {
		t.Fatalf("badge after the next failure = %d, want 3", len(again))
	}
}

// A stale or malformed dismissal is refused and hints nothing; an id that is
// not an automation's, an approval's for instance, is not found.
func TestDismissalRefusals(t *testing.T) {
	f := newFixture(t)
	runID := f.chat("sess_a")
	f.approval("appr_a", runID, f.base)
	automationID, key := f.failedAutomation("Nightly", f.base)
	newer := f.failRun(automationID, "Nightly", f.base.Add(time.Minute))
	for _, test := range []struct {
		name         string
		automationID string
		key          string
		want         codes.Code
	}{
		{name: "a stale key", automationID: automationID, key: key, want: codes.FailedPrecondition},
		{name: "an approval", automationID: "appr_a", key: "appr_a", want: codes.NotFound},
		{name: "a deletion", automationID: "sess_gone", key: "sess_gone", want: codes.NotFound},
		{name: "no automation", key: newer, want: codes.InvalidArgument},
		{name: "no key", automationID: automationID, want: codes.InvalidArgument},
		{name: "an over-long key", automationID: automationID, key: strings.Repeat("k", 257), want: codes.InvalidArgument},
	} {
		_, err := f.server.DismissAttention(f.ctx, &turingv1.DismissAttentionRequest{AutomationId: test.automationID, OccurrenceKey: test.key})
		if status.Code(err) != test.want {
			t.Fatalf("%s: err = %v, want %s", test.name, err, test.want)
		}
	}
	if f.hints.published() != 0 {
		t.Fatalf("hints = %d, want none for refused dismissals", f.hints.published())
	}
	if rows := f.list("", 0).GetRows(); len(rows) != 2 || rows[1].GetOccurrenceKey() != newer {
		t.Fatalf("rows = %+v, want the newer occurrence still listed", rows)
	}
}

// Deletion between a list and the next read is handled: a row whose
// conversation was deleted is not returned by a later page, and dismissing
// an automation deleted after it was listed is not found.
func TestDeletionBetweenListAndActIsHandled(t *testing.T) {
	f := newFixture(t)
	runA := f.chat("sess_a")
	// A conversation made the way a person makes one, so its deletion runs the
	// real withdrawal.
	sessionB, err := f.repo.CreateSession(f.ctx, "Going away")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := f.repo.EnqueueUserMessage(f.ctx, repository.EnqueueUserMessageInput{
		SessionID: sessionB.SessionID, Content: "x", AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.approval("appr_a", runA, f.base)
	f.approval("appr_b", enqueued.RunID, f.base.Add(-time.Minute))
	automationID, key := f.failedAutomation("Nightly", f.base)
	first := f.list("", 1)
	if got := keys(first.GetRows()); !slices.Equal(got, []string{"appr_a"}) {
		t.Fatalf("page 1 = %v", got)
	}
	if err := repotest.DeleteSession(f.ctx, f.repo, sessionB.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.DeleteAutomation(f.ctx, automationID); err != nil {
		t.Fatal(err)
	}
	if got := keys(f.list(first.GetPage().GetNextCursor(), 0).GetRows()); len(got) != 0 {
		t.Fatalf("page 2 = %v, want nothing left", got)
	}
	_, err = f.server.DismissAttention(f.ctx, &turingv1.DismissAttentionRequest{AutomationId: automationID, OccurrenceKey: key})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("dismissing a deleted automation = %v, want NotFound", err)
	}
}

// A repository failure is reported without detail.
func TestARepositoryFailureIsInternal(t *testing.T) {
	f := newFixture(t)
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := f.server.ListAttention(f.ctx, &turingv1.ListAttentionRequest{})
	if status.Code(err) != codes.Internal || errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("err = %v, want a bare Internal", err)
	}
	_, err = f.server.DismissAttention(f.ctx, &turingv1.DismissAttentionRequest{AutomationId: "auto_x", OccurrenceKey: "run_x"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("dismiss err = %v, want Internal", err)
	}
}

// Every stage of the pure table has its wire value.
func TestStagesMapOntoTheWire(t *testing.T) {
	for stage, want := range map[projection.StageID]turingv1.AttentionStage{
		projection.PersonApproval: turingv1.AttentionStage_ATTENTION_STAGE_PERSON_APPROVAL,
		projection.PersonConsent:  turingv1.AttentionStage_ATTENTION_STAGE_PERSON_CONSENT,
		projection.PersonBlocked:  turingv1.AttentionStage_ATTENTION_STAGE_PERSON_BLOCKED,
		projection.PersonDeletion: turingv1.AttentionStage_ATTENTION_STAGE_PERSON_DELETION,
		projection.PersonProposal: turingv1.AttentionStage_ATTENTION_STAGE_PERSON_PROPOSAL,
		projection.Unavailable:    turingv1.AttentionStage_ATTENTION_STAGE_UNAVAILABLE,
	} {
		if got := wireStage(stage); got != want {
			t.Fatalf("wireStage(%s) = %s, want %s", stage, got, want)
		}
	}
}
