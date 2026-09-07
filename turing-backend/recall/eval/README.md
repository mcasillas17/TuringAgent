# Deterministic recall evaluation

MEM-003 measures the current retrieval path without changing it. It is a local,
synthetic evidence benchmark, not an evaluation of generated answers, model
reasoning, semantic memory, or the separate EVAL-001 whole-agent benchmark.
See the [canonical roadmap](../../../docs/NORTH_STAR.md#5-mem-003---deterministic-recall-evaluation)
and [retrieval architecture](../../../docs/architecture/session-recall.md).

## Run and reproduce

From the repository root, with the existing Go dependencies available, Go 1.25+
and a working CGO C toolchain:

```bash
go test -tags sqlite_fts5 ./turing-backend/recall/eval -count=1
```

This loads `testdata/corpus.v1.json`, creates temporary migrated SQLite databases,
executes the real retrieval path, and compares each case against
`testdata/baseline.v1.json`. The existing root CI race suite includes this package
and fails on regressions; no separate job or duplicate evaluation step is needed.
The suite also runs hand-computed metric tests, malformed-input tests,
controlled negative gates, and two independent executions whose reports and
baseline candidates must agree exactly.

No credentials, user data, Docker, running Ollama, external services or model
judge are required. SessionService uses in-memory gRPC connections. A capturing
provider replaces only inference, not request assembly or token estimation.

For the detailed deterministic JSON report, choose an output path that does
not already exist:

```bash
TURING_RECALL_REPORT=/tmp/recall-report.json \
  go test -tags sqlite_fts5 ./turing-backend/recall/eval \
  -run '^TestEvaluation$' -count=1
```

Unset `TURING_RECALL_BASELINE_CANDIDATE` for a normal comparison run. Both output
variables use exclusive creation and mode 0600: an existing path is an error,
not an overwrite. Reports contain only the checked-in synthetic conversations.

## Three distinct stages

| Report stage | What actually runs | What the result means |
| --- | --- | --- |
| `phrase_search` | Real repository search and SessionService, in both HITS and legacy formats, with the fixture's literal `phrase`, scope and limit 5. | Ranked source retrieval. All four projections must agree; snippets have a separate span-coverage measurement. |
| `runtime_selection` | Real runtime legacy client and `Recaller.PrepareRecall`, called by `GeneralAssistant.Execute` with the fixture's natural-language `query`. | The final observed selection/render pass, before optional recall admission. Every actual convergence pass and its admitted context is retained in `execution.passes`. |
| `request_evidence_set` | The request passed to the capturing provider's `StreamChat`, after the real assistant's budgeting and convergence. | Evidence that reached the provider boundary, combining admitted history and recalled excerpts, excluding the live question. This is a set, not a fabricated ranking. |

The fixture's phrase and natural-language query are intentionally separate
inputs. These are observations of two product paths, not an assertion that the
RPC and runtime perform the same query. No `SearchHit.score` magnitude enters
the metrics: only returned order is used.

The runtime still searches individual terms, deduplicates by identity/context,
orders earlier-session candidates before omitted current-session candidates,
and applies its existing excerpt budgets. Within each scope it prefers more
distinct matched terms, then recency, then stable encounter order. The evaluator
does not replace this logic. An optional instance-scoped observer receives
copied selections after rendering; it is unset in production.

The rendered block contains date and role attribution, not message/session IDs.
Those identities are observation metadata from the real selected rows.
The final-request framing gate permits only one ID-less system recall block,
before history and the live question, in these fixtures with no pinned memory
or skills. Persisted history and the final live anchor retain their own
identity, role and content.

## Corpus and labels

Version 1 contains **8 sessions, 76 persisted messages and 21 cases**. Each
case seeds its own database, including its own live anchor; cases cannot
pollute one another. IDs are stable, timestamps are explicit canonical UTC
seconds, and insertion order defines per-session sequence. Active/archived,
withdrawing and deleted sessions coexist. Setup first confirms that the
withdrawn messages are searchable, then uses the real withdrawal/deletion
lifecycle and reopens the database before measuring.

Each case explicitly supplies `judgments`, including `[]` for a reviewed
no-support case. A judgment names an existing source, an exact source `span`,
a grade and a validity interval:

- Grade **2**: direct evidence; **1**: supporting evidence, including individual
  pieces required by multi-session questions; **0**: irrelevant or excluded.
- Unjudged returned rows have grade 0. Distinct rows with identical content
  remain distinct identities.
- `valid_from` is inclusive and `valid_until`, when present, is exclusive.
  `as_of` determines label validity; it is not an invented production time
  filter. `exclude: stale` covers both expired and not-yet-valid evidence.
- `exclude: privacy` requires a withdrawing/deleted source and a labelled span.
  Those IDs and spans must not appear anywhere in the observations.
  `scope` exclusions describe retrieval scope rather than temporal invalidity.
- Ground truth is hand-authored, never inferred from the actual retrieval
  output. Keep it independent when updating expected metrics.

| Cases | Boundary exercised |
| --- | --- |
| `exact-id`, `literal-phrase`, `literal-operators` | Identifiers, exact phrases, and input that looks like FTS operators. |
| `paraphrase-miss` | Bike/underground versus bicycle/cellar has no lexical bridge. |
| `corrected-current`, `historical-asof` | Old and corrected facts, including a correction invalid at the requested historical time. |
| `multi-session-synthesis` | Both the date and location must be retrieved from different sessions; no answer synthesis is judged. |
| `cjk-whole`, `cjk-partial`, `cjk-short` | Full contiguous CJK match, substring miss, and a two-character RPC match dropped by runtime term extraction. |
| `hostile-history` | Forged instruction lines, control characters and an embedded end marker stay bounded quoted evidence. |
| `deletion-visibility` | Active and archived sources remain visible; withdrawing and deleted sources remain excluded after reopen. |
| `no-support`, `unsupported-overlap` | Empty retrieval versus irrelevant lexical overlap, without pretending either measures answer hallucination. |
| `same-content-identities` | Current-session user/assistant twins are admitted as history, while the earlier identical row remains recallable. |
| `older-than-fetch-window`, `budget-convergence` | Real history-fetch cutoff and iterative recovery of history excluded by the context budget. |
| `excerpt-early-evidence`, `excerpt-tail-lost` | The same retrieved source can retain early evidence while losing a later required span. |
| `whole-recall-omission` | Selection succeeds but the complete recalled block cannot fit the final request. |
| `query-work-cap` | The first-six-term policy and bounded query work, without asserting semantic relevance. |

## Metric definitions

For each ranked stage and **k = 1, 3, 5**, let `R` be all positively graded
source IDs in the case, and let `g_i` be the grade of the ID at one-based rank
`i`. Duplicate IDs consume a rank but earn no second credit; duplicates also
fail the independent identity gate. Returned order breaks all ties:
repository ties are `message_id ASC` after BM25, and runtime ties follow its
production ordering. There is no average-rank or random tie handling.

| Metric | Formula / empty case |
| --- | --- |
| Recall@k | Distinct positive IDs in the first k results divided by `|R|`. Missing results earn no credit and do not shrink the denominator. |
| MRR@k | Reciprocal rank of the first positive ID within k, or 0 when none appears. Reported macro MRR is the mean of these per-case reciprocal ranks. |
| nDCG@k | `DCG = sum((2^g_i - 1) / log2(i + 1))` through k; divide by ideal DCG from all positive grades sorted descending through k. |
| Source coverage | Positive source IDs present divided by `|R|`, regardless of whether their required spans survived. |
| Evidence coverage | Positive source IDs whose labelled span survives in that stage's text divided by `|R|`. One exact span per judgment; no semantic matching. |
| Selected-evidence admission | Positive spans surviving the final selection/render pass that also occur in the request, divided by all positive spans surviving that pass. |
| Stale-use | Count of returned source IDs labelled `stale`; rate divides by the number of returned IDs. This is temporal contamination of observed sources, not an assertion that a model used a stale fact in an answer. |
| Unsupported-evidence rate | Returned IDs lacking positive span evidence divided by returned IDs. In context this includes irrelevant history, not just recall. |
| No-support abstention | Only for cases with no positive judgments: true iff the stage contains no source IDs. The live question is not evidence. |

If `R` is empty, all ranking/source/evidence quality values are **null**, not
perfect scores. Such cases are excluded from quality macro means. With positive
judgments but no results, quality is 0. Ratios with no returned or selected
items are null, not division by zero. `no_relevance_case_rate` reports corpus
composition (2/21), not a success metric.

Macros give each supported case equal weight; there is no weighting by session
size. The same ground truth is used at all stages. Therefore history deduplication
can lower runtime-selection recall without harming final context coverage:
`same-content-identities` selects 1/3 sources but admits all three through
history plus recall. Context IDs are sorted only to make set reports stable.

## Baseline and regression policy

The checked-in version-1 baseline is SHA-256-bound to the **exact corpus
bytes**. It records per-case floors for ranking, source/span coverage, snippet
coverage and admission, and ceilings for stale/unsupported/no-support source
counts and cost. All keys must be present, including explicit zero constraints.
Comparisons use a `1e-12` numerical tolerance. Macro improvements cannot offset
a per-case regression.

The measured version-1 quality macros at k=5 are:

| Stage (19 supported cases) | Recall@5 | MRR@5 | nDCG@5 |
| --- | --- | --- | --- |
| Phrase search | 0.894737 | 0.815789 | 0.835604 |
| Runtime selection | 0.754386 | 0.710526 | 0.706091 |

These selection metrics do not imply that all evidence reached the final
request. The late excerpt and whole-omission cases have zero final evidence
coverage despite finding their sources. Temporal cases admit one invalid
source each. Paraphrase and partial-CJK misses remain labelled, and short CJK
works at the RPC stage but not runtime recall. No fixtures are skipped.

Quality floors permit improvement; lexical limitations are labels, not expected
failures that turn green results red. Explicit nonzero temporal/unsupported
ceilings preserve today's limitations, while independent hard gates always
reject privacy/deletion leakage, broken provenance/identity, inconsistent
search projections, invalid request framing and violated execution bounds.
Negative tests inject each kind of regression and require the named gate to
fail, even when average quality could improve.

To propose an update, use a new candidate path:

```bash
TURING_RECALL_BASELINE_CANDIDATE=/tmp/recall-candidate.json \
  go test -tags sqlite_fts5 ./turing-backend/recall/eval \
  -run '^TestEvaluation$' -count=1
diff -u turing-backend/recall/eval/testdata/baseline.v1.json /tmp/recall-candidate.json
```

Candidate mode **skips the old baseline comparison**, but still runs all hard
gates. It never overwrites the baseline. Review corpus labels and each changed
floor/ceiling before deliberately copying the candidate to
`testdata/baseline.v1.json`; rerun the normal command without candidate mode.
Even corpus-only formatting changes require a hash update. Do not automatically
accept a candidate just because it matches a new implementation.

## Units, bounds and latency

`execution.prompt_runes` counts Unicode scalar values in message content;
`prompt_bytes` counts UTF-8 bytes of that same content. Neither includes the
surrounding wire envelope. `prompt_estimated_tokens` uses the real Ollama
serialized-request estimator, including tools and protocol overhead, not a
characters-divided-by-four proxy. No inference runs: `usage_reported` is false,
and no provider-reported usage count is fabricated.

The current runtime's `MaxChars` budget is implemented in **bytes** with
UTF-8-safe cuts, despite the field name. The evaluator leaves it unchanged,
observes the final rendered text, and distinguishes source presence from
exact span survival. These fixtures reserve 64 output tokens and vary the
configured context window, including 1700 and 400 for budget pressure.

Input limits are 1 MiB JSON, nesting depth 24, 64 cases, 64 sessions, 256
messages, 16 KiB per message, 512 bytes per query/phrase, and 256 judgments per
case. IDs, timestamps, grades, references, intervals, lifecycle and mandatory
categories are validated. Duplicate keys (including escaped spellings),
case aliases, unknown fields, nulls and trailing JSON fail clearly.

Execution hard limits are one provider dispatch, 12 runtime searches, 480
returned rows, four observed rank/render passes, five selected excerpts per
pass, 4096 rendered bytes per pass and 512 KiB serialized observation per case.
Written report/candidate JSON is capped at 1 MiB. Prompt rune/byte/estimated-token
ceilings have conservative headroom up to the window minus the 64-token reserve;
these are separate ceilings in their own units, not a unit conversion.
Cost headroom permits better retrieval without freezing an empty result.
Exceeding a bound requires a deliberate policy/code review, not a timing waiver.

Measure latency and allocations separately:

```bash
go test -tags sqlite_fts5 ./turing-backend/recall/eval \
  -run '^$' -bench '^BenchmarkRecall$' -benchmem -count=1
```

Benchmarks cover phrase RPC and complete assistant execution for exact lookup,
the six-term work cap and budget convergence. They exclude migration/seeding/
withdrawal/reopen setup and include the actual measured path thereafter.
Output includes `ns/op`, `B/op`, allocations, and runtime query/row/token counts.
Record host/toolchain and compare like-for-like runs; no machine-specific
millisecond threshold is committed. Thirty-second setup/execution contexts
bound a stuck fixture, while deterministic work/output limits are the portable
regression policy. Existing production recall/convergence deadlines are not
disabled; exceptional local overload can still surface as an evaluation failure.
