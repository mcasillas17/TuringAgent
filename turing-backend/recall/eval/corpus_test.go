package eval

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBytes(t testing.TB) []byte {
	t.Helper()
	data, err := readBounded("testdata/corpus.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStrictCorpusValidation(t *testing.T) {
	data := fixtureBytes(t)
	if _, err := loadCorpus(data); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"case-insensitive field alias": func(v map[string]any) { v["Version"] = v["version"] },
		"unknown field":                func(v map[string]any) { v["surprise"] = true },
		"bad schema":                   func(v map[string]any) { v["version"] = 2 },
		"missing category":             func(v map[string]any) { v["cases"] = v["cases"].([]any)[:1] },
		"duplicate ID": func(v map[string]any) {
			rows := v["messages"].([]any)
			rows[1].(map[string]any)["id"] = rows[0].(map[string]any)["id"]
		},
		"malformed ID":      func(v map[string]any) { v["messages"].([]any)[0].(map[string]any)["id"] = "line\nbreak" },
		"missing reference": func(v map[string]any) { v["messages"].([]any)[0].(map[string]any)["session"] = "ses_missing" },
		"invalid time":      func(v map[string]any) { v["messages"].([]any)[0].(map[string]any)["at"] = "yesterday" },
		"non UTC":           func(v map[string]any) { v["messages"].([]any)[0].(map[string]any)["at"] = "2026-01-01T00:00:00+01:00" },
		"bad grade":         func(v map[string]any) { firstJudgment(v)["grade"] = 3 },
		"missing grade":     func(v map[string]any) { delete(firstJudgment(v), "grade") },
		"missing judgments": func(v map[string]any) {
			delete(v["cases"].([]any)[0].(map[string]any), "judgments")
		},
		"null grade":                func(v map[string]any) { firstJudgment(v)["grade"] = nil },
		"missing span":              func(v map[string]any) { firstJudgment(v)["span"] = "not present in source" },
		"invalid temporal interval": func(v map[string]any) { firstJudgment(v)["valid_until"] = "1900-01-01T00:00:00Z" },
		"query bound": func(v map[string]any) {
			v["cases"].([]any)[0].(map[string]any)["query"] = strings.Repeat("q", maxQueryBytes+1)
		},
		"body bound": func(v map[string]any) {
			v["messages"].([]any)[0].(map[string]any)["body"] = strings.Repeat("x", maxBodyBytes+1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatal(err)
			}
			mutate(raw)
			bad, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadCorpus(bad); err == nil {
				t.Fatal("invalid corpus accepted")
			}
		})
	}
	for name, bad := range map[string][]byte{
		"trailing data": append(append([]byte{}, data...), []byte("{}")...),
		"duplicate key": bytes.Replace(data, []byte(`"version": 1`), []byte(`"version": 1, "version": 1`), 1),
		"invalid UTF8":  {0xff},
		"byte bound":    bytes.Repeat([]byte(" "), maxJSONBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadCorpus(bad); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
}

func TestExplicitEmptyJudgments(t *testing.T) {
	c, err := loadCorpus(fixtureBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range c.Cases {
		if test.ID == "no-support" {
			if test.Judgments == nil || len(test.Judgments) != 0 {
				t.Fatal("no-support must explicitly declare an empty judgment array")
			}
			return
		}
	}
	t.Fatal("missing no-support fixture")
}

func firstJudgment(v map[string]any) map[string]any {
	return v["cases"].([]any)[0].(map[string]any)["judgments"].([]any)[0].(map[string]any)
}

func TestBoundedFileRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte(" "), maxJSONBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBounded(path); err == nil {
		t.Fatal("oversized fixture file accepted")
	}
}

func TestStrictBaselineValidation(t *testing.T) {
	corpus, err := loadCorpus(fixtureBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	base := baselineFor(report{Version: 1, CorpusHash: corpus.Hash, Cases: []caseReport{{ID: corpus.Cases[0].ID}}})
	// An incomplete case set must fail even if all its numeric gates are zero.
	raw, _ := json.Marshal(base)
	if _, err := loadBaseline(raw, corpus); err == nil {
		t.Fatal("partial baseline accepted")
	}
	var cases []caseReport
	for _, c := range corpus.Cases {
		cases = append(cases, caseReport{ID: c.ID, Window: c.Window})
	}
	base = baselineFor(report{Version: 1, CorpusHash: corpus.Hash, Cases: cases})
	raw, _ = json.Marshal(base)
	if _, err := loadBaseline(raw, corpus); err != nil {
		t.Fatalf("complete explicit zero baseline: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unknown":      func(v map[string]any) { v["surprise"] = 1 },
		"schema":       func(v map[string]any) { v["version"] = 99 },
		"hash":         func(v map[string]any) { v["corpus_sha256"] = strings.Repeat("0", 64) },
		"missing case": func(v map[string]any) { delete(v["cases"].(map[string]any), corpus.Cases[0].ID) },
		"extra case": func(v map[string]any) {
			v["cases"].(map[string]any)["unreviewed"] = v["cases"].(map[string]any)[corpus.Cases[0].ID]
		},
		"missing quality floor":       func(v map[string]any) { delete(baselineMap(v, corpus.Cases[0].ID, "floors"), "rpc.recall@1") },
		"missing zero safety ceiling": func(v map[string]any) { delete(baselineMap(v, corpus.Cases[0].ID, "ceilings"), "context.stale") },
		"unknown gate":                func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "floors")["not-a-gate"] = 0 },
		"negative":                    func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "ceilings")["context.stale"] = -1 },
		"null":                        func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "ceilings")["context.stale"] = nil },
		"rate range":                  func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "floors")["rpc.recall@1"] = 2 },
		"work bound":                  func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "ceilings")["work.queries"] = 13 },
		"fractional count":            func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "ceilings")["context.stale"] = 0.5 },
		"impossible source count":     func(v map[string]any) { baselineMap(v, corpus.Cases[0].ID, "ceilings")["rpc.stale"] = 1000 },
		"wrong polarity": func(v map[string]any) {
			f := baselineMap(v, corpus.Cases[0].ID, "floors")
			delete(f, "rpc.recall@1")
			baselineMap(v, corpus.Cases[0].ID, "ceilings")["rpc.recall@1"] = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatal(err)
			}
			mutate(v)
			bad, _ := json.Marshal(v)
			if _, err := loadBaseline(bad, corpus); err == nil {
				t.Fatal("invalid baseline accepted")
			}
		})
	}
	for name, bad := range map[string][]byte{
		"nonfinite":      bytes.Replace(raw, []byte(`"context.stale":0`), []byte(`"context.stale":1e9999`), 1),
		"duplicate gate": bytes.Replace(raw, []byte(`"context.stale":0`), []byte(`"context.stale":0,"context.stale":0`), 1),
		"trailing":       append(append([]byte{}, raw...), []byte("{}")...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadBaseline(bad, corpus); err == nil {
				t.Fatal("invalid baseline encoding accepted")
			}
		})
	}
}

func baselineMap(v map[string]any, id, direction string) map[string]any {
	return v["cases"].(map[string]any)[id].(map[string]any)[direction].(map[string]any)
}
