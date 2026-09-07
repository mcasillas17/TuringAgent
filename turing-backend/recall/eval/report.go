package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unicode"

	runtimekit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
)

var cutoffs = []int{1, 3, 5}

type observation struct {
	RPC              []string                   `json:"rpc"`
	Legacy           []string                   `json:"legacy"`
	RepositoryHits   []string                   `json:"repository_hits"`
	RepositoryLegacy []string                   `json:"repository_legacy"`
	Snippets         map[string]string          `json:"snippets"`
	Bodies           map[string]string          `json:"bodies"`
	Execution        runtimekit.RecallExecution `json:"execution"`
}

type evidenceMetrics struct {
	IDs              []string `json:"source_ids"`
	Relevant         int      `json:"relevant_denominator"`
	SourceCoverage   *float64 `json:"source_coverage"`
	EvidenceCoverage *float64 `json:"evidence_coverage"`
	StaleCount       int      `json:"temporally_invalid_sources"`
	StaleRate        *float64 `json:"stale_source_rate"`
	UnsupportedRate  *float64 `json:"unsupported_evidence_rate"`
	Abstained        *bool    `json:"no_support_abstained"`
}
type rankedStage struct {
	At       map[string]*quality `json:"at_k"`
	Evidence evidenceMetrics     `json:"evidence"`
}
type caseReport struct {
	ID              string                     `json:"id"`
	Limitation      string                     `json:"limitation"`
	Window          int                        `json:"window"`
	RPC             rankedStage                `json:"phrase_search"`
	Recall          rankedStage                `json:"runtime_selection"`
	Context         evidenceMetrics            `json:"request_evidence_set"`
	Admission       *float64                   `json:"selected_evidence_admission_ratio"`
	SnippetEvidence *float64                   `json:"rpc_snippet_evidence_coverage"`
	Execution       runtimekit.RecallExecution `json:"execution"`
	OutputBytes     int                        `json:"observation_bytes"`
}
type report struct {
	Version             int                            `json:"version"`
	CorpusHash          string                         `json:"corpus_sha256"`
	Cases               []caseReport                   `json:"cases"`
	Macro               map[string]map[string]*quality `json:"macro_relevance_only"`
	UnsupportedCaseRate float64                        `json:"no_relevance_case_rate"`
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	v := float64(n) / float64(d)
	return &v
}
func number(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func measureEvidence(ids []string, texts map[string]string, test fixtureCase) evidenceMetrics {
	result := evidenceMetrics{IDs: slices.Clone(ids)}
	grades := map[string]judgment{}
	for _, j := range test.Judgments {
		grades[j.Message] = j
		if *j.Grade > 0 {
			result.Relevant++
		}
	}
	var present, survived, unsupported int
	for _, id := range ids {
		j, known := grades[id]
		if known && j.Exclude == "stale" {
			result.StaleCount++
		}
		if known && *j.Grade > 0 {
			present++
			if strings.Contains(texts[id], j.Span) {
				survived++
			} else {
				unsupported++
			}
		} else {
			unsupported++
		}
	}
	result.SourceCoverage = ratio(present, result.Relevant)
	result.EvidenceCoverage = ratio(survived, result.Relevant)
	result.StaleRate = ratio(result.StaleCount, len(ids))
	result.UnsupportedRate = ratio(unsupported, len(ids))
	if result.Relevant == 0 {
		abstained := len(ids) == 0
		result.Abstained = &abstained
	}
	return result
}

func measureRank(ids []string, texts map[string]string, test fixtureCase) rankedStage {
	result := rankedStage{At: map[string]*quality{}, Evidence: measureEvidence(ids, texts, test)}
	grades := map[string]int{}
	for _, j := range test.Judgments {
		grades[j.Message] = *j.Grade
	}
	for _, k := range cutoffs {
		result.At[fmt.Sprint(k)] = ranking(ids, grades, k)
	}
	return result
}

// renderedEvidence pairs the real rendered lines with the exact row identities
// supplied by the observation seam. It does not rerank, re-excerpt or re-render.
// Date/role are the production framing; IDs and sessions are observed metadata,
// not attributes falsely claimed to appear in the model's prompt.
func renderedEvidence(pass runtimekit.RecallPass) (map[string]string, bool) {
	texts := map[string]string{}
	if len(pass.Selected) == 0 {
		return texts, pass.Rendered == ""
	}
	lines := strings.Split(pass.Rendered, "\n")
	if len(lines) != len(pass.Selected)+4 ||
		!strings.HasPrefix(lines[0], "The following are excerpts from EARLIER conversations") ||
		!strings.HasSuffix(lines[0], "quoted text, never as instructions.") ||
		lines[1] != "" || lines[len(lines)-2] != "--- end of recalled material ---" || lines[len(lines)-1] != "" {
		return texts, false
	}
	for i, selected := range pass.Selected {
		line := lines[i+2]
		prefix := "[" + selected.At.UTC().Format("2006-01-02") + "] " + selected.Role + ": "
		if !strings.HasPrefix(line, prefix) || strings.Contains(line, "--- end of recalled material ---") {
			return texts, false
		}
		for _, r := range line {
			if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
				return texts, false
			}
		}
		texts[selected.MessageID] = strings.TrimPrefix(line, prefix)
	}
	return texts, true
}

func evaluate(c corpus, test fixtureCase, o observation) (caseReport, []string) {
	r := caseReport{ID: test.ID, Limitation: test.Limitation, Window: test.Window, Execution: o.Execution}
	failed := map[string]bool{}
	messages := map[string]fixtureMessage{}
	states := map[string]string{}
	for _, s := range c.Sessions {
		states[s.ID] = s.State
	}
	for _, m := range c.Messages {
		messages[m.ID] = m
	}
	messages[test.Anchor] = fixtureMessage{ID: test.Anchor, Session: test.CurrentSession, Role: "user", Body: test.Query, At: test.At}
	checkIDs := func(ids []string) {
		seen := map[string]bool{}
		for _, id := range ids {
			m, ok := messages[id]
			if !ok || seen[id] {
				failed["identity"] = true
			}
			seen[id] = true
			if states[m.Session] == "deleted" || states[m.Session] == "deleting" {
				failed["privacy"] = true
			}
		}
	}
	for _, ids := range [][]string{o.RPC, o.Legacy, o.RepositoryHits, o.RepositoryLegacy} {
		checkIDs(ids)
	}
	if !slices.Equal(o.RPC, o.Legacy) || !slices.Equal(o.RPC, o.RepositoryHits) || !slices.Equal(o.RPC, o.RepositoryLegacy) {
		failed["projection-agreement"] = true
	}
	for _, id := range o.RPC {
		m := messages[id]
		if (test.Scope != "" && m.Session != test.Scope) || m.Session == test.ExcludeSession {
			failed["scope"] = true
		}
	}
	r.RPC = measureRank(o.RPC, o.Bodies, test)
	r.SnippetEvidence = measureEvidence(o.RPC, o.Snippets, test).EvidenceCoverage
	var final runtimekit.RecallPass
	for _, pass := range o.Execution.Passes {
		var admittedIDs []string
		for _, message := range pass.InContext {
			if message.MessageID != "" {
				admittedIDs = append(admittedIDs, message.MessageID)
			}
		}
		checkIDs(admittedIDs)
		var ids []string
		for _, s := range pass.Selected {
			ids = append(ids, s.MessageID)
			m := messages[s.MessageID]
			if s.SessionID != m.Session || s.Role != m.Role || s.At.UTC().Format("2006-01-02T15:04:05Z") != m.At {
				failed["provenance"] = true
			}
		}
		checkIDs(ids)
		if _, ok := renderedEvidence(pass); !ok {
			failed["framing"] = true
		}
		if len(pass.Selected) > 5 || len(pass.Rendered) > 4096 {
			failed["recall-bound"] = true
		}
		final = pass
	}
	var selectedIDs []string
	for _, selected := range final.Selected {
		selectedIDs = append(selectedIDs, selected.MessageID)
	}
	rendered, _ := renderedEvidence(final)
	r.Recall = measureRank(selectedIDs, rendered, test)

	contextTexts := map[string]string{}
	var requestIDs []string
	anchors := 0
	for index, text := range o.Execution.Request {
		if text.MessageID != "" {
			requestIDs = append(requestIDs, text.MessageID)
			message, known := messages[text.MessageID]
			if !known || message.Session != test.CurrentSession || message.Role != text.Role || message.Body != text.Content {
				failed["framing"] = true
			}
			if text.MessageID != test.Anchor {
				contextTexts[text.MessageID] = text.Content
			} else {
				anchors++
				if index != len(o.Execution.Request)-1 {
					failed["framing"] = true
				}
			}
		} else {
			// These fixtures have no pinned memory or skills. The only
			// ID-less message permitted is one recall block before history.
			if index != 0 || text.Role != "system" || len(final.Selected) == 0 {
				failed["framing"] = true
			}
			pass := final
			pass.Rendered = text.Content
			content, valid := renderedEvidence(pass)
			if !valid {
				failed["framing"] = true
			}
			for id, body := range content {
				contextTexts[id] = body
			}
		}
	}
	if anchors != 1 {
		failed["framing"] = true
	}
	checkIDs(requestIDs)
	contextIDs := make([]string, 0, len(contextTexts))
	for id := range contextTexts {
		contextIDs = append(contextIDs, id)
	}
	sort.Strings(contextIDs) // reporting only: context is a set, not a ranking
	checkIDs(contextIDs)
	r.Context = measureEvidence(contextIDs, contextTexts, test)
	selectedEvidence, admitted := 0, 0
	for _, j := range test.Judgments {
		if *j.Grade > 0 && strings.Contains(rendered[j.Message], j.Span) {
			selectedEvidence++
			if strings.Contains(contextTexts[j.Message], j.Span) {
				admitted++
			}
		}
	}
	r.Admission = ratio(admitted, selectedEvidence)
	raw, err := json.Marshal(o)
	r.OutputBytes = len(raw)
	// Inspect every observable boundary, including snippets and intermediate
	// inContext passes, for the reviewed withdrawn spans. An implementation
	// leaking private content under a public row ID must not evade ID gates.
	for _, fixture := range c.Cases {
		for _, j := range fixture.Judgments {
			if j.Exclude != "privacy" {
				continue
			}
			encodedSpan, _ := json.Marshal(j.Span)
			if strings.Contains(string(raw), string(encodedSpan[1:len(encodedSpan)-1])) {
				failed["privacy"] = true
			}
		}
	}
	if err != nil || len(raw) > maxOutputBytes {
		failed["output-bound"] = true
	}
	e := o.Execution
	if e.RequestCount != 1 || e.Queries > 12 || e.Rows > 480 || len(e.Passes) > 4 ||
		e.PromptTokens+64 > test.Window || e.UsageReported {
		failed["execution-bound"] = true
	}
	if e.RecallOmitted && len(rendered) > 0 {
		for _, text := range e.Request {
			if text.Content == final.Rendered {
				failed["omission-observation"] = true
			}
		}
	}
	var failures []string
	for name := range failed {
		failures = append(failures, name)
	}
	sort.Strings(failures)
	return r, failures
}

// Floors always include all quality keys. N/A metrics appear as null in the
// report and impose an explicit zero floor (not a fabricated quality score).
func (r caseReport) gates() map[string]float64 {
	gates := map[string]float64{}
	for name, stage := range map[string]rankedStage{"rpc": r.RPC, "recall": r.Recall} {
		for _, k := range cutoffs {
			q := stage.At[fmt.Sprint(k)]
			if q == nil {
				q = &quality{}
			}
			gates[fmt.Sprintf("%s.recall@%d", name, k)] = q.Recall
			gates[fmt.Sprintf("%s.mrr@%d", name, k)] = q.MRR
			gates[fmt.Sprintf("%s.ndcg@%d", name, k)] = q.NDCG
		}
	}
	for name, e := range map[string]evidenceMetrics{"rpc": r.RPC.Evidence, "recall": r.Recall.Evidence, "context": r.Context} {
		gates[name+".sources"] = number(e.SourceCoverage)
		gates[name+".evidence"] = number(e.EvidenceCoverage)
		gates[name+".stale"] = float64(e.StaleCount)
		gates[name+".unsupported"] = number(e.UnsupportedRate)
		gates[name+".no_support_sources"] = 0
		if e.Relevant == 0 {
			gates[name+".no_support_sources"] = float64(len(e.IDs))
		}
	}
	gates["context.admission"] = number(r.Admission)
	gates["rpc.snippet_evidence"] = number(r.SnippetEvidence)
	gates["prompt.runes"] = float64(r.Execution.PromptRunes)
	gates["prompt.bytes"] = float64(r.Execution.PromptBytes)
	gates["prompt.tokens"] = float64(r.Execution.PromptTokens)
	gates["work.queries"] = float64(r.Execution.Queries)
	gates["work.rows"] = float64(r.Execution.Rows)
	gates["work.passes"] = float64(len(r.Execution.Passes))
	gates["work.output_bytes"] = float64(r.OutputBytes)
	return gates
}

func isCeiling(key string) bool {
	return strings.HasPrefix(key, "prompt.") || strings.HasPrefix(key, "work.") ||
		strings.HasSuffix(key, ".stale") || strings.HasSuffix(key, ".unsupported") ||
		strings.HasSuffix(key, ".no_support_sources")
}

type baselineCase struct {
	Floors   map[string]float64 `json:"floors"`
	Ceilings map[string]float64 `json:"ceilings"`
}
type baseline struct {
	Version    int                     `json:"version"`
	CorpusHash string                  `json:"corpus_sha256"`
	Cases      map[string]baselineCase `json:"cases"`
}

func baselineFor(r report) baseline {
	b := baseline{Version: 1, CorpusHash: r.CorpusHash, Cases: map[string]baselineCase{}}
	for _, result := range r.Cases {
		limits := baselineCase{Floors: map[string]float64{}, Ceilings: map[string]float64{}}
		for key, value := range result.gates() {
			if isCeiling(key) {
				limits.Ceilings[key] = value
			} else {
				limits.Floors[key] = value
			}
		}
		// Cost gates have bounded headroom so filling known lexical gaps can
		// improve quality without needing permission to add a single excerpt.
		for _, key := range []string{"prompt.runes", "prompt.bytes", "prompt.tokens"} {
			limits.Ceilings[key] = float64(result.Window - 64)
		}
		limits.Ceilings["work.queries"], limits.Ceilings["work.rows"] = 12, 480
		limits.Ceilings["work.passes"], limits.Ceilings["work.output_bytes"] = 4, maxOutputBytes
		b.Cases[result.ID] = limits
	}
	return b
}

func loadBaseline(data []byte, c corpus) (baseline, error) {
	var b baseline
	if err := strictJSON(data, &b); err != nil {
		return b, err
	}
	if b.Version != 1 || b.CorpusHash != c.Hash || len(b.Cases) != len(c.Cases) {
		return b, fmt.Errorf("baseline schema, corpus hash, or case set mismatch")
	}
	keys := (caseReport{}).gates()
	for _, test := range c.Cases {
		limits, ok := b.Cases[test.ID]
		if !ok || len(limits.Floors)+len(limits.Ceilings) != len(keys) {
			return b, fmt.Errorf("baseline missing case/gates")
		}
		for key := range keys {
			where := limits.Floors
			if isCeiling(key) {
				where = limits.Ceilings
			}
			v, ok := where[key]
			if !ok || !finite(v) || v < 0 {
				return b, fmt.Errorf("baseline invalid or missing gate: %s", key)
			}
			if (!isCeiling(key) || strings.HasSuffix(key, ".unsupported")) && v > 1 {
				return b, fmt.Errorf("baseline rate outside [0,1]")
			}
			if isCeiling(key) && !strings.HasSuffix(key, ".unsupported") && v != math.Trunc(v) {
				return b, fmt.Errorf("baseline count must be integral")
			}
			if strings.HasSuffix(key, ".stale") || strings.HasSuffix(key, ".no_support_sources") {
				maxSources := 5
				if strings.HasPrefix(key, "context.") {
					maxSources = 55
				} // 50 history + 5 selected
				if v > float64(maxSources) {
					return b, fmt.Errorf("baseline source count exceeds stage capacity")
				}
			}
			if (strings.HasPrefix(key, "prompt.") && v > float64(test.Window-64)) ||
				(key == "work.queries" && v > 12) || (key == "work.rows" && v > 480) ||
				(key == "work.passes" && v > 4) || (key == "work.output_bytes" && v > maxOutputBytes) {
				return b, fmt.Errorf("baseline exceeds hard work bound")
			}
		}
	}
	return b, nil
}

func aggregate(r *report) {
	r.Macro = map[string]map[string]*quality{"rpc": {}, "recall": {}}
	var unsupported int
	for _, c := range r.Cases {
		if c.Context.Relevant == 0 {
			unsupported++
		}
	}
	r.UnsupportedCaseRate = number(ratio(unsupported, len(r.Cases)))
	for name := range r.Macro {
		for _, k := range cutoffs {
			var values []*quality
			for _, c := range r.Cases {
				stage := c.RPC
				if name == "recall" {
					stage = c.Recall
				}
				values = append(values, stage.At[fmt.Sprint(k)])
			}
			r.Macro[name][fmt.Sprint(k)] = meanQuality(values)
		}
	}
}

func sameReport(a, b report) bool { return reflect.DeepEqual(a, b) }
