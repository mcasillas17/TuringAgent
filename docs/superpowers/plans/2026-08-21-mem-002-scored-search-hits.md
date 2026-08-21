# MEM-002 Scored Search Hits Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Return negotiated, higher-is-better SQLite FTS5 search hits with safe bounded snippets, centered on the match whenever FTS5's snippet window could mark one, while preserving the exact legacy message response for old callers.

**Architecture:** Add an append-only protobuf response-format negotiation so unspecified and legacy requests use the existing message-only repository path, while hit requests use a new search-specific repository projection. Keep score normalization, marker parsing, and snippet sanitization in a focused repository file; keep service negotiation, runtime compatibility, Dart mapping, and UI rendering at their existing boundaries.

**Tech Stack:** Proto3/gRPC, Go 1.23+, `database/sql`, SQLite FTS5 through `github.com/mattn/go-sqlite3`, Dart protobuf/grpc, Flutter, Go `testing`, Flutter test.

---

## Starting point and constraints

- Approved design:
  `docs/superpowers/specs/2026-08-20-mem-002-scored-search-hits-design.md`
  at `6f46448efb846fef26a7951e44238504b3f51179`.
- This branch normally merged `origin/main` at `183cfe244e89b5f11a208b47b5f4d7b73e90d38f`
  in merge commit `137d1238`.
- Landed TUR-008 leaves `SearchMessagesRequest` field 5 and
  `SearchMessagesResponse` field 2 unused.
- Keep the existing search RPC, query phrase semantics, session scope,
  exclusion, limit behavior, and message ordering.
- Do not implement MEM-003 metrics, MEM-004 structured multi-term search,
  MEM-016 tokenization changes, semantic retrieval, or a UI relevance redesign.
- Every changed behavior follows RED -> GREEN. Do not batch implementation ahead
  of its failing test.

## File map

| File | Responsibility |
| --- | --- |
| `proto/turing/v1/sessions.proto` | Add response format, canonical wire `SearchHit`, and append-only fields 5/2. |
| `gen/turing/v1/go/turing/v1/sessions*.pb.go` | Pinned generated Go contract. |
| `turing-client/turing_app/lib/generated/turing/v1/sessions*.dart` | Pinned generated Dart contract. |
| `turing-backend/tests/proto_contract_test.go` | Pin field numbers, kinds, and enum values. |
| `turing-backend/orchestrator-go/internal/repository/search_hits.go` | Score normalization, exact marker grammar/parser, sanitization, and hit query. |
| `turing-backend/orchestrator-go/internal/repository/search_hits_test.go` | Focused score, marker, snippet, lifecycle, and driver canaries. |
| `turing-backend/orchestrator-go/internal/repository/sessions.go` | Retain legacy query and share predicate construction without changing its result. |
| `turing-backend/orchestrator-go/internal/repository/sessions_test.go` | Preserve legacy query behavior and parity fixtures. |
| `turing-backend/orchestrator-go/internal/service/sessions/service.go` | Resolve response format, map one response array, and emit content-free invariant logs. |
| `turing-backend/orchestrator-go/internal/service/sessions/service_test.go` | Format negotiation, parity, errors, logs, limits, lifecycle, and transport-size regression. |
| `turing-backend/agent-runtime-go/internal/orchestrator/client_test.go` | Prove runtime leaves response format unspecified. |
| `turing-client/turing_app/lib/models/search_hit.dart` | Add nullable score/snippet for old-server fallback. |
| `turing-client/turing_app/lib/models/grpc_mappers.dart` | Strict canonical-hit and legacy-message mappers with value-free errors. |
| `turing-client/turing_app/lib/networking/grpc_client.dart` | Request hit format, prefer hits, and fall back only on a successful old-server response. |
| `turing-client/turing_app/lib/features/search/search_screen.dart` | Render canonical snippet; retain legacy rune-safe excerpt. |
| `turing-client/turing_app/test/models/grpc_mappers_test.dart` | Mapping and value-free malformed-hit errors. |
| `turing-client/turing_app/test/networking/grpc_client_test.dart` | Request negotiation, old-server fallback, no concatenation, and no error retry. |
| `turing-client/turing_app/test/features/search/search_screen_test.dart` | Canonical snippet rendering, semantics, Unicode, archived hits, and chronological grouping. |
| `docs/architecture/session-recall.md` | Document score, snippet, response negotiation, lifecycle, and migration. |
| `docs/architecture/2026-08-18-personal-agent-audit.md` | Mark MEM-002 shipped without claiming MEM-003/MEM-004. |

### Task 1: Pin the additive protobuf contract

**Files:**
- Modify: `turing-backend/tests/proto_contract_test.go:47-71`
- Modify: `proto/turing/v1/sessions.proto:117-126`
- Regenerate: `gen/turing/v1/go/turing/v1/sessions.pb.go`
- Regenerate: `gen/turing/v1/go/turing/v1/sessions_grpc.pb.go`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/sessions.pb.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbenum.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbgrpc.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbjson.dart`

- [ ] **Step 1: Extend the descriptor test first**

Add these assertions to `TestSearchMessagesProtoContract`:

```go
assertProtoField(t, request, "exclude_session_id", 4, protoreflect.StringKind, false, "")
assertProtoField(t, request, "response_format", 5, protoreflect.EnumKind, false, "")

format := file.Enums().ByName("SearchMessagesResponseFormat")
if format == nil {
	t.Fatal("SearchMessagesResponseFormat is missing")
}
for name, number := range map[protoreflect.Name]protoreflect.EnumNumber{
	"SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED":     0,
	"SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES": 1,
	"SEARCH_MESSAGES_RESPONSE_FORMAT_HITS":            2,
} {
	value := format.Values().ByName(name)
	if value == nil || value.Number() != number {
		t.Fatalf("%s = %v, want number %d", name, value, number)
	}
}

hit := file.Messages().ByName("SearchHit")
assertProtoField(t, hit, "message", 1, protoreflect.MessageKind, false, "turing.v1.Message")
assertProtoField(t, hit, "score", 2, protoreflect.DoubleKind, false, "")
assertProtoField(t, hit, "snippet", 3, protoreflect.StringKind, false, "")
assertProtoField(t, response, "hits", 2, protoreflect.MessageKind, true, "turing.v1.SearchHit")
```

- [ ] **Step 2: Add RED cross-version wire tests**

In `proto_contract_test.go`, add:

```go
func TestSearchMessagesNewRequestIsReadableByLegacyDescriptor(t *testing.T)
func TestSearchMessagesNewResponseIsReadableByLegacyDescriptor(t *testing.T)
func TestSearchMessagesLegacyResponseIsReadableByNewBindings(t *testing.T)
```

For the first two tests, clone the generated file descriptor with
`protodesc.ToFileDescriptorProto`, remove request field 5 or response field 2
from the clone, build it with:

```go
legacyFile, err := protodesc.NewFile(cloned, protoregistry.GlobalFiles)
if err != nil {
	t.Fatal(err)
}
legacyMessage := dynamicpb.NewMessage(
	legacyFile.Messages().ByName("SearchMessagesRequest"),
)
```

Marshal a new generated request containing `HITS`, unmarshal into the legacy
dynamic request, and assert fields 1-4 remain exact while the descriptor has no
field 5. Marshal a new generated response containing one hit, unmarshal into
the legacy dynamic response, and assert it exposes no field 2. Finally marshal
`turingv1.SearchMessagesResponse{Messages: []*turingv1.Message{{
MessageId: "legacy",
}}}`, unmarshal into the new generated type, and assert message ID `legacy`
remains exact and hits are empty.

- [ ] **Step 3: Run contract tests and verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/tests \
  -run '^TestSearchMessages' -count=1
```

Expected: FAIL because the additive enum/message/fields do not exist.

- [ ] **Step 4: Add the exact append-only schema**

Insert before `SearchMessagesRequest`:

```proto
// Selects one response representation so hit metadata does not duplicate
// unbounded message bodies.
enum SearchMessagesResponseFormat {
  // Resolves to LEGACY_MESSAGES for old callers.
  SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED = 0;
  // Returns only SearchMessagesResponse.messages.
  SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES = 1;
  // Returns only SearchMessagesResponse.hits.
  SEARCH_MESSAGES_RESPONSE_FORMAT_HITS = 2;
}

message SearchHit {
  Message message = 1;

  // Finite and non-negative. Higher means a more relevant match within the
  // same SearchMessages response. Not comparable across queries or snapshots.
  double score = 2;

  // Bounded single-line plain text selected from message.content around the
  // match. Contains no server-added markup and must not be treated as HTML.
  string snippet = 3;
}
```

Append fields without changing 1-4 or response field 1:

```proto
message SearchMessagesRequest {
  string query = 1;
  string session_id = 2; // optional scope; empty = all sessions
  int32 limit = 3;
  string exclude_session_id = 4; // optional exclusion; applied after session_id scope
  // Unsupported numeric values are rejected with InvalidArgument.
  SearchMessagesResponseFormat response_format = 5;
}

message SearchMessagesResponse {
  // Legacy compatibility field. New consumers request HITS.
  repeated Message messages = 1;
  repeated SearchHit hits = 2;
}
```

- [ ] **Step 5: Install and select protoc 34.1**

The machine's Homebrew `protoc` is 35.1, while generation hard-requires 34.1.
Install the official arm64 macOS archive outside the repository:

```bash
TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
PROTOC_ROOT="$TOOLS_ROOT/protoc-34.1"
mkdir -p "$PROTOC_ROOT"
gh release download v34.1 --repo protocolbuffers/protobuf \
  --pattern 'protoc-34.1-osx-aarch_64.zip' \
  --dir "$PROTOC_ROOT" --clobber
unzip -qo "$PROTOC_ROOT/protoc-34.1-osx-aarch_64.zip" -d "$PROTOC_ROOT"
export PATH="$PROTOC_ROOT/bin:$(go env GOPATH)/bin:$PATH"
test "$(protoc --version)" = "libprotoc 34.1"
```

Expected: the version assertion exits 0. Keep this exact tool directory through
Task 10, then remove it as targeted cleanup.

- [ ] **Step 6: Regenerate with the pinned toolchain**

Run:

```bash
TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
export PATH="$TOOLS_ROOT/protoc-34.1/bin:$(go env GOPATH)/bin:$PATH"
tools/proto/generate.sh
```

Expected: exit 0 with only the listed generated session files changed.
Prerequisites are protoc 34.1, `protoc-gen-go` 1.36.11,
`protoc-gen-go-grpc` 1.6.2, and globally activated Dart `protoc_plugin` 22.5.0.

- [ ] **Step 7: Verify GREEN and compatibility**

Run:

```bash
TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
export PATH="$TOOLS_ROOT/protoc-34.1/bin:$(go env GOPATH)/bin:$PATH"
go test -tags sqlite_fts5 ./turing-backend/tests \
  -run '^TestSearchMessages' -count=1
tools/proto/check.sh
test "$(buf --version)" = "1.72.0"
tools/proto/breaking.sh origin/main
```

Expected: all four commands exit 0.

- [ ] **Step 8: Commit the contract**

```bash
git add proto/turing/v1/sessions.proto \
  gen/turing/v1/go/turing/v1/sessions.pb.go \
  gen/turing/v1/go/turing/v1/sessions_grpc.pb.go \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pb.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbenum.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbgrpc.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbjson.dart \
  turing-backend/tests/proto_contract_test.go
git commit -m "feat(proto): add scored search hits"
```

### Task 2: Implement exact score and marker primitives

**Files:**
- Create: `turing-backend/orchestrator-go/internal/repository/search_hits.go`
- Create: `turing-backend/orchestrator-go/internal/repository/search_hits_test.go`

- [ ] **Step 1: Write RED table tests for score normalization**

Create `search_hits_test.go` in package `repository` with:

```go
func TestNormalizeSearchScore(t *testing.T) {
	for _, test := range []struct {
		name    string
		raw     float64
		want    float64
		wantErr bool
	}{
		{name: "negative", raw: -1.5, want: 1.5},
		{name: "positive zero", raw: 0, want: 0},
		{name: "negative zero", raw: math.Copysign(0, -1), want: 0},
		{name: "positive", raw: 0.1, wantErr: true},
		{name: "nan", raw: math.NaN(), wantErr: true},
		{name: "positive infinity", raw: math.Inf(1), wantErr: true},
		{name: "negative infinity", raw: math.Inf(-1), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSearchScore(test.raw)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidSearchScore) {
					t.Fatalf("error = %v, want ErrInvalidSearchScore", err)
				}
				return
			}
			if err != nil || got != test.want || math.Signbit(got) {
				t.Fatalf("normalizeSearchScore(%v) = %v, %v", test.raw, got, err)
			}
		})
	}
}
```

- [ ] **Step 2: Write RED tests for exact marker construction**

Pass deterministic entropy through the helper's `io.Reader` parameter; do not
mutate package globals:

```go
func TestNewSearchSnippetMarkersUsesExactASCIIGrammar(t *testing.T) {
	start, end, err := newSearchSnippetMarkers(
		bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantNonce := strings.Repeat("ab", 16)
	if start != "[[TURING-FTS5-SNIPPET-START:v1:"+wantNonce+"]]" {
		t.Fatalf("start = %q", start)
	}
	if end != "[[TURING-FTS5-SNIPPET-END:v1:"+wantNonce+"]]" {
		t.Fatalf("end = %q", end)
	}
	if len(start) != 65 || len(end) != 63 || start == end {
		t.Fatalf("marker lengths/distinctness = %d/%d/%v", len(start), len(end), start == end)
	}
	if strings.IndexByte(start, 0) >= 0 || strings.IndexByte(end, 0) >= 0 {
		t.Fatal("marker contains NUL")
	}
}
```

Add a second test where the supplied reader returns 15 bytes plus
`io.ErrUnexpectedEOF`; expect `ErrSearchMarkerEntropy`.

Define the fixed test helper used throughout this file:

```go
func fixedSearchMarkers() (string, string) {
	nonce := strings.Repeat("a", 32)
	return "[[TURING-FTS5-SNIPPET-START:v1:" + nonce + "]]",
		"[[TURING-FTS5-SNIPPET-END:v1:" + nonce + "]]"
}
```

- [ ] **Step 3: Write RED marker scanner tests**

Use fixed marker values and test exact state transitions. The reference parser
below is the test-only oracle: it materializes the payload, which is exactly
what production must not do, so it stays in a `_test.go` file and gives the
scanner something independent to be differentially checked against.

```go
func TestParseMarkedSearchSnippet(t *testing.T) {
	start := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("a", 32) + "]]"
	end := "[[TURING-FTS5-SNIPPET-END:v1:" + strings.Repeat("a", 32) + "]]"

	valid := []byte("before " + start + "needle" + end + " middle " +
		start + "second" + end + " after")
	parsed, err := parseMarkedSearchSnippet(valid, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if string(parsed.text) != "before needle middle second after" {
		t.Fatalf("text = %q", parsed.text)
	}
	if !reflect.DeepEqual(parsed.matches, []byteSpan{{7, 13}, {21, 27}}) {
		t.Fatalf("matches = %+v", parsed.matches)
	}
}
```

Table-test missing end, end in text, start in match, reversed order, trailing
match, and an empty snippet; each must return `ErrInvalidSearchSnippetMarkers`.
Add a separate success test for a snippet with zero complete markers: FTS5's
32-token window cannot mark a phrase wider than itself, so an unmarked fragment
is a legitimate result whose payload must come back unchanged with no match
spans. Add invalid UTF-8 directly around markers and prove marker recognition
runs before repair.

Table the same states directly against `scanMarkedSearchSnippet`, asserting the
emitted chunk stream rather than a reassembled payload: emission order, an empty
match emitted as a boundary with no payload, adjacent pairs emitting no empty
text between them, and every chunk being a subslice of the input. A wrapper can
hide an emission bug that a reassembled payload happens to smooth over.

- [ ] **Step 4: Run primitive tests and verify RED**

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'Test(NormalizeSearchScore|NewSearchSnippetMarkers|ParseMarkedSearchSnippet)' \
  -count=1
```

Expected: FAIL with undefined primitive symbols.

- [ ] **Step 5: Implement the primitives**

Create focused types and errors:

```go
var (
	ErrInvalidSearchScore           = errors.New("invalid search score")
	ErrSearchMarkerEntropy          = errors.New("search marker entropy unavailable")
	ErrSearchSnippetMarkerCollision = errors.New("search snippet marker collision")
	ErrInvalidSearchSnippetMarkers  = errors.New("invalid search snippet markers")
	ErrInvalidSearchSnippet         = errors.New("invalid search snippet")
)

const (
	searchSnippetMaxRunes = 200
	searchSnippetMaxBytes = 800
)

type byteSpan struct {
	start int
	end   int
}

type runeSpan struct {
	start int
	end   int
}

type markedSearchSnippetShape struct {
	pairs        int
	payloadBytes int
}

func normalizeSearchScore(raw float64) (float64, error) {
	if math.IsNaN(raw) || math.IsInf(raw, 0) || raw > 0 {
		return 0, ErrInvalidSearchScore
	}
	score := -raw
	if score == 0 {
		score = 0
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0, ErrInvalidSearchScore
	}
	return score, nil
}

func newSearchSnippetMarkers(entropyReader io.Reader) (string, string, error) {
	var entropy [16]byte
	if _, err := io.ReadFull(entropyReader, entropy[:]); err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrSearchMarkerEntropy, err)
	}
	nonce := hex.EncodeToString(entropy[:])
	return "[[TURING-FTS5-SNIPPET-START:v1:" + nonce + "]]",
		"[[TURING-FTS5-SNIPPET-END:v1:" + nonce + "]]",
		nil
}
```

Implement `scanMarkedSearchSnippet` as the two-state scanner from the spec,
over `string` and using `strings.Index`. It must search for both next exact
markers, reject the marker invalid in the current state, hand every stretch of
non-marker bytes to `emit` as a zero-copy subslice tagged with its state, count
pairs and payload bytes instead of accumulating them, and return typed errors
without including source values. A `nil` emit makes it a validation-only pass.
Zero complete markers is a success with no match spans, not a failure; only a
completely empty snippet still fails, because FTS5 returns some text for every
row it matches.

- [ ] **Step 6: Add collision and sanitizer RED tests**

Add a pure collision test:

```go
func TestRejectSearchSnippetMarkerCollision(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, content := range []string{start, "prefix " + end + " suffix"} {
		if err := rejectSearchSnippetMarkerCollision(content, start, end); !errors.Is(err, ErrSearchSnippetMarkerCollision) {
			t.Fatalf("error = %v, want collision", err)
		}
	}
	markerLike := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]]"
	if err := rejectSearchSnippetMarkerCollision(markerLike, start, end); err != nil {
		t.Fatalf("marker-like content = %v", err)
	}
}
```

Add table tests covering:

```go
func TestSanitizeSearchSnippetRepairsAndBoundsText(t *testing.T) {
	start, end := fixedSearchMarkers()
	raw := append([]byte("lead \xff\n\u202E "),
		[]byte(start+"needle"+end+" "+strings.Repeat("界", 300))...)
	parsed, err := parseMarkedSearchSnippet(raw, start, end)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sanitizeSearchSnippet(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 200 || len(got) > 800 {
		t.Fatalf("invalid bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}
	if strings.Contains(got, start) || strings.Contains(got, end) ||
		strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\u202E') ||
		!strings.Contains(got, "needle") {
		t.Fatalf("unsafe snippet = %q", got)
	}
}
```

Also cover C0/C1/DEL, tabs and Unicode whitespace, bidi controls, RTL, emoji,
combining marks, CJK, middle/end match windows, one oversized matched token,
empty post-sanitization output, and exact 200-rune/800-byte boundaries. The
boundary cases must include a complete match of exactly 200 scalars and one of
exactly 800 bytes with source context on both sides, asserting the published
snippet is the whole match with no indicator and no truncation, alongside a
198-scalar match where both indicators do fit. Cover the allocation bound too,
measured around the *whole* pipeline rather than after a parse: feeding
`sanitizeMarkedSearchSnippet` a marked multi-megabyte single token, and a
marker-free one, must stay inside a fixed named budget at both 2 MiB and 4 MiB —
one budget across two sizes is what proves independence from the input — while
the working window stays under `searchSnippetWindowRunes` and the published
snippet still honours both caps. Add a repository-level companion that bounds
one hit query's allocation against the legacy projection over the same row, so
the end-to-end path is covered and not just the helper. Add
`<script>alert(1)</script>` and `**bold**` payloads and assert those bytes remain
literal rather than entity-escaped or emphasized; output may add only U+2026 at
a cut edge or U+FFFD for explicitly replaced invalid/control input. The
empty-output case must assert `errors.Is(err, ErrInvalidSearchSnippet)`.

- [ ] **Step 7: Run sanitizer and collision tests and verify RED**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'Test(RejectSearchSnippetMarkerCollision|SanitizeSearchSnippet)' \
  -count=1
```

Expected: FAIL because collision and sanitizer helpers are undefined.

- [ ] **Step 8: Implement sanitization and verify GREEN**

Implement these focused helpers:

```go
func rejectSearchSnippetMarkerCollision(content, start, end string) error
func sanitizeMarkedSearchSnippet(raw, start, end string) (string, error)
func normalizeMarkedSnippetWindow(raw, start, end string) (snippetWindow, error)
func boundSearchSnippet(window snippetWindow) string
func isSearchSnippetBidiControl(r rune) bool
```

Preserve the first complete match whenever the sanitized match itself fits both
caps, collapse whitespace, and replace invalid/control/bidi data with U+FFFD.
Return `ErrInvalidSearchSnippet` if bounded output is empty or contains no
retained match text.

U+2026 marks each cut edge, but it ranks below the complete match: the
indicators are added only when every edge that needs one can be paid for beside
the whole match, and are dropped together otherwise rather than truncating a
match that fits. Context grows outward only after the whole match is secured,
and each scalar of context is charged for its indicators as it is taken. Only a
matched token larger than the caps is truncated to the largest prefix that fits,
indicators included.

`normalizeMarkedSnippetWindow` never materializes the fragment, at any stage.
FTS5's 32-token bound does not bound one token, so a multi-megabyte token
arrives as a single fragment; the marked string the driver returned is scanned
in place and its chunks stream into the window, which retains at most
`searchSnippetWindowRunes` scalars around the first retained match and counts
the rest, because every publishable window lies within one cap of that match
start.

It scans twice: once with a `nil` emit to validate the marker structure and
learn the pair count, and once to feed the normalizer. Both are needed. A broken
snippet must fail before anything reaches the sanitizer, and a marker-free
fragment has to be recognized as one *before* normalization so its single chunk
can be streamed as the implicit whole-fragment match. The source is therefore
read end to end twice — collapsing trailing whitespace makes even one full read
unavoidable — but nothing proportional to it is allocated.

The normalizer consumes `(chunk string, inMatch bool)`, opening a span before an
in-match chunk and closing it after. Chunk splits must be invisible: carry the
opening bytes of a scalar whose encoding continues into the next chunk in a
fixed `utf8.UTFMax` buffer, defer any span boundary that lands while a scalar is
carried until the decoder passes it, and let invalid-byte runs collapse across
chunks. The deferred-boundary queue is a fixed array indexed by byte distance,
because a boundary can never sit further ahead than a carried scalar is long.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'Test(NormalizeSearchScore|NewSearchSnippetMarkers|ParseMarkedSearchSnippet|SanitizeSearchSnippet)' \
  -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the primitives**

```bash
git add turing-backend/orchestrator-go/internal/repository/search_hits.go \
  turing-backend/orchestrator-go/internal/repository/search_hits_test.go
git commit -m "feat(search): add score and snippet primitives"
```

### Task 3: Add the scored repository query without changing legacy search

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go:307-358`
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions_test.go:248-423`
- Modify: `turing-backend/orchestrator-go/internal/repository/search_hits.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/search_hits_test.go`
- Verify: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`

- [ ] **Step 1: Write RED ranking, parity, and driver canary tests**

Add:

```go
func TestSearchMessageHitsExposeHigherIsBetterScoresAndLegacyParity(t *testing.T)
func TestSearchMessageHitsBreakEqualScoresByMessageID(t *testing.T)
func TestSearchMessageHitsCanaryPinsFiniteNonPositiveBM25(t *testing.T)
func TestSearchMessageHitsRoundTripExactMarkersThroughSQLite(t *testing.T)
func TestSearchMessageHitsFailClosedOnMarkerCollision(t *testing.T)
func TestSearchMessageHitsReturnMatchCenteredBoundedSnippets(t *testing.T)
func TestSearchMessageHitsReturnBoundedSnippetWhenPhraseExceedsFTS5Window(t *testing.T)
func TestSearchMessageHitsNeverReadSnippetTextFromNeighborMessage(t *testing.T)
func TestSearchMessageHitsDocumentDivergentExternalContentBehavior(t *testing.T)
func TestSearchMessageHitsKeepExactIdentifierSemantics(t *testing.T)
func TestSearchMessageHitsIncludeActiveAndArchivedSessions(t *testing.T)
func TestSearchMessageHitsExcludeDeletingSessions(t *testing.T)
func TestSearchMessageHitsPreserveScopeExclusionAndLimits(t *testing.T)
func TestSearchMessageHitsKeepLiteralPhraseInjectionResistance(t *testing.T)
func TestSearchMessageHitsTokenlessInputReturnsEmptySlice(t *testing.T)
func TestSearchMessageHitsReturnContextAndRowErrors(t *testing.T)
```

The first test calls `SearchMessages` and `SearchMessageHits` against the same
unchanged fixture and compares `Message` values by `reflect.DeepEqual`.
The driver canary calls `newSearchSnippetMarkers` directly with deterministic
entropy, selects both markers in its own bound `TEXT` query, asserts exact
bytes/no NUL/distinctness, then issues its own FTS snippet queries at
start/middle/end. The exported `SearchMessageHits` exposes no entropy seam.

The BM25 canary inserts 200 documents with one term in 199, selects raw
`bm25(messages_fts)` directly, and asserts every value is finite and
non-positive. The provenance test places a secret sentinel only in a neighboring
message and proves it is absent from the selected hit's snippet.

`TestSearchMessageHitsPreserveScopeExclusionAndLimits` also queries a real
token absent from every row and asserts an empty non-nil hit slice, separately
from the tokenless-input case.

The divergence test temporarily drops the update trigger before changing source
content: a shortened row yields no markers, which is the same shape an
over-window phrase produces, so it returns an excerpt of its own current content
rather than failing; a same-length replacement may retain balanced stale
offsets, but its snippet must likewise contain bytes only from that returned
message. Restore trigger-consistent state inside test cleanup. The identifier
test proves an ID in `message.content` matches and centers, while an ID present
only in `messages.id` returns no hit.

`TestSearchMessageHitsReturnBoundedSnippetWhenPhraseExceedsFTS5Window` inserts
100 filler words, an exact 40-word phrase, and 20 trailing words, then queries
that 40-word phrase. It first selects the marked projection directly and asserts
neither marker appears, so the fixture provably exercises the marker-free
window. `SearchMessages` must return the row and `SearchMessageHits` must return
the same message with a valid score and a snippet that satisfies both caps,
carries no marker bytes, and — with edge ellipses stripped — is a substring of
that hit's own `Message.Content`.

The collision integration test uses deterministic entropy:

```go
entropy := bytes.NewReader(bytes.Repeat([]byte{0xaa}, 16))
start, _, err := newSearchSnippetMarkers(
	bytes.NewReader(bytes.Repeat([]byte{0xaa}, 16)),
)
if err != nil {
	t.Fatal(err)
}
insertSearchMessage(t, ctx, database, "collision", "s1", start+" needle", 1)
hits, err := repo.searchMessageHits(ctx, "", "", "needle", 10, entropy)
if !errors.Is(err, ErrSearchSnippetMarkerCollision) || hits != nil {
	t.Fatalf("hits, error = %+v, %v", hits, err)
}
```

- [ ] **Step 2: Run repository hit tests and verify RED**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run '^TestSearchMessageHits' -count=1
```

Expected: FAIL because `SearchMessageHits` does not exist.

- [ ] **Step 3: Extract only the shared predicate builder**

Add:

```go
type searchMessagesInput struct {
	sessionID         string
	excludedSessionID string
	query             string
	limit             int
}

func searchMessagesPredicate(input searchMessagesInput) (string, []any, bool) {
	limit := input.limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := strings.ReplaceAll(input.query, "\x00", " ")
	if !hasFTS5Token(query) {
		return "", nil, false
	}
	predicate := `
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		JOIN sessions s
		  ON s.id = m.session_id
		 AND s.deletion_state = 'active'
		 AND s.status IN ('active', 'archived')
		WHERE messages_fts MATCH ?`
	args := []any{fts5Phrase(query)}
	if input.sessionID != "" {
		predicate += ` AND m.session_id = ?`
		args = append(args, input.sessionID)
	}
	if input.excludedSessionID != "" {
		predicate += ` AND m.session_id <> ?`
		args = append(args, input.excludedSessionID)
	}
	predicate += ` ORDER BY bm25(messages_fts), m.id LIMIT ?`
	args = append(args, limit)
	return predicate, args, true
}
```

Keep legacy `SearchMessages`' selected columns, scan, and return type unchanged.

- [ ] **Step 4: Implement the hit query**

Add:

```go
type SearchHit struct {
	Message Message
	Score   float64
	Snippet string
}

func (r *Repository) SearchMessageHits(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
) ([]SearchHit, error) {
	return r.searchMessageHits(
		ctx, sessionID, excludedSessionID, query, limit, rand.Reader,
	)
}

func (r *Repository) searchMessageHits(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
	entropy io.Reader,
) ([]SearchHit, error) {
	predicate, predicateArgs, ok := searchMessagesPredicate(searchMessagesInput{
		sessionID:         sessionID,
		excludedSessionID: excludedSessionID,
		query:             query,
		limit:             limit,
	})
	if !ok {
		return []SearchHit{}, nil
	}
	start, end, err := newSearchSnippetMarkers(entropy)
	if err != nil {
		return nil, err
	}
	sqlQuery := `
		SELECT
			m.id, m.session_id, COALESCE(m.run_id, ''), m.role, m.content,
			m.content_type, m.sequence, m.created_at,
			bm25(messages_fts),
			snippet(messages_fts, 0, ?, ?, '…', 32)` + predicate
	args := append([]any{start, end}, predicateArgs...)
	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	hits := make([]SearchHit, 0)
	for rows.Next() {
		var (
			message       Message
			rawScore      float64
			markedSnippet string
		)
		if err := rows.Scan(
			&message.MessageID, &message.SessionID, &message.RunID,
			&message.Role, &message.Content, &message.ContentType,
			&message.Sequence, &message.CreatedAt,
			&rawScore, &markedSnippet,
		); err != nil {
			return nil, err
		}
		if err := rejectSearchSnippetMarkerCollision(message.Content, start, end); err != nil {
			return nil, err
		}
		score, err := normalizeSearchScore(rawScore)
		if err != nil {
			return nil, err
		}
		snippet, err := sanitizeMarkedSearchSnippet(markedSnippet, start, end)
		if err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{
			Message: message,
			Score:   score,
			Snippet: snippet,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}
```

Import `crypto/rand` and generate markers with
`newSearchSnippetMarkers(entropy)` before `QueryContext`. Build the hit SQL with marker
placeholders in the SELECT and prepend marker arguments before predicate
arguments:

```sql
SELECT
  m.id, m.session_id, COALESCE(m.run_id, ''), m.role, m.content,
  m.content_type, m.sequence, m.created_at,
  bm25(messages_fts),
  snippet(messages_fts, 0, ?, ?, '…', 32)
```

For each row:

1. scan complete message, raw score, and marked snippet;
2. reject a complete marker appearing in `Message.Content`;
3. normalize score;
4. sanitize the marked snippet *as the string the driver returned*: never
   `[]byte(markedSnippet)`, and never through a parser that materializes the
   payload. `sanitizeMarkedSearchSnippet` validates the markers, streams the
   fragment's chunks into the bounded window, and treats a fragment with no
   marker pair as one implicit whole-fragment match so the bounding pass has a
   window to open around;
5. append one `SearchHit`.

The row's own bytes are already charged twice by this projection — the message
content and the marked snippet the driver materializes for the extra column —
and both are response data. Snippet processing must add nothing proportional on
top of them.

Close and drain this one reader before returning; never issue a nested query.
If the shared predicate reports tokenless input, return `[]SearchHit{}, nil`,
not nil and not an error.

- [ ] **Step 5: Add the status-domain schema contract**

In `search_hits_test.go`, read the `sessions` table SQL from `sqlite_schema` and
assert it still contains:

```sql
CHECK (status IN ('active','archived'))
```

This forces a future lifecycle status to make an explicit search-visibility
decision.

- [ ] **Step 6: Verify repository GREEN and deletion precedence**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestSearch(MessageHits|Messages)|TestDeleteSessionRemovesMessagesFromTheSearchIndex' \
  -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit repository search hits**

```bash
git add turing-backend/orchestrator-go/internal/repository/sessions.go \
  turing-backend/orchestrator-go/internal/repository/sessions_test.go \
  turing-backend/orchestrator-go/internal/repository/search_hits.go \
  turing-backend/orchestrator-go/internal/repository/search_hits_test.go
git commit -m "feat(search): return scored repository hits"
```

### Task 4: Negotiate legacy and hit responses in the service

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go:359-378`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service_test.go:970-1089`
- Modify: `turing-backend/agent-runtime-go/internal/orchestrator/client_test.go:80-150`

- [ ] **Step 1: Write RED format tests**

Add service tests:

```go
func TestSessionServiceSearchMessagesDefaultsToLegacyMessages(t *testing.T)
func TestSessionServiceSearchMessagesReturnsOnlyHitsWhenRequested(t *testing.T)
func TestSessionServiceSearchMessagesRejectsUnknownResponseFormat(t *testing.T)
func TestSessionServiceSearchMessagesFormatsHaveMessageParity(t *testing.T)
func TestSessionServiceSearchMessagesReturnsBoundedSnippetForOverWindowPhrase(t *testing.T)
func TestSessionServiceSearchMessagesLogsOnlyInvariantClass(t *testing.T)
func TestSessionServiceSearchMessagesDoesNotDuplicateLargeLegacyPayload(t *testing.T)
func TestSessionServiceSearchMessagesCannotReturnWithdrawnContentInEitherFormat(t *testing.T)
```

For legacy requests, assert `Messages` populated and `Hits` empty. For explicit
hits, assert the inverse. For an unknown enum:

```go
ResponseFormat: turingv1.SearchMessagesResponseFormat(99)
```

expect `codes.InvalidArgument`.

In `TestSessionServiceSearchMessagesReturnsOnlyHitsWhenRequested`, call
`h.repo.SearchMessageHits` against the same unchanged fixture before the RPC.
Assert every response hit's message is protobuf-equal to the mapped repository
message and `GetScore()`/`GetSnippet()` exactly equal the repository values; the
service must not normalize or regenerate metadata.

Make the parity test table-driven over ranked, tied, scoped, excluded, archived,
limited, and empty fixtures. Each case makes separate legacy and HITS calls
against unchanged data and compares `legacy.Messages` to every
`hit.GetMessage()` by protobuf equality and order.

`TestSessionServiceSearchMessagesReturnsBoundedSnippetForOverWindowPhrase`
extends that parity to a phrase wider than the 32-token snippet window: a
message of 100 filler words, an exact 40-word phrase, and 20 trailing words is
queried in both formats. Both must return the same message, and the hit must
carry a non-empty snippet within both caps, free of marker bytes and a substring
of the source content once edge ellipses are stripped — never `Internal`.

Call the content-free error-mapping helper directly with
`repository.ErrSearchMarkerEntropy`, capture `log.SetOutput`, and assert the log
contains `search_messages` plus `marker_entropy`, but does not contain
query/message/snippet/session sentinel strings or a wrapped error value. A
separate hit-path test proves repository errors flow through that helper.

Table-test every invariant classification:

```text
ErrSearchMarkerEntropy           -> marker_entropy
ErrInvalidSearchScore            -> invalid_score
ErrSearchSnippetMarkerCollision  -> marker_collision
ErrInvalidSearchSnippetMarkers   -> marker_structure
ErrInvalidSearchSnippet          -> invalid_snippet
```

The withdrawal test seeds a sentinel, starts deletion, and queries both
response formats; neither may return it. Use a real persistent test database,
not the existing shared-memory helper:

```go
func openSessionTestDBAt(t *testing.T, path string) *db.DB {
	t.Helper()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		_ = database.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func newSessionHarnessWithDB(t *testing.T, database *db.DB) *sessionHarness {
	t.Helper()
	repo := repository.New(database)
	capabilities := &sessionCapabilitySource{
		providers: map[turingv1.ModelProvider][]*turingv1.ModelCapability{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: {{
				Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
				Model:            "llama3.2",
				MaxContextTokens: 8192,
			}},
		},
		agents: map[turingv1.AgentId]bool{
			turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT: true,
		},
		tools: []string{"custom/custom.scan", "files/files.create", "system/system.time"},
	}
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	bus := eventsvc.NewBus(16)
	service := New(repo, config.Config{
		FilesMCPEnabled:   true,
		ApprovalJWTSecret: "approval-secret",
		CursorHMACKey:     [32]byte{1},
		OllamaModel:       "llama3.2",
		OpenAIEnabled:     true,
		OpenAIBaseURL:     "https://api.openai.com/v1",
		OpenAIModel:       "gpt-4o-mini",
	}, capabilities, bus)
	turingv1.RegisterSessionServiceServer(grpcServer, service)
	go func() { _ = grpcServer.Serve(lis) }()
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	// NewClient starts IDLE; connect eagerly so handshake latency does not land
	// inside a test deadline.
	conn.Connect()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &sessionHarness{
		database:     database,
		repo:         repo,
		conn:         conn,
		capabilities: capabilities,
		bus:          bus,
		service:      service,
	}
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	return newSessionHarnessWithDB(t, openSessionTestDB(t))
}
```

The test uses `path := filepath.Join(t.TempDir(), "turing.db")`, closes the first
database after its final request, reopens exactly that path, and builds a second
harness. The first harness receives no further requests and both gRPC servers
stop through their registered cleanup. Repeat both response formats after
reopen, close the second database in cleanup, and assert an unrelated control
message still exists so sentinel absence cannot pass against an
empty/reinitialized store.

- [ ] **Step 2: Run service tests and verify RED**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  -run '^TestSessionServiceSearchMessages' -count=1
```

Expected: the package first fails to build because `searchMessagesError` is
undefined; after that helper exists, the hit-format assertions remain RED until
the handler negotiates response formats.

- [ ] **Step 3: Implement explicit response negotiation**

Use a switch with no default fallback:

```go
format := req.GetResponseFormat()
switch format {
case turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED,
	turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES:
	messages, err := s.repo.SearchMessages(
		ctx, req.SessionId, req.ExcludeSessionId, req.Query, int(req.Limit),
	)
	if err != nil {
		return nil, searchMessagesError(err)
	}
	return &turingv1.SearchMessagesResponse{
		Messages: mapSearchMessages(messages),
	}, nil
case turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS:
	hits, err := s.repo.SearchMessageHits(
		ctx, req.SessionId, req.ExcludeSessionId, req.Query, int(req.Limit),
	)
	if err != nil {
		return nil, searchMessagesError(err)
	}
	return &turingv1.SearchMessagesResponse{
		Hits: mapSearchHits(hits),
	}, nil
default:
	return nil, status.Error(codes.InvalidArgument, "response_format is invalid")
}
```

`searchMessagesError` logs only typed metadata invariant class names and returns
the existing `Internal: search messages failed`. It must not log ordinary
database error values or any content.

Define mapping helpers rather than relying on undefined symbols:

```go
func mapSearchMessages(messages []repository.Message) []*turingv1.Message {
	out := make([]*turingv1.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, mapMessage(message.SessionID, message))
	}
	return out
}

func mapSearchHits(hits []repository.SearchHit) []*turingv1.SearchHit {
	out := make([]*turingv1.SearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, &turingv1.SearchHit{
			Message: mapMessage(hit.Message.SessionID, hit.Message),
			Score:   hit.Score,
			Snippet: hit.Snippet,
		})
	}
	return out
}
```

- [ ] **Step 4: Pin runtime legacy-format compatibility**

In the existing runtime client request-capture test, add:

```go
if fake.gotReq.GetResponseFormat() !=
	turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED {
	t.Fatalf("ResponseFormat = %v, want unspecified", fake.gotReq.GetResponseFormat())
}
```

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/agent-runtime-go/internal/orchestrator \
  -run '^TestSearchMessages' -count=1
```

Expected: PASS without changing production runtime code. This is a regression
guard, not a RED behavior change: the new enum's zero value is intentionally
the legacy default.

- [ ] **Step 5: Verify service GREEN**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/agent-runtime-go/internal/orchestrator \
  -run 'Test(SessionServiceSearchMessages|SearchMessages)' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit service negotiation**

```bash
git add turing-backend/orchestrator-go/internal/service/sessions/service.go \
  turing-backend/orchestrator-go/internal/service/sessions/service_test.go \
  turing-backend/agent-runtime-go/internal/orchestrator/client_test.go
git commit -m "feat(search): negotiate scored hit responses"
```

### Task 5: Map canonical and legacy hits in Dart

**Files:**
- Modify: `turing-client/turing_app/lib/models/search_hit.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart:375-391`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart:321-335`
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart:178-200`
- Modify: `turing-client/turing_app/test/networking/grpc_client_test.dart:215-262,739-878`

- [ ] **Step 1: Write RED domain and mapper tests**

Replace the current message-shaped mapper test with canonical proto-hit tests:

```dart
test('maps a canonical scored search hit', () {
  final mapped = GrpcMappers.searchHitToModel(
    sessionpb.SearchHit(
      message: commonpb.Message(
        messageId: 'message_42',
        sessionId: 'session_42',
        runId: 'run_42',
        role: commonpb.MessageRole.MESSAGE_ROLE_USER,
        content: 'prefix needle suffix',
        sequence: Int64(99),
        createdAt: timestamppb.Timestamp.fromDateTime(
          DateTime.utc(2026, 8, 13, 12, 34, 56),
        ),
      ),
      score: 0.75,
      snippet: 'prefix needle suffix',
    ),
  );
  expect(mapped.sessionId, 'session_42');
  expect(mapped.score, 0.75);
  expect(mapped.snippet, 'prefix needle suffix');
  expect(mapped.message.messageId, 'message_42');
  expect(mapped.message.runId, 'run_42');
  expect(mapped.message.role, 'user');
  expect(mapped.message.content, 'prefix needle suffix');
  expect(mapped.message.sequence, 99);
  expect(
    mapped.message.createdAt,
    DateTime.utc(2026, 8, 13, 12, 34, 56),
  );
});
```

Table-test missing message, NaN, infinity, negative score, and empty snippet.
Assert each throws `FormatException` whose message is one fixed class string and
does not contain source/query/session sentinels.

Add `legacySearchHitToModel(commonpb.Message)` coverage proving null score and
snippet.

- [ ] **Step 2: Run mapper tests and verify RED**

```bash
( cd turing-client/turing_app &&
  flutter test test/models/grpc_mappers_test.dart )
```

Expected: FAIL because the mapper accepts `Message` and the model has no metadata.

- [ ] **Step 3: Implement model and mappers**

Use:

```dart
class SearchHit {
  const SearchHit({
    required this.sessionId,
    required this.message,
    this.score,
    this.snippet,
  });

  final String sessionId;
  final Message message;
  final double? score;
  final String? snippet;
}
```

Define:

```dart
static model_search_hit.SearchHit searchHitToModel(
  sessionpb.SearchHit hit,
)

static model_search_hit.SearchHit legacySearchHitToModel(
  commonpb.Message message,
)
```

Validate `hit.hasMessage()`, `score.isFinite && score >= 0`, and non-empty
snippet. Throw only constant messages such as `search hit message is missing`,
`search hit score is invalid`, and `search hit snippet is invalid`.

- [ ] **Step 4: Write RED networking negotiation tests**

Change the capturing service to return configurable `hits` and `messages`.
Assert:

```dart
expect(
  service.searchMessagesRequest?.responseFormat,
  sessionpb.SearchMessagesResponseFormat
      .SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
);
```

Add tests proving:

- hits are preferred if both arrays arrive;
- a successful messages-only old-server response maps through legacy fallback;
- both-empty returns empty;
- `Internal`, `ResourceExhausted`, and deadline errors result in one RPC call and
  propagate without a legacy retry.
- calling `api.searchMessages(query: 'default limit')` without an explicit
  limit sends `limit == 50`.

- [ ] **Step 5: Implement client negotiation**

```dart
final response = await _sessions.searchMessages(
  sessionpb.SearchMessagesRequest(
    query: query,
    sessionId: '',
    limit: limit,
    responseFormat: sessionpb.SearchMessagesResponseFormat
        .SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
  ),
  options: grpc.CallOptions(timeout: _startupUnaryTimeout),
);
if (response.hits.isNotEmpty) {
  return response.hits.map(GrpcMappers.searchHitToModel).toList(growable: false);
}
return response.messages
    .map(GrpcMappers.legacySearchHitToModel)
    .toList(growable: false);
```

Do not catch RPC errors in this method.

- [ ] **Step 6: Verify Dart mapping/network GREEN**

```bash
( cd turing-client/turing_app &&
  flutter test test/models/grpc_mappers_test.dart \
    test/networking/grpc_client_test.dart )
```

Expected: PASS.

- [ ] **Step 7: Commit Dart protocol consumption**

```bash
git add turing-client/turing_app/lib/models/search_hit.dart \
  turing-client/turing_app/lib/models/grpc_mappers.dart \
  turing-client/turing_app/lib/networking/grpc_client.dart \
  turing-client/turing_app/test/models/grpc_mappers_test.dart \
  turing-client/turing_app/test/networking/grpc_client_test.dart
git commit -m "feat(search): consume scored hit responses"
```

### Task 6: Render safe server snippets without changing UI ordering

**Files:**
- Modify: `turing-client/turing_app/lib/features/search/search_screen.dart:13-40,609-635`
- Modify: `turing-client/turing_app/test/features/search/search_screen_test.dart`

- [ ] **Step 1: Write RED canonical snippet widget tests**

Add tests where `message.content` is an oversized body whose match is at the
end, but `SearchHit.snippet` is `…server match at end`. Assert rendered `Text`
and semantics use the snippet and do not contain the full body.

Name the first test exactly:

```dart
testWidgets('renders the canonical server snippet', (tester) async {
  final handle = tester.ensureSemantics();
  final api = _FakeSearchApi();
  await _pumpScreen(tester, api);
  final body =
      'opening ${List<String>.filled(1000, "x").join()} server match at end';

  await tester.enterText(find.byKey(const Key('search-field')), 'server match');
  await tester.testTextInput.receiveAction(TextInputAction.search);
  await tester.pump();
  api.searchCalls.single.completer.complete([
    _hit(
      id: 'canonical',
      sessionId: 'session-1',
      content: body,
      score: 0.5,
      snippet: '…server match at end',
      createdAt: DateTime.utc(2026, 8, 21),
    ),
  ]);
  await tester.pump();

  expect(find.text('…server match at end'), findsOneWidget);
  expect(find.text(body), findsNothing);
  expect(
    tester.getSemantics(find.byKey(const ValueKey('hit-canonical'))).label,
    contains('…server match at end'),
  );
  expect(
    tester.getSemantics(find.byKey(const ValueKey('hit-canonical'))).label,
    isNot(contains(body)),
  );
  handle.dispose();
});
```

Add fixtures for `<script>`, Markdown, ANSI-looking bytes represented as text,
bidi replacement output, emoji, RTL, and CJK. Assert one inert `Text` value, no
extra widget interpretation, and bounded semantics.

Add one legacy hit with `snippet == null` and assert the existing 200-rune
excerpt remains.

Extend the existing `_hit` helper:

```dart
SearchHit _hit({
  required String id,
  required String sessionId,
  String role = 'user',
  String content = 'deploy staging',
  int sequence = 1,
  double? score,
  String? snippet,
  required DateTime createdAt,
}) {
  return SearchHit(
    sessionId: sessionId,
    score: score,
    snippet: snippet,
    message: Message(
      messageId: id,
      role: role,
      content: content,
      sequence: sequence,
      createdAt: createdAt,
    ),
  );
}
```

Add a mapper-failure UI test using the constant
`FormatException('search hit score is invalid')`. Assert the visible error copy
and live-region semantics contain only that class text and no message, snippet,
query, or session sentinels.

- [ ] **Step 2: Run focused UI tests and verify RED**

```bash
( cd turing-client/turing_app &&
  flutter test test/features/search/search_screen_test.dart \
    --plain-name 'renders the canonical server snippet' )
```

Expected: FAIL because `_buildHit` always excerpts full content.

- [ ] **Step 3: Use canonical snippet with explicit legacy fallback**

Replace:

```dart
final excerpt = _excerpt(hit.message.content);
```

with:

```dart
final excerpt = hit.snippet ?? _excerpt(hit.message.content);
```

Update the comments above `_maxExcerptRunes` and `_excerpt` to say they are
mixed-version fallback only. Do not change grouping or comparator functions.

- [ ] **Step 4: Verify all search UI behavior**

```bash
( cd turing-client/turing_app &&
  flutter test test/features/search/search_screen_test.dart )
```

Expected: PASS, including chronological grouping, active/archived hit taps,
title concurrency, loading/empty/error states, value-free error semantics, and
the existing tests proving relevance-order input does not change chronological
within-session or group order.

- [ ] **Step 5: Commit UI rendering**

```bash
git add turing-client/turing_app/lib/features/search/search_screen.dart \
  turing-client/turing_app/test/features/search/search_screen_test.dart
git commit -m "feat(search): render safe match snippets"
```

### Task 7: Update architecture and roadmap documentation

**Files:**
- Modify: `docs/architecture/session-recall.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`

- [ ] **Step 1: Update session recall architecture**

Document:

- unspecified/legacy requests return `messages`; explicit HITS returns `hits`;
- `score = -bm25`, higher-is-better only within one query/snapshot;
- ties use `message_id ASC`;
- snippets are single-line plain text, at most 200 scalars and
  800 UTF-8 bytes, with no public markup, centered on the match unless the
  phrase is wider than FTS5's 32-token snippet window;
- active and archived sessions remain visible while deleting/deleted sessions
  do not;
- runtime recall intentionally remains legacy until MEM-004.

- [ ] **Step 2: Mark MEM-002 shipped in the roadmap**

In the MEM-002 section, add the shipped behavior and acceptance evidence. Keep
MEM-003 and MEM-004 pending and do not claim one-RPC recall or evaluation
metrics.

- [ ] **Step 3: Run documentation contract tests**

The merged tree has no documentation-specific test for these two files:
repo-wide searches for `session-recall.md`, `Session Recall Scope`, `MEM-002`,
and `scored search hits` under `turing-backend/`, `tools/`, `.github/`, and
`turing-client/` return no test references. Validate the docs with the
repository's broad architecture-adjacent test package and diff checks:

```bash
go test -tags sqlite_fts5 ./turing-backend/tests -count=1
git --no-pager diff --check 137d1238...HEAD
```

Expected: PASS.

- [ ] **Step 4: Commit documentation**

```bash
git add docs/architecture/session-recall.md \
  docs/architecture/2026-08-18-personal-agent-audit.md
git commit -m "docs(memory): document scored search hits"
```

### Task 8: Review the full implementation until zero feedback

**Files:**
- Review every file changed since `137d1238`.

- [ ] **Step 1: Self-review against the approved spec**

Check every spec section against the diff:

```bash
git --no-pager diff --stat 137d1238...HEAD
git --no-pager diff 137d1238...HEAD
git --no-pager diff 137d1238...HEAD |
  rg -n 'TBD|TODO|FIXME|placeholder|deprecated = true' || true
rg -n 'testWidgets?\(.*skip:|group\(.*skip:|\btest\(.*skip:' \
  turing-client/turing_app/test || true
rg -n '\bt\.Skip\(' turing-backend --glob '*_test.go' || true
```

Fix placeholders, stale comments, content-bearing errors/logs, accidental files,
and scope creep.

- [ ] **Step 2: Run fresh Opus 5 spec-conformance review**

Dispatch Claude Opus 5 with `xhigh` reasoning and `long_context`. Provide the
approved spec, implementation plan, merge base `137d1238`, current HEAD, and
full changed-file list. Require findings only for correctness, spec gaps,
security/privacy, wire compatibility, lifecycle, test coverage, and scope.

Fix every valid finding and repeat until the exact result is:

```text
NO REMAINING FEEDBACK
```

- [ ] **Step 3: Run fresh Opus 5 quality review**

Dispatch a separate Claude Opus 5 `xhigh`/`long_context` review focused on
simplification, naming, reuse, failure handling, cost bounds, and test quality.
Fix every valid finding and repeat until:

```text
NO REMAINING FEEDBACK
```

- [ ] **Step 4: Run required Opus 4.8 full-diff review**

Dispatch Claude Opus 4.8 over the complete diff and explicitly request:

- correctness bugs, edge cases, and gaps against MEM-002;
- concrete reuse/simplification/naming improvements; and
- unit-test coverage, calling out any behavior without a test that fails before
  its fix.

Resolve every valid finding or record a precise technical rejection.

- [ ] **Step 5: Commit review fixes**

```bash
git add proto/turing/v1/sessions.proto \
  gen/turing/v1/go/turing/v1/sessions.pb.go \
  gen/turing/v1/go/turing/v1/sessions_grpc.pb.go \
  turing-backend/tests/proto_contract_test.go \
  turing-backend/orchestrator-go/internal/repository/search_hits.go \
  turing-backend/orchestrator-go/internal/repository/search_hits_test.go \
  turing-backend/orchestrator-go/internal/repository/sessions.go \
  turing-backend/orchestrator-go/internal/repository/sessions_test.go \
  turing-backend/orchestrator-go/internal/service/sessions/service.go \
  turing-backend/orchestrator-go/internal/service/sessions/service_test.go \
  turing-backend/agent-runtime-go/internal/orchestrator/client_test.go \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pb.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbenum.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbgrpc.dart \
  turing-client/turing_app/lib/generated/turing/v1/sessions.pbjson.dart \
  turing-client/turing_app/lib/models/search_hit.dart \
  turing-client/turing_app/lib/models/grpc_mappers.dart \
  turing-client/turing_app/lib/networking/grpc_client.dart \
  turing-client/turing_app/lib/features/search/search_screen.dart \
  turing-client/turing_app/test/models/grpc_mappers_test.dart \
  turing-client/turing_app/test/networking/grpc_client_test.dart \
  turing-client/turing_app/test/features/search/search_screen_test.dart \
  docs/architecture/session-recall.md \
  docs/architecture/2026-08-18-personal-agent-audit.md
git commit -m "fix(search): address scored hit review"
```

Skip this commit only if no review changed a file.

### Task 9: Run full verification and open the roadmap PR

**Files:**
- Verify all changed files.
- No new source files unless a failing verification requires a scoped fix.

- [ ] **Step 1: Run the project `/verify` skill**

Run the complete matrix, including:

```bash
TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
export PATH="$TOOLS_ROOT/protoc-34.1/bin:$(go env GOPATH)/bin:$PATH"
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files &&
  go test ./... -count=1 && go test -race ./... -count=1 &&
  go build ./cmd/server )
( cd turing-backend/mcp-system &&
  go test ./... -count=1 && go test -race ./... -count=1 &&
  go build ./... )
( cd turing-client/turing_app && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command exits 0. If the root race suite flakes, reproduce the
exact failure, diagnose it, and rerun the full root race suite; do not silently
accept an isolated pass.

- [ ] **Step 2: Verify generated and compatibility state**

```bash
TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
export PATH="$TOOLS_ROOT/protoc-34.1/bin:$(go env GOPATH)/bin:$PATH"
tools/proto/check.sh
tools/proto/breaking.sh origin/main
git --no-pager diff --check 137d1238...HEAD
git status --short
```

Expected: proto commands and diff check exit 0; status contains only intended
committed work.

- [ ] **Step 3: Push the branch**

```bash
git push -u origin mcasillas17-mem-002-scored-search-hits
```

- [ ] **Step 4: Open the PR into main**

Use the PR tool with:

```text
Title: MEM-002: return scored search hits
Label: turing-roadmap
```

The body must include:

- negotiated legacy/hit wire compatibility;
- higher-is-better score scope and deterministic ties;
- exact safe snippet bounds and marker grammar;
- preserved TUR-004/TUR-008 behavior;
- explicit MEM-003/MEM-004 non-goals;
- Opus 5 and Opus 4.8 review outcomes; and
- the full verification matrix.

Then apply the required roadmap label:

```bash
gh pr edit --add-label turing-roadmap
gh pr view --json labels --jq '.labels[].name' | rg '^turing-roadmap$'
```

Expected: the second command prints `turing-roadmap`.

- [ ] **Step 5: Wait for all six CI jobs**

Require these exact jobs green:

```text
Go tests and build
MCP files module
MCP system module
Lint
Proto and script checks
Flutter tests
```

Use:

```bash
gh pr checks --watch --fail-fast
```

If a job fails, inspect its log, fix with RED -> GREEN, rerun relevant local
coverage, push a normal follow-up commit, and wait for all six again.

### Task 10: Merge normally and verify live main

**Files:**
- No source changes expected.

- [ ] **Step 1: Confirm approval and clean PR state**

```bash
gh pr view --json reviewDecision,mergeable,statusCheckRollup
```

Expected: approved/mergeable and all six required jobs successful.

- [ ] **Step 2: Merge through the normal repository flow**

This repository's recent mainline uses squash merges. Merge without force-push,
history rewrite, or local `main` mutation:

```bash
gh pr merge --squash --delete-branch=false
```

Record the resulting GitHub merge commit SHA.

- [ ] **Step 3: Fetch and verify live main contains MEM-002**

```bash
git fetch origin main
MERGE_SHA="$(gh pr view --json mergeCommit --jq .mergeCommit.oid)"
git merge-base --is-ancestor "$MERGE_SHA" origin/main
git show origin/main:proto/turing/v1/sessions.proto | \
  rg 'response_format = 5|repeated SearchHit hits = 2'
```

Expected: ancestor check exits 0 and both field contracts appear.

- [ ] **Step 4: Run live-main targeted verification**

Create no new branch or session. Always test the fetched `origin/main` in a
temporary detached worktree:

```bash
LIVE_MAIN_WORKTREE="$(dirname "$(git rev-parse --show-toplevel)")/mem-002-live-main"
test ! -e "$LIVE_MAIN_WORKTREE"
cleanup_live_main_worktree() {
  git worktree remove --force "$LIVE_MAIN_WORKTREE" 2>/dev/null || true
}
trap cleanup_live_main_worktree EXIT
git worktree add --detach "$LIVE_MAIN_WORKTREE" origin/main
(
  cd "$LIVE_MAIN_WORKTREE"
  TOOLS_ROOT="$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
  export PATH="$TOOLS_ROOT/protoc-34.1/bin:$(go env GOPATH)/bin:$PATH"
  go test -tags sqlite_fts5 \
    ./turing-backend/orchestrator-go/internal/repository \
    ./turing-backend/orchestrator-go/internal/service/sessions \
    ./turing-backend/agent-runtime-go/internal/orchestrator \
    ./turing-backend/tests -count=1
  (
    cd turing-client/turing_app
    flutter pub get
    flutter test test/models/grpc_mappers_test.dart \
      test/networking/grpc_client_test.dart \
      test/features/search/search_screen_test.dart
  )
  tools/proto/check.sh
)
git worktree remove "$LIVE_MAIN_WORKTREE"
trap - EXIT
rm -rf -- "$(dirname "$(git rev-parse --show-toplevel)")/.turing-mem002-tools"
```

Expected: all commands exit 0 against the fetched `origin/main`.

- [ ] **Step 5: Report completion**

Report:

- PR URL and merge SHA;
- live-main ancestor and field-contract evidence;
- six green CI jobs;
- final Opus 5 spec/quality results;
- Opus 4.8 full-diff result; and
- live-main targeted verification.
