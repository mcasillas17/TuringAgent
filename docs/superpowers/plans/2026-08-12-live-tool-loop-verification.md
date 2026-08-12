# Live Tool-Loop Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove that a **real** model, running against the **real** stack, chooses a tool on its own and uses the result — and that the Flutter client renders it.

**Architecture:** A new on-demand verification script (`scripts/verify-tool-loop.sh` + a mode on the existing smoke client) that brings up compose, sends a natural-language prompt, and asserts the model emitted a tool call, the tool ran, and the answer used its output. Deliberately **not** a CI test — see below.

**Tech Stack:** Go 1.23, existing `turing-backend/scripts/grpc-smoke-client.go` pattern, Docker Compose, Ollama on the host.

---

## Read this first: what is already covered

Do **not** rebuild these. The loop's wiring is already tested deterministically:

| Test | File | Covers |
|---|---|---|
| `TestModelDrivenToolCallCompletesRun` | `turing-backend/tests/grpc_harness_test.go:1313` | The whole loop — but against `fakeModelServer` |
| `TestApprovalRequiredToolFlow` | `:1521` | Approval-gated tool path |
| `TestDiscoveredToolsAppearInListTools` | `:923` | Discovery surfacing |

Every one of them drives a **fake model** that is scripted to emit `tool_calls`. They prove the plumbing reacts correctly *given* a tool call. **Nothing anywhere proves a real model ever produces one.**

The existing smoke test does not close that gap either: `grpc-smoke-client.go:183` sends `/tool system.time`, which `general_assistant.go:112` intercepts via `tryDebugTool` **before the model is ever consulted**. It is a plumbing check wearing an end-to-end costume.

**This plan is exactly that missing piece, and nothing else.**

## The core problem: this check is non-deterministic

A real model choosing a tool is probabilistic. That drives every design decision here.

1. **This must NOT go in CI.** It needs Ollama and a downloaded model on the runner, takes minutes, and can fail for reasons unrelated to the code. Gating PRs on a 3B model's judgement would produce exactly the flaky-red-build culture that teaches people to ignore CI. It is an on-demand script, run like `smoke-grpc.sh`.
2. **Failure must distinguish "the loop is broken" from "the model declined."** These need different reactions and must never be conflated. The script reports one of three outcomes: `PASS`, `FAIL (loop broken)` — the model emitted a tool call and the pipeline mishandled it, and `INCONCLUSIVE (model did not call a tool)` — no tool call in N attempts. Only `FAIL` is a code defect.
3. **Retry, then report honestly.** N attempts (default 3), and if none produce a tool call, exit **non-zero with `INCONCLUSIVE`** and print the model's actual replies. Silence is not success.

## Known risk: llama3.2 may simply not be good at this

`OLLAMA_MODEL=llama3.2` is the default (`.env.example:18`). Ollama's llama3.2 templates do support tools, but small models call them unreliably, and this is precisely the class of model that struggles (the same reason the memory plan forbids text-matched edits).

**Therefore:** the model is overridable via `TURING_VERIFY_MODEL`, and the script prints which model produced the result. If llama3.2 comes back `INCONCLUSIVE`, that is a **finding about model choice, not a bug** — record it and re-run with a stronger tool-calling model (e.g. `qwen2.5`, `llama3.1`) to isolate which it is. Do not "fix" the loop in response to an inconclusive run until a capable model has also failed.

## Not in scope

- **Removing `tryDebugTool`.** It should probably go once this passes — it intercepts before the model and masks whether the loop works — but the current smoke test depends on it, so the two changes belong together in a follow-up.
- **Making `ListTools` non-empty** (the runtime reporting discovered tools to the orchestrator). **This plan does not depend on it:** the runtime builds its own registry directly from MCP `tools/list` (`internal/agent/toolregistry.go:92`) and advertises it at `general_assistant.go:141`. The orchestrator's `ListTools` is a separate, client-facing surface.

---

## Task 0: Preflight — is a live check even possible right now?

**Files:** none yet; this is a manual gate that becomes Task 4's preflight.

- [ ] **Step 1: Confirm Ollama is up and the model is present.**

```bash
curl -s http://localhost:11434/api/tags | grep -o '"name":"[^"]*"' | head
```
Expected: a list including the model. If the command fails, Ollama is not running — start it before anything else. It runs on the **host**, not in compose (`OLLAMA_BASE_URL=http://host.docker.internal:11434`).

- [ ] **Step 2: Confirm the model can call tools at all**, independently of this repo:

```bash
curl -s http://localhost:11434/api/chat -d '{
  "model": "llama3.2",
  "messages": [{"role":"user","content":"What time is it right now? Use a tool."}],
  "tools": [{"type":"function","function":{"name":"system.time","description":"Get the current time","parameters":{"type":"object","properties":{}}}}],
  "stream": false
}' | head -40
```
Expected: the response contains `tool_calls`. **If it does not, stop and report that** — no amount of work in this repo will make the loop fire, and this is the single fastest way to learn it. Try another model before concluding.

- [ ] **Step 3: Record the outcome** (model name, whether `tool_calls` appeared) in the PR description. This is the baseline every later result is interpreted against.

## Task 1: Add a model-driven mode to the smoke client

**Files:**
- Modify: `turing-backend/scripts/grpc-smoke-client.go`

- [ ] **Step 1: Add a flag** alongside the existing `-health-only` (see `run()`):

```go
modelDriven := flags.Bool("model-driven", false, "send a natural-language prompt and require the MODEL to choose a tool")
attempts := flags.Int("attempts", 3, "how many times to retry when the model does not call a tool")
```

- [ ] **Step 2: Send a natural-language prompt, never `/tool`.** Add a `runModelDrivenSmoke` alongside `runFullSmoke`. The prompt must not name the tool in the `/tool x` form that `tryDebugTool` matches:

```go
const modelDrivenPrompt = "What is the current time? Use the tools available to you."
```

Send it with the same `SendMessage` shape `runFullSmoke` uses, but read the model from `TURING_VERIFY_MODEL` when set, falling back to the configured default.

- [ ] **Step 3: Verify the debug path is genuinely bypassed.** Assert in code that the prompt does not begin with `/tool`, so a future edit cannot silently turn this back into the shortcut test:

```go
if strings.HasPrefix(strings.TrimSpace(modelDrivenPrompt), "/tool") {
	return errors.New("model-driven prompt must not use the /tool debug shortcut")
}
```

- [ ] **Step 4:** `go build ./turing-backend/scripts/...` — confirm it compiles. Commit.

## Task 2: Assert the three things that make this a real proof

**Files:** `turing-backend/scripts/grpc-smoke-client.go`

The stream carries `ChatStreamEvent`s. Collect across the run and assert all three; each maps to a distinct failure the others would miss.

- [ ] **Step 1: A tool call actually happened** — a `tool.call.started` (or the `ToolCallStarted` stream variant) was observed. Without this the model never chose a tool → `INCONCLUSIVE`, not `FAIL`.

- [ ] **Step 2: The tool completed** — a `tool.call.completed` for the same `toolCallId`. If a call started but never completed, that IS a loop defect → `FAIL`.

- [ ] **Step 3: The answer used the result.** Assert the final assistant content is non-empty and does not merely restate the question. A tool that runs but whose output never reaches the model is the subtlest failure here and the one worth catching:

```go
// The loop must feed the tool result back and produce a final turn. A run that
// stops at tool.call.completed with an empty answer means the result never made
// it back into the model request.
if strings.TrimSpace(finalContent) == "" {
	return fmt.Errorf("FAIL: tool ran but the model produced no final answer")
}
```

- [ ] **Step 4: Assert the frozen payload keys.** The merged Flutter UI reads `toolCallId`, `toolName`, and optionally `serverName`/`error`; if the runtime ever renames them the UI silently renders nothing and no Go test would notice. Assert `toolCallId` and `toolName` are non-empty on the observed events.

- [ ] **Step 5: Commit.**

## Task 3: Three-outcome reporting

**Files:** `turing-backend/scripts/grpc-smoke-client.go`

- [ ] **Step 1: Implement the retry loop** — up to `-attempts` runs, stopping at the first attempt that produces a tool call.

- [ ] **Step 2: Print an unambiguous verdict**, and make the model visible in every one:

```
PASS: model=llama3.2 chose system.time on attempt 2/3; answer="It is 14:32 UTC."
FAIL: model=llama3.2 called system.time but the run produced no final answer
INCONCLUSIVE: model=llama3.2 did not call a tool in 3 attempts (the loop was never exercised).
  attempt 1 answer: "I don't have access to the current time."
  ...
  This is a model-capability result, not necessarily a code defect. Re-run with
  TURING_VERIFY_MODEL=<a stronger tool-calling model> to tell them apart.
```

- [ ] **Step 3: Exit codes** — `0` PASS, `1` FAIL, `2` INCONCLUSIVE. Distinct codes so a wrapper can treat them differently.

- [ ] **Step 4: Commit.**

## Task 4: The runnable script

**Files:**
- Create: `turing-backend/scripts/verify-tool-loop.sh`

- [ ] **Step 1: Write it, modelled on `smoke-grpc.sh`** — same init/up/teardown lifecycle (read that script and mirror it, including its trap-based teardown so a failure never leaves containers running).

Preflight before bringing anything up, failing fast with an actionable message:

```bash
if ! curl -sf -m 3 "${OLLAMA_BASE_URL:-http://localhost:11434}/api/tags" >/dev/null; then
  echo "Ollama is not reachable. It runs on the HOST, not in compose. Start it and retry." >&2
  exit 2
fi
```

- [ ] **Step 2: Run the client in model-driven mode** after the health check passes, passing through `TURING_VERIFY_MODEL` and `-attempts`.

- [ ] **Step 3: `bash -n turing-backend/scripts/verify-tool-loop.sh`** to syntax-check. Note `.github/workflows/ci.yml` syntax-checks a fixed list of scripts — adding this one there is optional and safe (it is only `bash -n`), but if you do, `.github/workflows/ci_test.go` self-guards that command string and must be updated in the same commit or CI fails itself.

- [ ] **Step 4: Commit.**

## Task 5: Run it and record what actually happened

- [ ] **Step 1:** `./turing-backend/scripts/verify-tool-loop.sh`
- [ ] **Step 2: Record the verbatim verdict in the PR** — including an `INCONCLUSIVE`. A negative result about llama3.2's tool-calling is genuinely valuable and must not be buried.
- [ ] **Step 3: If `FAIL`**, that is a real defect in #19's loop. Debug with `superpowers:systematic-debugging` — do not paper over it by loosening an assertion.
- [ ] **Step 4: If `INCONCLUSIVE`**, re-run with `TURING_VERIFY_MODEL=qwen2.5` (or another known-good tool-calling model) and record both. Two inconclusive runs with a capable model point at the loop; one with llama3.2 alone points at the model.

## Task 6: Confirm the Flutter client renders a live tool call

This is the client's first ever exposure to a real tool event — every existing assertion runs against synthetic events.

- [ ] **Step 1:** With the stack up from Task 5, run the client: `cd turing-client/turing_app && flutter run -d macos`
- [ ] **Step 2:** Send the same natural-language prompt from the UI.
- [ ] **Step 3: Confirm** a tool card appears inline showing the tool name, transitions from spinner to a completed check, and the final answer renders **below** the card.
- [ ] **Step 4: Screenshot it and attach to the PR.** This is the only visual evidence the UI works against real events.
- [ ] **Step 5: If the card never appears** while the script says PASS, the payload keys have drifted from `toolCallId`/`toolName` — check `GrpcMappers` and the runtime's emit payloads against each other, and report it; that is a real integration defect.

---

## Self-review checklist

- Does not duplicate the deterministic harness tests, and says why ✓
- The prompt cannot be the `/tool` shortcut, enforced in code ✓
- Non-determinism handled by three distinct outcomes and distinct exit codes ✓
- `INCONCLUSIVE` is never reported as success, and never silently ✓
- Model is overridable and always printed, so model-capability and code defects can be told apart ✓
- Explicitly out of CI, with the reason stated ✓
- Asserts the frozen payload contract the Flutter UI depends on ✓
- Covers the answer-uses-the-result case, not just that a tool ran ✓
- No dependency on the runtime reporting tools to the orchestrator, and says why ✓
