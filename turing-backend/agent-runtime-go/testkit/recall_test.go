package testkit

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
)

type selectionFixture struct{}

func (selectionFixture) SearchMessages(_ context.Context, _, sessionID, _ string, _ int) ([]memory.Excerpt, error) {
	if sessionID != "" {
		return nil, nil
	}
	return []memory.Excerpt{
		{MessageID: "one", SessionID: "old", Role: "user", Content: "quartz fact", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{MessageID: "two", SessionID: "old", Role: "assistant", Content: "quartz fact", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	}, nil
}

func TestSelectionObserverCannotAlterReturnedRecall(t *testing.T) {
	ctx := context.Background()
	recaller := memory.NewRecaller(selectionFixture{})
	live := []llm.ChatMessage{{MessageID: "live", Role: "user", Content: "quartz"}}
	want, ok := recaller.Recall(ctx, "current", "quartz", live)
	if !ok {
		t.Fatal("expected real rendered recall")
	}
	var identities []string
	recaller.ObserveSelection = func(rows []memory.Excerpt) {
		for _, row := range rows {
			identities = append(identities, row.MessageID)
		}
		rows[0].Content = "observer cannot replace evidence"
		rows[0].MessageID = "observer cannot replace identity"
	}
	got, ok := recaller.Recall(ctx, "current", "quartz", live)
	if !ok || !reflect.DeepEqual(got, want) || len(identities) != 2 || identities[0] == identities[1] {
		t.Fatalf("observer changed rank/render or lost distinct identical-content rows: %v", identities)
	}
	recaller.ObserveSelection = nil
	got, ok = recaller.Recall(ctx, "current", "quartz", live)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatal("observer mutated cached/source state")
	}
}
