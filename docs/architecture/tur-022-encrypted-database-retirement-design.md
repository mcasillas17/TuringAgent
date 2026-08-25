# TUR-022: Encrypted Database Retirement — Feasibility & Design

This is the entry-criterion artifact the TUR-022 roadmap entry demands: the
design the project owner approves **before** any implementation begins. It is
docs-only by construction. Implementation is a separate cycle that
additionally **cannot start before TUR-016 lands** — the roadmap is explicit
that encryption must not precede proven backup and restore integrity — and
TUR-004's pending-merge deletion-withdrawal work is the other named
dependency. Approving this artifact approves encryption at rest and the
retirement *design*; per the roadmap, it does **not** itself approve
retirement on any platform — each platform must separately prove exclusive,
inspectable key custody at implementation time (§Key custody).

Every claim below about an external driver, library, or platform was verified
against its upstream repository or documentation on **2026-08-24**; each such
claim carries its source inline. Claims about this repository cite the real
files.

## The substrate, as it exists today

- `turing-backend/orchestrator-go/internal/db/connection.go` registers a
  custom driver name (`turing_sqlite3`) over `mattn/go-sqlite3` with a
  `ConnectHook` that executes `PRAGMA temp_store = MEMORY` on every pooled
  connection (the orchestrator runs on a read-only root filesystem); the DSN
  is `file:<path>?_foreign_keys=on&_journal_mode=WAL`; the pool is pinned to
  a **single connection** (`SetMaxOpenConns(1)`); `secureSQLiteFile` opens
  the database and its `-journal`/`-shm`/`-wal` siblings with
  `O_NOFOLLOW`, demands a regular file owned by the current user, and
  chmods 0600.
- `go.mod` pins `github.com/mattn/go-sqlite3 v1.14.24`, whose bundled
  amalgamation is SQLite **3.46.1** (read from the module source,
  `sqlite3-binding.c`).
- Migrations are embedded `schema/*.sql` files applied in **full-filename
  sort order** by `internal/db/migrations.go`, one transaction per
  migration, tracked in `schema_migrations`. The 0011 upgrade retains legacy
  skill bodies in `legacy_skill_export_recovery` and re-exports them to
  `skills/imported/<id>/SKILL.md` on every startup through hardened
  atomic writes that stage `.skill-export-<hex>` temporary files
  (`internal/db/legacy_skills_export.go`).
- `internal/secretbox` seals third-party credentials under
  `TURING_INTEGRATION_KEY` (AES-256-GCM, 4-byte key fingerprint in the
  sealed header so a rotated/lost key is detectable without decrypting).
  That key lives in `.env`, is credential-specific, and is **not** a
  database key.
- The orchestrator container (`turing-backend/infra/docker-compose.yml`)
  runs `restart: unless-stopped`, `read_only: true`, mounts `../data` at
  `/app/data`, and is built with `GOFLAGS=-tags=sqlite_fts5`
  (`orchestrator-go/Dockerfile`, `golang:1.23-bookworm` builder,
  `debian:bookworm-slim` runtime). Only orchestrator `:3000` is published;
  the orchestrator is the only service that opens the database.
- FTS5 is load-bearing: session recall is FTS5-backed and
  `internal/db/fts5_test.go` (`TestFTS5IsCompiledIn`) gates its presence
  under the `sqlite_fts5` build tag.

## Driver comparison and selection (recorded evidence)

The candidates, each verified upstream on 2026-08-24:

| Driver | Licensing | `sqlite_fts5` | Builds / platforms | WAL & temp files | Backup integration | Migration safety | Freshness |
|---|---|---|---|---|---|---|---|
| **`mattn/go-sqlite3` v1.14.24 + `libsqlite3` tag, linked against SQLCipher built from source (SELECTED)** | Driver MIT ([README](https://github.com/mattn/go-sqlite3)); SQLCipher BSD-3-Clause ([LICENSE.md](https://github.com/sqlcipher/sqlcipher/blob/master/LICENSE.md)) | Guaranteed by our own pinned SQLCipher compile (`-DSQLITE_ENABLE_FTS5`); runtime-gated by the existing `TestFTS5IsCompiledIn` | `libsqlite3` linking documented for linux/darwin amd64+arm64 ([README](https://github.com/mattn/go-sqlite3)); we control the containerized build | SQLCipher encrypts main file, journal pages, statement journals, and WAL pages; **transient files are not encrypted** — file-based temp storage must stay disabled ([design doc](https://www.zetetic.net/sqlcipher/design/)) | Driver ships the online backup API (`backup.go` in the repo); SQLCipher additionally documents `sqlcipher_export()` ([API](https://www.zetetic.net/sqlcipher/sqlcipher-api/)) | Keeps the exact driver code the repo runs today (same `ConnectHook`, DSN semantics, pool behavior); engine moves **forward** 3.46.1 → 3.53.4 | SQLCipher v4.18.0 released 2026-08-18, upstream SQLite baseline 3.53.4 ([releases](https://github.com/sqlcipher/sqlcipher/releases)) |
| `mutecomm/go-sqlcipher` v4 | Mix: mattn MIT + SQLCipher BSD-3 + libtomcrypt ("covered by their respective licenses" — [README](https://github.com/mutecomm/go-sqlcipher)) | Yes — `sqlite3_opt_fts5.go` carries `// +build sqlite_fts5 fts5` and `-DSQLITE_ENABLE_FTS5` ([source](https://raw.githubusercontent.com/mutecomm/go-sqlcipher/master/sqlite3_opt_fts5.go)) | Self-contained cgo bundle; `sqlite3_windows.go` and `sqlite3_solaris.go` present beside the unix build ([repo file listing](https://github.com/mutecomm/go-sqlcipher)) | Same SQLCipher codec semantics as selected; WAL via mattn-inherited DSN | mattn-inherited `backup.go` present ([repo file listing](https://github.com/mutecomm/go-sqlcipher)) | README documents keying **through the DSN** (`?_pragma_key=x'…'`) — the pattern this design forbids; `ConnectHook` does exist in its [`sqlite3.go`](https://raw.githubusercontent.com/mutecomm/go-sqlcipher/master/sqlite3.go) | **Frozen: last commit and tag (v4.4.2, bundling SQLCipher 4.4.2) are 2020-12-07** ([tags](https://github.com/mutecomm/go-sqlcipher/tags), [commits](https://github.com/mutecomm/go-sqlcipher/commits/master)) |
| `ncruces/go-sqlite3` + `adiantum` VFS | MIT ([README](https://github.com/ncruces/go-sqlite3)) | Not verified — not load-bearing for its rejection | cgo-free Wasm/wazero; very broad platform list ([README](https://github.com/ncruces/go-sqlite3)) | Adiantum VFS encrypts main db, WAL, journals in 4 KiB blocks; temp files encrypted with random keys ([VFS README](https://github.com/ncruces/go-sqlite3/blob/main/vfs/adiantum/README.md)) | "online backup" listed among its advanced features ([README](https://github.com/ncruces/go-sqlite3)) | **Not SQLCipher-compatible** (no compatibility claimed); encryption is "fully deterministic" and the package "does not claim [to] protect databases against tampering or forgery" — no page MACs ([VFS README](https://github.com/ncruces/go-sqlite3/blob/main/vfs/adiantum/README.md)); adopting it is also a whole-driver rewrite | Active — its docs currently reference v0.35.3 ([pkg.go.dev](https://pkg.go.dev/github.com/ncruces/go-sqlite3)) |
| `modernc.org/sqlite` | BSD-3-Clause ([pkg.go.dev](https://pkg.go.dev/modernc.org/sqlite)) | Not verified — not load-bearing for its rejection | cgo-free transpiled amalgamation; 23 platform/arch combinations at SQLite 3.53.3 ([pkg.go.dev](https://pkg.go.dev/modernc.org/sqlite)) | n/a | n/a | **No encryption support at all** — no mention of encryption or SQLCipher anywhere in its documentation ([pkg.go.dev](https://pkg.go.dev/modernc.org/sqlite)) | Active ([pkg.go.dev](https://pkg.go.dev/modernc.org/sqlite)) |
| SQLite Encryption Extension (SEE) | **Proprietary**: $2,000 perpetual source license ([sqlite.org/purchase/see](https://sqlite.org/purchase/see)) | Would inherit from build | Source product, compile yourself | Own codec, not SQLCipher format | Standard SQLite backup API | Not SQLCipher-compatible; closed source | Commercial |

**Selection: keep `mattn/go-sqlite3` and link it against a pinned SQLCipher
4.18.0 built from source in the orchestrator image (`libsqlite3` build
tag).** Why the runners-up lose:

- **`mutecomm/go-sqlcipher` loses on freshness and on engine direction.**
  Its last commit is 2020-12-07; it bundles SQLCipher 4.4.2. Adopting it
  would *downgrade* the SQL engine this repo already runs (mattn v1.14.24
  bundles SQLite 3.46.1) to a 2020-era baseline and freeze us out of nearly
  six years of SQLCipher and SQLite fixes — a migration-safety regression
  masquerading as a convenience. Its documented keying pattern is the DSN,
  which this design's key-hygiene rule forbids (a workaround via its
  inherited `ConnectHook` exists, but we would be depending on a frozen fork
  against its own documentation). It stays viable as a **fallback** if the
  `libsqlite3`+SQLCipher link fails validation during implementation — its
  FTS5 support uses the same `sqlite_fts5` tag we already build with.
- **`ncruces` + adiantum loses on the entry criterion itself** (it is not a
  SQLCipher-compatible driver) and on cryptographic posture: deterministic,
  unauthenticated encryption is upstream's own stated limitation, strictly
  weaker than SQLCipher's per-page random IV + HMAC
  ([design doc](https://www.zetetic.net/sqlcipher/design/)). It would also
  replace the entire driver layer the repo's behavior is calibrated against.
- **`modernc.org/sqlite` loses outright** — no encryption exists to select.
- **SEE loses on licensing** — a proprietary, paid source license is the
  wrong dependency for a local-first personal project, and it is not
  SQLCipher-compatible.

Why the selected path is sound, with its costs stated:

- SQLCipher is "a standalone fork of the SQLite database library" that
  behaves "just like the standard SQLite library" when no key is supplied
  ([README](https://github.com/sqlcipher/sqlcipher)), so mattn's
  `libsqlite3` linking — documented in its README for exactly this
  link-against-a-system-library purpose — carries over. The C API is the
  SQLite API.
- We pin and compile SQLCipher ourselves in the Dockerfile builder stage
  with the flags upstream documents (`SQLITE_HAS_CODEC`,
  `SQLITE_TEMP_STORE=2` or `3`,
  `SQLITE_EXTRA_INIT=sqlcipher_extra_init…`, link `-lcrypto`;
  [README](https://github.com/sqlcipher/sqlcipher)) **plus
  `-DSQLITE_ENABLE_FTS5`**, so FTS5 presence is a property of our pinned
  build, not of a distro package. Codegen-style determinism applies: the
  SQLCipher version is pinned by tag and checksum in the Dockerfile.
- **The link mechanism itself, recorded as selection evidence:** mattn's
  [`sqlite3_libsqlite3.go` at v1.14.24](https://github.com/mattn/go-sqlite3/blob/v1.14.24/sqlite3_libsqlite3.go)
  hardcodes `-lsqlite3` (and, on darwin, Homebrew `sqlite`-keg include/lib
  paths) — verified in the module source — so a SQLCipher named
  `libsqlcipher` is not found by the tag as-is. In the
  containerized builder the Dockerfile owns the mapping: it installs the
  pinned SQLCipher build into the builder prefix under the `sqlite3`
  library and header names the tag hardcodes (an install-prefix mapping the
  image controls end-to-end), so the link is deterministic and no distro
  `libsqlite3` can shadow it. On darwin dev machines the tag's hardcoded
  keg paths point at plain `sqlite`, so an encrypted-flavor build there
  requires explicit `CGO_CFLAGS`/`CGO_LDFLAGS` overrides — the encrypted
  flavor is container-first, and G1 remains the runtime kill for any build
  where a plain SQLite was linked and `PRAGMA key` silently no-opped.
- **Cost 1 — the `sqlite_fts5` build tag becomes inert under `libsqlite3`.**
  mattn's tag injects `-DSQLITE_ENABLE_FTS5` into the *bundled*
  amalgamation; when linking a system library the amalgamation is not
  compiled. The tag stays (the default, non-encrypted build everywhere else
  in the matrix is unchanged), and FTS5 remains gated at **runtime** by
  `TestFTS5IsCompiledIn` plus gate G1 below.
- **Cost 2 — dual build flavors.** Unit tests on dev machines and in the
  existing CI jobs keep the bundled plain-SQLite build (fast, hermetic,
  unchanged verification matrix). Encryption behavior is proven by a new CI
  job that builds the SQLCipher-linked flavor and runs the full root-module
  test suite plus the encryption gate suite against it — that job is what
  closes the test/prod engine gap (bundled 3.46.1 vs linked 3.53.4). CI's
  self-guard test (`.github/workflows/ci_test.go`) moves in the same change.
- **Cost 3 — macOS dev parity.** Homebrew ships `sqlcipher` 4.18.0
  ([formula](https://formulae.brew.sh/formula/sqlcipher)), but its FTS5
  compile status is not documented there; a dev-machine SQLCipher build is
  therefore verified at runtime by the same gates, never assumed.

## The envelope, and the key domains (locked)

One **data encryption key (DEK)**: 32 random bytes, generated once at
encryption-migration time, applied per-connection as SQLCipher's raw-key
form — `PRAGMA key = "x'<64 hex>'"` — which uses the bytes directly and
skips passphrase KDF ([API](https://www.zetetic.net/sqlcipher/sqlcipher-api/)).
The DEK never exists outside process memory except **wrapped**: a single
managed wrapper blob at `data/keys/turing.db.dek` (0600, same
`secureSQLiteFile` posture), sealed by the **key-encryption key (KEK)** held
in OS-keystore custody (§next section). Envelope wrapping is what makes
rotation of the custody key cheap (rewrap one blob; zero data rewrite) and
what makes retirement a wrapper destruction rather than a file-shredding
promise.

**Key domains stay distinct.** `TURING_INTEGRATION_KEY` remains exactly what
`internal/secretbox` says it is: a credential-sealing key for third-party
secrets, in `.env`. The DEK/KEK pair is a new, database-scoped domain; the
two coexist, neither derives from the other, and no code path may substitute
one for the other. Superseding the credential key with the database domain
is a **deliberate deferral** (§Deferred) — the roadmap permits coexistence
unless an approved design explicitly migrates it, and this design explicitly
does not: credential sealing must keep working when the database is being
restored (the acceptance criteria require restore to report a
missing/rotated credential key *separately*, which `secretbox`'s existing
header fingerprint already enables, *without* blocking database recovery).

**Configuration is a keystore selector, never key material.** `.env.example`
and compose gain one variable, `DATABASE_ENCRYPTION` (`off` | `keystore`),
plus nothing else. Key bytes, passphrases, and wrapped blobs never appear in
`.env`, the DSN, environment blocks, process arguments, logs, or error text
— errors name the *state* ("database locked: encryption key unavailable"),
never material. The rejected alternative — an `.env`-resident database key in
the `TURING_INTEGRATION_KEY` mold — is rejected because a file-backed key on
the same disk, covered by the same host backups, is precisely the
"synced/escrowed/exportable" wrapper class the roadmap declares
retirement-ineligible, and because it would make `docker inspect` and
`/proc/<pid>/environ` key-disclosure surfaces.

## Key custody: retirement-eligible vs encryption-only (locked)

Two custody classes, decided per platform, defaulting closed:

- **Encryption-only custody.** The KEK is an OS-keystore item that the
  platform may sync, escrow, export, or include in device backups — or whose
  behavior we cannot prove. The database is encrypted at rest; **cryptographic
  retirement is unavailable** on this custody, because destroying our wrapper
  cannot be shown to destroy the last wrapper. Unknown custody state is
  treated as this class (fail closed), matching the roadmap's rule that
  "unknown, synced, escrowed, exportable, or device-backed-up wrappers make
  that platform retirement-ineligible."
- **Retirement-eligible custody.** The KEK is provably exclusive and
  inspectable: a hardware-resident, non-exportable key. On macOS — the
  project's current host platform — that is a Secure Enclave–protected key:
  Apple's Platform Security guide states enclave-held keys "aren't made
  visible even to sepOS software" and that software "can't extract the keys"
  ([Apple Platform Security](https://support.apple.com/guide/security/secure-enclave-sec59b0b31ff/web)),
  which is exactly the exclusivity property retirement needs. The
  implementation-time proof obligation per platform is: (a) the KEK is
  created non-exportable and device-bound, (b) the wrapper inventory
  (§Inventory) enumerates every wrapped-DEK copy Turing has ever written,
  and (c) no export/sync/escrow path for the KEK exists or has been enabled.

Encryption approval is not retirement approval: a user on encryption-only
custody gets at-rest protection today and a visible "retirement unavailable
on this platform's key custody" state, not a false ceremony. Non-macOS host
platforms are unclassified in this design and therefore encryption-only
until a platform-specific proof is added (§Deferred).

## Containerized key delivery after host restart (a decision, not a hand-wave)

The problem: the orchestrator is a Linux container; the KEK lives in the
macOS keystore, which no container can reach. After a host restart the
container auto-starts (`restart: unless-stopped`) with no key. Something on
the host must unwrap the DEK and deliver it — the question is what, over
what channel, holding what.

**Selected: the Flutter client is the unlock agent, over the existing
authenticated loopback gRPC channel.** The desktop app already runs on the
host, already holds the client API key, and already speaks to orchestrator
`:3000`. A new `Unlock` RPC carries the DEK (not the KEK) exactly once per
orchestrator boot. The app obtains the wrapper blob through a
**`FetchWrapper` RPC on the same authenticated status/unlock surface** —
selected over the alternative of the app reading
`turing-backend/data/keys/turing.db.dek` directly off the host filesystem
(the `data/` directory is a host bind mount, so the app *could*), which is
rejected because the client's contract today is purely gRPC with no
knowledge of backend directory layout, and a path-coupled client breaks
silently when the layout moves and invites exactly the ad-hoc file handling
`secureSQLiteFile`'s 0600/owner posture exists to prevent. Serving the
wrapper pre-unlock does not weaken custody: the wrapper is KEK-sealed
ciphertext whose protection comes from the keystore, the RPC still requires
the client API key, and the LOCKED surface already exists. The app then
asks the OS keystore to unwrap the blob — the KEK never leaves keystore
custody; on retirement-eligible custody the unwrap happens *inside* the
Secure Enclave — and sends the unwrapped DEK to the orchestrator, which
holds it in memory only and moves LOCKED → OPEN.

**The wrap direction is the same surface, host-side.** Every KEK operation
happens in the app, where the KEK lives: at the initial encryption ceremony
the app generates the 32-byte DEK from the host CSPRNG, wraps it via the
keystore, persists the wrapper through a `StoreWrapper` RPC, and delivers
the DEK through `Unlock`; KEK rotation is fetch → unwrap → rewrap under the
new KEK → store as `data/keys/turing.db.dek.next` → commit — all host-side,
no orchestrator-side KEK crypto ever. The alternative — the orchestrator
generating the DEK in-container and round-tripping it to the app for
wrapping — is rejected: it adds a second plaintext-DEK transit over the
loopback hop for zero custody gain.

**The client is therefore a transient key holder, and the design says so.**
Between unwrap and `Unlock` completing, the plaintext DEK exists in the
app's process memory. Requirements: the app zeroes its DEK buffer
immediately after `Unlock` (or `StoreWrapper`+`Unlock`) returns, never
persists it, and never logs it; the retirement ceremony's zero-live-holders
check counts the client (§Retirement, step 4); G11 asserts the client-side
handling.
Compared alternatives, each rejected:

- **Environment injection** (compose reads the keystore via
  `security find-generic-password` and passes the key in an `environment:`
  entry): rejected — violates the key-hygiene rule outright; the key would
  be visible in `docker inspect`, `/proc/<pid>/environ`, and compose
  diagnostics. This is the one alternative the non-negotiables already
  forbid.
- **File or tmpfs handoff** (host writes the raw DEK to a mounted path the
  container reads and deletes): rejected — creates a file-backed plaintext
  key artifact with an unbounded copy surface (host backups, crash
  leftovers), the exact artifact class the inventory exists to prevent.
- **Running the orchestrator natively on the host** (keystore directly
  reachable): rejected as an architecture change far beyond TUR-022's scope
  — it would abandon the container hardening posture TUR-005 just built.
- **A dedicated host broker daemon** (launchd agent holding unlock duty,
  delivering over a mounted UNIX socket): *deferred, not rejected* — it is
  the natural follow-up that unlocks unattended restarts at login without
  the app open, but it introduces a new privileged host component; Phase 1
  uses the component that already exists and already holds a session.

**Residuals, named:** the DEK transits one loopback hop under client-API-key
authentication on a channel that is not TLS; a root-privileged local
observer could capture loopback traffic. That observer can also read
orchestrator process memory, where the DEK must live to be used at all —
the residual is not enlarged by the hop, and it matches the honesty
`internal/secretbox`'s package comment already practices ("it buys nothing
against someone who can read … the process memory"). Binding the `Unlock`
RPC to loopback origins and never logging its payload are implementation
requirements, gated by G3.

## Unavailable-key behavior: LOCKED is a real state (locked)

When `DATABASE_ENCRYPTION=keystore` and no DEK has been delivered, the
orchestrator is **LOCKED**:

- The database file is **never opened for writing, truncated, replaced, or
  recreated**. The encrypted database is opened only after a successful key
  probe, with open flags that exclude create semantics — concretely, two
  things in today's `connection.go` must change on the encrypted path: the
  file URI carries `mode=rw` — SQLite's URI documentation specifies `rw`
  opens read-write without create, versus `rwc`'s read-write-create
  default ([sqlite.org/uri.html](https://sqlite.org/uri.html)) — and
  `secureSQLiteFile(path, true)`'s `O_CREAT` open must not run against a
  locked or missing encrypted database. A missing wrapper, a failed unwrap,
  or a wrong key each produce their own typed state.
  SQLCipher makes wrong-key detection deterministic: the documented probe is
  `SELECT count(*) FROM sqlite_master`, which fails unless the key is right
  ([API](https://www.zetetic.net/sqlcipher/sqlcipher-api/)). A wrong key
  must never be "recovered" by reinitializing the file — the empty-database
  trap (SQLite creating a fresh db at a path it cannot read) is gate G5's
  named kill.
- Only the status/unlock surface serves. Every data RPC fails closed with a
  typed LOCKED error the client renders; nothing queues.
- **Scheduled automation cannot bypass the lock.** Automations that fire
  while LOCKED record a typed durable failure — the exact mold TUR-003
  established for consent ("automations record a typed durable failure
  instead of inheriting interactive consent") — and do not retry into the
  lock, do not buffer work, and do not run with degraded state.
- **Restart behavior is deterministic and documented:** `unless-stopped`
  restarts land back in LOCKED, serving status, waiting. A crash loop
  cannot corrupt the database because LOCKED never opens it writable.
- **Key-loss recovery UX in Flutter distinguishes three states,
  non-destructively:** *locked* (wrapper present, keystore reachable —
  offer unlock), *keystore unavailable* (wrapper present, unwrap failing —
  name the platform condition, offer retry), and *key loss* (wrapper
  missing/corrupt or KEK destroyed — state plainly that the encrypted
  database cannot be opened, then offer three explicit actions in this
  order: restore from a managed backup via TUR-016; **promote the retained
  plaintext predecessor** `data/turing.db.pre-encryption` where one still
  exists, with its data-loss window — everything written since the
  encryption swap — stated in the confirmation (§Migration's rollback
  paragraph is the authority on why this is a last resort); and, **never
  as a default**, creating a new database — an explicit, separately
  confirmed operation that renames the unreadable file aside rather than
  deleting it).

## Connection discipline under the selected driver (locked)

An ordering fact, verified against the mattn v1.14.24 module source
([`sqlite3.go` at v1.14.24](https://github.com/mattn/go-sqlite3/blob/v1.14.24/sqlite3.go):
the `PRAGMA journal_mode` exec sits near line 1700, the `ConnectHook`
invocation near line 1773): `Open` executes DSN-derived PRAGMAs (including
`_journal_mode`) **before** invoking `ConnectHook`. SQLCipher requires the
key before the first page read, and
`PRAGMA journal_mode = WAL` writes the database header. Therefore:

- The DSN shrinks to the path plus non-I/O parameters only
  (`_foreign_keys=on` is a connection flag and stays; `_journal_mode=WAL`
  **moves out of the DSN**; `_locking_mode=EXCLUSIVE` **moves in** — next
  bullet). The DSN never carries key material — mutecomm's `_pragma_key`
  pattern is structurally impossible here because the parameter does not
  exist in mattn.
- **EXCLUSIVE locking rides the DSN, and that placement is load-bearing.**
  `PRAGMA locking_mode` is a connection-mode setting that performs no file
  I/O by itself, and mattn supports it as the `_locking_mode` DSN parameter,
  executed inside `Open` before `ConnectHook` runs (parameter parsing near
  line 1330, the exec near line 1709 — which mattn performs unconditionally,
  defaulting to NORMAL — versus the hook invocation near line 1773;
  [`sqlite3.go` at v1.14.24](https://github.com/mattn/go-sqlite3/blob/v1.14.24/sqlite3.go)).
  Putting it in the DSN makes the no-`-shm` guarantee structural: EXCLUSIVE
  is set before *any* file access can occur, so no ordering convention
  inside the hook can regress it. A hook-ordered `locking_mode` was
  considered and rejected: the key probe is the first WAL-mode file access
  (the database header persists WAL), so any hook sequence that probes
  before setting EXCLUSIVE silently creates the wal-index — exactly the
  wrong implementation G2 kills.
- The `ConnectHook` becomes the ordered keying sequence, run on every pooled
  connection: `PRAGMA key = "x'…'"` → key probe (G1/G5; with EXCLUSIVE
  already set from the DSN, this first access creates no `-shm`) → `PRAGMA
  temp_store = MEMORY` (re-establishing today's guard under the new build —
  SQLCipher's own design doc requires it: "transient files are not
  encrypted, so you must disable file based temporary storage";
  [design doc](https://www.zetetic.net/sqlcipher/design/)) → `PRAGMA
  journal_mode = WAL` (a no-op on an already-WAL database; load-bearing
  only on first creation of the staging database) → the remaining session
  pragmas.
- **The `-shm` wal-index is avoided entirely, not argued about.** Setting
  EXCLUSIVE locking before the first WAL access means SQLite "never
  attempts to call any of the shared-memory methods and hence no
  shared-memory wal-index is ever created"
  ([sqlite.org/wal.html](https://sqlite.org/wal.html)) — the roadmap's
  "approved locking mode" branch. This is safe here because the pool is
  already one connection and the orchestrator is already the database's only
  client. Defense in depth: even where a wal-index exists, sqlite.org
  documents it as a non-persistent reader index, not database content — but
  the design does not lean on that; it removes the file. **Named cost, and a
  constraint this design places on TUR-016 — a coordination note, not an
  existing fact:** under EXCLUSIVE locking no second process can attach
  while the orchestrator runs, so encrypted-era backups must run
  **in-process** through the driver's online backup API — and because
  EXCLUSIVE also bars a second connection to the same file, the backup's
  *source* side is the one pooled connection, so backups must run in
  bounded backup-step increments that yield between steps, under the same
  single-connection discipline the migration budgets impose (the
  destination is a separate file and gets its own keyed connection).
  TUR-016 has not landed and owns its own backup design; nothing today
  constrains it. But
  note the constraint is mostly forced by encryption itself, not by the
  locking mode: once the file is SQLCipher-encrypted, any backup path must
  read through a *keyed* connection regardless of locking, so an external
  unkeyed `sqlite3 .backup` design would be invalidated by TUR-022 anyway.
  What EXCLUSIVE locking additionally forecloses is a *second keyed
  process* — and that alternative is compared and rejected here: delivering
  the DEK to a second process would widen key custody (two live key
  holders where the retirement ceremony's "zero live key holders" check
  currently has one), and shared locking would re-create the `-shm`
  wal-index this section exists to remove. If TUR-016 nevertheless lands an
  out-of-process backup design, this locking decision must be revisited
  before TUR-022 implementation starts — recorded as a cross-task risk in
  §Deferred.
- With SQLCipher active, main file, journal pages, statement journals, and
  WAL pages are encrypted under the database key
  ([design doc](https://www.zetetic.net/sqlcipher/design/)); with
  `temp_store=MEMORY` and `SQLITE_TEMP_STORE≥2` compiled in, no plaintext
  temporary spill path remains on any supported operation.

## The plaintext→encrypted migration, with numeric budgets (locked)

**Shape: staged copy with a durable progress journal, not a one-shot.** The
two upstream conveniences are both rejected for the same reason:
`sqlcipher_export()` "will duplicate the entire contents of the main
database" in one unbounded operation, and `PRAGMA rekey` "can not be used to
encrypt a standard SQLite database" at all
([API](https://www.zetetic.net/sqlcipher/sqlcipher-api/)) — neither is
bounded, resumable, or cancellable, so neither can meet the budgets below.
Instead:

1. On first keyed startup with a plaintext database present, the
   orchestrator enters **MIGRATING**: ingress fenced (data RPCs refuse with
   a typed migrating state; automations record the same typed durable
   failure as LOCKED), status surface live.
2. A staged encrypted database is created at
   `data/turing.db.enc-staging` through the keyed driver; schema is applied
   by copying `sqlite_master` DDL (FTS5 virtual tables and triggers
   included); a `_migration_progress` table inside the staging database
   records, per table, the last-copied rowid/key — the resume cursor is
   inside the artifact it describes, so a crash cannot desynchronize them.
3. Tables copy in batches (budgets below), each batch one transaction in
   the staging database, reading the plaintext source read-only. Between
   batches the loop yields, checks cancellation, and re-reads the fence —
   this is what "resumes without monopolizing SQLite's single connection"
   means concretely: no batch holds either database's connection longer
   than the transaction budget.
4. Verification before the swap: per-table row counts match, `PRAGMA
   integrity_check` passes on staging, and the FTS5 probe query returns on
   staging. Then the finalize step, whose two renames are **not** one
   atomic act and are therefore bracketed by a durable intent marker:
   plaintext database checkpointed and closed, staging checkpointed and
   closed, a `data/turing.db.swap-intent` marker written and fsynced,
   *then* rename 1 (plaintext original to `data/turing.db.pre-encryption` —
   it becomes a **legacy plaintext predecessor**, inventory §below,
   retained until encrypted operation is verified, destroyed only by the
   retirement ceremony or by explicit user action), rename 2 (staging to
   `data/turing.db`), directory fsync, wrapper blob committed, marker
   removed. Crash rules are marker-driven and deterministic: **before the
   marker exists**, the plaintext database is authoritative and staging is
   discardable/resumable; **while the marker exists**, startup completes
   the swap *forward* (the staging copy is already verified — rename it
   into place, never fall back to the predecessor, and never let the
   ordinary open path run against the empty canonical path: between the
   renames nothing exists at `data/turing.db`, and today's
   `secureSQLiteFile(path, true)` + default open flags would silently
   create an empty database there — G5's named kill reached through the
   migration path). Completing forward is **order-checked, never blind**:
   recovery first determines which renames already happened —
   `data/turing.db.pre-encryption` absent means rename 1 has not run and
   the canonical path still holds the plaintext original, so rename 1
   executes first; only then rename 2. (The presence check is sound across
   encrypt→rollback→re-encrypt cycles because a completed rollback
   archives its predecessor under a distinct `superseded-*` name — §the
   rollback paragraph — so a bare `.pre-encryption` always belongs to the
   in-flight attempt.) A recovery that runs rename 2
   unconditionally would let POSIX `rename()` silently replace the
   still-present plaintext original with the staging file, destroying the
   predecessor this design promises to retain — the wrong implementation
   G4's marker-write fault injection kills. **After the marker is
   removed**, the encrypted database is authoritative with the
   predecessor intact. There is no
   window with zero readable databases on disk, and no crash point with
   two authoritative ones.
5. Cancellation (user-initiated from the status surface) before finalize
   discards nothing the user needs: the plaintext database was never
   modified; staging and its journal are deleted; the system returns to
   unencrypted operation.

**Budgets, with reasoning.** These are enforcement thresholds the
implementation must meter itself against — gates, not performance claims:

| Budget | Number | Reasoning |
|---|---|---|
| Per-batch transaction time | **≤ 250 ms** | The pool is one connection per database (`SetMaxOpenConns(1)`); SQLite serializes writers per database file. 250 ms bounds how long any status/cancellation query queued on the shared plumbing waits behind a batch, and keeps WAL growth per transaction small. The batch controller measures each batch and **halves the batch size when a batch overruns**, to an adaptive floor of 16 rows — budgets hold on slow disks by shrinking work, not by hoping. |
| Batch size ceiling | **≤ 1,000 rows or ≤ 4 MiB of row payload, whichever first** | Large blob rows (message bodies, tool results) must not blow the transaction budget through row count alone; small rows must not incur per-transaction overhead ten thousand times. The dual ceiling is the starting point the adaptive controller shrinks from. |
| Startup pause to a serving status surface | **≤ 5 s** | The Flutter client and `restart: unless-stopped` both need the process responsive fast; a migration must never look like a hung boot. The pause covers boot and fence setup only — under the selected key delivery the orchestrator never unwraps anything (it boots LOCKED and *receives* the DEK over `Unlock`, which cannot arrive before the surface serves), and copying happens *after* the surface is up, inside MIGRATING. Once the DEK has been delivered, added overhead to OPEN (key probe + open) must stay **≤ 2 s**. The unlock round-trip itself is user-dependent (the app must be running) and is deliberately unbudgeted. |
| Finalize/swap quiesce | **≤ 5 s** | Checkpoint-close-rename-fsync of two databases on a local SSD; anything longer indicates an unquiesced writer, which is a bug the gate should catch, not a wait to extend. |
| Cancellation latency | **≤ 500 ms** | Cancellation is checked between batches; worst case is one in-flight batch (≤ 250 ms) plus its commit/rollback and fence release. |
| Resume overhead after interruption | **≤ 1 batch** | The progress journal commits with each batch's transaction; at most the interrupted batch is re-copied (idempotent by cursor). |

Rotation reuses this machinery wholesale: **wrapping-key rotation** is a
rewrap of the single DEK blob (no data rewrite, no budgets consumed);
**DEK rotation** is the same staged copy encrypted→encrypted under the same
budgets. `PRAGMA rekey` is rejected for rotation for the same
unbounded/uncancellable reason.

**Rollback — reverting encryption after the swap — is the same machinery
run in reverse, and it is not predecessor promotion.** A user who enables
encryption and later reverts gets a staged copy encrypted→plaintext (same
batches, budgets, progress journal, verification, intent marker, and swap),
which preserves **every** write made since encryption. The tempting
shortcut — promoting `data/turing.db.pre-encryption` back into place — is
compared and rejected as the *default* path because the predecessor is
frozen at swap time: promoting it silently discards everything written
since, an unbounded data-loss window. Predecessor promotion survives only
as the explicit **key-loss last resort** (§Unavailable-key behavior's
recovery UX), where the encrypted database is unreadable by definition, the
loss window is stated to the user in the confirmation, and the unreadable
encrypted file is renamed aside, never deleted. After a completed rollback
the wrapper blob is destroyed, `DATABASE_ENCRYPTION` returns to `off`, and
the inventory drops to the plaintext-era artifact set — **plus the
predecessor's explicit disposition**: the now-stale
`data/turing.db.pre-encryption` (frozen at the original swap; the rollback
output supersedes it) is renamed to a distinct archival name,
`data/turing.db.pre-encryption.superseded-<attempt-id>`, stays in the
managed legacy-plaintext inventory, is offered for deletion in the rollback
confirmation, and is destroyed at latest by retirement's predecessor sweep.
The rename is load-bearing, not housekeeping: it keeps the bare
`.pre-encryption` name meaning exactly one thing — *the current encryption
attempt's rename 1 already ran* — so a later re-encryption's order-checked
finalize recovery cannot misread a leftover from an earlier cycle. An
interrupted rollback resumes exactly as an interrupted migration does —
same journal, same marker rules.

The schema migration runner is untouched: encryption is a file-level
lifecycle that runs **before** `ApplyMigrations`, and the full-filename sort
order of `schema/*.sql` is preserved exactly as CLAUDE.md requires.

## Backups and restore: TUR-016's semantics, encrypted (locked)

TUR-016 owns consistent backup, restore verification, and migration
checksums; this design **reuses those semantics and encrypts the managed
artifacts** — it does not fork them, and implementation cannot start before
TUR-016 lands. Concretely:

- Whatever backup mechanism TUR-016 lands, the requirement TUR-022 adds for
  the encrypted era is: managed backups are taken through the driver's
  online backup API on a keyed connection with the **target also opened
  through the keyed driver** — the backup API copies pages through the
  destination connection's codec, so an unkeyed target would silently
  produce a *plaintext* backup; gate G8's named kill. (The in-process
  constraint and its rationale are §Connection discipline's coordination
  note.) Managed backups are therefore encrypted under the same DEK and
  carry **no key material**: a backup of the wrapper beside the backup of
  the ciphertext would quietly widen the wrapper inventory.
- TUR-016's optional user-passphrase-encrypted *export* remains what the
  roadmap says it is: outside managed database-key custody and outside
  retirement — a user-created artifact, disclosed as such.
- Restore keeps the credential domain separate: after restoring a database,
  `secretbox`'s header fingerprint detects sealed credentials whose
  `TURING_INTEGRATION_KEY` is missing or rotated, and restore **reports that
  separately** — integrations show as needing reconnection — without
  blocking database recovery and without ever writing credentials in
  plaintext.

## The managed-artifact inventory (locked)

Everything Turing manages that can hold database content or key material,
enumerated. The implementation maintains this as a checked manifest (gate
G10), not a doc paragraph:

**Managed database artifacts — encrypted, and retire with the DEK:**
- `data/turing.db` (main), `data/turing.db-wal`, `data/turing.db-journal`;
  `data/turing.db-shm` listed defensively though EXCLUSIVE locking prevents
  its creation.
- Migration staging: `data/turing.db.enc-staging` and its `-wal`/`-journal`,
  including the `_migration_progress` journal inside it.
- Legacy plaintext predecessors: `data/turing.db.pre-encryption`, any
  `data/turing.db.pre-encryption.superseded-<attempt-id>` archived by a
  completed rollback, and any sibling `-wal`/`-journal` retained at swap
  time — readable predecessors **must be migrated (then destroyed) before
  retirement can succeed**.
- Managed backups under `data/backups/` (TUR-016's location, whatever it
  lands as) and any restore-staging files TUR-016 creates.
- **Database rows are database content:** the retained
  `legacy_skill_export_recovery` rows ride inside `turing.db` and retire
  with it — retiring the database is what finally withdraws the *rows*.

**Managed wrapped-key copies:**
- Exactly one primary wrapper: `data/keys/turing.db.dek`.
- Rotation staging: `data/keys/turing.db.dek.next` during a rewrap, removed
  on commit. Nothing else; managed backups exclude key material by design.

**Separately governed file-backed copies — inventoried and disclosed, never
counted as retired:**
- The emitted skill recovery exports `skills/imported/<id>/SKILL.md` and
  their atomic-export staging files (`.skill-export-<hex>`,
  `legacy_skills_export.go`) — these are plaintext files the 0011 recovery
  path deliberately writes and re-verifies on startup. **Database
  retirement does not withdraw legacy skill content**; the ceremony's
  report names these files explicitly so the user can act on them. The
  operator cleanup of the recovery *table* changes shape under encryption —
  §next section, a deliberate amendment, not a footnote.
- Sandbox artifacts under TUR-004's provenance manifest, session/memory
  exports (TUR-015), and every user-created copy of anything — outside
  Turing's custody, disclosed, out of scope.

## The recovery-table cleanup under encryption (a deliberate amendment)

CLAUDE.md's documented cleanup for `legacy_skill_export_recovery` is
plaintext-era: "stop the orchestrator, back up the database, verify every
legacy file under `skills/imported/`, then use a SQLite client to
`DROP TABLE legacy_skill_export_recovery;` before restarting." Once the
database is SQLCipher-encrypted, the load-bearing step is broken: a generic
unkeyed `sqlite3` client cannot open the file at all, and under this
design's key-delivery decision no raw key ever exists outside orchestrator
process memory for an operator to type into an interactive session. This
design therefore **amends the cleanup procedure** rather than pretending it
survives:

- **Selected: the drop moves in-process, behind an explicit operator
  ceremony.** A dedicated maintenance flow (surfaced in the client, executed
  by the orchestrator) that runs only on explicit operator confirmation and
  only after, in order: a fresh managed backup exists (TUR-016's receipt),
  every recovery row is re-verified against a byte-identical
  `skills/imported/<id>/SKILL.md` file using the verification machinery the
  startup re-export already has (`legacy_skills_export.go`), and the
  confirmation names the row count being dropped. Any unverifiable row
  fails the whole ceremony closed.
- This narrows, deliberately, the existing invariant that "application code
  never deletes nonempty recovery." That invariant's *reason* — no atomic
  commit boundary between SQLite and the filesystem, so silent deletion
  could destroy the only copy — is preserved in full: nothing is deleted
  silently, nothing at startup, nothing without per-row verified file
  presence and a backup receipt. What changes is only *who holds the keyed
  connection*, because after encryption the orchestrator is the only
  process that can.
- **Rejected alternative: hand the operator the raw DEK for a `sqlcipher`
  CLI session.** This preserves the "offline, external client" shape at the
  cost of the key-hygiene rule — key bytes would transit a terminal
  (shell history, process arguments, scrollback), the exact disclosure
  surfaces §the envelope forbids — and it creates a second live key holder
  outside the inventory. Rejected.
- **Rejected alternative: leave the procedure as documented.** Impossible
  under encryption; documenting an impossible procedure is worse than
  amending it.

The implementation PR must rewrite the CLAUDE.md gotcha's cleanup steps to
this ceremony (not merely add notes beside them) — §Documentation.

## The retirement ceremony (locked)

Retirement is an explicit, operator-confirmed ceremony, unavailable unless
every precondition holds — each step fails closed:

1. **Eligibility:** custody class is retirement-eligible (§Key custody);
   the wrapper inventory is complete and every wrapper copy is enumerated;
   **external wrapper state is known and no recoverable external wrapper
   exists** — unknown state blocks retirement, full stop.
2. **Predecessor sweep:** every readable managed plaintext predecessor and
   every managed backup is either migrated into encrypted custody or
   already encrypted; a readable plaintext artifact anywhere in the managed
   inventory blocks retirement.
3. **Fencing:** ingress fenced, schedulers fenced (automations get the
   typed durable failure), and **automatic process restart suppressed** — a
   durable retirement sentinel in `data/` that startup honors: a restarted
   container boots into a RETIRING state that serves status only and never
   requests or accepts a key, so `unless-stopped` cannot resurrect a key
   holder mid-ceremony.
4. **Quiesce:** every client stream closed, database handles closed, WAL
   checkpointed; then **zero live key holders confirmed**. There are
   exactly two processes that ever hold the plaintext DEK — the
   orchestrator (holding it for use) and the Flutter unlock agent (holding
   it transiently during delivery, §Containerized key delivery) — so the
   confirmation covers both: the orchestrator's database and key state
   closed and zeroed, and the client quiesced past its zeroization point
   with no unlock in flight, recorded in the ceremony receipt.
5. **Destruction:** every managed wrapper destroyed (primary and any
   staging wrapper), and on retirement-eligible custody the KEK itself
   destroyed in the keystore; encrypted files may additionally be deleted
   as hygiene, but unreachability comes from key destruction, not deletion.
6. **Receipt:** a durable, human-readable retirement report: what was
   migrated, what was destroyed, and the disclosure list — the separately
   governed skill recovery files, residual storage bytes, and everything
   out of custody.

**Honest boundaries, stated as product text requirements:** retirement is
cryptographic, not forensic — it does not erase process-memory remnants,
pre-encryption storage remnants, or SSD wear-leveling copies; user-created
copies and exports are outside Turing's control; and **one database key
cannot erase one session** — per-session withdrawal remains TUR-004's
logical deletion, and product copy must never conflate the two.

## The test gates

Numbered against TUR-022's acceptance bullets; each names the wrong
implementation it kills. Break the gate, watch the test fail, restore.

1. **G1 — The cipher is real and FTS5 survives it.** Against the
   SQLCipher-linked build: `PRAGMA cipher_version` returns non-empty at
   startup and the FTS5 probe (`TestFTS5IsCompiledIn`'s query) passes on an
   encrypted database. *Kills:* the silent catastrophe where the binary
   links plain SQLite — which ignores `PRAGMA key` without error and runs
   happily in plaintext forever — and the build that drops
   `-DSQLITE_ENABLE_FTS5` from the SQLCipher compile.
2. **G2 — No plaintext byte on any supported path.** Write a
   high-entropy sentinel through every write path (messages, FTS index,
   skills enablement, events), then scan the main file, `-wal`,
   `-journal`, the (absent) `-shm`, and the temp directory for the sentinel
   and for SQLite's plaintext magic; assert `-shm` does not exist under
   EXCLUSIVE locking; assert `temp_store` is MEMORY on every pooled
   connection under the new driver. *Kills:* the migration that encrypts
   the main file but leaves WAL/journal plaintext; the driver swap that
   silently drops the `ConnectHook` temp-store guard; the locking-mode
   regression that re-creates a wal-index.
3. **G3 — Key hygiene end to end, and the domains stay apart.** Assert the
   DSN string, the process environment, compose config, process arguments,
   log capture, and every error text contain no key bytes, no wrapper
   bytes, and no passphrase under fault injection (wrong key, failed
   unwrap, failed unlock). Assert the DEK and KEK are never derived from,
   equal to, or substitutable for `TURING_INTEGRATION_KEY` — sealing a
   credential with the database key, or keying the database with the
   credential key, both fail. *Kills:* the mutecomm-style `_pragma_key`
   DSN; the "helpful" error that embeds the key it failed with; the debug
   log line in the unlock path; the "convenient" reuse that collapses the
   two key domains.
4. **G4 — Budgets are enforced across migration, rotation, restore, and
   rollback — not aspirational.** Fault-injected and metered runs:
   per-batch transaction time ≤ 250 ms with adaptive halving observed
   under an artificially slowed VFS; cancellation observed ≤ 500 ms from
   request to plaintext-intact stop; kill -9 at arbitrary batch boundaries
   **and at every finalize boundary** (marker write, between the two
   renames, before the directory fsync) resumes or completes forward with
   ≤ 1 batch re-copied and never opens the empty canonical path with
   create semantics; the same interruption matrix runs against DEK
   rotation, TUR-016-shaped restore, and rollback (the acceptance's four
   named fault-injection surfaces); status surface serves within 5 s of
   boot with a migration pending. *Kills:* the one-shot `sqlcipher_export`
   implementation wearing a progress bar; the resume that restarts from
   zero; the cancel that leaves a half-swapped pair; the crash between
   renames that "recovers" by creating an empty database; the rollback
   that quietly promotes the stale predecessor.
5. **G5 — Missing or wrong keys never destroy anything.** With no key,
   wrong key, corrupt wrapper, or absent keystore: the database file's
   bytes are unchanged after every startup path (hash before/after), no
   file is created at the database path, LOCKED serves status, and the
   three Flutter recovery states render distinctly and non-destructively.
   *Kills:* the open-with-CREATE that "recovers" by making an empty
   encrypted database over the user's data; the retry loop that truncates;
   the UI that offers "start fresh" as the default.
6. **G6 — The lock is automation-proof.** Scheduled automations firing
   during LOCKED and MIGRATING record the typed durable failure and
   execute nothing; no queued automation runs on unlock without its own
   trigger. *Kills:* the scheduler that buffers work through the lock and
   replays it — the automation bypass the roadmap names.
7. **G7 — Restart behavior is deterministic.** Container restart during
   LOCKED lands in LOCKED; during MIGRATING resumes MIGRATING; during
   RETIRING lands in RETIRING and never requests a key. *Kills:* the boot
   path with a state-dependent default that quietly reopens plaintext or
   re-arms a retired database.
8. **G8 — Backups are encrypted and restore stays honest.** A managed
   backup taken through the flow contains no plaintext sentinel and fails
   to open without the key; restore of a backup with a rotated
   `TURING_INTEGRATION_KEY` recovers the database and reports the
   credential domain separately. *Kills:* the backup API call with an
   unkeyed destination connection silently writing plaintext; the restore
   that refuses to proceed because integrations can't unseal.
9. **G9 — Rotation is envelope-shaped.** KEK rotation rewraps without
   touching the database file (hash unchanged); DEK rotation runs under
   the migration budgets and fault-injection matrix; interrupted rotation
   of either kind recovers to exactly one usable wrapper. *Kills:* the
   rotation that rewrites the database to rotate a wrapper; the crash
   window with zero valid wrappers.
10. **G10 — The inventory is checked, complete, and honest.** The manifest
    enumerates every artifact in §Inventory; a test creates each artifact
    class (staging, predecessor, backup, wrapper, rotation staging, skill
    recovery export + its staging file) and asserts the inventory reports
    it in the correct class — managed-retirable vs separately-governed
    disclosure. *Kills:* the inventory that forgets `-journal` or rotation
    staging; the report that counts `SKILL.md` recovery files as retired.
11. **G11 — Retirement is fail-closed and total.** Retirement refuses when
    custody is encryption-only, when wrapper state is unknown, or when a
    readable plaintext predecessor remains. A completed ceremony: fences
    held, restart sentinel honored, zero live key holders in the receipt —
    including the unlock agent, whose DEK buffer is asserted zeroed after
    every `Unlock`/`StoreWrapper` and never persisted or logged — every
    managed wrapper gone, and the receipt reporting residual storage bytes
    and the separately governed recovery/staging files as *disclosed, not
    retired*; then **neither a previously connected client nor a fresh
    process restart can read any encrypted-era managed database or
    backup**, asserted by attempting both. *Kills:* the ceremony that
    deletes files but leaves a wrapper; the one that retires while a
    predecessor backup is still readable; the eligibility check that
    defaults open on unknown custody; the unlock agent that keeps a copy;
    the receipt that counts disclosure items as retired.
12. **G12 — Product text tells the truth.** The client's retirement and
    deletion surfaces distinguish whole-database retirement from
    per-session logical withdrawal, disclose the separately governed
    recovery files and user-created copies, and never claim forensic
    erasure. *Kills:* the settings page that says "permanently erased."
13. **G13 — The recovery-cleanup ceremony is exactly as narrow as
    designed.** The in-process `legacy_skill_export_recovery` drop refuses
    when any row's `skills/imported/<id>/SKILL.md` is missing or not
    byte-identical (one corrupted file fails the whole ceremony, no partial
    drop); refuses without a fresh TUR-016 backup receipt; never runs at
    startup or without an explicit operator confirmation naming the row
    count; and after success exactly the verified rows are gone and every
    exported file is untouched. *Kills:* the drop that trusts row counts
    without per-row content verification; the maintenance flow that
    proceeds on a stale or absent backup receipt; the "cleanup" that quietly
    widens into startup code — the exact regression the original
    never-delete-nonempty-recovery invariant existed to prevent.

## Deferred, deliberately

- **`TURING_INTEGRATION_KEY` supersession.** The credential domain stays
  as-is; folding it into database-key custody is a future migration with
  its own design, as the roadmap allows.
- **Host broker / launchd auto-unlock agent** — unattended unlock at login
  without opening the app; a new privileged component, designed separately.
- **Custody classification for non-macOS hosts** — each platform lands with
  its own exclusivity proof or stays encryption-only.
- **`PRAGMA cipher_memory_security = ON`** — upstream disables it by
  default for performance ([API](https://www.zetetic.net/sqlcipher/sqlcipher-api/));
  evaluate with real workloads during implementation, off by default here.
- **Per-session cryptographic erasure** — explicitly out; the roadmap's
  boundary ("one database key cannot erase one session") is a design fact,
  not a gap to close later with this machinery.
- **`mutecomm/go-sqlcipher` fallback path** — exercised only if the
  `libsqlite3`+SQLCipher link fails its validation spike; its staleness
  costs are recorded above so that decision, if forced, is made with open
  eyes.
- **Cross-task risk, recorded: TUR-016's backup mechanism.** TUR-016 lands
  first and owns backup/restore. If it chooses an out-of-process backup
  design, §Connection discipline's EXCLUSIVE-locking decision must be
  revisited before TUR-022 implementation starts; the keyed-connection
  requirement for encrypted-era backups stands either way.

## Documentation the implementation PR must update

- `docs/architecture/2026-08-18-personal-agent-audit.md` — TUR-022 status
  moves from pending-approval to implementation-tracking.
- `CLAUDE.md` — the SQLCipher-linked build flavor, the new CI job, the
  `DATABASE_ENCRYPTION` selector, the retirement ceremony's operator notes,
  and a **rewrite of the `legacy_skill_export_recovery` gotcha's cleanup
  steps** to the in-process ceremony (§The recovery-table cleanup under
  encryption) — the plaintext-era "use a SQLite client" instruction becomes
  wrong the day encryption ships.
- `turing-backend/.env.example` — the selector variable only, with a
  comment stating key material never lives in configuration.
- Privacy/security/operator docs — LOCKED/MIGRATING/RETIRING states, the
  key-loss recovery paths, and the retirement disclosure text (G12's
  wording lives here).
