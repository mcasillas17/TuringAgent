// Package eval implements the deterministic MEM-003 retrieval evaluator.
// It judges observable evidence, never generated answers or BM25 magnitudes.
package eval

import (
	"math"
	"sort"
)

type quality struct {
	Recall float64 `json:"recall"`
	MRR    float64 `json:"mrr"`
	NDCG   float64 `json:"ndcg"`
}

// Only response order is used. Duplicate IDs consume rank but earn no second
// credit. Missing relevant rows remain in recall's denominator and ideal DCG.
func ranking(ids []string, grades map[string]int, k int) *quality {
	var ideal []int
	for _, grade := range grades {
		if grade > 0 {
			ideal = append(ideal, grade)
		}
	}
	if len(ideal) == 0 {
		return nil
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ideal)))
	var result quality
	var dcg, idcg float64
	seen := map[string]bool{}
	for i, id := range ids {
		if i >= k {
			break
		}
		grade := grades[id]
		if grade > 0 && !seen[id] {
			result.Recall++
			if result.MRR == 0 {
				result.MRR = 1 / float64(i+1)
			}
			dcg += (math.Exp2(float64(grade)) - 1) / math.Log2(float64(i+2))
		}
		seen[id] = true
	}
	for i, grade := range ideal {
		if i >= k {
			break
		}
		idcg += (math.Exp2(float64(grade)) - 1) / math.Log2(float64(i+2))
	}
	result.Recall /= float64(len(ideal))
	if idcg > 0 {
		result.NDCG = dcg / idcg
	}
	return &result
}

func meanQuality(values []*quality) *quality {
	var sum quality
	var n float64
	for _, q := range values {
		if q == nil {
			continue
		}
		n++
		sum.Recall += q.Recall
		sum.MRR += q.MRR
		sum.NDCG += q.NDCG
	}
	if n == 0 {
		return nil
	}
	return &quality{Recall: sum.Recall / n, MRR: sum.MRR / n, NDCG: sum.NDCG / n}
}

func compareGates(actual, floors, ceilings map[string]float64) []string {
	var failed []string
	check := func(limits map[string]float64, floor bool) {
		for key, limit := range limits {
			value, ok := actual[key]
			if !ok || !finite(value) || !finite(limit) ||
				(floor && value+1e-12 < limit) || (!floor && value-1e-12 > limit) {
				failed = append(failed, key)
			}
		}
	}
	check(floors, true)
	check(ceilings, false)
	sort.Strings(failed)
	return failed
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
