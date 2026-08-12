# Runtime Reconnect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop a single dropped gRPC stream from permanently killing the agent runtime. Today the container exits and stays down, and every later chat message queues a job no worker will ever claim — with nothing surfaced to the user.

**Architecture:** Wrap `Worker.Run` in a reconnect loop with capped exponential backoff and jitter in `cmd/runtime/main.go`, exiting only on deliberate shutdown. Add a Compose restart policy as the outer safety net. Fix the silent `RunAssigned` drop that the reconnect loop newly makes reachable.

**Tech Stack:** Go 1.23, `turing-backend/agent-runtime-go`, `turing-backend/infra/docker-compose.yml`.

---

## The defect, precisely

`Worker.Run` (`internal/worker/worker.go:296`) returns — never retries — on every one of these:

| Line | Exit |
|---|---|
| ~311 | `w.client.ConnectWorker(streamCtx)` fails |
| ~333 | the initial `WorkerReady` send fails |
| ~368 | a heartbeat send exceeds `UpdateSendTimeout` |
| ~375 | `stream.Recv()` returns any error |
| ~378 | `handleCommand` returns a non-shutdown error |
| ~356 | `<-fatal` |

`cmd/runtime/main.go:65` is `return runtimeWorker.Run(ctx)`, and `main` calls `log.Fatal(err)` (`:23`). `turing-backend/infra/docker-compose.yml` declares **zero `restart:` keys** — verified — so Compose's default `no` applies: the container exits and stays down until someone notices.

**The code was written for reconnect and never wired up.** `startOutboundWriter`/`stopOutboundWriter` (`:596`, `:605`) nil out and restart the writer, and `TestOutboundWriterCanRestartForASubsequentStream` already proves a second stream works. Only the caller is missing.

**Cold start has the same shape.** `depends_on: condition: service_started` does not wait for the gRPC listener, and `orchestrator.Dial` uses `grpc.NewClient` with no wait-for-ready, so on a fresh `dev.sh` the runtime can exit before the orchestrator is accepting connections.

**Why it matters more now than it used to:** with the fake model, a run lasted milliseconds. With a real model plus tool iterations, a run occupies minutes — orders of magnitude more exposure to an orchestrator restart, a rebuild, or a network blip. And the failure is permanent rather than self-healing.

## Is `Worker` actually safe to `Run` twice? Yes — verified, with one caveat

The whole approach rests on this, so it was checked empirically rather than assumed.

`Worker` carries mutable state across `Run` calls (`active`, `approvals`, `toolCalls`, `decisions`, `generations`, `writer`, `fatal`). The ones that matter are handled:

- **`writer`** — `stopOutboundWriter` nils it, `startOutboundWriter` rebuilds it. `TestOutboundWriterCanRestartForASubsequentStream` already covers a second stream.
- **`fatal`** — `defer w.setFatalChannel(nil)` clears it.
- **`active`** — cleaned, but *asynchronously*. `cancelActiveRuns` only marks entries stopping (the run goroutine then returns early at `worker.go:490`, before its own `deleteActive`); the actual removal happens in `waitForActiveRuns`, which spawns a goroutine per entry that waits on `entry.done` and calls `deleteActiveEntry`.

Probed directly by starting a run and then invoking `Run`'s teardown verbatim: `len(w.active)` went **1 → 0**. **There is no permanent leak** — an earlier reading of this code suggested there was, and that reading was wrong.

**The caveat that does matter:** `waitForActiveRuns` gives up after `DisconnectCleanupTimeout` and returns while those goroutines are still waiting. A run that drains slowly can therefore still occupy `w.active` when the next `Run` begins. With the default `MaxConcurrentRuns: 1`, the reconnected worker would refuse assignments until the drain finishes — which is exactly why **Task 3's negative ack is not optional**: without it that refusal is silent and the run hangs. The backoff delay also works in your favour here.

**No existing test runs `Run` twice on the same `Worker`.** Add one — it is the only thing that will catch a future change breaking reuse.

The other maps (`approvals`, `toolCalls`, `decisions`, `generations`) are only ever deleted per item, never reset between streams. Nothing observed suggests a problem, and their keys will not recur, but **look at them while you are in here** rather than taking this paragraph's word for it — a stale `decisions` waiter surviving a reconnect would be an awkward bug to find later.

## Design decisions (locked)

1. **Reconnect in `main.go`, not inside `Worker.Run`.** `Run` already owns one stream's full lifecycle — connect, register, serve, and a `defer` that cancels active runs, drains them, and stops the writer. Retrying *around* it reuses that teardown exactly as written; retrying *inside* would mean unpicking it. It also keeps `Run` unit-testable as a single-stream function, which is how every existing test drives it.
2. **Deliberate shutdown must NOT reconnect.** Distinguish:
   - `ctx.Err() != nil` (SIGINT/SIGTERM via `signal.NotifyContext`) → return nil, exit 0.
   - `Run` returning `nil` — which is what `errShutdownRequested` becomes (`worker.go:~380`) — → the orchestrator asked us to stop. Return nil, **do not** reconnect.
   - anything else → reconnect.
   Getting this wrong turns a clean shutdown into a hot loop, or a recoverable blip into a permanent exit.
3. **Capped exponential backoff with jitter.** Start ~500ms, double to a ~30s cap, with full jitter. Jitter matters even with one runtime today: without it, a runtime and an orchestrator restarting together re-collide on a fixed rhythm.
4. **Reset the backoff after a connection that lived.** A stream that served for minutes then dropped should retry immediately, not at the 30s cap it inherited from an earlier bad patch. Reset when a stream survives past a threshold (~1 min) rather than on connect, so a connect/immediately-fail loop still backs off.
5. **Log every attempt.** A silently reconnecting runtime is nearly as hard to diagnose as one that silently died. One line per attempt with the error and the delay.
6. **Compose `restart: unless-stopped` as well.** The in-process loop handles stream failures; the restart policy handles the ones it cannot — a panic, an OOM kill, a `log.Fatal` from config load. `unless-stopped`, not `always`, so a deliberate `docker compose stop` stays stopped.

## File structure

- Modify: `turing-backend/agent-runtime-go/cmd/runtime/main.go` — the reconnect loop.
- Create: `turing-backend/agent-runtime-go/cmd/runtime/main_test.go` — tests for the loop's decisions.
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker.go` — negative-ack the dropped `RunAssigned` (Task 3).
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker_test.go`.
- Modify: `turing-backend/infra/docker-compose.yml` — `restart: unless-stopped` on the four services.

**Do not touch:** `internal/llm/**`, `internal/agent/general_assistant.go`, `internal/memory/**`, or `turing-backend/scripts/**` — other work is in flight there.

Verification (from the repo root):
```bash
( cd turing-backend/agent-runtime-go && go test ./... -count=1 && go build ./... )
go test -tags sqlite_fts5 ./... -count=1
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
```

---

## Task 1: Extract the retry decision as a pure function

Doing this first makes the policy testable without any gRPC, and keeps Task 2 mechanical.

**Files:** `cmd/runtime/main.go`, `cmd/runtime/main_test.go`

- [ ] **Step 1: Write the failing test** (`main_test.go`):

```go
package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShouldReconnect(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tc := range map[string]struct {
		ctx  context.Context
		err  error
		want bool
	}{
		"stream dropped":            {context.Background(), errors.New("rpc error: code = Unavailable"), true},
		"orchestrator not up yet":   {context.Background(), errors.New("connection refused"), true},
		"graceful worker shutdown":  {context.Background(), nil, false},
		"process signalled":         {cancelled, errors.New("rpc error: code = Canceled"), false},
		"signalled with nil error":  {cancelled, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := shouldReconnect(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("shouldReconnect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	var delays []time.Duration
	d := initialBackoff
	for i := 0; i < 10; i++ {
		delays = append(delays, d)
		d = nextBackoff(d)
	}
	if delays[0] != initialBackoff {
		t.Fatalf("first delay = %v, want %v", delays[0], initialBackoff)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] {
			t.Fatalf("backoff decreased at %d: %v", i, delays)
		}
	}
	if last := delays[len(delays)-1]; last != maxBackoff {
		t.Fatalf("backoff did not cap: %v", last)
	}
}
```

- [ ] **Step 2: Run, confirm failure** — `( cd turing-backend/agent-runtime-go && go test ./cmd/runtime/ -count=1 )`, expect undefined symbols.

- [ ] **Step 3: Implement** in `main.go`:

```go
const (
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 30 * time.Second
	// A stream that served this long was healthy; the next failure is a fresh
	// problem and should retry promptly rather than inherit an old delay.
	healthyStreamDuration = time.Minute
)

// shouldReconnect reports whether a Worker.Run return warrants another attempt.
//
// A nil error means the worker stopped deliberately — the orchestrator sent a
// shutdown command — and must not be restarted. A cancelled context means the
// process was signalled. Everything else is a transport or orchestrator problem
// the runtime is expected to ride out.
func shouldReconnect(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return err != nil
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
```

- [ ] **Step 4: Run, confirm pass. Commit.**

## Task 2: The reconnect loop

**Files:** `cmd/runtime/main.go`

- [ ] **Step 1: Replace `return runtimeWorker.Run(ctx)` (line 65)** with the loop. Everything above it — config, dial, providers, toolset, executor, worker construction — stays exactly as is and is built once; only `Run` is retried:

```go
	backoff := initialBackoff
	for {
		started := time.Now()
		err := runtimeWorker.Run(ctx)
		if !shouldReconnect(ctx, err) {
			if ctx.Err() != nil {
				log.Printf("runtime worker stopping: %v", ctx.Err())
				return nil
			}
			log.Print("runtime worker stopped by the orchestrator")
			return nil
		}
		// A stream that lived a while was healthy; do not inherit the delay from
		// an older bad patch.
		if time.Since(started) >= healthyStreamDuration {
			backoff = initialBackoff
		}
		delay := jitter(backoff)
		log.Printf("runtime worker disconnected (%v); reconnecting in %s", err, delay.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		backoff = nextBackoff(backoff)
	}
```

- [ ] **Step 2: Add `jitter`.** Full jitter over `[delay/2, delay]` keeps a floor so a tight failure loop cannot spin:

```go
// jitter spreads retries so a runtime and an orchestrator restarting together do
// not re-collide on a fixed rhythm.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}
```
Use `math/rand` (this is scheduling, not security — do not reach for `crypto/rand`).

- [ ] **Step 3: Verify the loop actually exits on signal.** Add a test that a cancelled context returns promptly rather than sleeping out the backoff — that is what the `select` on `ctx.Done()` inside the delay is for, and it is easy to get wrong by writing `time.Sleep`.

- [ ] **Step 4: Add the missing `Worker` reuse test** in `internal/worker/worker_test.go`. Nothing currently runs `Run` twice on the same `Worker`, so nothing would catch a change that breaks reuse — and reuse is the entire premise of this plan.

Drive it with the existing helpers (`newFakeStream`, `fakeRuntimeClient`, `blockingProvider`, `nextSent`): run, assign a run, tear the stream down, `Run` again on the same `Worker`, and assert the second `Run` sends a fresh `worker_ready` **and** accepts a new `RunAssigned`. `fakeRuntimeClient` hands out a single stream, so give it a way to return a second one — that small harness change is part of the task.

- [ ] **Step 5:** `go build ./...`, `go test ./cmd/runtime/ ./internal/worker/ -count=1`. Commit.

## Task 3: Negative-ack the dropped `RunAssigned`

The reconnect loop makes this reachable, so it belongs in the same change.

**Files:** `internal/worker/worker.go`, `internal/worker/worker_test.go`

`startRun` (`worker.go:464-473`) currently drops the job on the floor:

```go
	if _, exists := w.active[job.GetRunId()]; exists || len(w.active) >= w.options.MaxConcurrentRuns {
		w.mu.Unlock()
		cancel(context.Canceled)
		return          // <-- no ack, orchestrator believes it dispatched
	}
```

After a reconnect the worker re-registers and the orchestrator re-dispatches pending work. If a previous run is still draining (`waitForActiveRuns` has a timeout, so entries can outlive the stream), or the concurrency limit is full, the duplicate assignment vanishes silently and the run hangs until something else times it out.

- [ ] **Step 1: Failing test** — with `MaxConcurrentRuns: 1` and a run in flight, assign a second run and assert a `RuntimeRunFailed` is sent with `Retryable: true`, rather than nothing at all. Add a second case for a duplicate `RunId` while one is active.

- [ ] **Step 2: Run, confirm failure** (today nothing is sent).

- [ ] **Step 3: Implement** — send a retryable failure instead of returning silently:

```go
	if _, exists := w.active[job.GetRunId()]; exists || len(w.active) >= w.options.MaxConcurrentRuns {
		w.mu.Unlock()
		cancel(context.Canceled)
		// Tell the orchestrator we did not take it. Silence looks identical to
		// "accepted and still running", so the run would hang until some other
		// timeout noticed.
		sendCtx, sendCancel := context.WithTimeout(parent, w.options.UpdateSendTimeout)
		defer sendCancel()
		_ = w.send(sendCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
			RunFailed: &turingv1.RuntimeRunFailed{
				RunId:     job.GetRunId(),
				Code:      "worker_busy",
				Message:   "worker cannot accept the run",
				Retryable: true,
			},
		}})
		return
	}
```

`RuntimeRunFailed` is `{run_id, code, message, retryable}` (`proto/turing/v1/runtime.proto`). **Confirm before relying on it:** check how `orchestrator-go/internal/service/runtime/service.go` handles `Retryable: true` — whether it requeues the job or only records the flag. If it does not requeue, say so in the PR rather than implying the run recovers; the ack is still strictly better than silence, but the claim must match the behaviour.

- [ ] **Step 4: Run, confirm pass.** Commit.

## Task 4: Compose restart policy

**Files:** `turing-backend/infra/docker-compose.yml`

- [ ] **Step 1: Add `restart: unless-stopped`** to all four services: `turing-orchestrator` (line ~2), `turing-agent-runtime-general` (~42), `turing-mcp-system` (~81), `turing-mcp-files` (~103).

`unless-stopped` rather than `always`: a deliberate `docker compose stop` should stay stopped, and `always` would fight the operator.

- [ ] **Step 2: Check the security test still passes** — `turing-backend/tests/docker_compose_security_test.go` asserts properties of this file. Run `go test -tags sqlite_fts5 ./turing-backend/tests/ -run DockerCompose -count=1`.

- [ ] **Step 3: Commit.**

## Task 5: Prove it against the real stack

Unit tests cannot show that a restarted orchestrator is survived.

- [ ] **Step 1:** `cd turing-backend && ./scripts/init.sh && ./scripts/dev.sh` (or `docker compose -f infra/docker-compose.yml up -d`).
- [ ] **Step 2:** Confirm the runtime registers — `docker compose logs turing-agent-runtime-general` shows it connected.
- [ ] **Step 3: Restart the orchestrator alone** — `docker compose restart turing-orchestrator`.
- [ ] **Step 4: Assert the runtime survived.** Its logs show reconnect attempts and then a successful re-registration, and `docker compose ps` shows it still `Up` (not restarted, ideally — the in-process loop should absorb this without the container dying).
- [ ] **Step 5: Send a chat message** and confirm it completes. **Before this change that message would hang forever** — that is the regression being closed.
- [ ] **Step 6: Record the before/after in the PR**, including the log lines showing backoff.


## Repo conventions this plan is subject to

- **Work on an isolated worktree + feature branch; open a PR into `main`.** Do not commit to `main` (CLAUDE.md, "Repo etiquette").
- **A pre-push review is required, not optional.** CLAUDE.md mandates dispatching a subagent with **Opus 4.8** to review the full diff before pushing — correctness and edge cases, concrete improvements, and **unit-test coverage for every new behaviour and every fixed bug**. Act on the findings or state why one is rejected. A green test run is not a substitute.
- **`/verify` runs the full matrix.** Every step in it has a CI counterpart, so a local failure is a failure that will block the PR.

---

## Self-review checklist

- Deliberate shutdown (signal, and orchestrator-requested) does not reconnect ✓
- Backoff caps, jitters, and resets only after a stream proved healthy ✓
- The delay is interruptible by `ctx.Done()`, not a bare sleep ✓
- Every reconnect attempt is logged with cause and delay ✓
- The `RunAssigned` drop that reconnect makes reachable is fixed in the same change ✓
- Claims about requeue behaviour are checked against the orchestrator, not assumed ✓
- Compose restart policy added as the outer net, `unless-stopped` not `always` ✓
- Proven against a real orchestrator restart, not only unit tests ✓
