package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
)

func TestListSessionsPageFiltersAndOrders(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertListSession(t, ctx, database, "active-newest", "active", "2026-08-20T04:00:05.000000000Z")
	insertListSession(t, ctx, database, "same-z", "active", "2026-08-20T04:00:04.000000000Z")
	insertListSession(t, ctx, database, "same-a", "active", "2026-08-20T04:00:04.000000000Z")
	insertListSession(t, ctx, database, "active-oldest", "active", "2026-08-20T04:00:03.000000000Z")
	insertListSession(t, ctx, database, "archived-newest", "archived", "2026-08-20T04:00:06.000000000Z")

	testCases := []struct {
		name   string
		filter SessionListFilter
		want   []string
	}{
		{
			name:   "active",
			filter: SessionListActive,
			want:   []string{"active-newest", "same-z", "same-a", "active-oldest"},
		},
		{
			name:   "archived",
			filter: SessionListArchived,
			want:   []string{"archived-newest"},
		},
		{
			name:   "all",
			filter: SessionListAll,
			want:   []string{"archived-newest", "active-newest", "same-z", "same-a", "active-oldest"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			sessions, err := repo.ListSessionsPage(ctx, ListSessionsInput{
				Filter: testCase.filter,
				Limit:  10,
			})
			if err != nil {
				t.Fatal(err)
			}
			assertSessionIDs(t, sessions, testCase.want)
		})
	}
}

func TestListSessionsPageAndGetExcludeDeletingSessions(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	active, err := repo.CreateSession(ctx, "Delete active")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := repo.CreateSession(ctx, "Delete archived")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ArchiveSession(ctx, archived.SessionID); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{active.SessionID, archived.SessionID} {
		if _, err := repo.BeginSessionDeletion(ctx, sessionID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.GetSession(ctx, sessionID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("GetSession(%q) error = %v, want sql.ErrNoRows", sessionID, err)
		}
	}

	for _, filter := range []SessionListFilter{
		SessionListActive,
		SessionListArchived,
		SessionListAll,
	} {
		sessions, err := repo.ListSessionsPage(ctx, ListSessionsInput{
			Filter: filter,
			Limit:  10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 0 {
			t.Fatalf("filter %q returned deleting sessions: %+v", filter, sessions)
		}
	}
}

func TestListSessionsPageUsesStableKeysetDuringConcurrentInsert(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertListSession(t, ctx, database, "older-3", "active", "2026-08-20T04:00:03.000000000Z")
	insertListSession(t, ctx, database, "older-2", "active", "2026-08-20T04:00:02.000000000Z")
	insertListSession(t, ctx, database, "older-1", "active", "2026-08-20T04:00:01.000000000Z")

	first, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListActive, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, first, []string{"older-3", "older-2"})

	insertListSession(t, ctx, database, "inserted-newest", "active", "2026-08-20T04:00:04.000000000Z")
	second, err := repo.ListSessionsPage(ctx, ListSessionsInput{
		Filter: SessionListActive,
		After: &SessionCursor{
			UpdatedAt: first[len(first)-1].UpdatedAt,
			SessionID: first[len(first)-1].SessionID,
		},
		Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, second, []string{"older-1"})
}

func TestListSessionsPageRejectsUnsupportedPersistedStatus(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints = ON`); err != nil {
		t.Fatal(err)
	}
	insertListSession(t, ctx, database, "internal-status", "future_internal", "2026-08-20T04:00:00.000000000Z")
	if _, err := database.ExecContext(ctx, `PRAGMA ignore_check_constraints = OFF`); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListAll, Limit: 10})
	if !errors.Is(err, ErrInvalidSessionStatus) {
		t.Fatalf("ListSessionsPage error = %v, want ErrInvalidSessionStatus", err)
	}
}

func TestListSessionsPageRejectsMalformedPersistedTimestamp(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, status, created_at, updated_at)
		VALUES ('malformed-time', 'active', 'not-a-time', '2026-08-20T04:00:00.000000000Z')`,
	); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ListSessionsPage(ctx, ListSessionsInput{Filter: SessionListActive, Limit: 10})
	if !errors.Is(err, ErrInvalidSessionTimestamp) {
		t.Fatalf("ListSessionsPage error = %v, want ErrInvalidSessionTimestamp", err)
	}
}

func TestSessionQueryPlansUseLifecycleIndexes(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	for i := 0; i < 500; i++ {
		status := "active"
		if i%3 == 0 {
			status = "archived"
		}
		insertListSession(
			t,
			ctx,
			database,
			fmt.Sprintf("query-plan-%03d", i),
			status,
			fmt.Sprintf("2026-08-20T04:%02d:%02d.000000000Z", (i/60)%60, i%60),
		)
	}

	testCases := []struct {
		name      string
		input     ListSessionsInput
		wantIndex string
	}{
		{
			name: "filtered",
			input: ListSessionsInput{
				Filter: SessionListActive,
				After: &SessionCursor{
					UpdatedAt: "2026-08-20T04:04:00.000000000Z",
					SessionID: "query-plan-240",
				},
				Limit: 50,
			},
			wantIndex: "idx_sessions_status_updated",
		},
		{
			name: "all",
			input: ListSessionsInput{
				Filter: SessionListAll,
				After: &SessionCursor{
					UpdatedAt: "2026-08-20T04:04:00.000000000Z",
					SessionID: "query-plan-240",
				},
				Limit: 50,
			},
			wantIndex: "idx_sessions_updated",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			query, args, err := listSessionsQuery(testCase.input)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := database.QueryContext(ctx, "EXPLAIN QUERY PLAN\n"+query, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rows.Close() }()
			var details []string
			for rows.Next() {
				var id, parent, notUsed int
				var detail string
				if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			wantSearch := "SEARCH sessions USING INDEX " + testCase.wantIndex
			if !strings.Contains(plan, wantSearch) {
				t.Fatalf("query plan = %q, want keyset search %q", plan, wantSearch)
			}
			if strings.Contains(plan, "SCAN sessions") {
				t.Fatalf("query plan performs a sessions scan: %q", plan)
			}
		})
	}
}

func TestSearchMessagesRanksAndScopes(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchSession(t, ctx, database, "s2")
	insertSearchMessage(t, ctx, database, "m-rank-high", "s1", "rankterm rankterm rankterm", 1)
	insertSearchMessage(t, ctx, database, "m-rank-s1", "s1", "rankterm", 2)
	insertSearchMessage(t, ctx, database, "m-rank-s2", "s2", "rankterm", 1)
	insertSearchMessage(t, ctx, database, "m-not-a-match", "s2", "unrelated", 2)
	if _, err := database.ExecContext(ctx, `UPDATE messages SET run_id = 'run-rank-high' WHERE id = 'm-rank-high'`); err != nil {
		t.Fatalf("set message run_id: %v", err)
	}

	global, err := repo.SearchMessages(ctx, "", "", "rankterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages global: %v", err)
	}
	assertSearchMessageIDs(t, global, []string{"m-rank-high", "m-rank-s1", "m-rank-s2"})
	if got := []string{global[0].SessionID, global[1].SessionID, global[2].SessionID}; !reflect.DeepEqual(got, []string{"s1", "s1", "s2"}) {
		t.Fatalf("global session IDs = %v, want [s1 s1 s2]", got)
	}
	if global[0].RunID != "run-rank-high" {
		t.Fatalf("global run ID = %q, want run-rank-high", global[0].RunID)
	}

	scoped, err := repo.SearchMessages(ctx, "s2", "", "rankterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages scoped: %v", err)
	}
	assertSearchMessageIDs(t, scoped, []string{"m-rank-s2"})
	if scoped[0].SessionID != "s2" {
		t.Fatalf("scoped SessionID = %q, want s2", scoped[0].SessionID)
	}

	excluded, err := repo.SearchMessages(ctx, "", "s1", "rankterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages excluded: %v", err)
	}
	assertSearchMessageIDs(t, excluded, []string{"m-rank-s2"})
}

func TestSearchMessagesLimitsAndNoMatches(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	for i := 1; i <= 21; i++ {
		insertSearchMessage(t, ctx, database, fmt.Sprintf("m-limit-%02d", i), "s1", "limitterm", int64(i))
	}

	valid, err := repo.SearchMessages(ctx, "", "", "limitterm", 2)
	if err != nil {
		t.Fatalf("SearchMessages valid limit: %v", err)
	}
	assertSearchMessageIDs(t, valid, []string{"m-limit-01", "m-limit-02"})

	for _, limit := range []int{0, -1, 101} {
		results, err := repo.SearchMessages(ctx, "", "", "limitterm", limit)
		if err != nil {
			t.Fatalf("SearchMessages limit %d: %v", limit, err)
		}
		assertSearchMessageIDs(t, results, []string{
			"m-limit-01", "m-limit-02", "m-limit-03", "m-limit-04", "m-limit-05",
			"m-limit-06", "m-limit-07", "m-limit-08", "m-limit-09", "m-limit-10",
			"m-limit-11", "m-limit-12", "m-limit-13", "m-limit-14", "m-limit-15",
			"m-limit-16", "m-limit-17", "m-limit-18", "m-limit-19", "m-limit-20",
		})
	}

	noMatch, err := repo.SearchMessages(ctx, "", "", "missingterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages no match: %v", err)
	}
	assertSearchMessageIDs(t, noMatch, []string{})
}

func TestSearchMessagesTreatsInputAsLiteralPhrase(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-literal", "s1", "operatorword OR broadword", 1)
	insertSearchMessage(t, ctx, database, "m-operator-only", "s1", "operatorword", 2)
	insertSearchMessage(t, ctx, database, "m-broad-only", "s1", "broadword", 3)
	insertSearchMessage(t, ctx, database, "m-malformed", "s1", "\"unterminated", 4)

	operatorLooking, err := repo.SearchMessages(ctx, "", "", "operatorword OR broadword", 10)
	if err != nil {
		t.Fatalf("SearchMessages operator-looking query: %v", err)
	}
	assertSearchMessageIDs(t, operatorLooking, []string{"m-literal"})

	malformed, err := repo.SearchMessages(ctx, "", "", "\"unterminated", 10)
	if err != nil {
		t.Fatalf("SearchMessages malformed query: %v", err)
	}
	assertSearchMessageIDs(t, malformed, []string{"m-malformed"})
}

func TestSearchMessagesTreatsNULAsLiteralDelimiter(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-nul-phrase", "s1", "alpha beta", 1)
	insertSearchMessage(t, ctx, database, "m-nul-alpha", "s1", "alpha", 2)
	insertSearchMessage(t, ctx, database, "m-nul-beta", "s1", "beta", 3)

	results, err := repo.SearchMessages(ctx, "", "", "alpha\x00beta", 10)
	if err != nil {
		t.Fatalf("SearchMessages NUL query: %v", err)
	}
	assertSearchMessageIDs(t, results, []string{"m-nul-phrase"})
}

func TestSearchMessagesNULOnlyInputReturnsNoMatches(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-nul-content", "s1", "anything", 1)

	results, err := repo.SearchMessages(ctx, "", "", "\x00", 10)
	if err != nil {
		t.Fatalf("SearchMessages NUL-only query: %v", err)
	}
	assertSearchMessageIDs(t, results, []string{})
}

func TestSearchMessagesPunctuationOnlyInputReturnsNoMatches(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-punctuation", "s1", "ordinary searchable content", 1)

	for _, query := range []string{"...", "!!!", "🤖"} {
		results, err := repo.SearchMessages(ctx, "", "", query, 10)
		if err != nil {
			t.Fatalf("SearchMessages punctuation-only query %q: %v", query, err)
		}
		assertSearchMessageIDs(t, results, []string{})
	}
}

func TestHasFTS5Token(t *testing.T) {
	for _, test := range []struct {
		query string
		want  bool
	}{
		{query: "...", want: false},
		{query: "🤖", want: false},
		{query: "\u0301", want: false},
		{query: "letter", want: true},
		{query: "123", want: true},
		{query: "東京", want: true},
		{query: "\ue000", want: true},
	} {
		if got := hasFTS5Token(test.query); got != test.want {
			t.Errorf("hasFTS5Token(%q) = %v, want %v", test.query, got, test.want)
		}
	}
}

func TestSearchMessagesReturnsQueryErrors(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.SearchMessages(ctx, "", "", "anything", 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SearchMessages error = %v, want context canceled", err)
	}
}

// TestSearchMessagesIncludeArchivedAndExcludeDeletingSessions pins the legacy
// projection's lifecycle visibility: archived conversations stay searchable and
// a deleting session's messages do not. The shared predicate's explicit status
// clause is redundant against today's schema, so it is pinned by the fragment
// and DDL-domain tests rather than by this behavioral one.
func TestSearchMessagesIncludeArchivedAndExcludeDeletingSessions(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertListSession(t, ctx, database, "s-active", "active", "2026-08-20T04:00:00.000000000Z")
	insertListSession(t, ctx, database, "s-archived", "archived", "2026-08-20T04:00:01.000000000Z")
	insertListSession(t, ctx, database, "s-deleting", "active", "2026-08-20T04:00:02.000000000Z")
	insertSearchMessage(t, ctx, database, "m-visible-1-active", "s-active", "visibilityterm", 1)
	insertSearchMessage(t, ctx, database, "m-visible-2-archived", "s-archived", "visibilityterm", 1)
	insertSearchMessage(t, ctx, database, "m-visible-3-deleting", "s-deleting", "visibilityterm", 1)
	if _, err := database.ExecContext(ctx,
		`UPDATE sessions SET deletion_state = 'deleting' WHERE id = 's-deleting'`,
	); err != nil {
		t.Fatalf("mark session deleting: %v", err)
	}

	results, err := repo.SearchMessages(ctx, "", "", "visibilityterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	assertSearchMessageIDs(t, results, []string{"m-visible-1-active", "m-visible-2-archived"})
}

// TestSearchMessagesPredicateBuildsOneSharedFragment pins the fragment and
// argument order both projections depend on. The hit projection prepends its
// own marker arguments, so a reordered predicate argument would bind a marker
// to the phrase or the limit.
func TestSearchMessagesPredicateBuildsOneSharedFragment(t *testing.T) {
	predicate, args, ok := searchMessagesPredicate(searchMessagesInput{
		sessionID:         "s1",
		excludedSessionID: "s2",
		query:             `alpha"beta` + "\x00gamma",
		limit:             7,
	})
	if !ok {
		t.Fatal("predicate reported tokenless input for a tokenized query")
	}
	normalized := strings.Join(strings.Fields(predicate), " ")
	for _, want := range []string{
		"FROM messages_fts",
		"JOIN messages m ON m.rowid = messages_fts.rowid",
		"JOIN sessions s ON s.id = m.session_id AND s.deletion_state = 'active' AND s.status IN ('active', 'archived')",
		"WHERE messages_fts MATCH ?",
		"AND m.session_id = ?",
		"AND m.session_id <> ?",
		"ORDER BY bm25(messages_fts), m.id LIMIT ?",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("predicate = %q, want it to contain %q", normalized, want)
		}
	}
	if !reflect.DeepEqual(args, []any{`"alpha""beta gamma"`, "s1", "s2", 7}) {
		t.Fatalf("predicate args = %#v", args)
	}

	scopeless, scopelessArgs, ok := searchMessagesPredicate(searchMessagesInput{
		query: "alpha",
		limit: 5,
	})
	if !ok {
		t.Fatal("predicate reported tokenless input for a tokenized query")
	}
	if strings.Contains(scopeless, "AND m.session_id = ?") ||
		strings.Contains(scopeless, "AND m.session_id <> ?") {
		t.Fatalf("predicate = %q, want no scope or exclusion clause", scopeless)
	}
	if !reflect.DeepEqual(scopelessArgs, []any{`"alpha"`, 5}) {
		t.Fatalf("scopeless args = %#v", scopelessArgs)
	}
}

func TestSearchMessagesPredicateAppliesLimitAndTokenRules(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		query     string
		limit     int
		wantOK    bool
		wantLimit int
	}{
		{name: "valid limit", query: "alpha", limit: 1, wantOK: true, wantLimit: 1},
		{name: "maximum limit", query: "alpha", limit: 100, wantOK: true, wantLimit: 100},
		{name: "zero limit defaults", query: "alpha", limit: 0, wantOK: true, wantLimit: 20},
		{name: "negative limit defaults", query: "alpha", limit: -1, wantOK: true, wantLimit: 20},
		{name: "oversized limit defaults", query: "alpha", limit: 101, wantOK: true, wantLimit: 20},
		{name: "punctuation only", query: "...", limit: 10},
		{name: "nul only", query: "\x00", limit: 10},
		{name: "empty", query: "", limit: 10},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			predicate, args, ok := searchMessagesPredicate(searchMessagesInput{
				query: testCase.query,
				limit: testCase.limit,
			})
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if !testCase.wantOK {
				if predicate != "" || args != nil {
					t.Fatalf("tokenless predicate = %q, args = %#v", predicate, args)
				}
				return
			}
			if got := args[len(args)-1]; got != testCase.wantLimit {
				t.Fatalf("limit arg = %v, want %v", got, testCase.wantLimit)
			}
		})
	}
}

func insertSearchSession(t *testing.T, ctx context.Context, database *db.DB, id string) {
	t.Helper()
	_, err := database.ExecContext(ctx, `INSERT INTO sessions (id, created_at, updated_at) VALUES (?, '2026-08-10T00:00:00Z', '2026-08-10T00:00:00Z')`, id)
	if err != nil {
		t.Fatalf("insert session %s: %v", id, err)
	}
}

func insertListSession(t *testing.T, ctx context.Context, database *db.DB, id, status, updatedAt string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sessions (id, status, created_at, updated_at)
		VALUES (?, ?, '2026-08-20T04:00:00.000000000Z', ?)`,
		id,
		status,
		updatedAt,
	); err != nil {
		t.Fatal(err)
	}
}

func assertSessionIDs(t *testing.T, sessions []Session, want []string) {
	t.Helper()
	got := make([]string, 0, len(sessions))
	for _, session := range sessions {
		got = append(got, session.SessionID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("session IDs = %v, want %v", got, want)
	}
}

func insertSearchMessage(t *testing.T, ctx context.Context, database *db.DB, id, sessionID, content string, sequence int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, 'user', ?, 'text', ?, '2026-08-10T00:00:00Z')`, id, sessionID, content, sequence)
	if err != nil {
		t.Fatalf("insert message %s: %v", id, err)
	}
}

func assertSearchMessageIDs(t *testing.T, messages []Message, want []string) {
	t.Helper()
	got := make([]string, 0, len(messages))
	for _, message := range messages {
		got = append(got, message.MessageID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("message IDs = %v, want %v", got, want)
	}
}

// completedRunForHistory drives one fresh run all the way to a committed
// success through the real writers, so a history test asserts against the state
// those writers actually produced rather than a row it wrote by hand.
func completedRunForHistory(t *testing.T, repo *Repository, title, content string) (EnqueueUserMessageResult, RunState) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	completed, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		Content:              content,
		ExpectedStateVersion: running.StateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	return enqueued, completed.State
}

func messageByID(t *testing.T, messages []Message, messageID string) Message {
	t.Helper()
	for _, message := range messages {
		if message.MessageID == messageID {
			return message
		}
	}
	t.Fatalf("message %q missing from page %+v", messageID, messages)
	return Message{}
}

func TestListMessagesReturnsEmptyPageForActiveSessionWithoutMessages(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Empty history")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	messages, err := repo.ListMessages(ctx, session.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("ListMessages returned %+v, want an empty page", messages)
	}

	messages, err = repo.ListMessagesBefore(ctx, session.SessionID, "", 50)
	if err != nil {
		t.Fatalf("ListMessagesBefore: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("ListMessagesBefore returned %+v, want an empty page", messages)
	}
}

// TestListMessagesEmbedsMatchingRunStateWithoutChangingCardinality is the whole
// point of the join: reopening a conversation must answer "what happened to
// this run" from the same page that carries the message, and it must answer it
// without turning one message into two rows.
func TestListMessagesEmbedsMatchingRunStateWithoutChangingCardinality(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, committed := completedRunForHistory(t, repo, "Embedded state", "the answer")

	messages, err := repo.ListMessages(ctx, enqueued.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("page size = %d, want the two message rows the run owns", len(messages))
	}
	user := messageByID(t, messages, enqueued.UserMessageID)
	if user.RunState != nil {
		t.Fatalf("user message carries run state %+v, want none", user.RunState)
	}
	assistant := messageByID(t, messages, enqueued.AssistantMessageID)
	if assistant.RunState == nil {
		t.Fatal("assistant message carries no run state")
	}
	// The digest is internal duplicate-report identity. The history reader has
	// no use for it, so it is never selected and never carried out of the join.
	want := committed
	want.ContentSHA256 = ""
	if *assistant.RunState != want {
		t.Fatalf("embedded state = %+v, want the committed state %+v", *assistant.RunState, want)
	}
	if assistant.RunState.ContentSHA256 != "" {
		t.Fatalf("history join carried the internal content digest %q", assistant.RunState.ContentSHA256)
	}
}

// TestListMessagesOmitsStateForNullOrSingleMismatchedLegacyCorrelation covers
// the neutral legacy path. A message with no run, or with a link only one side
// agrees with, has no provable owner — so the reader returns the message and no
// state rather than attaching a run's outcome to somebody else's turn.
func TestListMessagesOmitsStateForNullOrSingleMismatchedLegacyCorrelation(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Legacy correlation", "the answer")
	other, err := repo.CreateSession(ctx, "Foreign session")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	const legacyAt = "2026-08-12T00:00:00.000000000Z"
	// An assistant turn from before runs were correlated at all.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_legacy_null', ?, 'assistant', 'orphan', 'text', 90, ?)
	`, enqueued.SessionID, legacyAt); err != nil {
		t.Fatal(err)
	}
	// A message naming a run that names a different message back. The run's
	// own assistant turn exists and is untouched, so exactly one side of the
	// circular link disagrees.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_mismatch_target', ?, 'assistant', 'the run''s real turn', 'text', 91, ?)
	`, enqueued.SessionID, legacyAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id,
			status, model_provider, model_name, created_at, state_version, state_updated_at, outcome_reason,
			assistant_content_sha256)
		VALUES ('run_mismatch', ?, ?, 'msg_mismatch_target', 'general_assistant', 'trace_mismatch',
			'completed', 'ollama', 'llama3.2', ?, 3, ?, 'none', ?)
	`, enqueued.SessionID, enqueued.UserMessageID, legacyAt, legacyAt, emptyAssistantContentSHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_legacy_mismatch', ?, 'run_mismatch', 'assistant', 'mismatched', 'text', 92, ?)
	`, enqueued.SessionID, legacyAt); err != nil {
		t.Fatal(err)
	}
	// A run that lives in another session entirely.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_foreign_assistant', ?, 'assistant', 'foreign', 'text', 1, ?)
	`, other.SessionID, legacyAt); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id,
			status, model_provider, model_name, created_at, state_version, state_updated_at, outcome_reason,
			assistant_content_sha256)
		VALUES ('run_foreign', ?, ?, 'msg_foreign_assistant', 'general_assistant', 'trace_foreign',
			'completed', 'ollama', 'llama3.2', ?, 3, ?, 'none', ?)
	`, other.SessionID, enqueued.UserMessageID, legacyAt, legacyAt, emptyAssistantContentSHA256); err != nil {
		t.Fatal(err)
	}
	// The foreign run's assistant message is moved into this session's history
	// while the run stays behind, so only the session disagrees.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_session_mismatch', ?, 'run_foreign', 'assistant', 'cross session', 'text', 93, ?)
	`, enqueued.SessionID, legacyAt); err != nil {
		t.Fatal(err)
	}
	// A user turn wearing a run ID is still not a run's assistant answer.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_wrong_role', ?, ?, 'user', 'not an answer', 'text', 94, ?)
	`, enqueued.SessionID, enqueued.RunID, legacyAt); err != nil {
		t.Fatal(err)
	}

	messages, err := repo.ListMessages(ctx, enqueued.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 7 {
		t.Fatalf("page size = %d, want every message row exactly once", len(messages))
	}
	for _, id := range []string{
		"msg_legacy_null", "msg_legacy_mismatch", "msg_mismatch_target",
		"msg_session_mismatch", "msg_wrong_role",
	} {
		if state := messageByID(t, messages, id).RunState; state != nil {
			t.Fatalf("message %q carries run state %+v, want the neutral legacy path", id, state)
		}
	}
	if messageByID(t, messages, enqueued.AssistantMessageID).RunState == nil {
		t.Fatal("the one provable link lost its state alongside the legacy rows")
	}
}

// TestListMessagesRejectsValueFreeDuplicateCorrelation covers ownership that is
// ambiguous rather than merely absent. Two assistant turns claiming one run
// means no reader can say which of them the outcome belongs to, so history
// fails closed — and says so without echoing a row.
func TestListMessagesRejectsValueFreeDuplicateCorrelation(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, _ := completedRunForHistory(t, repo, "Duplicate ownership", "the answer")

	// The unique index is exactly what stops this today. Dropping it is how a
	// database that predates the index, or one restored from a corrupt backup,
	// reaches this reader.
	if _, err := database.ExecContext(ctx, `DROP INDEX idx_messages_assistant_run_unique`); err != nil {
		t.Fatalf("drop correlation index: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_second_claimant', ?, ?, 'assistant', 'second claimant', 'text', 90, '2026-08-12T00:00:00.000000000Z')
	`, enqueued.SessionID, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	// An anchor after both claimants, so the older page actually contains the
	// ambiguity. The check sees the page the reader returns, which is the same
	// bound everything else here obeys.
	if _, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES ('msg_anchor', ?, 'user', 'anchor', 'text', 91, '2026-08-12T00:00:01.000000000Z')
	`, enqueued.SessionID); err != nil {
		t.Fatal(err)
	}

	_, err := repo.ListMessages(ctx, enqueued.SessionID, 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessages error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_second_claimant", "second claimant", "the answer")

	_, err = repo.ListMessagesBefore(ctx, enqueued.SessionID, "msg_anchor", 50)
	if !errors.Is(err, runcorrelation.ErrConflict) {
		t.Fatalf("ListMessagesBefore error = %v, want the correlation conflict sentinel", err)
	}
	assertValueFreeCorrelationError(t, err, enqueued, "msg_second_claimant", "second claimant", "the answer")
}

// assertValueFreeCorrelationError proves the failure names a remediation class
// and nothing else. An operator reads this in a log; a row value read there is
// a leak that no amount of care downstream can take back.
//
// The rendered text is pinned to the bare sentinel rather than merely searched,
// because the cheapest way to leak a row is to wrap the sentinel in a message
// that explains which one — and every secret this test knows to look for is a
// value some future writer might think is safe to name.
func assertValueFreeCorrelationError(t *testing.T, err error, enqueued EnqueueUserMessageResult, extra ...string) {
	t.Helper()
	message := err.Error()
	secrets := append([]string{
		enqueued.RunID, enqueued.SessionID, enqueued.AssistantMessageID, enqueued.UserMessageID,
	}, extra...)
	for _, secret := range secrets {
		if strings.Contains(message, secret) {
			t.Fatalf("correlation error %q leaked %q", message, secret)
		}
	}
	if message != runcorrelation.ErrConflict.Error() {
		t.Fatalf("correlation error = %q, want only the sentinel %q", message, runcorrelation.ErrConflict.Error())
	}
}

// TestRunProjectionDoesNotIssuePerMessageQueries pins the cost of the join. The
// alternative design — read the page, then look up each message's run — is
// invisible in the returned values and only shows up as a conversation that
// gets slower the longer it gets, so the query count is asserted directly.
func TestRunProjectionDoesNotIssuePerMessageQueries(t *testing.T) {
	counter := &countingSQLiteDriver{}
	database := openCountingTestDB(t, counter)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Query budget")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	var anchor string
	for turn := 0; turn < 8; turn++ {
		enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: fmt.Sprintf("turn %d", turn), AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatalf("EnqueueUserMessage: %v", err)
		}
		if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
			t.Fatalf("MarkRunRunning: %v", err)
		}
		running, err := repo.GetRunState(ctx, enqueued.RunID)
		if err != nil {
			t.Fatalf("GetRunState: %v", err)
		}
		if _, err := repo.CompleteRunCanonical(ctx, CompleteRunInput{
			RunID:                enqueued.RunID,
			AssistantMessageID:   enqueued.AssistantMessageID,
			Content:              fmt.Sprintf("answer %d", turn),
			ExpectedStateVersion: running.StateVersion,
		}); err != nil {
			t.Fatalf("CompleteRunCanonical: %v", err)
		}
		if anchor == "" {
			anchor = enqueued.AssistantMessageID
		}
	}

	counter.reset()
	newest, err := repo.ListMessages(ctx, session.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if got := counter.count(); got != 1 {
		t.Fatalf("ListMessages issued %d queries for %d messages, want exactly one history query", got, len(newest))
	}
	if len(newest) != 16 {
		t.Fatalf("page size = %d, want all sixteen message rows", len(newest))
	}
	states := 0
	for _, message := range newest {
		if message.RunState != nil {
			states++
		}
	}
	if states != 8 {
		t.Fatalf("embedded states = %d, want one per completed run", states)
	}

	counter.reset()
	if _, err := repo.ListMessagesBefore(ctx, session.SessionID, anchor, 50); err != nil {
		t.Fatalf("ListMessagesBefore: %v", err)
	}
	// One anchor resolution plus one history query. Neither scales with the
	// page, which is the property under test.
	if got := counter.count(); got != 2 {
		t.Fatalf("ListMessagesBefore issued %d queries, want one anchor lookup and one history query", got)
	}
}

// countingDriverSequence keeps each registered counting driver name unique.
// database/sql panics on a duplicate registration, and a test binary run with
// -count=2 would otherwise register the same name twice.
var countingDriverSequence atomic.Int64

// countingSQLiteDriver wraps the driver the repository actually uses and counts
// the queries that reach it.
//
// It counts at the driver rather than through a SQLite trace hook because the
// question is what database/sql sent, and because the available SQLite bindings
// expose no trace API this test could depend on.
type countingSQLiteDriver struct {
	base    driver.Driver
	queries atomic.Int64
}

func (d *countingSQLiteDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, owner: d}, nil
}

func (d *countingSQLiteDriver) reset()       { d.queries.Store(0) }
func (d *countingSQLiteDriver) count() int64 { return d.queries.Load() }

type countingConn struct {
	driver.Conn
	owner *countingSQLiteDriver
}

// QueryContext is the fast path database/sql prefers. A driver that declines
// with ErrSkip did not issue the query, so the count is taken back.
func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.owner.queries.Add(1)
	rows, err := queryer.QueryContext(ctx, query, args)
	if errors.Is(err, driver.ErrSkip) {
		c.owner.queries.Add(-1)
	}
	return rows, err
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return execer.ExecContext(ctx, query, args)
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// The wrapped driver is the repository's own SQLite driver, which
	// implements the context form. Anything else is a bug in this wrapper, not
	// a case to paper over with the deprecated one.
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("counting driver: wrapped connection cannot begin a context transaction")
	}
	return beginner.BeginTx(ctx, opts)
}

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, owner: c.owner}, nil
}

// PrepareContext is wrapped for the same reason Prepare is: a prepared
// statement is the fallback path, and an uncounted fallback would let a
// per-message read hide behind it.
func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.Conn.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}
	stmt, err := preparer.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, owner: c.owner}, nil
}

type countingStmt struct {
	driver.Stmt
	owner *countingSQLiteDriver
}

// QueryContext is the only query path database/sql can take here: it prefers
// StmtQueryContext, which this wrapper implements, so no statement query can
// slip past the counter through the older form.
func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.owner.queries.Add(1)
	rows, err := queryer.QueryContext(ctx, args)
	if errors.Is(err, driver.ErrSkip) {
		s.owner.queries.Add(-1)
	}
	return rows, err
}

// openCountingTestDB opens a migrated database on the counting wrapper. The
// wrapped driver is read off a real repository connection rather than looked up
// by name, so the count is taken on exactly the driver production uses.
func openCountingTestDB(t *testing.T, counter *countingSQLiteDriver) *db.DB {
	t.Helper()
	counter.base = openTestDB(t).Driver()
	name := fmt.Sprintf("turing_sqlite3_counting_%d", countingDriverSequence.Add(1))
	sql.Register(name, counter)
	sqlDB, err := sql.Open(name, "file:"+filepath.Join(t.TempDir(), "counted.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open counted db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}
