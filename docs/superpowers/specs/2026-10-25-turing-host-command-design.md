# The `turing` host command: design

**Status:** proposed.

**Date:** 2026-10-25.

**Roadmap:** OPS-001 in `docs/NORTH_STAR.md`. This design also delivers slice 2
of the [local inference engines design](2026-10-25-turing-local-inference-engines-design.md)
(choosing an engine for the machine).

## 1. Goal

Set up, run and reconfigure a Turing server without editing `.env` or reading
shell scripts. One command on the server machine walks through first-time
setup, shows whether everything is healthy, changes settings safely, and tells
the user how to connect the app.

### Why the app alone is not enough

The Turing app can only configure a server that is already running and that
it can reach. Four kinds of work happen before that point, or outside what the
orchestrator is allowed to do:

1. **First-time setup.** This means generating secrets, creating the data
   directories, and getting the app's key into the app. The app cannot
   connect until this is done.
2. **Facts only the host knows.** This covers the hardware, which inference
   engines are listening, and what the Docker bridge address is on Linux.
3. **Container lifecycle.** This means start, stop, restart, logs, and
   applying a setting that needs a restart. The orchestrator has no Docker
   control, and the roadmap keeps it that way (owner decision 7: no
   self-installation or self-escalation).
4. **Reach.** The orchestrator listens only on `127.0.0.1`, with no
   encryption on the connection. Using the app from another machine needs a
   bridge until SEC-001 ships device pairing and TLS (owner decision 8).

The server-side surface stays small. Anything the orchestrator can safely
change while running belongs in the app (OPS-002, a separate design).

### What was asked, and what is assumed

The user asked:

- for a UX-friendly way to change Turing's server configuration ("I don't want
  to be changing md or .env files by hand");
- whether Turing also needs a server-side UX ("I think we might need that too").

The user plans to run Turing on a 128 GB Framework Desktop with ds4.

These assumptions were made under autopilot and are open to correction:

1. **The Framework Desktop runs headless,** and the user reaches it from the
   Mac over SSH. Sitting at the machine works the same way.
2. **The server UX is a terminal command, not a web console** (section 5).
3. **Each setting has one home** (section 4). Until OPS-002 ships, every
   setting a user should change is changed through this command.
4. **Editing specialist profiles and skills** in a form is a later, separate
   design (OPS-003), because it changes the read-only `/team` mount's threat
   model.

## 2. Where Turing is today

- **Setup is `scripts/init.sh`.**
  - It creates `.env` from `.env.example` (mode 0600) and generates every
    token and secret.
  - It provisions `data/`, `sandbox/`, `skills/`, `mcp/`, `memory/` and
    `team/`.
  - It prints the app's key at the end.
  - It is idempotent and never asks anything.
- **Running is `scripts/dev.sh`.** It runs `init.sh`, then
  `scripts/compose.sh up --build` in the foreground.
- **`scripts/compose.sh`** wraps `docker compose`.
  - It validates every bind source (real directory, mode 0700) before Docker
    resolves it.
  - It pins `--env-file .env`.
  - It recovers from a stale project.
- **Every service has `restart: unless-stopped`.** The stack comes back after
  a reboot once Docker starts, unless the user stopped it.
- **Changing a setting means editing `.env`** and recreating the containers
  that read it. There are about 50 keys, with comments in `.env.example`. A
  bad value fails the service at startup, and the error names the key.
- **`scripts/rotate-client-key.sh`** replaces the app's key with `sed`.
- **The app stores a backend URL and key** in secure storage. The URL
  defaults to `http://localhost:3000`, and the user pastes the key.
- **The public port is published as `127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT}`**
  and serves plain gRPC. `HealthService/Check` requires the client key, like
  every public method.
- **The local engine is configured by hand.** After #169, ds4 works by
  setting the `TURING_LOCAL_*` keys. The engine design's `local-engine.sh`
  (detect, use, check) is not built yet.

## 3. Summary of the design

The design adds a `turing` command that the user runs in a terminal on the
server.

```text
turing setup               guided first-time setup; rerun to change choices
turing status              one screen: is everything working, and if not, why
turing start | stop | restart [--build]
turing logs [service] [-f]
turing connect [--rotate-key]
turing config [show | get KEY | set KEY [VALUE]]
turing engine detect | use | check
```

- It is a Go program in the root module, at `turing-backend/cmd/turing`.
  A wrapper script, `turing-backend/turing`, runs it.
- It drives the existing scripts rather than replacing them:
  - `init.sh` still generates every secret and provisions every directory;
  - `compose.sh` still runs every Docker command and validates every bind;
  - `dev.sh` keeps working for development.
- It writes only `.env` and nothing else.
- It never opens the database.
- It reads the orchestrator's state only through `HealthService/Check`, with
  the client key, exactly as the app does.

## 4. Where each setting is changed

Each setting has one home and one way to change it. The "later" column is
OPS-002's proposed direction, recorded here so the two designs agree. OPS-002
decides it.

| Settings | Keys | Changed by (this design) | Later (OPS-002) |
| --- | --- | --- | --- |
| Generated secrets and service tokens | `TURING_*_TOKEN`, `*_SECRET`, `TURING_INTEGRATION_KEY`, `MCP_*_TOKEN_GENERAL` | Never by hand. `init.sh` generates them once. | Unchanged |
| The app's key | `TURING_CLIENT_API_KEY` | `turing connect --rotate-key` | Unchanged until SEC-001 |
| Internal wiring | `ORCHESTRATOR_GRPC_ADDR`, `ORCHESTRATOR_INTERNAL_PORT`, `DATABASE_PATH`, `*_ROOT`, `*_BASE_URL`, `HOST_UID`, `HOST_GID`, `MEMORY_DISPLAY_ROOT` | Not editable. `init.sh` writes what it must. | Unchanged |
| App port | `ORCHESTRATOR_PUBLIC_PORT` | `turing config` | Unchanged (host) |
| Local engine | `TURING_LOCAL_*`, `OLLAMA_KEEP_ALIVE` | `turing setup`, `turing engine use`, `turing config` | Unchanged. Only the host can detect and check its engine. |
| Remote model | `OPENAI_*` | `turing setup`, `turing config` | In the app; the key sealed in the database |
| External agents' keys | `TURING_AGENT_API_KEYS` | `turing config` | In the app |
| Agent team | `TURING_AGENT_TEAM_ENABLED`, `TURING_MAX_DELEGATIONS_PER_RUN`, `TURING_DELEGATION_RESULT_MAX_BYTES` | `turing setup`, `turing config` | In the app, applied live |
| Queue, limits, timeouts | `TURING_JOB_*`, `TURING_QUEUE_*`, `TURING_*_TIMEOUT_MS`, `TURING_MAX_*`, `TURING_AUTOMATION_TICK_MS` | `turing config` | In the app, applied live |
| Persona and profile | `memory/persona.md`, `memory/profile.md` | The app (shipped) | Unchanged |
| Specialist profiles and skills | `team/<id>/AGENT.md`, `skills/**/SKILL.md` | Files, as today | Forms in the app (OPS-003) |

The deprecated `OLLAMA_*` names that #169 replaced are migrated, not exposed.
Saving any setting renames a deprecated key to its new name, or drops it when
the new name is already set (the new name already wins). `turing status`
reports any deprecated key that is left.

## 5. Approaches considered

**A. A host command in the terminal (chosen).**

- It works over SSH on a headless machine, and at the machine itself.
- It adds no network listener and no daemon.
- It can drive Docker because the user running it already can. Nothing in
  Turing gains that power.
- It reuses the scripts and their tests.
- It is testable with scripted input and fake command runners.

**B. A local web console served by a host daemon.**

- A daemon with Docker access is root-equivalent. It would be a new,
  always-running privileged HTTP listener.
- On loopback it still needs an SSH tunnel from the Mac. On the LAN it needs
  SEC-001's device identity and TLS first.
- It duplicates the app.
- Rejected. It can be reconsidered as a skin over the same commands once
  SEC-001 exists.

**C. A "host mode" in the Flutter app (Linux build) with a privileged helper.**

- It needs a display on the server and still needs the helper from B.
- It mixes the client's trust plane with host control.
- Rejected.

Within A, the prompts are plain numbered menus with defaults in brackets, not
a full-screen TUI. They work in any terminal and over any SSH session, read
cleanly in a screen reader, and need no TUI dependency. Every question has a
flag, so `--yes` runs setup unattended.

## 6. Design

### 6.1 Running it

- `turing-backend/turing` is a short bash wrapper.
  - It resolves its own path through symlinks, so a link in `~/.local/bin`
    works.
  - It changes to the repository root and runs
    `go run ./turing-backend/cmd/turing`.
  - It passes the backend directory along.
- Go 1.26+ is already a requirement for running the stack. The first run
  compiles, and later runs use Go's build cache.
- The command refuses to run as root. `init.sh` takes `HOST_UID` and
  `HOST_GID` from the invoking user, and `compose.sh` already refuses a root
  ID.
- The command needs no build tags; it imports no SQLite package.

### 6.2 `turing setup`

This is the first thing a user runs on a new machine. Rerunning it shows the
current choices as defaults, and it never regenerates a secret.

1. **Preflight.**
   - Docker is running.
   - The user is not root.
   - The checkout is a TuringAgent backend.
2. **Provision.** Run `init.sh`, unchanged.
3. **Local engine.**
   - Run `engine detect` (section 6.7).
   - Show the machine, the engines that answered, and the recommendation.
   - Offer the recommended engine, any other engine found, Ollama with
     today's defaults, or a URL the user types.
   - Then offer the models that engine reports, with the recommended one
     first.
4. **Remote model, optional.**
   - Off by default.
   - Asks for a base URL, a model and an API key. The key is read with echo
     off.
   - The prompt says that each send to it asks the user first.
5. **Agent team, optional.** Off by default, and labelled experimental.
6. **Review and save.**
   - Show the changes.
   - On confirmation, save them through the same path as `turing config`
     (section 6.5), including its validation.
7. **Start.**
   - Build and start the stack.
   - Wait for `HealthService/Check`.
   - Run `engine check`.
8. **Connect.** Print what `turing connect` prints.

An example on the Framework Desktop:

```text
$ ./turing setup
Checking Docker... ok
Provisioning (scripts/init.sh)... ok

This machine: Linux, AMD Ryzen AI Max (Strix Halo), 128 GB
Engines answering: ds4 at 172.17.0.1:8000 (models: deepseek-chat)
Recommended: ds4 with DeepSeek V4 Flash (see docs/architecture/local-engines.md)

Local engine:
  1) ds4 at 172.17.0.1:8000  (recommended)
  2) Ollama with qwen2.5:7b
  3) Another address
Choose [1]:
Model:
  1) deepseek-chat  (recommended: answers without a separate thinking pass)
  2) Another model name
Choose [1]:
Add a remote model? Each send to it asks you first. [y/N]:
Turn on the agent team (experimental)? [y/N]:

Saving: TURING_LOCAL_ENGINE=openai, TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000,
        TURING_LOCAL_MODEL=deepseek-chat, TURING_LOCAL_ENGINE_LABEL=ds4
Checking the settings with the orchestrator and runtime... ok
Building and starting... ok
Checking the engine from the runtime container... reachable, model listed, streams, calls tools
Turing is running. To connect the app: see below, or run ./turing connect
```

### 6.3 `turing status`

`status` prints one screen and exits 0 only when everything is healthy, so a
script or a cron job can use it.

| Line | How it is checked |
| --- | --- |
| Setup | `.env` exists, is a regular file at mode 0600, and holds the generated keys. |
| Docker | `docker info` answers. On Linux, `systemctl is-enabled docker` also runs, and a disabled Docker produces a warning that Turing will not come back after a reboot. |
| Services | `compose.sh ps`: each service's state, and its health where it has a healthcheck. |
| App port | `HealthService/Check` on `127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT}` with the client key. |
| Engine | The engine's model list, fetched from the host. `host.docker.internal` maps to `127.0.0.1` on macOS and to the bridge gateway on Linux. It notes that `turing engine check` is the in-container proof. |
| Agent team | On or off, labelled experimental. |
| Warnings | Deprecated keys, a stopped service, a world-readable `.env`, or a Linux engine listening only on `127.0.0.1`. |

`status` never prints a secret.

### 6.4 `turing connect`

`connect` prints the app's backend URL and key. The key is shown only by this
command, with a line saying that anyone holding it can use Turing.

**From another machine, before SEC-001.** When `SSH_CONNECTION` is set,
`connect` also prints an SSH tunnel to run on the machine with the app:

```text
On the machine where the app runs, open a tunnel and keep it open:
  ssh -N -L 3000:127.0.0.1:3000 mike@192.168.1.40
Then use Backend URL http://localhost:3000 in the app.
To keep it, add to ~/.ssh/config:
  Host turing
    HostName 192.168.1.40
    User mike
    LocalForward 3000 127.0.0.1:3000
```

The host and user come from `SSH_CONNECTION` (the server address) and `$USER`.
The tunnel keeps owner decision 8:

- the orchestrator still sees a loopback connection;
- SSH supplies authentication and encryption;
- no port opens to the network.

SEC-001 replaces this with `turing pair`.

**`--rotate-key`:**

1. Generate a new key in the existing format (`tk_` and 64 hex characters).
2. Save it.
3. On confirmation, restart the orchestrator.
4. Print the new key.

`rotate-client-key.sh` stays as a thin wrapper that calls this.

### 6.5 `turing config`

- **`turing config`** with no arguments opens a menu.
  - First pick a section from section 4's table, then a setting.
  - Each setting shows:
    - its description, taken from the comment above it in `.env.example`, so
      there is one source of text;
    - its current value and its default;
    - a prompt for the new value.
- **`show`** lists every editable setting, with each secret shown only as
  `set` or `not set`.
- **`get KEY`** prints one value, refusing a secret.
- **`set KEY VALUE`** saves one value.
  - A secret is never accepted on the command line, where shell history and
    `ps` would keep it. `set OPENAI_API_KEY` prompts with echo off, or reads
    standard input with `--stdin`.
  - `TURING_AGENT_API_KEYS` is edited one agent at a time, as
    `set TURING_AGENT_API_KEYS.<name>` and `unset TURING_AGENT_API_KEYS.<name>`.
    The user never writes JSON.

**The registry.** A table in the command lists every key in `.env.example`
and in Compose. Each key is marked editable or not, with its section, kind
(text, number, duration, boolean, choice, URL, secret, or secret map) and its
deprecated names. A test keeps the registry, `.env.example` and the compose
file in agreement. Editable keys are those read only by the orchestrator and
the runtime, and a test enforces that too.

**Validation.** The services validate their own settings, so the command
never re-implements a rule.

- Both service binaries gain a `check-config` subcommand.
  - It loads configuration exactly as startup does.
  - It stops before opening the database, a file, a socket or the network.
  - It prints the first error, which names the key, and exits 0 or 1.
- Prompts may give instant feedback by reusing shared packages, such as
  `internal/egress`'s endpoint and label rules. That is advisory only:
  `check-config` is the gate.

**Saving:**

1. Take an advisory lock (`flock`) on `.env.lock`, so two commands cannot
   interleave.
2. Read `.env`, refusing a symlink or a non-regular file, as `init.sh` does.
3. Replace each changed key's value on its existing line. A missing key is
   appended. Comments, order and unknown keys are kept.
4. Write the result atomically: a temporary file in the same directory at
   mode 0600, `fsync`, then rename.
   - A value is written plain when `init.sh`'s literal-safe rule allows it,
     and as a Compose single-quoted literal otherwise.
   - A test runs the same cases through the bash writer and the Go writer.
5. If the images exist, run `check-config` in the orchestrator and runtime
   containers through `compose.sh run --rm --no-deps`. On a failure, write
   the previous bytes back and show the service's message.
   - If the images do not exist yet, say that the settings are checked at
     first start. In setup they always exist, because setup builds first.
6. Offer to apply. `compose.sh up -d` recreates only the containers whose
   configuration changed. After that comes a health wait, plus
   `engine check` when an engine key changed.
   - The prompt warns that a restart stops a reply in progress.

### 6.6 `turing start | stop | restart | logs`

- **`start`** runs `compose.sh up -d`. **`--build`** rebuilds first, which is
  what to run after pulling a new version. It waits for health.
- **`stop`** runs `compose.sh stop`. Because of `unless-stopped`, a stopped
  stack stays stopped after a reboot. `stop` says so.
- **`restart`** is `stop` then `start`, asks for confirmation, and passes
  `--build` through.
- **`logs`** runs `compose.sh logs --tail 200` with friendly service names
  (`orchestrator`, `runtime`, `files`, `system`) and `-f` to follow.

`dev.sh` remains the foreground development loop.

### 6.7 `turing engine`

This is section 5.4 of the local inference engines design, delivered here
with three changes:

- **It lives in Go.** It is `turing engine detect | use | check` instead of
  `local-engine.sh`, and it shares the registry, writer and validation in
  section 6.5.
- **`detect` also lists the models** an engine reports, from `/api/tags` or
  `/v1/models`, so setup can offer them.
- **`init.sh` does not run `detect`.** `turing setup` asks instead. `init.sh`
  stays non-interactive, so `dev.sh` and CI behave as before.

Everything else is as written there:

- the hardware facts read on macOS and Linux;
- the recommendation table;
- the Linux bridge-gateway guidance;
- the engine-cache privacy note;
- `use` rewriting a host-side `localhost` to `host.docker.internal`;
- `check` running the runtime's `check-engine` subcommand in the runtime
  container. Its four steps are reachable, model present, streaming and tool
  calling, and the probe sends a fixed prompt with no user data.

## 7. Security and trust boundaries

- **Only a person runs it.**
  - The command is not an MCP tool or an RPC, and it does not run in any
    container.
  - The orchestrator gains no Docker socket and no write access to `.env`.
  - Nothing an agent does can reach it (owner decision 7).
  - A settings change made here is the host owner's own action. It needs no
    approval, because the person at the terminal already controls the
    machine.
- **The orchestrator stays the owner of state** (owner decision 2).
  - The command never opens SQLite.
  - It talks to the orchestrator as a client, with the client key, and only
    for `HealthService/Check`.
- **There is no new listener or daemon.** Remote use before SEC-001 is an SSH
  tunnel, so LAN membership grants nothing.
- **Secrets:**
  - never on the command line;
  - read with echo off;
  - shown as `set` or `not set`;
  - the app's key printed only by `connect`;
  - never written to a log or into the command's own output otherwise.
- **`.env` integrity:**
  - mode 0600, regular file, no symlink;
  - an atomic replace;
  - an advisory lock;
  - the previous bytes restored when validation fails.
- **Not root.** It refuses to run as root, for the ownership reasons in
  section 6.1.

## 8. Error handling

Every failure is one sentence that names what is wrong, followed by the next
command to run.

- **Docker is not running.** Say so, and say how to start it on this OS.
- **Not set up.** Say "run `./turing setup`".
- **A setting was refused.**
  - Show the service's message, which names the key.
  - The file is left as it was.
- **A Compose failure.** Show the last lines of Compose's output and point to
  `turing logs`.
- **The engine is unreachable or fails a check step.** Show the step and what
  to change, per section 6 of the engine design. On Linux this includes the
  `--host` flag for the bridge gateway.
- **The health wait times out.** Show which service is not healthy and the
  `logs` command for it.

## 9. Testing

- **Registry:**
  - every `.env.example` key and every Compose environment key is classified;
  - editable keys are read only by the orchestrator and the runtime;
  - descriptions come from `.env.example`.
- **Writer:**
  - in-place replacement and appending;
  - comments and order kept;
  - mode 0600;
  - a symlink refused;
  - the lock respected;
  - an atomic replace;
  - deprecated names migrated;
  - the bash and Go literal writers agreeing on the same cases.
- **`check-config` in both services:**
  - a valid configuration exits 0;
  - each existing startup refusal exits 1 and names the key;
  - it opens no database, file or socket.
- **`config`:**
  - a secret is refused on the command line;
  - `--stdin`;
  - `show` masks secrets;
  - the secret-map edit;
  - a failed validation restores the previous bytes.
- **Lifecycle and status** run with a fake command runner that records
  `compose.sh` calls and returns fixture output, plus a fake health server.
  The checks cover:
  - each status line, healthy and unhealthy;
  - the exit codes;
  - no secret in any output.
- **`connect`:**
  - the tunnel text from a fixture `SSH_CONNECTION`;
  - no tunnel text without it;
  - rotation in the key format, followed by a restart.
- **`setup`:**
  - scripted answers to every prompt;
  - `--yes`;
  - a rerun keeping existing choices and never touching a secret.
- **`engine`:** the engine design's tests (fixture `/proc` and `sysctl`,
  fake engines, the four check steps), now in Go.
- **Docs guards:** `tools/docs` is updated wherever the README's quick start
  or a key list changes.

## 10. Delivery slices

1. **OPS-001a: operate without editing files.**
   - The wrapper.
   - `status`, `start`, `stop`, `restart`, `logs`, and `connect` (with
     rotation).
   - `config`: the registry, the writer, `check-config` in both services,
     and applying.
   - The README's quick start.
   - After this slice, ds4 on the Framework Desktop is configured with
     `turing config` rather than an editor.
2. **OPS-001b: set up for the machine.**
   - `engine detect`, `use` and `check`.
   - The runtime's `check-engine`.
   - `setup`.
   - `docs/architecture/local-engines.md`.
   - This closes slice 2 of the engine design.

OPS-002 (settings in the app, applied live) and OPS-003 (profile and skill
editors) each get their own design afterwards.

## 11. Non-goals

- A web console, a daemon, or any new network listener (section 5).
- Installing, starting or supervising an inference engine, or downloading a
  model. This is the engine design's non-goal too.
- Updating the checkout. `git pull` followed by `turing restart --build` stays
  the user's step.
- Backups and restore (TUR-016).
- Reaching the server without SSH before SEC-001.
- Live settings and storing settings in the database (OPS-002).
- Editing profiles and skills (OPS-003).
- Windows hosts.

## 12. Open questions (decided, open to correction)

1. **Terminal or web?** Terminal (section 5).
2. **Go or bash?** Go. It reuses the shared validators and the generated
   health client, and it is testable with fakes. The scripts stay the engines
   for provisioning and Compose.
3. **Who validates a setting?** The services, through `check-config`, so
   there is one set of rules.
4. **How does the Mac reach a headless server before SEC-001?** An SSH tunnel
   that `connect` writes out.
5. **Should this command edit settings that OPS-002 will move into the app?**
   Yes, until OPS-002 ships; leaving them hand-edited in the meantime is what
   the user asked to stop. OPS-002 decides the move. The proposal is to import
   them from `.env` once, after which `turing config` edits them through an
   orchestrator settings RPC, as a client.
