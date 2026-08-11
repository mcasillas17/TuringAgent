package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func at(day int) time.Time {
	return time.Date(2026, 8, day, 12, 0, 0, 0, time.UTC)
}

type fakeSearcher struct {
	byTerm  map[string][]Excerpt
	queries []string
	err     error
	block   chan struct{}
}

func (f *fakeSearcher) SearchMessages(ctx context.Context, query string, limit int) ([]Excerpt, error) {
	f.queries = append(f.queries, query)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.byTerm[query], nil
}

func TestTermsDropsStopwordsAndShortWordsPreservingOrder(t *testing.T) {
	got := terms("How did we deploy the staging cluster?")
	want := []string{"deploy", "staging", "cluster"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("terms = %v, want %v", got, want)
	}
}

func TestTermsDedupesAndCaps(t *testing.T) {
	if got := terms("deploy deploy DEPLOY"); len(got) != 1 || got[0] != "deploy" {
		t.Fatalf("terms did not dedupe case-insensitively: %v", got)
	}
	long := strings.Repeat("alpha bravo charlie delta echo foxtrot golf hotel ", 2)
	if got := terms(long); len(got) > maxTerms {
		t.Fatalf("terms returned %d, want <= %d", len(got), maxTerms)
	}
}

func TestTermsReturnsNothingWhenOnlyStopwords(t *testing.T) {
	if got := terms("a an the of to is"); len(got) != 0 {
		t.Fatalf("terms = %v, want empty", got)
	}
}

func TestRankScoresByDistinctTermMatchesThenRecency(t *testing.T) {
	two := Excerpt{SessionID: "s1", Role: "user", Content: "deploy the staging cluster", CreatedAt: at(1)}
	one := Excerpt{SessionID: "s1", Role: "user", Content: "cluster notes", CreatedAt: at(2)}
	hits := map[string][]Excerpt{
		"deploy":  {two},
		"cluster": {two, one},
	}
	got := rank(hits, "current", 10, 10000)
	if len(got) != 2 {
		t.Fatalf("want 2 excerpts, got %d", len(got))
	}
	if got[0].Content != two.Content {
		t.Fatalf("two-term match should rank first, got %q", got[0].Content)
	}
}

func TestRankBreaksTiesTowardRecency(t *testing.T) {
	older := Excerpt{SessionID: "s1", Role: "user", Content: "older cluster", CreatedAt: at(1)}
	newer := Excerpt{SessionID: "s1", Role: "user", Content: "newer cluster", CreatedAt: at(9)}
	got := rank(map[string][]Excerpt{"cluster": {older, newer}}, "current", 10, 10000)
	if len(got) != 2 || got[0].Content != newer.Content {
		t.Fatalf("recent excerpt should win the tie, got %+v", got)
	}
}

func TestRankExcludesCurrentSessionAndNonConversationRoles(t *testing.T) {
	hits := map[string][]Excerpt{"cluster": {
		{SessionID: "current", Role: "user", Content: "in current session", CreatedAt: at(1)},
		{SessionID: "s1", Role: "tool", Content: "tool output", CreatedAt: at(1)},
		{SessionID: "s1", Role: "system", Content: "system prompt", CreatedAt: at(1)},
		{SessionID: "s1", Role: "assistant", Content: "kept", CreatedAt: at(1)},
	}}
	got := rank(hits, "current", 10, 10000)
	if len(got) != 1 || got[0].Content != "kept" {
		t.Fatalf("filtering wrong: %+v", got)
	}
}

func TestRankDedupesTheSameMessageAcrossTerms(t *testing.T) {
	same := Excerpt{SessionID: "s1", Role: "user", Content: "deploy the cluster", CreatedAt: at(1)}
	got := rank(map[string][]Excerpt{"deploy": {same}, "cluster": {same}}, "current", 10, 10000)
	if len(got) != 1 {
		t.Fatalf("same message returned by two terms must appear once, got %d", len(got))
	}
}

func TestRankHonoursExcerptCap(t *testing.T) {
	hits := map[string][]Excerpt{"cluster": {
		{SessionID: "s1", Role: "user", Content: "one", CreatedAt: at(1)},
		{SessionID: "s1", Role: "user", Content: "two", CreatedAt: at(2)},
		{SessionID: "s1", Role: "user", Content: "three", CreatedAt: at(3)},
	}}
	if got := rank(hits, "current", 2, 10000); len(got) != 2 {
		t.Fatalf("want 2 excerpts, got %d", len(got))
	}
}

func TestRankTruncatesRatherThanDroppingWhenOverCharBudget(t *testing.T) {
	long := strings.Repeat("x", 500)
	got := rank(map[string][]Excerpt{"cluster": {
		{SessionID: "s1", Role: "user", Content: long, CreatedAt: at(1)},
	}}, "current", 10, 100)
	if len(got) != 1 {
		t.Fatalf("over-long excerpt should be truncated, not dropped: %+v", got)
	}
	if len(got[0].Content) > 100 {
		t.Fatalf("excerpt not truncated to budget: %d chars", len(got[0].Content))
	}
	if !strings.HasSuffix(got[0].Content, ellipsis) {
		t.Fatalf("truncation must be visible, got %q", got[0].Content)
	}
}

func TestRenderLabelsRecalledMaterialWithAbsoluteDates(t *testing.T) {
	block, ok := Render([]Excerpt{
		{SessionID: "s1", Role: "user", Content: "we deployed on friday", CreatedAt: at(4)},
		{SessionID: "s1", Role: "assistant", Content: "noted", CreatedAt: at(4)},
	})
	if !ok {
		t.Fatal("expected a block")
	}
	if block.Role != "system" {
		t.Fatalf("recalled material must not enter as a conversation turn, got role %q", block.Role)
	}
	for _, want := range []string{"EARLIER", "NOT part of the current", "2026-08-04", "user:", "assistant:", "we deployed on friday"} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("rendered block missing %q:\n%s", want, block.Content)
		}
	}
	if strings.Contains(block.Content, "recently") {
		t.Fatal("dates must be absolute; the block may be re-read weeks later")
	}
}

func TestRenderReturnsFalseForNoExcerpts(t *testing.T) {
	if _, ok := Render(nil); ok {
		t.Fatal("no excerpts must render no block")
	}
}

func newRecaller(s Searcher) *Recaller {
	return &Recaller{Search: s, MaxExcerpts: 5, MaxChars: 2000, Timeout: time.Second}
}

func TestRecallQueriesEachTermAndRendersABlock(t *testing.T) {
	f := &fakeSearcher{byTerm: map[string][]Excerpt{
		"deploy":  {{SessionID: "s1", Role: "user", Content: "deploy notes", CreatedAt: at(1)}},
		"cluster": {{SessionID: "s1", Role: "user", Content: "cluster notes", CreatedAt: at(2)}},
	}}
	block, ok := newRecaller(f).Recall(context.Background(), "current", "how did we deploy the cluster?")
	if !ok {
		t.Fatal("expected a recalled block")
	}
	if len(f.queries) != 2 {
		t.Fatalf("expected one query per term, got %v", f.queries)
	}
	if !strings.Contains(block.Content, "cluster notes") {
		t.Fatalf("block missing recalled content:\n%s", block.Content)
	}
}

// Recall is an enhancement. A backend that is down, slow, or empty must degrade
// to "no block" — never to a failed turn.
func TestRecallSwallowsSearchErrors(t *testing.T) {
	f := &fakeSearcher{err: errors.New("orchestrator unavailable")}
	if _, ok := newRecaller(f).Recall(context.Background(), "current", "deploy cluster"); ok {
		t.Fatal("a failing search must not produce a block")
	}
}

func TestRecallGivesUpOnTimeout(t *testing.T) {
	f := &fakeSearcher{block: make(chan struct{})}
	r := newRecaller(f)
	r.Timeout = 20 * time.Millisecond
	done := make(chan bool, 1)
	go func() {
		_, ok := r.Recall(context.Background(), "current", "deploy cluster")
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a timed-out search must not produce a block")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recall did not honour its timeout")
	}
	close(f.block)
}

func TestRecallIssuesNoQueriesWithoutTerms(t *testing.T) {
	f := &fakeSearcher{}
	if _, ok := newRecaller(f).Recall(context.Background(), "current", "a an the"); ok {
		t.Fatal("no terms must produce no block")
	}
	if len(f.queries) != 0 {
		t.Fatalf("no terms must issue no queries, got %v", f.queries)
	}
}

func TestRecallReturnsNothingWhenOnlyCurrentSessionMatches(t *testing.T) {
	f := &fakeSearcher{byTerm: map[string][]Excerpt{
		"cluster": {{SessionID: "current", Role: "user", Content: "already in context", CreatedAt: at(1)}},
	}}
	if _, ok := newRecaller(f).Recall(context.Background(), "current", "cluster"); ok {
		t.Fatal("current-session hits are already in context and must not be recalled")
	}
}
