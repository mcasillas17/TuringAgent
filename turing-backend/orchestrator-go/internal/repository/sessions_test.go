package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
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
