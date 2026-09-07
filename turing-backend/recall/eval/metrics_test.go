package eval

import (
	"math"
	"reflect"
	"testing"
)

func TestRankingHandComputed(t *testing.T) {
	grades := map[string]int{"direct": 2, "support": 1, "noise": 0}
	got := ranking([]string{"noise", "support", "direct"}, grades, 3)
	wantDCG := 1/math.Log2(3) + 3/math.Log2(4)
	wantIdeal := 3.0 + 1/math.Log2(3)
	if got == nil || got.Recall != 1 || got.MRR != .5 || math.Abs(got.NDCG-wantDCG/wantIdeal) > 1e-12 {
		t.Fatalf("hand-computed ranking: got %+v", got)
	}
	atOne := ranking([]string{"noise", "support", "direct"}, grades, 1)
	if atOne == nil || *atOne != (quality{}) {
		t.Fatalf("irrelevant first result: %+v", atOne)
	}
	atFive := ranking([]string{"direct"}, grades, 5)
	if atFive == nil || atFive.Recall != .5 || atFive.MRR != 1 {
		t.Fatalf("missing relevant evidence must stay in denominator: %+v", atFive)
	}
	if ranking(nil, nil, 5) != nil || ranking([]string{"noise"}, map[string]int{"noise": 0}, 3) != nil {
		t.Fatal("no relevance is N/A, not perfect or zero quality")
	}
}

func TestRankingDuplicateRowsNeverInflateRecall(t *testing.T) {
	got := ranking([]string{"a", "a", "b"}, map[string]int{"a": 2, "b": 1}, 3)
	if got == nil || got.Recall != 1 || got.NDCG > 1 {
		t.Fatalf("duplicate identity counted twice: %+v", got)
	}
}

func TestMacroExcludesNoRelevance(t *testing.T) {
	got := meanQuality([]*quality{{Recall: 1, MRR: 1, NDCG: 1}, nil, {}})
	if got == nil || *got != (quality{Recall: .5, MRR: .5, NDCG: .5}) {
		t.Fatalf("macro denominator includes unsupported cases: %+v", got)
	}
	if meanQuality([]*quality{nil}) != nil {
		t.Fatal("all unsupported macro must be null")
	}
}

func TestIndependentRegressionGates(t *testing.T) {
	floors := map[string]float64{"rpc.recall@1": 1, "recall.evidence": 1, "context.evidence": 1, "context.admission": 1}
	ceilings := map[string]float64{"recall.stale": 0, "context.unsupported": 0, "prompt.bytes": 800, "work.rows": 40}
	actual := map[string]float64{}
	for k, v := range floors {
		actual[k] = v
	}
	for k, v := range ceilings {
		actual[k] = v
	}
	for _, test := range []struct {
		name  string
		key   string
		value float64
	}{
		{"worse ranking", "rpc.recall@1", 0},
		{"missing excerpt evidence", "recall.evidence", 0},
		{"missing request evidence", "context.evidence", 0},
		{"context admission loss", "context.admission", 0},
		{"stale inclusion", "recall.stale", 1},
		{"no support inclusion", "context.unsupported", 1},
		{"prompt growth", "prompt.bytes", 801},
		{"work growth", "work.rows", 41},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := make(map[string]float64, len(actual))
			for k, v := range actual {
				changed[k] = v
			}
			changed[test.key] = test.value
			if got := compareGates(changed, floors, ceilings); !reflect.DeepEqual(got, []string{test.key}) {
				t.Fatalf("want only named gate %q, got %v", test.key, got)
			}
		})
	}
	if got := compareGates(actual, floors, ceilings); len(got) != 0 {
		t.Fatalf("unchanged observation failed: %v", got)
	}
	actual["rpc.recall@1"] = math.NaN()
	if len(compareGates(actual, floors, ceilings)) == 0 {
		t.Fatal("nonfinite measurement bypassed gate")
	}
	delete(actual, "rpc.recall@1")
	if len(compareGates(actual, floors, ceilings)) == 0 {
		t.Fatal("missing measurement bypassed gate")
	}
}
