package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimekit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
)

// TestEvaluation is the CI gate. Normal runs only read the reviewed baseline.
// Candidate generation is explicit, creates a NEW path exclusively, and still
// enforces every hard gate. Never generates ground truth from observations.
func TestEvaluation(t *testing.T) {
	c, err := loadCorpus(fixtureBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	r := report{Version: 1, CorpusHash: c.Hash}
	for _, test := range c.Cases {
		t.Run(test.ID, func(t *testing.T) {
			o := runCase(t, c, test)
			result, hard := evaluate(c, test, o)
			if len(hard) > 0 {
				t.Fatalf("hard gates: %v", hard)
			}
			r.Cases = append(r.Cases, result)
		})
	}
	if t.Failed() {
		return
	}
	aggregate(&r)
	if path := os.Getenv("TURING_RECALL_REPORT"); path != "" {
		writeNewJSON(t, path, r)
	}
	if path := os.Getenv("TURING_RECALL_BASELINE_CANDIDATE"); path != "" {
		// O_EXCL in writeNewJSON prevents overwriting any reviewed artifact,
		// including when an absolute path or path alias is supplied.
		writeNewJSON(t, path, baselineFor(r))
		t.Logf("candidate only; review before replacing testdata/baseline.v1.json")
		return
	}
	data, err := readBounded("testdata/baseline.v1.json")
	if err != nil {
		t.Fatalf("reviewed baseline required: %v", err)
	}
	b, err := loadBaseline(data, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range r.Cases {
		limits := b.Cases[result.ID]
		if failures := compareGates(result.gates(), limits.Floors, limits.Ceilings); len(failures) > 0 {
			t.Errorf("%s regression gates: %v", result.ID, failures)
		}
	}
	raw, _ := json.Marshal(r.Macro)
	t.Logf("macro relevance-only metrics: %s; unsupported-case rate %.6f", raw, r.UnsupportedCaseRate)
}

func writeNewJSON(t testing.TB, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxJSONBytes {
		t.Fatal("report JSON byte bound")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func TestFixtureMechanisms(t *testing.T) {
	c, err := loadCorpus(fixtureBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"older-than-fetch-window", "budget-convergence", "same-content-identities", "whole-recall-omission", "excerpt-tail-lost", "cjk-short"} {
		t.Run(id, func(t *testing.T) {
			test := c.Cases[slices.IndexFunc(c.Cases, func(v fixtureCase) bool { return v.ID == id })]
			o := runCase(t, c, test)
			r, hard := evaluate(c, test, o)
			if len(hard) != 0 {
				t.Fatal(hard)
			}
			switch id {
			case "older-than-fetch-window":
				if number(r.Context.EvidenceCoverage) != 1 || len(o.Execution.Passes[0].InContext) != 51 {
					t.Fatal("50-message fetch cutoff not exercised")
				}
				for _, m := range o.Execution.Passes[0].InContext {
					if m.MessageID == "msg_cutoff_target" {
						t.Fatal("target was fetched, not recalled")
					}
				}
			case "budget-convergence":
				if len(o.Execution.Passes) < 2 || o.Execution.HistoryOmitted == 0 || number(r.Context.EvidenceCoverage) != 1 {
					t.Fatalf("real iterative budget admission not exercised: %+v", o.Execution)
				}
			case "same-content-identities":
				if number(r.Context.SourceCoverage) != 1 || len(r.Recall.Evidence.IDs) != 1 ||
					r.Recall.Evidence.IDs[0] != "msg_twin_earlier" {
					t.Fatal("row identity dedup is not exercised")
				}
			case "whole-recall-omission":
				if !o.Execution.RecallOmitted || number(r.Recall.Evidence.EvidenceCoverage) != 1 || number(r.Context.EvidenceCoverage) != 0 {
					t.Fatal("whole recall omission not exercised")
				}
			case "excerpt-tail-lost":
				if number(r.Recall.Evidence.SourceCoverage) != 1 {
					t.Fatal("source presence was confused with span survival")
				}
			case "cjk-short":
				// A future lexical improvement may issue a short-CJK query. Do
				// not freeze the known miss; verify that no queries means no
				// recalled evidence, while RPC has a real matching source.
				if number(r.RPC.Evidence.EvidenceCoverage) != 1 || (o.Execution.Queries == 0 && len(r.Recall.Evidence.IDs) != 0) {
					t.Fatal("short CJK runtime limitation not exercised")
				}

			}
		})
	}
}

func TestReportReproducibility(t *testing.T) {
	c, err := loadCorpus(fixtureBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	var reports [2]report
	for i := range reports {
		reports[i] = report{Version: 1, CorpusHash: c.Hash}
		for _, test := range c.Cases {
			t.Run(fmt.Sprintf("%d/%s", i, test.ID), func(t *testing.T) {
				o := runCase(t, c, test)
				r, hard := evaluate(c, test, o)
				if len(hard) != 0 {
					t.Fatal(hard)
				}
				reports[i].Cases = append(reports[i].Cases, r)
			})
		}
		aggregate(&reports[i])
	}
	if !sameReport(reports[0], reports[1]) {
		t.Fatal("independent databases produced different deterministic reports")
	}
	first, _ := json.Marshal(baselineFor(reports[0]))
	second, _ := json.Marshal(baselineFor(reports[1]))
	if string(first) != string(second) {
		t.Fatal("candidate baselines are not byte-stable")
	}
}

// Latency is measured separately, excluding migrations, seeding and lifecycle
// setup. No millisecond pass/fail thresholds: CI uses coarse process timeouts,
// deterministic query/row/output bounds, and Go's ns/op and allocation reports.
func BenchmarkRecall(b *testing.B) {
	c, err := loadCorpus(fixtureBytes(b))
	if err != nil {
		b.Fatal(err)
	}
	for _, id := range []string{"exact-id", "query-work-cap", "budget-convergence"} {
		test := c.Cases[slices.IndexFunc(c.Cases, func(v fixtureCase) bool { return v.ID == id })]
		b.Run(id, func(b *testing.B) {
			h := newCaseHarness(b, c, test)
			b.Run("phrase-rpc", func(b *testing.B) {
				client := turingv1.NewSessionServiceClient(h.conn)
				req := &turingv1.SearchMessagesRequest{Query: test.Phrase, ExcludeSessionId: test.ExcludeSession, SessionId: test.Scope, Limit: 5,
					ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, err := client.SearchMessages(ctx, req)
					cancel()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("assistant-execute", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					o, err := runtimekit.ExecuteRecall(ctx, h.conn, caseJob(test), test.Window)
					cancel()
					if err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(o.Queries), "queries/op")
					b.ReportMetric(float64(o.Rows), "rows/op")
					b.ReportMetric(float64(o.PromptTokens), "estimated-tokens/op")
				}
			})
		})
	}
}

func Example_reportSemantics() {
	fmt.Println("Quality: source-order Recall/MRR/nDCG at 1,3,5; no BM25 magnitudes.")
	fmt.Println("Context: evidence set coverage; live question is not supporting evidence.")
	fmt.Println("Prompt runes/bytes: message content; tokens: real Ollama wire-size estimator.")
	fmt.Println("Stale/abstention: observable source use, not model answers; usage: absent.")
	// Output:
	// Quality: source-order Recall/MRR/nDCG at 1,3,5; no BM25 magnitudes.
	// Context: evidence set coverage; live question is not supporting evidence.
	// Prompt runes/bytes: message content; tokens: real Ollama wire-size estimator.
	// Stale/abstention: observable source use, not model answers; usage: absent.
}
