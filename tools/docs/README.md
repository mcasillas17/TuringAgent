# Documentation guards

`docs/NORTH_STAR.md` is the canonical roadmap. `docs/VISION.md` and the former
personal-agent audit remain short historical pointers, not competing status
inventories.

The two historical roadmap stubs must remain short, visible, single-heading
pointers (at most 120 words), not regain heading/list/table inventories. The
selected May 2026 pre-Go/migration records listed in `TestCanonicalRoadmapLinks`
retain their contents behind checked historical notices. Other dated development
records are outside that notice check's scope.

Run the offline guards from the repository root:

```bash
go test ./tools/docs -count=1
```

The existing root CI command, `go test -tags sqlite_fts5 -race ./... -count=1`,
includes this package. After normal Go dependency setup, the checks make no
network requests. No new CI job, external service, Docker, credentials,
model, user vault, Git executable or other checkout is needed. The Go tool
resolves four explicitly declared Go dependency profiles (orchestrator, agent
runtime, mcp-files and mcp-system) offline. These inspected-baseline profiles use
Linux, `sqlite_fts5` for root binaries, no tags for MCP modules, and CGO enabled
only for the orchestrator. Architecture and toolchain are those available to the
Go tool on this host. A missing module-cache entry fails rather than downloading
during the guard; prepare dependencies normally first. This includes
`go mod download` in each module directory: the root suite inspects all three
modules offline. CI's root job explicitly prepares the nested module caches
before testing, so independent module bumps need not match the root build list.
The codegen behavior probe uses Bash and standard file utilities used by the repository
scripts, not protoc or the real generators. `versions_test.go` guards toolchain
pins; the feature-status tests guard the explicit tables delimited
by `status-guard:begin` / `status-guard:end` in the canonical roadmap and
Flutter README.

## What the status guard checks

Each row has a stable **Claim**, a closed **Status** (`shipped` or `pending`)
and a nonempty, human-readable **Scope**. The canonical table
contains all guarded claims; the Flutter guide repeats the client-relevant
ones. Missing, unknown, duplicate or malformed rows and missing evidence fail
closed. Table spacing, backticks/bold around identifiers/statuses and scope
wording (including escaped pipes) may change without changing the contract.
Headers and separators are required. Goldmark parses GFM context so the markers
must be top-level comments around a rendered table, not text hidden in code or
an HTML block. Literal code spans and ordinary angle-bracket prose elsewhere
do not invalidate a table. Goldmark is used here for documentation tests.
The offline build-graph check rejects both direct and transitive Goldmark
dependencies in those four declared entry-point/profile combinations, using
each module's own vendor mode. This is not validation of built container images.
A host-build positive control must find Goldmark in this test package's
dependency graph; a local-module fixture proves `!cgo` dependencies are observed.
Client relevance is explicit per claim.

There is no second list of status strings in Go. A claim's witnesses must
all validate. A still-present **limitation** witness forces `pending` and
cannot justify `shipped`; otherwise the bounded implementation witnesses
justify `shipped`. Missing/changed witnesses yield an error, not an automatic
promotion. Landing a capability requires reconciling the limiting witnesses
with new implementation and behavioral coverage.
The meaning of a witness is explicit: absence of initialization is a
limitation, whereas absence of placeholder destinations supports the wired
workspace claim. A task registered through `pendingTask` is explicitly
remaining work, not evidence that a heading alone proves runtime absence.
A pending claim also requires a concrete, non-document implementation
limitation; a roadmap heading plus unrelated working code is insufficient.
Every claim needs positive evidence from a modeled implementation format;
absence alone or prose in any file format cannot establish a shipped capability.

| Claims | Witnesses and assurance |
| --- | --- |
| `proto-breaking`, `proto-codegen` | An unfiltered PR trigger, separate gating commands (including required-Buf behavioral tests), parsed Buf FILE policy and removal fixture. A black-box probe executes real check.sh with a deterministic fake generator: unchanged output passes; modification, addition, nested addition and removal in each proto/Go/Dart tree must fail. |
| `flutter-search` | Shell entry, search page API call, typed interface, gRPC forwarding, RPC declaration/registration, backend search call, backend and Flutter regression-test declarations. |
| `flutter-workspace` | Shell page wiring, per-page read API call, gRPC forwarding, public service registration and widget-test witnesses; not just `implemented: true`. |
| `mcp-registry` | Page and gRPC calls, RPC contracts, service/repository operations and named regressions for registration, import, enablement, token rotation and tool policies. |
| `mcp-lifecycle` | Positive tools/list and tools/call witnesses in both clients and bundled servers, no initialize/initialized method literals there, and the CON-001 task marker. |
| `remote-model-routing`, `agent-delegation` | Endpoint management, mounted SessionAgentBar get/set/clear routing, service/repository calls, job snapshot, runtime resolver wiring and disclosure/consent regressions. The current adapter performs inference; bounded A2A identifier checks cover current runtime/orchestrator entry points. A2A-001 tracks the separate delegation contract. |
| `github-tools`, `other-integration-tools` | GitHub discovery, dispatch, request builders, argument-bound approval consumption and mutating-policy refusal plus named regressions. IMAP/CalDAV/Notion credential acceptance and the GitHub-only lookup/dispatch boundary are checked separately; INT-001 is not implemented by relabeling a row. |
| `mobile-client`, `mobile-reachability` | Parsed main/debug/profile permissions, which back the guides' debug/profile qualification; responsive layout, loopback Compose publication versus in-container listener, shared-bearer authentication, SEC-001 and MOB-001 markers. |

The witnesses are code, configuration and behavioral-test anchors, **not a
second document containing copied status strings**. Go evidence is parsed
without comments; named functions are checked within their own declarations.
Go/Dart/proto code-token comparisons ignore whitespace and C-style comments.
Dart/proto regex predicates are refused until a context-aware implementation
exists; those formats use token witnesses rather than raw-text regexes.
YAML witnesses use parsed mappings/steps; shell witnesses use line-anchored
patterns and the codegen behavior probe, not a code lexer that misunderstands
`#`. Token predicates use an allowlist of modeled formats; an unknown format
must get a parser rather than silently reusing C-style comment handling.
Android manifest permission checks parse XML, including SDK-23 declarations, rather
than matching commented examples. Scope text needs a printable base character,
not only whitespace, formatting marks or default-ignorable Unicode.

## Limits

This is a bounded repository guard, not an automatic feature detector:

- The witness specification is trusted, human-reviewed input. Deleting or
  weakening a limitation or pending-task witness can change which status is
  accepted without adding capability. A green run cannot detect its own
  assertions being weakened; witness edits need review against the behavioral
  coverage that justifies them.
- Dependency profiles are explicit, trusted inputs too. The guard does not
  interpret Dockerfile stages, build targets, arbitrary Go flags or Compose
  build-argument/path overrides. A recipe change requires reviewing the
  profiles and their entry points; a green profile check does not establish
  that an independently built image used that profile. General Docker/Compose
  build interpretation is outside this guard's scope.
- It verifies explicit status cells, not arbitrary natural-language prose,
  scope semantics, screenshots, every roadmap bullet, or every README.
- A regression-test witness proves that the named coverage is present, not
  that it passes. Run the existing Go/Flutter/proto suites for behavioral
  assurance; changing assertions while keeping a name may need human review.
- The codegen probe is a deliberate exception: it executes the checker against
  synthetic output drift. It neither regenerates this checkout nor proves that
  the real protoc/plugins are deterministic; the pinned CI codegen job does that.
- Markdown checks use GFM structure, not browser CSS or arbitrary HTML DOM
  visibility. Historical-pointer checks constrain structure, not prose meaning.
- Canonical-reference checks require a readable link to the canonical file;
  they do not validate heading fragments. Anchor changes and explanatory
  prose in unguarded guides require documentation review alongside the inventory.
- Lexical wiring witnesses cannot prove reachability through every branch,
  successful live model/provider calls, deployed configuration, complete MCP
  conformance or production mobile behavior.
- Named private call sites are deliberately refactor-sensitive: moving or
  renaming one requires reconciling its witness, even when behavior is
  unchanged. Whitespace is not significant.
- Workflow checks accept omitted controls or literal `if: true` /
  `continue-on-error: false`. Other expressions, filtered PR triggers or job
  dependencies require reconciliation rather than speculative evaluation.
  Step/job/workflow run defaults are resolved in order; custom execution shells
  or non-root working directories likewise require reconciliation.
- Absence checks are deliberately limited to named current transport files.
  Future adapters in other files require explicit review and witness updates.
  Task markers track the remaining scope; they alone never prove a feature
  shipped or that all possible implementations are absent.
- `shipped` is scoped to the row's capability. A real management page does not
  certify every provider, and a registry does not certify its protocol.
- The test runs against this checkout. It cannot determine whether a branch
  is merged; the roadmap separately records its inspected mainline baseline
  and the fact that DOC-001's implementation follows that baseline.

## Reconciling a failure

A diagnostic names the document, known claim, evidence path/function and
witness index (or table row ordinal), the mismatch, and the reconciliation
needed. It does not print file contents, credentials or unknown entry text.

1. Inspect the named witness in `status_claims_test.go` and the implementation
   it references. Do not fix a broken call by downgrading its documentation
   to `pending`: missing or moved evidence is itself an error.
2. For a real capability change, add/run behavioral coverage for the new
   boundary first, then update the scoped status and its implementation
   witnesses together. Review related README/setup prose and additional
   narrative/scenario tables as well; only the explicit status cells are
   machine-checked. Active guides must retain links to the canonical inventory.
3. Keep unfinished work explicit in the canonical task contract. Adding
   Android network permission alone, for example, does not deliver device
   identity or secure reachability.
4. Run the documentation guard and the affected behavioral suites. The full
   contributor verification matrix remains in `CLAUDE.md`; CI also runs vet
   and explicit workflow checks.

`status_fixtures_test.go` builds isolated in-memory filesystem fixtures for
false shipped/pending/placeholder claims, malformed or missing entries,
contradictions, missing evidence, implementation drift, comment-only evidence
and harmless formatting/wording changes. `status_history_test.go` covers
historical notices, pointers and link contexts. `status_codegen_test.go` uses
temporary directories (including a fixture-local `TMPDIR`) and a minimal
environment to expose cleared flags, skipped loops, early success, restored
output, omitted Dart checks and late generation. `status_dependency_test.go`
checks the actual runtime dependency graphs with module downloads and workspace
overrides disabled. No fixture mutates the checkout.

`status_preparation_test.go` checks README.md, tech-stack.md, the integration
checklist, CLAUDE.md and the existing verify reference. Any fenced block there
that runs the root Go suite must prepare both nested module caches first.
An active guide that cites the canonical roadmap must be listed in
`TestCanonicalRoadmapLinks` so its canonical-file reference remains checked.
