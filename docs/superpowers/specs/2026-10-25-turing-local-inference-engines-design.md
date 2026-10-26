# Local inference engines: design

**Status:** proposed, awaiting review.

**Date:** 2026-10-25.

## 1. Goal

Turing should run its local model on whichever inference engine suits the
machine it is installed on, not only Ollama. A 24 GB Mac mini keeps Ollama and
a small model. A 128 GB Framework Desktop (AMD Strix Halo) can run a much
larger model through an engine built for that hardware, such as ds4.

"Local" keeps its current meaning: the model runs on this machine, so a send
needs no remote-egress consent and a specialist may use it.

### What was asked, and what is assumed

The user asked:

- whether Turing still uses Ollama (it does, as its only local engine);
- whether ds4 (`dwarfstar.sh`) would fit;
- for Turing to "support different options of local engine for running models
  depending on the machine it is running on". The user owns a Framework
  Desktop with 128 GB of RAM.

This design assumes the following. These were decided under autopilot and are
open to correction:

1. **One engine per install.** Turing and the engine run on the same machine,
   and the operator picks one engine.
2. **Setup only recommends.** "Depending on the machine" means setup detects
   the hardware, recommends an engine and model, and writes the choice. It
   never installs an engine or downloads weights.
3. **A LAN engine is not local.** Turing on the Mac mini calling an engine on
   the Framework Desktop over the network is out of scope. Data would leave
   the machine, which is remote egress by definition. To use the Framework
   Desktop, run Turing on it.

## 2. Where Turing is today

- **Two providers exist** (`proto/turing/v1/common.proto`):
  `MODEL_PROVIDER_OLLAMA = 1` and `MODEL_PROVIDER_OPENAI_COMPATIBLE = 2`.
  - The stored string `ollama` is how every policy check recognizes the
    local route: chat egress resolution, egress decisions, delegation
    (`service/team`'s `childProvider = "ollama"`), automations and session
    defaults.
- **The runtime advertises one local model,** `OLLAMA_MODEL`
  (`agent-runtime-go/cmd/runtime/main.go`, `advertisedModels`).
  - It reaches that model through Ollama's native `POST /api/chat`
    (`llm/ollama.go`), with a per-request `keep_alive`.
- **`openai_compatible` is the remote provider.** Every send asks for consent.
  - A credentialed endpoint must be HTTPS unless it is a loopback literal
    (`internal/egress/endpoint.go`).
  - From inside Docker, a server on the host (`http://host.docker.internal:8000`)
    is therefore refused, and delegation never routes to it.
- **The local endpoint is checked by address.** `ParseLocalEndpoint` accepts
  only `localhost`, `host.docker.internal` or a loopback IP literal.
- **The runtime guards the boundary.** It refuses a local job whose provider
  exposes a remote endpoint (`agent/egress.go`, `validateEgressProvider`).
- **The model name is shared.** `OLLAMA_MODEL` and
  `OLLAMA_CONTEXT_WINDOW_TOKENS` are passed to both the orchestrator and the
  runtime. The orchestrator uses the model as the default for chat, the team,
  automations and the provider list.
- **The runtime container can reach the host.** It has
  `extra_hosts: host.docker.internal:host-gateway`, so that name also resolves
  on Linux.

## 3. ds4 on the Framework Desktop

ds4 (`github.com/antirez/ds4`) is a C inference engine for a small set of
models, each in a tested GGUF layout. The facts below come from its docs,
read on 2026-10-25:

- **On AMD Strix Halo with 96 GB or more, over ROCm** (`make strix-halo`), it
  runs:
  - DeepSeek V4 Flash and GLM 5.x, including vision and SSD streaming;
  - DeepSeek V4.1 Q2 at 128 GB, text-only, streaming its experts from the
    SSD, with large prefills "not yet tuned".
  - Qwen3.8 is **not** supported on ROCm.
- **`ds4-server` speaks the OpenAI API.** It offers
  `POST /v1/chat/completions` with tools and SSE streaming, and
  `GET /v1/models`.
  - It listens on `http://127.0.0.1:8000` by default.
  - `--host` changes the listening address.
  - Model names are aliases: the GGUF loaded at startup decides the model.
- **DeepSeek thinks by default.** A non-thinking alias such as `deepseek-chat`
  gives direct answers. Reasoning comes back separately from the visible text.
- **Tool calls carry the engine's own IDs.** ds4 assigns unguessable tool-call
  IDs and replays history exactly when a client sends those IDs back.
- **`--kv-disk-dir` keeps a disk cache.** It holds prompt text and model
  state; the docs say to "treat the directory as private".

So ds4 fits the Framework Desktop for DeepSeek V4 Flash or GLM 5.3. It needs
nothing from Turing beyond a way to call a **local** OpenAI-compatible server,
which is this design.

It does not fit the 24 GB Mac mini: its smallest supported setup needs a
64 GB Mac.

## 4. Approaches

**A. Engines behind the one local provider (recommended).**

- The local provider stays one identity.
- The runtime gains a choice of engine *protocol*:
  - `ollama`: the native API, as today;
  - `openai`: any OpenAI-compatible server on this machine, such as ds4,
    llama.cpp's `llama-server`, LM Studio, Lemonade or vLLM.
- Locality is proven by the endpoint, with the same rule as today, not by the
  engine's name.
- Egress, consent, delegation and automations need no new policy, because
  the local route is still the local route.

**B. One provider per engine** (`MODEL_PROVIDER_LLAMA_CPP`, `…_DS4`, …).

- Rejected. Every new engine would need a proto change, regenerated code and
  a change to each policy check that today asks "is this the local route?",
  which would become "is this one of the local providers?".
- Wire values would multiply for what is the same trust boundary.

**C. Point the remote `openai_compatible` provider at the local server.**

- Rejected. It would ask for consent on every send, for data that never
  leaves the machine. That teaches people to click through consent.
- It would also be refused over plain HTTP to `host.docker.internal`, and
  delegation could never use it.

**Naming, within A.** The wire and stored identity stays `ollama`
(`MODEL_PROVIDER_OLLAMA = 1`), even though it now names the local route
whatever the engine. Two alternatives were rejected:

- **Renaming value 1** fails the repository's `buf breaking` `FILE` gate.
- **Adding `MODEL_PROVIDER_LOCAL = 3`** would mean a migration of every stored
  `ollama` string, including queued job payloads and egress decisions. It
  would also need the runtime and the Flutter client upgraded in lockstep, all
  for no change in behaviour.

Instead, the code names it honestly. One Go constant,
`repository.LocalProvider = "ollama"`, replaces the scattered literals, with a
comment saying it is the local route's wire identity, chosen before other
engines existed. The proto value gets the same comment. The UI says **Local**.

## 5. Design

### 5.1 Configuration

**Shared** by the orchestrator and the runtime. They are passed to both, as
the old names are today.

| Key | Default | Replaces |
| --- | --- | --- |
| `TURING_LOCAL_MODEL` | `qwen2.5:7b` | `OLLAMA_MODEL` |
| `TURING_LOCAL_CONTEXT_WINDOW_TOKENS` | `32768` | `OLLAMA_CONTEXT_WINDOW_TOKENS` |

**Runtime only.**

| Key | Meaning |
| --- | --- |
| `TURING_LOCAL_ENGINE` | `ollama` (default) or `openai`. Any other value refuses startup. |
| `TURING_LOCAL_ENGINE_URL` | The engine's base URL. It must pass `ParseLocalEndpoint`, so it names `localhost`, `host.docker.internal` or a loopback literal, or startup is refused. For `ollama` it falls back to `OLLAMA_BASE_URL`; for `openai` it is required. For `openai` it is the base URL that `/v1/chat/completions` hangs off, for example `http://host.docker.internal:8000/v1`. |
| `TURING_LOCAL_ENGINE_API_KEY` | Optional and `openai` only. Some servers want any bearer value. It goes into `.env` like the other secrets and never into a log or capability. |
| `TURING_LOCAL_MAX_OUTPUT_TOKENS` | Replaces `OLLAMA_MAX_OUTPUT_TOKENS`. |
| `TURING_LOCAL_ENGINE_LABEL` | Display only, such as `ds4`, `llama.cpp` or `Ollama`. It defaults to `Ollama` for `ollama` and `OpenAI-compatible` for `openai`. |
| `OLLAMA_KEEP_ALIVE` | Unchanged, and read only by the `ollama` engine. |

**Upgrades.** Each old name is read when its new name is unset, so an existing
`.env` keeps working unchanged. When the old name is used, the service logs it
once at startup, naming the new key. The new name wins when both are set.
Compose passes both names through.

### 5.2 The runtime's local provider

- **With `TURING_LOCAL_ENGINE=ollama`,** the provider is today's
  `llm.Ollama`, unchanged.
- **With `openai`,** the runtime builds a local provider. It wraps the
  existing OpenAI-compatible transport (`llm/openai_compatible.go`: streaming,
  tool calls, usage, failure origins) and differs from the remote one in three
  ways:
  - It is built only from an endpoint that passed `ParseLocalEndpoint`.
  - It does **not** implement `llm.EgressProvider`. `validateEgressProvider`
    therefore keeps refusing a remote job resolved to it, and keeps allowing a
    local one.
  - Its `ID()` is the local identity, so error messages name
    `TURING_LOCAL_MAX_OUTPUT_TOKENS`, not `OPENAI_MAX_OUTPUT_TOKENS`.
- **Reasoning output is dropped.** The transport ignores delta fields it does
  not know, including an engine's separate reasoning field. A thinking model
  therefore works, just more slowly, and its reasoning is never stored or
  shown. A test pins that a delta carrying only reasoning produces no text.
  Use a non-thinking alias where the engine has one; for ds4 that is
  `deepseek-chat`.
- **Tool-call IDs flow back unchanged.** Within a run, the runtime returns the
  engine's tool-call IDs in the tool-result messages, as it does today, so ds4
  can replay its own history exactly. Across turns, history may carry other
  IDs. ds4 then rebuilds part of the prefix: that is slower, not wrong.
- **The OpenAI-model heuristic does not apply.** `usesMaxCompletionTokens`
  matches OpenAI's own model names, so the local engine always sends
  `max_tokens`.

### 5.3 Capabilities and the client

- **`ModelCapability` gains `string engine = 4`** (non-breaking). The runtime
  sets it to the engine label on its local model.
- **The orchestrator never routes on it.** It passes it through to
  `ProviderConfig.models`.
- **The client says where the model runs.**
  - The provider selector reads **Local · ds4** (the engine label) instead of
    "Ollama".
  - Settings names the engine and model serving the local route.
  - The `ollama` strings in the client stay as the wire identity, behind one
    Dart constant.

### 5.4 Choosing an engine for the machine

A new script, `turing-backend/scripts/local-engine.sh`, has three commands.

**`detect`** prints what it finds and a recommendation. It changes nothing.

- **The machine:**
  - On macOS: `uname -m`, `sysctl hw.memsize`, and Apple Silicon or not.
  - On Linux: `/proc/meminfo`, GPU vendors from
    `/sys/class/drm/card*/device/vendor`, and an AMD Ryzen AI Max (Strix Halo)
    CPU from `/proc/cpuinfo`.
- **Which engines answer:**
  - Ollama at its configured URL, via `GET /api/version`.
  - An OpenAI-compatible server, via `GET /v1/models`, at ds4's documented
    default (`127.0.0.1:8000`) and at any `--url` given.
- **The recommendation** comes from this table, which holds only facts that
  were checked:

| Machine | Engine | Model | Source |
| --- | --- | --- | --- |
| Apple Silicon under 64 GB, such as the 24 GB Mac mini | Ollama | `qwen2.5:7b` (today's default) | this repository |
| Apple Silicon, 64 GB | ds4 (Metal) | Qwen3.8 Q2, about 42 GiB resident | ds4 hardware page |
| Apple Silicon, 96–128 GB | ds4 (Metal) | DeepSeek V4 Flash Q2 | ds4 hardware page |
| AMD Strix Halo, 96 GB+, such as the Framework Desktop | ds4 (ROCm) | DeepSeek V4 Flash Q2 or GLM 5.3 Q2; **not** Qwen3.8 | ds4 hardware page |
| NVIDIA DGX Spark or CUDA Linux, 128 GB class | ds4 (CUDA) | DeepSeek V4 Flash or GLM 5.3 Q2 | ds4 hardware page |
| Anything else | Ollama | today's default | this repository |

  Any other OpenAI-compatible server works through `openai`. The script calls
  it usable only once `check` passes.

**`use <ollama|openai> [--url URL] [--model M] [--context N] [--label L]`**
writes the `TURING_LOCAL_*` keys into `.env`:

- It writes them as Compose single-quoted literals, with the same writer and
  tests as `MEMORY_DISPLAY_ROOT`.
- It keeps the file at mode 0600.
- It rewrites a host-side `localhost` or `127.0.0.1` URL to
  `host.docker.internal`, which is the name the runtime container uses.
- It refuses a URL that fails the local-endpoint rule.
- It touches no other key.

**`check`** runs a new runtime subcommand, `runtime check-engine`, inside the
runtime container through `compose.sh run`. The probe therefore uses the real
configuration, the container's network and the production provider code. It
reports each step, and exits non-zero on the first failure:

1. **Reachable:** the engine answers from inside the container.
2. **Model present:** `TURING_LOCAL_MODEL` is listed, by `/api/tags` or
   `/v1/models`. An engine whose model names are aliases passes when it lists
   any model, and the result says the name is an alias.
3. **Streaming:** one short completion arrives as a stream.
4. **Tool calling:** a fixed prompt offering one tool named
   `turing_engine_check` produces a parsed tool call with valid JSON
   arguments. Turing depends on tool calling, and engines differ here, so this
   is the gate that turns "speaks the API" into "usable for Turing".

The probe sends a fixed prompt and no user data.

**`init.sh`** runs `detect` on first install. When exactly one engine answers
and it matches the recommendation, init writes that choice. Otherwise it keeps
today's Ollama defaults and prints the recommendation with the `use` command
to run. A rerun never changes an engine already chosen.

### 5.5 Linux host networking

On Docker Desktop for Mac, `host.docker.internal` reaches the host's loopback
interface. On Linux, `host-gateway` resolves to the Docker bridge's gateway
address, so an engine listening only on `127.0.0.1` cannot be reached from the
container.

On the Framework Desktop the engine must listen on the bridge's gateway
address, for example `ds4-server --host <gateway address>`. It should not
listen on `0.0.0.0` on a machine attached to a shared network: ds4's own docs
say to restrict access. The same applies to Ollama on Linux today
(`OLLAMA_HOST`).

`detect` prints the gateway address and the exact flag to use, and `check`
proves reachability from inside the container.

### 5.6 Delegation, egress and automations

- **Delegation, egress and automations behave the same.**
  - Any local engine is the local route.
  - A specialist's model routes through it exactly as through Ollama.
  - Child egress consent (PR 4) still covers only the tools that leave the
    machine.
- **A profile's `model:`** names a model the runtime advertises. With one
  local model per install, a profile naming another model shows Unavailable,
  as it does today.
- **The concurrency limit is unchanged.** `TURING_MAX_CONCURRENT_RUNS_GENERAL`
  stays 1. ds4's `--batched-session` could serve parallel specialists later;
  that belongs to AGT-000, not here.

### 5.7 Privacy: what the engine keeps

Turing's deletion lifecycle reaches only Turing's own stores. An engine that
caches prompts to disk keeps conversation text outside it; ds4's
`--kv-disk-dir` is one such cache.

The docs say this plainly. For Turing's engine, leave the disk cache off, or
accept that deleting a conversation in Turing does not clear the engine's
cache. `detect` repeats this when it finds ds4.

## 6. Error handling

- **Invalid configuration refuses startup** and names the key. This covers:
  - an unknown `TURING_LOCAL_ENGINE`;
  - a non-local `TURING_LOCAL_ENGINE_URL`;
  - `openai` with no URL;
  - a context window or output limit that fails `ValidateContextLimits`.
- **An engine that is down or misbehaving** fails a run through the existing
  provider transport and protocol failure codes and origins. The user sees the
  same failure card as for Ollama today.
- **`check`** turns each probe failure into one sentence naming the step and
  what to change.

## 7. Testing

- **Runtime configuration:**
  - engine selection;
  - each refusal listed in section 6;
  - the old-name fallbacks and their precedence;
  - the API key kept out of logs and capabilities.
- **Local OpenAI-compatible provider:**
  - It is not an `EgressProvider`.
  - `validateEgressProvider` allows a local job on it and refuses a remote job
    resolved to it.
  - A reasoning-only delta yields no text.
  - It sends `max_tokens`.
- **Capabilities:** the runtime advertises the label in `engine`, and the
  orchestrator passes it to `ProviderConfig`.
- **Orchestrator configuration:** `TURING_LOCAL_MODEL` and its fallback feed
  every default that `OLLAMA_MODEL` feeds today.
- **End to end:** the real runtime with `TURING_LOCAL_ENGINE=openai` runs
  against a fake `/v1/chat/completions` server, the counterpart of the
  existing `fakeOllama`:
  - a Turing turn with a tool call;
  - a delegated child completing.
- **`runtime check-engine`** against fake servers: each of the four steps
  passing and failing.
- **`local-engine.sh`:**
  - `detect` against fixture `/proc` and `sysctl` output;
  - `use` writing literals at mode 0600, translating `localhost`, refusing a
    non-local URL and leaving other keys alone;
  - `init.sh` never overwriting a chosen engine.
- **Client:** the selector and Settings show **Local · <engine>**.
- **Docs guards:** the `tools/docs` tests are updated wherever they list
  configuration keys.

## 8. Delivery slices

1. **PR 1: the local engine seam.**
   - The configuration in section 5.1 with its fallbacks.
   - The local OpenAI-compatible provider.
   - `repository.LocalProvider` replacing the literals, and the proto comment.
   - `ModelCapability.engine`.
   - Compose, `.env.example`, README, CLAUDE.md and
     `remote-egress-policy.md`. That document's "Ollama is the local provider
     identity" becomes "the local provider", with the same endpoint rule.
   - After this PR, ds4 on the Framework Desktop works by hand-editing `.env`.
2. **PR 2: choosing for the machine.**
   - `runtime check-engine`, `local-engine.sh` and the `init.sh` integration.
   - A new `docs/architecture/local-engines.md` holding the table in
     section 5.4, Linux networking and engine caches.
3. **PR 3: the client.** The **Local · engine** label in the selector and
   Settings. It is small, and could join PR 1 if review prefers.

## 9. Non-goals

- An inference engine on another machine; that is remote egress (section 1).
- Installing engines or downloading models.
- More than one local model or engine per install. A second runtime worker
  could advertise a second engine later; the router already routes by
  advertised model.
- Showing or storing a model's reasoning.
- Raising concurrency to use an engine's batching (AGT-000).
- Engine-specific request options. This includes ds4's `think:false`; a
  model alias covers that case.

## 10. Open questions (decided, open to correction)

1. **Rename the local provider's wire identity?** No (section 4). Revisit only
   if a second kind of local provider ever needs a separate trust identity.
2. **Should `init.sh` write an engine choice on its own?** Only when exactly
   one engine answers and it matches the recommendation (section 5.4).
   Otherwise it prints the recommendation.
3. **Should a failed `check` block startup?** No. `check` is advice for the
   operator. Startup refuses only invalid configuration; a working but weak
   engine is the operator's choice.
