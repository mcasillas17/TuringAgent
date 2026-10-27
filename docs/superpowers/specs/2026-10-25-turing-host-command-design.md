# The `turing` host command: design

**Status:** approved (#170). PR 1 (`check-config`) is implemented. On
2026-10-26 the owner decided section 12's question 4: **b**, the app runs on
the server until SEC-001.

**Date:** 2026-10-25.

**Roadmap:** OPS-001 in the [canonical roadmap](../../NORTH_STAR.md).
This design also covers slice 2
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
4. **Reach.** The orchestrator's port is published only on the host's
   `127.0.0.1`, with no encryption on the connection, and the README's
   supported setup runs the app on the same machine. Using the app from
   another machine needs SEC-001's device pairing and TLS (owner decision 8).
   Section 6.4 proposes an interim bridge that needs the owner's sign-off.

The server-side surface stays small. Anything the orchestrator can safely
change while running belongs in the app (OPS-002, a separate design).

### What was asked, and what is assumed

The user asked:

- for a UX-friendly way to change Turing's server configuration ("I don't want
  to be changing md or .env files by hand");
- whether Turing also needs a server-side UX ("I think we might need that too").

The user plans to run Turing on a 128 GB Framework Desktop with ds4.

These assumptions were made under autopilot and are open to correction:

1. **The app runs on the server until SEC-001.** The owner decided this
   (section 12, question 4): on the Framework Desktop, the Flutter Linux build
   is the client, and the Mac reaches Turing only once SEC-001 pairs it.
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
  - It skips those launch checks only for `down`, `stop`, `rm` and `kill`.
    `ps` and `logs` go through them, so on a broken install they fail too.
  - It pins `--env-file .env`.
  - When Compose cannot read `.env`, it retries a teardown without that file.
- **Compose reads the shell environment ahead of `--env-file`.** A key
  exported in the user's terminal overrides `.env`; `compose.sh` relies on
  this for `MEMORY_DISPLAY_ROOT`.
- **Neither service binary reads its arguments.** Both Dockerfiles use
  `ENTRYPOINT`, so an extra argument to `compose run` is ignored and the
  service starts normally, against the live database.
- **Every service has `restart: unless-stopped`.** The stack comes back after
  a reboot once Docker starts, unless the user stopped it.
- **Changing a setting means editing `.env`** and recreating the containers
  that read it. There are about 50 keys, with comments in `.env.example`. A
  bad value fails the service at startup, and the error names the key.
- **`scripts/rotate-client-key.sh`** replaces the app's key with `sed`.
- **The app stores a backend URL and key** in secure storage. The URL
  defaults to `http://localhost:3000`, and the user pastes the key.
- **The public port is published as `127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT}`**
  and serves plain gRPC. Inside its container the orchestrator listens on all
  interfaces; the host restriction comes from Compose's port publication, not
  an in-process check. `HealthService/Check` requires the client key, like
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
- It writes `.env` itself. Everything else it provisions is still written by
  `init.sh`.
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
| Queue, limits, timeouts | `TURING_JOB_*`, `TURING_QUEUE_*`, `TURING_*_TIMEOUT_MS`, `TURING_MAX_TOOL_CALLS_PER_RUN`, `TURING_AUTOMATION_TICK_MS` | `turing config` | In the app, applied live |
| Concurrency | `TURING_MAX_CONCURRENT_RUNS_GENERAL` | Not editable. It stays 1 until AGT-000 supplies the evidence to raise it. | Decided by AGT-000 |
| Persona and profile | `memory/persona.md`, `memory/profile.md` | The app (shipped) | Unchanged |
| Specialist profiles and skills | `team/<id>/AGENT.md`, `skills/**/SKILL.md` | Files, as today | Forms in the app (OPS-003) |

The deprecated `OLLAMA_*` names that #169 replaced are migrated, not exposed.
Saving any setting follows the services' own fallback, against the candidate
being saved:

- An old value moves to its new name only where the services would read it:
  the new name is empty or absent and, for `OLLAMA_BASE_URL`, the engine is
  `ollama`. The runtime deliberately ignores an old Ollama URL for the
  `openai` engine, so moving it there would turn a startup refusal into a
  local route pointing at the wrong server.
- In every case the old line is then removed, because nothing else reads it.

`turing status` reports any deprecated key that is left.

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
cleanly in a screen reader, and need no TUI dependency. Every question except
a secret has a flag, so `--yes` runs setup unattended. An unattended setup
leaves the remote model off unless its key arrives on standard input
(`--remote-key-stdin`).

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

This is the first thing a user runs on a new machine.

- **On a rerun,** the current choices are the defaults. A recommendation is
  shown beside them but never replaces one, and `setup --yes` on a rerun keeps
  every current choice.
- It never regenerates a secret.

The steps:

1. **Preflight.**
   - Docker is running.
   - The user is not root.
   - The checkout is a TuringAgent backend.
2. **Provision.** Run `init.sh`, unchanged.
3. **Build.** Run `compose.sh build`, so the images that validate settings
   exist before anything is saved (section 6.5).
4. **Local engine.**
   - Run `engine detect` (section 6.7).
   - Show the machine, the engines that answered, and the recommendation.
   - Offer the recommended engine, any other engine found, Ollama with
     today's defaults, or a URL the user types.
   - Then offer the models that engine reports, with the recommended one
     first.
5. **Remote model, optional.**
   - Off on a first run.
   - Asks for a base URL, a model and an API key. The key is read with echo
     off.
   - The prompt says that each send to it asks the user first.
6. **Agent team, optional.** Off on a first run, and labelled experimental.
7. **Review and save.**
   - Show the changes.
   - On confirmation, save them through the same path as `turing config`
     (section 6.5), including its validation.
8. **Start.**
   - Start the stack.
   - Wait for `HealthService/Check`.
   - Run `engine check`.
9. **Connect.** Print what `turing connect` prints.

An example on the Framework Desktop:

```text
$ ./turing setup
Checking Docker... ok
Provisioning (scripts/init.sh)... ok
Building the images... ok

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

Saving: TURING_LOCAL_ENGINE=openai, TURING_LOCAL_ENGINE_URL=http://host.docker.internal:8000/v1,
        TURING_LOCAL_MODEL=deepseek-chat, TURING_LOCAL_ENGINE_LABEL=ds4
Checking the settings with the orchestrator and runtime... ok
Starting... ok
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
| App port | `HealthService/Check` on `127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT}` with the client key from `.env`. When the check is refused as unauthenticated, the line says the key in `.env` is not the one the running orchestrator started with, which happens after a rotation that was not applied, and names `turing restart`. |
| Engine | The engine's model list, fetched from the host. `host.docker.internal` maps to `127.0.0.1` on macOS and to the bridge gateway on Linux. It notes that `turing engine check` is the in-container proof. |
| Agent team | On or off, labelled experimental. |
| Warnings | Deprecated keys; a stopped service; an `.env` not at mode 0600; a Linux engine listening only on `127.0.0.1`; and a registry key exported in the current shell, which Compose would otherwise prefer to `.env` (section 6.5). |

`status` never prints a secret.

### 6.4 `turing connect`

On the server, `connect` prints the app's backend URL
(`http://localhost:${ORCHESTRATOR_PUBLIC_PORT}`) and key, with a line saying
that anyone holding the key can use Turing. This is the README's supported
setup: the app on the same machine.

**From another machine: not until SEC-001 (owner decision, 2026-10-26).**
Owner decision 8 and the identity invariants require a device identity, a
scoped credential, revocation and encrypted transport for every non-loopback
native client, and that is SEC-001. Until it ships, the app runs on the
server. `connect` run over SSH (`SSH_CONNECTION` set) prints the same-machine
text, then says that the app must run on this machine until SEC-001 pairs
other devices, and prints no tunnel.

*Not adopted:* a bounded SSH exception was proposed. It would have used a
local forward from the owner's own devices to the server's
`127.0.0.1:${ORCHESTRATOR_PUBLIC_PORT}`, authenticated by SSH, with key
rotation on losing a device. The owner chose not to make an exception to the
identity invariant; such a device would have held the shared desktop key with
no per-device revocation.

**`--rotate-key`:**

1. Generate a new key in the existing format (`tk_` and 64 hex characters).
2. Save it with the locked, atomic writer from section 6.5, skipping
   `check-config`. The value is generated in a fixed format, and recovering
   from a lost device must not depend on Docker or on current images, just as
   `rotate-client-key.sh` needs only the file today.
3. On confirmation, apply it with `compose.sh up -d`, which recreates the
   orchestrator with the new environment. `docker compose restart` would not
   reload it.
4. Print the new key.

If the user declines step 3, the old key keeps working until the next start,
and `status` explains the mismatch. `rotate-client-key.sh` stays as a thin
wrapper that calls this.

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

**The shell cannot override a setting.** Compose prefers an exported variable
to `.env`, so every `compose.sh` call the command makes runs with every
registry key removed from its environment. The only exception is the
candidate values during validation, below. `status` warns when the user's
shell exports one, because the user's own `docker compose` calls would still
see it.

#### Validation

The services validate their own settings, so the command never re-implements
one of their rules.

- **`check-config`.** Both service binaries gain a `check-config` subcommand.
  - It loads configuration exactly as startup does.
  - It stops before opening the database, a file, a socket or the network.
  - It prints the first error, which names the key, and exits 0 or 1.
- **Unknown arguments are refused.** From the same change on, both binaries
  refuse any argument they do not know, instead of ignoring it.
- **Images say which checks they support.** Both Dockerfiles gain the label
  `org.turing.checks`, listing the check subcommands the binary knows:
  `config` from PR 1, and `config,engine` on the runtime from PR 3. The
  command reads it with `docker image inspect` before every check, and runs
  a check only when it is listed.
  - An image without the label was built before this change. Its binary would
    ignore `check-config` and start a second orchestrator on the live
    database, which would fence the runs in flight.
  - The command never runs such an image with an argument. It saves nothing,
    and says to run `turing start --build` first.
  - A missing image is treated the same way.
- **Host-side rules for host-owned values.** `ORCHESTRATOR_PUBLIC_PORT` must:
  - be between 1 and 65535;
  - differ from `ORCHESTRATOR_INTERNAL_PORT`;
  - be free on `127.0.0.1`, unless Turing itself holds it.

  The orchestrator's integer parser checks none of this.
- **Instant feedback.** Prompts may reuse shared packages, such as
  `internal/egress`'s endpoint and label rules. That is advisory only:
  `check-config` is the gate.
- **Refusals that need the database** cannot be caught without opening it.
  One example: the orchestrator refuses a zero `TURING_QUEUE_MAX_WAIT_MS` or
  `TURING_JOB_REAPER_INTERVAL_MS` while a delegated task waits for consent.
  Applying (below) catches these.

#### Saving

1. **Lock.** Take an advisory lock (`flock`) on `.env.lock`, so two commands
   cannot interleave.
2. **Read.** Read `.env`, refusing a symlink or a non-regular file, as
   `init.sh` does.
3. **Build the candidate in memory.**
   - Each changed key's value replaces its **last** occurrence, which is the
     one Compose and `compose.sh` read. Earlier duplicates are removed.
   - A missing key is appended.
   - Comments, order and unknown keys are kept.
   - Deprecated names are migrated (section 4).
4. **Encode values.**
   - A value is written bare only when it matches `^[A-Za-z0-9._:/@+-]*$`.
   - Any other value is written as a Compose single-quoted literal, with
     `init.sh`'s `compose_literal` rule.
   - A value that rule cannot carry (`is_compose_literal_safe`) is refused.
   - So `$NAME`, `#`, spaces and JSON arrive at the services byte for byte.
5. **Validate before writing.**
   - Run the host-side rules.
   - Run `check-config` in the orchestrator and runtime images through
     `compose.sh run --rm --no-deps`, with the changed keys passed in the
     process environment, which Compose prefers to `--env-file`. `.env` is
     not touched yet.
   - **A refusal:** nothing is saved, and the service's message is shown.
   - **An infrastructure failure** (Docker, `compose.sh`'s own checks, a
     timeout, a missing label): nothing is saved. The setting is reported as
     not checked, with the error.
6. **Write.** Only after both checks pass, write the candidate atomically: a
   temporary file in the same directory at mode 0600, `fsync`, then rename.
   An interruption before the rename leaves `.env` as it was.
7. **Apply, if the user accepts.**
   - `compose.sh up -d` recreates only the containers whose configuration
     changed, then the command waits for health. The prompt warns that a
     restart stops a reply in progress.
   - If the user declines, the change takes effect at the next start. When the
     change sets `TURING_QUEUE_MAX_WAIT_MS` or `TURING_JOB_REAPER_INTERVAL_MS`
     to 0, the command warns that the next start may refuse it, because only
     a running orchestrator can see whether a delegated task is waiting for
     consent.
   - The command keeps the previous bytes until that wait passes. If it
     fails, for example because of a refusal that needs the database or a
     port taken in the meantime, the command offers to restore the previous
     `.env` and apply again. The default is yes, and `--yes` restores without
     asking.

### 6.6 `turing start | stop | restart | logs`

- **`start`** runs `compose.sh up -d`. **`--build`** rebuilds first, which is
  what to run after pulling a new version. It waits for health.
- **`stop`** runs `compose.sh stop`. Because of `unless-stopped`, a stopped
  stack stays stopped after a reboot. `stop` says so.
- **`restart`** is `stop` then `start`, asks for confirmation, and passes
  `--build` through.
- **`logs`** runs `compose.sh logs --tail 200` with friendly service names
  (`orchestrator`, `runtime`, `files`, `system`) and `-f` to follow.
- **`compose.sh` change.** `ps` and `logs` become a separate read-only group.
  They start nothing, and `status` and `logs` must work on exactly the install
  whose bind sources are broken, so this group:
  - skips the launch checks;
  - runs Compose once, with `--env-file .env` when the file exists and
    without it otherwise;
  - never retries.

  They do not join the `down`, `stop`, `rm` and `kill` list, because that
  list also retries a failed command without `.env`. Ctrl-C makes
  `docker compose logs -f` exit with status 130, which that retry would read
  as an unreadable `.env`, restarting the stream behind the user's prompt.

`dev.sh` remains the foreground development loop.

### 6.7 `turing engine`

This is section 5.4 of the local inference engines design, delivered here
with these changes:

- **It lives in Go.** It is `turing engine detect | use | check` instead of
  `local-engine.sh`, and it shares the registry, writer and validation in
  section 6.5.
- **`use` follows section 6.5's writer.**
  - A value is written bare only when it is an allowlisted token; any other
    value is a Compose literal. Both forms read back byte for byte. The
    engine design wrote every value as a literal.
  - Besides the keys it sets, `use` also migrates deprecated names
    (section 4). The engine design said it touched no other key.
- **`detect` also lists the models** an engine reports, from `/api/tags` or
  `/v1/models`, so setup can offer them.
- **`init.sh` does not run `detect`.** `turing setup` asks instead. `init.sh`
  stays non-interactive, so `dev.sh` and CI behave as before.
- **Applying an engine change** through `config`, `use` or `setup` runs
  `engine check` after the health wait.

Everything else is as written there:

- the hardware facts read on macOS and Linux;
- the recommendation table;
- the Linux bridge-gateway guidance;
- the engine-cache privacy note;
- `use` rewriting a host-side `localhost` to `host.docker.internal`;
- `check` running the runtime's `check-engine` subcommand in the runtime
  container. Its four steps are reachable, model present, streaming and tool
  calling, and the probe sends a fixed prompt with no user data.
  - `check-engine` follows the same rules as `check-config`: it runs only
    when the image's `org.turing.checks` lists `engine`, so an image built
    before PR 3 asks for a rebuild rather than failing on an unknown
    argument; and unknown arguments are refused.

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
  - It never runs a service image that cannot prove, by its label, that it
    will treat an argument as a check rather than start the service.
- **There is no new listener or daemon.**
  - Reaching the server from another machine waits for SEC-001 (section
    6.4).
  - LAN membership grants nothing.
- **Secrets:**
  - never on the command line;
  - read with echo off;
  - shown as `set` or `not set`.
- **The app's key** is printed only by `connect`, and by `setup`, which ends
  with `connect`. During setup, `init.sh`'s own key line also passes through.
  No other secret is ever printed or logged.
- **Shell exports cannot override a setting.** Registry keys are removed from
  the environment of every Compose call (section 6.5).
- **`.env` integrity:**
  - mode 0600, regular file, no symlink;
  - validation before an atomic replace, so an interruption leaves it
    unchanged;
  - an advisory lock;
  - a restore offered when an applied change does not come up healthy.
- **Not root.** It refuses to run as root, for the ownership reasons in
  section 6.1.

## 8. Error handling

Every failure is one sentence that names what is wrong, followed by the next
command to run.

- **Docker is not running.** Say so, and say how to start it on this OS.
- **Not set up.** Say "run `./turing setup`".
- **The images are missing or predate `check-config`.** Nothing is saved.
  Say "run `./turing start --build`".
- **A setting was refused.**
  - Show the service's message, which names the key, or the host-side rule.
  - `.env` is unchanged.
- **A setting could not be checked** (Docker, `compose.sh`'s own checks, a
  timeout). Nothing is saved; say so, with the error.
- **An applied change did not come up healthy.**
  - Name the service that is not healthy.
  - Offer to restore the previous `.env` and apply again (section 6.5).
  - Point to `turing logs <service>`.
- **The health check is refused as unauthenticated.** The key in `.env`
  differs from the running orchestrator's. Say "run `./turing restart`".
- **A registry key is exported in the shell.** Warn and name it. The
  command's own Compose calls ignore it.
- **A Compose failure.** Show the last lines of Compose's output and point to
  `turing logs`, which works even when the launch checks fail (section 6.6).
- **The engine is unreachable or fails a check step.** Show the step and what
  to change, per section 6 of the engine design. On Linux this includes the
  `--host` flag for the bridge gateway.
- **The health wait times out.** Show which service is not healthy and the
  `logs` command for it.

## 9. Testing

Every new behaviour has a test that fails without it.

- **`check-config` and argument handling, in both services:**
  - a valid configuration exits 0;
  - each startup refusal that needs only the environment exits 1 and names
    the key;
  - nothing is opened: with `DATABASE_PATH` in an unwritable directory and
    the roots pointing at missing paths, the check passes or fails on
    configuration alone, and creates nothing;
  - an unknown argument is refused;
  - the Dockerfile label lists exactly the subcommands the binary dispatches
    (a test reads the Dockerfiles and the dispatch table).
- **Stale images:** with a fake Docker reporting an image with no label, a
  label not listing the check, or no image, `config set` and `engine check`
  run no `compose run`, save nothing and name `turing start --build`.
- **Registry:**
  - every `.env.example` key and every Compose environment key is classified;
  - editable keys are read only by the orchestrator and the runtime;
  - `TURING_MAX_CONCURRENT_RUNS_GENERAL` is not editable;
  - descriptions come from `.env.example`.
- **Writer:**
  - the last occurrence replaced and earlier duplicates removed;
  - appending;
  - comments and order kept;
  - mode 0600;
  - a symlink refused;
  - the lock respected;
  - deprecated names migrated by the services' rule, including an empty new
    name beside a set old name;
  - a round trip through Compose's own dotenv parser (`compose-go`, as a
    test-only dependency) for `$NAME`, `${NAME}`, ` #`, leading and trailing
    spaces, apostrophes, JSON, a value cleared to empty, and a value
    `is_compose_literal_safe` refuses.
- **`config`:**
  - a secret value on the command line is refused, for `set` and for the
    secret-map form;
  - `--stdin` reads a secret;
  - `get` refuses a secret;
  - `show` prints each secret only as `set` or `not set`;
  - `set` and `unset` of `TURING_AGENT_API_KEYS.<name>` change only that
    agent's entry, keep the JSON valid, and print no key;
  - an `OLLAMA_BASE_URL` left from before is moved for the `ollama` engine
    and dropped for `openai`;
  - declining apply with a zero queue bound prints the warning.
- **Validate before writing:**
  - a refusal leaves `.env` byte-identical;
  - an infrastructure failure leaves it byte-identical and says "not
    checked";
  - an interruption between validation and rename leaves it byte-identical;
  - the validation call passes the changed keys in its environment and no
    other registry key.
- **Shell shadowing:** with a registry key exported, the command's Compose
  calls do not see it, and `status` warns.
- **Host-side rules:** port 0, port 65536, the internal port, and a port held
  by another process are refused.
- **Apply and rollback:**
  - a failed health wait offers the restore, and `--yes` restores and
    re-applies;
  - a successful apply recreates the changed services with the new
    environment, through `up -d` rather than `restart`.
- **`compose.sh`,** in the existing fake-Docker harness
  (`scripts/compose_test.go`):
  - `ps` and `logs` succeed with a broken bind source;
  - a fake Docker exiting 130 on `logs` is called exactly once, with no retry
    message;
  - `run` with `check-config` still goes through the launch checks.
- **Lifecycle and status** run with a fake command runner that records
  `compose.sh` calls and returns fixture output, plus a fake health server.
  The checks cover:
  - each status line, healthy and unhealthy, including the unauthenticated
    explanation;
  - the exit codes;
  - no secret in any output.
- **`connect`:**
  - the same-machine text;
  - over SSH (a fixture `SSH_CONNECTION`), no tunnel and the SEC-001 note;
  - rotation in the key format, written without `check-config` or Docker,
    applied through `up -d`, and the declined path.
- **Root:** running as root is refused before anything is read or written.
- **`setup`:**
  - scripted answers to every prompt;
  - building before saving;
  - `--yes` on a first run and on a rerun, the rerun keeping every current
    choice;
  - `--remote-key-stdin`;
  - no secret ever touched.
- **`engine`:** the engine design's tests (fixture `/proc` and `sysctl`,
  fake engines, the four check steps), now in Go.
- **Docs guards:** `tools/docs` is updated wherever the README's quick start
  or a key list changes.

## 10. Delivery slices

These slices are pull requests within OPS-001. The roadmap tracks OPS-001 as
one item.

1. **PR 1: services that can check their settings.**
   - `check-config` in both binaries.
   - Refusing unknown arguments.
   - The Dockerfile label.
   - It ships alone, so that every image built from then on is safe to
     validate with.
2. **PR 2: operate without editing files.**
   - The wrapper.
   - `status`, `start`, `stop`, `restart` and `logs`, plus `compose.sh`'s
     read-only subcommands.
   - `connect` on the same machine, with rotation.
   - `config`: the registry, the writer, validation, and apply with rollback.
   - The README's quick start.
   - After this PR, ds4 on the Framework Desktop is configured with
     `turing config` rather than an editor.
   - **Follow-up, from the owner's decision on question 4:** prove the
     Flutter Linux desktop build that option b depends on. CI builds no
     Linux app today. This PR adds a CI `flutter build linux` step and README
     prerequisites. `flutter_secure_storage_linux` needs `libsecret-1-dev` to
     build, and a Secret Service keyring such as gnome-keyring at runtime.
3. **PR 3: set up for the machine.**
   - `engine detect`, `use` and `check`.
   - The runtime's `check-engine`, with `engine` added to its image's
     `org.turing.checks` label.
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
- Reaching the server from another machine before SEC-001 (section 6.4).
- Live settings and storing settings in the database (OPS-002).
- Editing profiles and skills (OPS-003).
- Raising concurrency (AGT-000).
- Windows hosts.

## 12. Open questions

Questions 1–3 and 5 were decided under autopilot and are open to correction.
The owner decided question 4.

1. **Terminal or web?** Terminal (section 5).
2. **Go or bash?** Go. It reuses the shared validators and the generated
   health client, and it is testable with fakes. The scripts stay the engines
   for provisioning and Compose.
3. **Who validates a setting?** The services, through `check-config`. The
   exceptions are the few host-owned values the services cannot judge, such as
   whether a port is free.
4. **How does the Mac reach a headless server before SEC-001?** Decided by
   the owner on 2026-10-26: **b**, run the app on the server until SEC-001.
   The other options were a bounded SSH exception (**a**, not adopted; see
   section 6.4) and bringing SEC-001 forward (**c**).
5. **Should this command edit settings that OPS-002 will move into the app?**
   Yes, until OPS-002 ships; leaving them hand-edited in the meantime is what
   the user asked to stop. OPS-002 decides the move. The proposal is to import
   them from `.env` once, after which `turing config` edits them through an
   orchestrator settings RPC, as a client.
