# Local inference engines

**Status:** shipped (OPS-001 PR 3). Turing's local route runs on one engine on
the machine that runs Turing: Ollama, the default, or any OpenAI-compatible
server on that machine (`TURING_LOCAL_ENGINE=openai`), such as ds4, llama.cpp's
`llama-server`, LM Studio, Lemonade or vLLM. This guide is what
`./turing engine detect` and `./turing setup` point at: which engine suits a
machine, where an engine must listen on Linux, what `./turing engine check`
proves, and what an engine keeps. The designs are
[local inference engines](../superpowers/specs/2026-10-25-turing-local-inference-engines-design.md)
and the [host command](../superpowers/specs/2026-10-25-turing-host-command-design.md);
the roadmap item is OPS-001 in the [canonical roadmap](../NORTH_STAR.md).

## Choosing an engine for the machine

`./turing setup` asks, and `./turing engine detect` only shows; neither
installs anything. Both read the machine and ask each candidate engine for its
models:

- **The machine.** On macOS: `uname -m`, `sysctl hw.memsize` and
  `sysctl machdep.cpu.brand_string`. On Linux: `/proc/meminfo`,
  `/proc/cpuinfo` and each card's vendor under `/sys/class/drm`.
- **The engines.** The engine `.env` configures, Ollama at
  `http://host.docker.internal:11434`, ds4 at its documented default
  (`127.0.0.1:8000`, so `http://host.docker.internal:8000/v1`), and any
  `--url` given to `detect`. A host-side `localhost` or `127.0.0.1` becomes
  `host.docker.internal`, the name the containers use for this machine. The
  request comes from the host, never through an HTTP proxy.

An engine that answers is listed with its models. `detect` then names the
recommendation and how to use an engine (`./turing engine use ...`, or
`./turing setup`), adds the exact command when the recommended engine answers,
and exits 0 whatever it finds; a `--url` the runtime would refuse is its one
failure.

### The recommendation table

| Machine | Engine | Model hint | Source |
| --- | --- | --- | --- |
| Apple Silicon, under the 64 GB class | Ollama | `qwen2.5:7b` | this repository: Turing's default |
| Apple Silicon, 64 GB class | ds4 (Metal) | Qwen3.8 Q2, about 42 GiB resident | the ds4 hardware page |
| Apple Silicon, 96–128 GB class | ds4 (Metal) | DeepSeek V4 Flash Q2 | the ds4 hardware page |
| AMD Strix Halo, 96 GB+ class | ds4 (ROCm) | DeepSeek V4 Flash Q2 or GLM 5.3 Q2; not Qwen3.8 | the ds4 hardware page |
| Linux with an NVIDIA GPU, 96 GB+ class | ds4 (CUDA) | DeepSeek V4 Flash or GLM 5.3 Q2 | the ds4 hardware page |
| Anything else | Ollama | `qwen2.5:7b` | this repository: Turing's default |

The ds4 rows come from the hardware guidance on [dwarfstar.sh](https://dwarfstar.sh),
ds4's own site, as recorded in the engine design's section 5.4 on 2026-10-25.
ds4's supported models change, so read that page before buying hardware for
it; Turing's table holds only what was checked. When memory cannot be read the
recommendation is Ollama, because a wrong guess toward ds4 costs more than a
wrong guess toward Ollama. Any other OpenAI-compatible server works through
`openai`, and Turing calls an engine usable only once `engine check` passes.

The served model Turing prefers is `qwen2.5:7b` on Ollama and, on ds4,
`deepseek-chat` (ds4's non-thinking alias) when the engine lists it.

### The memory classes

The classes are in GiB of memory. The tolerance below the nominal 64 and 96 GB
covers what firmware and the kernel keep for themselves, which is why a "64 GB"
machine reports less:

- Under 58 GiB is below the 64 GB class.
- 58 GiB to under 88 GiB is the 64 GB class.
- 88 GiB and over is the 96 GB+ class.

**Strix Halo.** On an AMD Strix Halo APU (a CPU named "Ryzen AI Max") the BIOS
can move most of the unified memory out of `MemTotal` and give it to the GPU
as a carve-out. The memory counted is therefore `MemTotal` plus the amdgpu
VRAM carve-out (`mem_info_vram_total`, summed over the cards). If the carve-out
cannot be read, `detect` and `setup` say so, and the memory they show may be
far lower than what is installed.

## Linux networking: where an engine must listen

On Docker Desktop for Mac, `host.docker.internal` reaches the host's loopback,
so an engine on `127.0.0.1` works. On Linux, Compose resolves
`host.docker.internal` to the Docker bridge's gateway, so an engine listening
only on `127.0.0.1` cannot be reached from the containers. `detect` and `setup`
look the gateway up (`docker network inspect bridge`; it is often
`172.17.0.1`), say which engines answer only on `127.0.0.1`, and print the
change that fixes each one:

- **ds4:** start the server on the gateway, as in
  `ds4-server --host <gateway>`.
- **Ollama:** it has no listen flag; it reads `OLLAMA_HOST`. Set it for the
  service, then restart it:

  ```bash
  sudo systemctl edit ollama
  # in the editor that opens, add:
  #   [Service]
  #   Environment="OLLAMA_HOST=<gateway>"
  sudo systemctl restart ollama
  ```

  Once the server listens on the gateway, the host's own `ollama` command
  (`ollama list`, `ollama pull`) needs `OLLAMA_HOST=<gateway>` too, in the shell
  that runs it; without it the command looks for the server on `127.0.0.1` and
  cannot find it.

Do not use `0.0.0.0` on a machine attached to a shared network: it publishes
the engine, which has no login of its own by default, to every network the
machine is on. `check` proves reachability from inside the container; when it
fails on Linux for an engine that answers only on `127.0.0.1`, it names the
same change, then `./turing engine check`.

## Proving an engine: `./turing engine check`

`./turing engine check` runs the runtime's `check-engine` subcommand in a
one-off runtime container (`compose run --rm --no-deps`), so the probe uses the
real configuration, the container's network and the production provider code.
It needs Docker, and it runs only in an image whose `org.turing.checks` label
lists `engine`; an older image is told to run `./turing start --build` first,
because an image that predates the check would ignore the argument and start
the service.

The probe sends a fixed prompt and no user data, and it never dials the
orchestrator. **`check-engine` is the only check that uses the network, and
only to reach the configured engine**: `check-config` in both services makes
no request at all.

It runs four steps in order and stops at the first that fails. The first two
are listings, so they get 10 s each. The other two each get 300 s, because a
large model may still be loading when the first completion arrives.

| Step | What it asks | Timeout | A failure means |
| --- | --- | --- | --- |
| 1/4 reachable | Ollama: `GET /api/version`. OpenAI-compatible: `GET <url>/models`. | 10 s | Nothing answers where the containers look: the engine is stopped, `TURING_LOCAL_ENGINE_URL` is wrong, on Linux the engine listens only on `127.0.0.1`, or it answers 401 or 403 because it wants an API key (or refused the one set). |
| 2/4 model present | Ollama: `/api/tags`, `models[].name`, where an untagged name also matches `<model>:latest`. OpenAI-compatible: `/models`, `data[].id`. | 10 s | `TURING_LOCAL_MODEL` is not there: Ollama does not have it (run `ollama pull`), or an OpenAI-compatible server lists no models. A model not listed while the list is non-empty still passes, with the note `TURING_LOCAL_MODEL is not listed; treating it as an alias`, because such a server's names are its own aliases. |
| 3/4 streaming | One short completion, which must stream at least one non-empty text delta and finish with a `completed` event. | 300 s | The model streamed no text (only reasoning, or nothing), the stream failed or ended early, or no complete answer came in time. |
| 4/4 tool calling | A fixed prompt offers one tool, `turing_engine_check`, and needs a tool call naming it with parsed arguments. A call to another tool beside it does not matter. | 300 s | The model or server does not do tool calling (it answered in text), the arguments did not parse as a JSON object, or the model called a different tool. Turing depends on tool calling, and engines differ here, so this is the step that turns "speaks the API" into "usable for Turing". |

Each passed step prints `N/4 <name>: ok` on stdout, with its note in
parentheses when it has one. A failure is one sentence on stderr, naming the
step, the reason and what to change, and exits 1:
`step N/4 (<name>) failed: <reason>; <what to change>`. When all four pass the
output ends with `the engine is usable for Turing`.

`./turing engine check` exits 0 when the engine is usable and 1 when a step
failed or the check could not run (Docker is not running, or the image predates
`check-engine`). `./turing status` lists the engine's models from the host, but
only `engine check` proves the engine works for Turing from inside the runtime.

A check never blocks startup. After `engine use`, `config set` of a local-engine
setting and `setup`, the check runs once Turing is healthy; when it fails, the
settings stay saved and Turing stays running, and the output says to fix the
engine and run `./turing engine check` again. With `--no-apply` nothing is
running yet, so the output says to run it after `./turing start`.

## Switching engines

- **`./turing setup`** again. It shows what answers and what Turing recommends,
  offers the engines found (the current choice is the default), saves, starts
  Turing, checks the engine and shows how the app connects. With `--yes` it
  asks nothing: flags win, a rerun keeps every current choice, and a first run
  takes the recommended engine only when it is the one engine answering and its
  model is clear. Otherwise it keeps Ollama with `qwen2.5:7b` and prints the
  `engine use` command for the recommendation. Keeping an engine that `.env`
  names at `localhost` or `127.0.0.1`, which inside a container is the
  container itself, saves it at `host.docker.internal`; with `--yes` setup
  changes nothing and prints the `engine use` command that does.
- **`./turing engine use <ollama|openai> [--url URL] [--model M] [--context N]
  [--label L] [--api-key-stdin] [--yes] [--no-apply]`.** `openai` needs `--url`
  and `--model`. It saves the choice the way `config set` does, checking it
  with the services first, then applies it and runs `engine check`. For example,
  for ds4 on this machine:

  ```bash
  ./turing engine use openai --url http://localhost:8000/v1 --model deepseek-chat
  ```

  That saves `http://host.docker.internal:8000/v1`, and the app labels the
  engine ds4 because it is an OpenAI-compatible server on port 8000 (`--label`
  names another). The engine's API key is never taken on the command line:
  `--api-key-stdin` reads it from standard input, or asks with typing hidden at
  a terminal. A saved key is kept only for the server it was set for: choosing
  Ollama, or an OpenAI-compatible engine at another address, removes it.
- **`./turing config set TURING_LOCAL_MODEL <model>`** (or another `TURING_LOCAL_*`
  key) changes one setting, then proves the engine the same way. Setting
  `TURING_LOCAL_ENGINE` or `TURING_LOCAL_ENGINE_URL` keeps the saved key only
  as `engine use` does.

An engine URL must name this machine: `localhost`, `127.0.0.1` or `[::1]`
(rewritten to `host.docker.internal`), `host.docker.internal`, with no user,
password, query or fragment. `engine use` and `setup` refuse any other URL and
save nothing.

## Engine caches

Turing's deletion lifecycle reaches only Turing's own stores. An engine that
caches prompts to disk keeps conversation text outside it. ds4's
`--kv-disk-dir` is one such cache: with it, session prefixes survive server
restarts. For Turing's engine, leave `--kv-disk-dir` off, or accept that
deleting a conversation in Turing does not clear the engine's cache.
`detect` and `setup` repeat this when they recommend ds4 or find an
OpenAI-compatible engine.

## What this does not do

- **It installs no engine and downloads no model.** The owner installs and
  starts the engine; Turing does not install, start or supervise one. `ollama
  pull` and `./download_model.sh` stay the owner's steps.
- **It does not support an engine on another machine.** That is remote egress,
  with its own per-send disclosure and consent, not a local engine. A LAN
  address is refused.
- **One local engine and model per install.**
