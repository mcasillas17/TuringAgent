# MCP Security and Integration Guide

This guide describes the implemented security boundary for bundled and
registered MCP servers and their integration with the agent runtime and
orchestrator.

## Deployment boundary

| Tier | Example | Egress | Sandbox-confined | Approval enforcement |
|---|---|---|---|---|
| Bundled | `mcp-system`, `mcp-files` | No | Yes | Cooperating MCP server |
| Local container, third-party | `http://vendor-mcp:9000/mcp` | No | No | Orchestrator caller |
| Remote URL | `https://vendor.example/mcp` | Yes, at enable (discovery) and per run (calls) | No | Orchestrator caller |
| stdio / `command` / `npx` | `command: "npx"` | Refused | Refused | Not registered |

The two bundled servers remain on private Docker networks; Compose does not
publish their ports to the host. Third-party local containers join the
dedicated internal-only `net-mcp-registry` network (`172.31.254.0/24`) and
expose no host port; the caller rejects any local-tier resolution outside that
subnet. An empty
configured bundled bearer token denies every request rather than opening the
service.

Servers can be registered directly from the Flutter MCPs page — a name, a
hardened URL, a local-container or remote-URL tier (the in-app form always
states one), and an optional write-only bearer — with no file edit and no
backend restart. In-app
registration runs the same validation an `mcp.json` import runs: name-pattern
and bundled-name refusal, stdio refusal, URL canonicalization/hardening, and
bearer-token normalization, then seals the token with the same
`internal/secretbox` sealer (the server name as AAD) under
`TURING_INTEGRATION_KEY`; no public or internal response contains a token or
ciphertext. Naming an existing non-bundled, url-empty legacy migration-0016
placeholder is the one existing name this call does not refuse — it is
treated as the operator's own consent to adopt that row in place (see below),
which lets a mobile operator who cannot edit `mcp.json` register a server the
backend already knows about by name. Any other existing name, or a bundled
name, is still refused. Every genuinely new registration still arrives
disabled, and registration itself never contacts the server's endpoint. The
`mcp.server.registered` audit record distinguishes which branch ran via an
`adopted: bool` field alongside the existing token-free name/tier/url keys —
computed inside the same transaction that decided which happened, so it can
never diverge from, or race, what was actually committed. The MCPs page
renders each server's canonical `url` (selectable, ellipsized, with a tooltip
for the full value) so an operator can verify the destination they
registered; an unadopted placeholder with no URL renders an explicit
"Endpoint not configured" warning instead of blank space.

The tier is always derived from the hardened URL, exactly as an `mcp.json`
import derives it. `RegisterMcpServerRequest.tier` is only a caller
assertion: an unspecified tier accepts whatever the URL classifies to (which
is what a client built against the tier-less form of this RPC sends), a
local-container or remote-url tier must match that classification or the
request is refused, and `MCP_SERVER_TIER_BUNDLED` is never accepted. An
explicit registration also clears the deletion tombstone for that name — the
user asking for the name by hand is the consent the tombstone was waiting for
— while file re-import never does. A stored bearer token can be replaced or
cleared afterwards (`RotateMcpServerToken`); a token that is present but only
whitespace is refused rather than silently treated as "clear it". The token
is write-only end to end, and no response, event, or audit row ever carries
it back.

Import is create-only: an existing row for a name that already has a real,
non-empty endpoint is skipped, not repointed. Its enabled state, endpoint,
tier, liveness, rotated token, tool snapshot, and policies are all left
completely untouched, and the reimport response names that entry explicitly
("already registered; existing settings were kept") rather than silently
treating a changed endpoint/token/tools in the file as having taken effect.
There is no in-place "change this server's endpoint" operation: repointing
one requires removing it first, then registering it again at the new
endpoint (see below) — which starts every policy/tools snapshot over from a
fail-closed (disabled, no tools) state, rather than mutating a live row's
endpoint out from under whatever the operator or a running session
currently trusts it to be. An explicit empty `tools` snapshot withdraws
whatever tools a *new* registration or a *legacy placeholder being adopted
in place* previously carried; it never applies to an already-registered,
skipped row, whose tools — like every other field of it — reimport leaves
alone. New servers never arrive enabled. Removal writes a local import
tombstone, so an unchanged `mcp.json` cannot silently recreate the server at
the next restart or re-import; bringing it back requires either a new name
in the file or an explicit in-app registration of the old one.

**Design note: reimport never overwrites an existing row's endpoint, even
when `mcp.json`'s own entry for that name has changed.** An earlier,
parallel implementation of this same MCP registry feature (developed on
`main` while this branch's own hardened version was in progress, and
reconciled into this one implementation once both landed) instead let a
reimport detect a changed `url`/`bearer_token` for an already-registered
name and repoint the live row in place. That behavior is deliberately *not*
carried forward here. The original brief for this feature is that editing
the mounted `mcp.json` file must be idempotent from the user's own point of
view: an operator can safely re-run Re-import mcp.json at any time, and it
either creates rows for genuinely new entries or leaves every already-
registered one exactly as it was — never silently reinterpreting an edited
file as explicit consent to change a live server's endpoint, sealed token,
enablement, or tool policies out from under whatever the operator (or a
running session) currently trusts them to be. Repointing a server is only
ever the explicit, in-app Remove-then-Register sequence described above,
which is unambiguously a deliberate user action rather than an inference
from a file edit. This is a considered product decision, not an oversight
left over from reconciling the two implementations.

`mcp.json` remains the bulk/config-file path. The orchestrator imports it at
startup and, without a restart, whenever an operator chooses Re-import
mcp.json on the MCPs page; both runs report imported, skipped, and refused
names with reasons. A `command` entry is refused as an import issue
explaining that stdio is unsupported, and no server row is created for it.
Malformed documents and entries whose token cannot be sealed are reported the
same way, without preventing the rest of the backend from starting.
`mcp.json` is read only if it is a regular file: the path is opened with
`unix.Open` using `O_NOFOLLOW` (so a symlink as the final path component
makes the raw `open(2)` call itself fail with `ELOOP` rather than being
resolved and followed) and `O_NONBLOCK` (so a FIFO's read-side open
returns immediately regardless of whether a writer is connected, rather
than blocking the calling goroutine), and the resulting descriptor — not
the path a second time — is checked with `Fstat`, refusing anything other
than a confirmed regular file. There is no separate `Lstat`-then-`Open`
pair of path-based syscalls here at all, and therefore no gap between two
such checks for a swapped-in FIFO, symlink, socket, or device node to land
in. This is what actually keeps a reimport from ever hanging: a *plain,
blocking* `os.Open` on a FIFO's read side would wait until a writer
connects, so bounding the *read* alone (see below) would not have been
enough to keep the whole call from stalling forever waiting for one that
never comes. Every other read failure —
including a directory, any non-regular file, or the file having been
replaced by one — maps to the same fixed "read mcp.json failed" message a
client sees, never the path or the underlying OS error text. A malformed or
oversized document is refused as one bounded `_document` reason rather than
failing the whole call outright, so a client can still be told why nothing
imported; a missing file is not a failure at all and instead clears any
previously recorded import issues, so reimporting after deleting `mcp.json`
shows a clean slate. Once confirmed regular, `mcp.json` is read through a
size-bounded reader (`io.LimitReader`, capped one byte past the maximum
supported document size) rather than `os.ReadFile`, so a huge or sparse file
cannot force an unbounded read either.

`ImportJSON` processes each `mcpServers` entry through its own,
independent repository transaction, in sorted-name order, so an earlier
entry's server row is already durably committed by the time a later one
is even attempted. A fatal error `ImportJSON` cannot attribute to a
single entry — a canceled request context, or some other repository
failure between two entries — no longer discards that already-committed
work: the returned report still carries every name already imported or
skipped, and every reason already recorded, exactly as if the run had
kept going. `ReimportConfiguredJSON` folds that failure into the same
report as one more bounded `"_document"` entry (mirroring the malformed/
oversized-document case above) and persists the merged issues through a
context detached from the caller's own cancellation — the same pattern
`auditMCPEvent` already uses for post-commit bookkeeping — so a client
that cancels mid-run does not also erase the record of what already
happened. That persistence itself failing too (a genuine, independent
repository problem, not merely the caller's own cancellation) is the one
case this still surfaces as `Internal` — but only after `ReimportMcpJson`
has already notified the runtime (if anything was imported) and recorded
exactly one audit row with the real, already-committed counts; a later
`Internal` mapping is never a second, separate audit of the same run, and
audit/notify are never skipped merely because the run did not fully
complete.

The root JSON object must declare the key `mcpServers` exactly once, spelled
exactly that way: a case variant (`McpServers`, `MCPSERVERS`, ...) is refused,
and so is an exact or case-insensitive duplicate of it (two roots both
spelled `mcpServers`, or one `mcpServers` alongside one `MCPSERVERS`) —
`encoding/json`'s own case-insensitive field-name fallback and its
last-key-wins handling of a duplicate object key would otherwise silently
accept either shape instead of refusing the ambiguity. The `mcpServers`
object's own entries are parsed the same deliberate way: two entries sharing
the exact same server name are refused as one whole-document failure — never
silently resolved to whichever definition a plain map decode would have kept
last, which could otherwise let a second, differently-configured definition
(a different url, a different bearer) quietly win. The entry count itself is
bounded during that same parse, the instant it would exceed the supported
limit, rather than only after a same-sized map of every entry has already
been built in memory — a document packed with far more (tiny) entries than
could ever actually register is refused without first materializing all of
them. An entry's `headers` object may carry at most one case-insensitive
`Authorization` key and nothing else: a second, case-insensitive-duplicate
Authorization key, or any other header name whatsoever, is refused with one
fixed, generic reason that never names the offending header — because a
header's own *key* is exactly as untrusted as its value, an earlier version
of this message named it directly, which meant a header deliberately (or
accidentally) named with the entry's own bearer token value would leak that
token straight into the refusal reason. The unsupported-header refusal always
takes precedence over the duplicate-Authorization refusal, deterministically,
regardless of how many of either are present. Each element of an entry's own
`tools` array is parsed exactly as strictly: only `name`, `description`, and
`inputSchema`, spelled exactly that way (`inputSchema` is the one canonical
spelling that is not all-lowercase), are recognized, so a case variant
(`Name`, `INPUTSCHEMA`, ...) or an exact-or-case-insensitive duplicate of any
of them is refused with the same fixed, generic, sentinel-free reason used
for every other malformed tool definition — never `encoding/json`'s own
`"unknown field %q"` wording, which would otherwise name the offending key
verbatim (a JSON key inside a tool definition is exactly as attacker-controlled
as its value). Each on-demand Reimport RPC's
response is call-local and deterministic: its `unsupported` list is built from
that call's own report, sorted by name, rather than by re-reading the shared
issues table two overlapping reimports could otherwise race and swap into
each other's response. Skipped names in that response mean the row already had a real,
non-empty endpoint and mcp.json's current url/token/policy for it was
**not** applied — the MCPs page's reimport dialog states this explicitly per
skipped name ("already registered; existing settings were kept") so an edit
to an already-registered entry is never mistaken for having taken effect.
The dialog also states how to actually repoint one: remove the existing
server, then add it again at the new endpoint. That is not merely a UI
convenience path — deleting first writes an import tombstone, and only an
explicit in-app registration naming that exact server clears the tombstone
and creates a genuinely new, disabled row rather than colliding with a live
one. A later `mcp.json` reimport naming that same server does **not** clear
it: `ImportMCPServer` checks the tombstone table first and refuses with the
same fixed "server was removed locally and remains suppressed" reason for as
long as it stands (see below), regardless of how many times mcp.json is
reimported in the meantime — repointing a deleted server by file alone is
not possible. There is no in-place "edit the endpoint of an existing server"
operation, by design, because create-only reimport and explicit-consent
registration are the only two paths that ever set url/sealed_token/tier, and
both start every policy/tools snapshot over from a fail-closed (disabled, no
tools) state rather than mutating a live row's endpoint out from under
whatever the operator or a running session currently trusts it to be.

Reimport is create-only: an existing row for a name that already has a real,
non-empty endpoint is left completely untouched — its enabled state,
endpoint, tier, liveness, rotated token, tool snapshot, and policies never
change. That entry's own `Tools` is decoded as part of the entry's strict
JSON-shape validation, which runs before the existing-row check (so a
malformed `tools` array — an unknown key nested inside one of its
elements, say, or a field of the wrong JSON type — still produces the
same decode-error refusal it would for a brand-new name, rather than a
silent skip that would hide the malformed shape), but it is never validated
against buildImportTools' own deeper rules (duplicate names, per-tool
schema/size limits, a token appearing in a tool's own metadata), never
reconciled through replaceServerToolsTx, and never persisted for that row.
The one exception
is a legacy migration-0016 placeholder: a non-bundled row seeded with
`url == ""` solely to carry a pre-registry tool policy forward until an
operator supplies a real endpoint. A file reimport or an explicit in-app
Register naming that exact server both adopt the row in place (same id) and
are fail-closed about it: url, sealed token, and tier update to the newly
supplied values; the row is forced disabled regardless of what it carried;
liveness resets to unknown/empty, because whatever status the placeholder's
`url == ""` row happened to carry says nothing about the endpoint now
replacing it; and every tool the placeholder carried is withdrawn
(present=0, enabled=0) before this call's own tools, if any, are considered.
A withdrawn tool is not gone for good — a valid static `tools` snapshot
supplied by that same reimport, or a later live discovery once the adopted
server is enabled, reconfirms any tool by matching name, and reconfirmation
preserves whatever policy an operator had already migrated/edited onto it
rather than resetting it to a default; only the tool's presence/enabled state
was ever touched by the withdrawal. Every genuinely new entry, whether from a
file import or in-app registration, still arrives disabled.

An mcp.json entry's optional static `tools` snapshot is fully validated
before the repository is ever touched — well-formed name/schema shape, no
bundled-namespace collision, and the entry's own configured bearer token
never appearing verbatim in a tool's name, its description, or its
serialized schema — and bounded by the exact same tool-count and
encoded-byte limits live `tools/list` discovery enforces, counted the same
way. A tool's optional `description` is scanned for the token and its bytes
count toward that same per-snapshot encoded-byte limit exactly like the
name and schema do, so it cannot hide a token or inflate the entry's real
footprint past the limit unnoticed — but the description itself is never
persisted or returned: only the tool's name and schema reach the repository
and every subsequent descriptor response. It is then handed to the same
repository helper (`replaceServerToolsTx`) live discovery's `RecordDiscovery`
also uses, inside the very same transaction as the server row insert or
placeholder adoption: an inter-server tool-name collision there rolls back
that whole transaction too. Either way, an invalid, colliding, token-bearing,
or oversized snapshot refuses the whole entry with a fixed, generic reason
that never echoes the token, the offending name/schema, or which check
tripped, and leaves no partial row behind for a corrected reimport to get
stuck skipping.

`replaceServerToolsTx` also enforces one further limit, *across* every
server in the registry rather than per server: `repository.MaxMCPRegistryToolBytes`
(256 KiB) bounds the total encoded (name + schema) bytes of *every* row the
`tools` table holds combined — present **and** withdrawn (`present = 0`)
rows alike, not only currently-present ones — checked transactionally
against a fresh query of the whole `tools` table after this server's own
withdrawal and every replacement upsert have already run (including any
bundled-server tools populated by the entirely separate `UpsertTools`
path, since the query reads the table's actual contents rather than a
separately maintained count). Counting withdrawn rows is deliberate, not
an overcount: `ListMCPServerTools` — and therefore `ListMcpServers`' own
per-server descriptor — returns every row attributed to a server
regardless of its `present` flag, since a withdrawn tool's policy is
intentionally preserved (never deleted) so an operator's edits survive a
tool's temporary disappearance. A budget that excluded those rows would
silently undercount what `ListMcpServers` actually sends: a vendor that
keeps rediscovering under a fresh, disjoint set of tool names every cycle
would leave every previous cycle's tools behind, forever withdrawn but
never deleted, and a present-only budget would let that grow the table —
and the response — without limit even while appearing to stay flat.
Counting every row instead makes that same withdrawn history spend the
same budget a present tool would, so repeated rediscovery under
ever-changing names is refused once the real total — exactly what a
client's response would carry — reaches the cap. Without an aggregate
limit at all, up to `MaxNonBundledMCPServers` (256) servers each
independently allowed a nearly-4 MiB snapshot could together make a single
`ListMcpServers` response exceed the 4 MiB gRPC message limit the backend
configures for both directions. 256 KiB leaves substantial margin even in
the worst measured case — a single tool whose schema is one large array of
minimal JSON scalars (`{"type":"object","x":[0,0,0,...]}`), which converts
far less efficiently than spreading the same bytes across many small
tools: each array element becomes a `google.protobuf.Value` carrying a
fixed 8-byte double, costing roughly 9-11 wire bytes against as few as 2
raw JSON bytes, an empirically measured ~5.5x expansion — spread across
the maximum server/URL/status/issue counts, that shape marshals to about
2.17 MiB, roughly 46% of margin under the 4 MiB cap. (An earlier version
of this budget, 1 MiB, was sized only against a weaker shape — many small,
distinct tools, which maximizes protobuf's fixed *per-message* overhead
rather than per-array-element overhead, measuring only ~1.55x expansion —
and did not actually hold: a single tool consuming that whole 1 MiB budget
in the number-array shape marshaled, by itself, to roughly 5.5 MiB —
already past the cap before any server descriptor, Unsupported entry, or
any other tool was even added.) The check itself runs *after* the
withdrawal and every replacement upsert for this reconciliation, not
computed beforehand from a present-only baseline plus the incoming tools'
own byte count, so it measures exactly the table's real resulting state —
the same state a concurrent `ListMcpServers` read would see. A refusal
here rolls back the whole transaction — the server row insert or
placeholder adoption for a static import, or the standalone transaction a
live rediscovery opens — so a server that already had tools is never left
with none, and a brand-new import that would have exceeded the budget
creates no row at all.

`UpsertTools` — the bundled/skills/legacy path the runtime uses to publish
worker tool capabilities (`system`, `files`, and `skills`), and the one
path the paragraph above already notes contributes to
`replaceServerToolsTx`'s own aggregate query — enforces the identical
`MaxMCPRegistryToolBytes` budget against its *own* replacement snapshot,
transactionally, the same way: checked after its own withdrawal (every
present bundled/`NULL`-`mcp_server_id` tool set to `present = 0`) and every
replacement upsert have already run, against every row the table holds —
present and withdrawn alike — with a third-party server's own tools
(populated entirely separately, via `replaceServerToolsTx`) still counted
since both paths read the same table. A refusal rolls back the whole
transaction, the same way, so a refused snapshot never leaves the tools it
was about to replace withdrawn with nothing reconfirmed. Before this,
`UpsertTools` was the one write path that enforced no aggregate budget of
its own at all, even though its own writes count toward the same total
every other path already bounded.

On top of that shared full aggregate, `replaceServerToolsTx` enforces one
further, narrower budget of its own whenever the server being reconciled
is non-bundled ("third-party"):
`repository.MaxThirdPartyMCPRegistryToolBytes` (128 KiB, exactly half of
`MaxMCPRegistryToolBytes`) caps how much of the full aggregate every
third-party server *combined* may ever occupy, checked (and, if both would
otherwise apply, preferred over the full-aggregate reason) before the
unchanged full-aggregate check. `UpsertTools` enforces no such sub-cap —
only the same full aggregate as always. Without this, a sequence of
third-party imports or live rediscoveries could grow their own share of
the aggregate arbitrarily close to the full 256 KiB cap, leaving little or
no headroom for a worker's own, entirely separate, next `ConnectWorker`
call to publish (or grow) `system`/`files`/`skills`' own tool schemas via
`UpsertTools`: a worker connecting after third-party servers had already
filled the aggregate would have its own registration — and therefore
every bundled tool the runtime depends on — refused by the same aggregate
guard, through no fault of its own. Reserving half the aggregate
exclusively for third-party servers guarantees the other half is always
available for `UpsertTools`, regardless of how many third-party servers
exist or how large their own snapshots are. 128 KiB is not merely assumed:
`internal/service/runtime`'s own
`TestFirstPartyBundledToolSchemasFitWithinReservedHeadroom` measures the
real, combined byte total of every tool `system`/`files`/`skills`
register today (about 3.2 KiB) against this reservation and asserts at
least 90% of it stays free, and
`TestConnectWorkerSucceedsWhenThirdPartyToolsFillExactlyTheReservedSubBudget`
proves a worker still connects successfully — through the real
`ConnectWorker` path, not merely at the repository layer — even when
third-party servers already occupy their sub-budget's own exact cap.

As defense in depth on top of every write path now sharing these budgets,
`ListMcpServers` reads the complete server+tools+issues registry state
from a single SQLite read transaction (`repository.MCPRegistrySnapshot`)
rather than as several separately-acquired queries: the database's single
connection (`db.Open`'s `SetMaxOpenConns(1)`) means only one `*sql.Tx` can
be open at a time, so a concurrent tool reconciliation cannot commit
partway through that one read the way it could between two independently
acquired queries, which could otherwise let an earlier guard's decision
(computed against an earlier state) disagree with rows a later query in
the same call actually returns. That same snapshot re-checks three
independent bounds before reading any tool row: the current full tool-byte
aggregate (the same all-rows query, `OverBudget`), the total `mcp_servers`
row count against `MaxMCPRegistryServers` (`ServersOverCap`), and the total
`mcp_import_issues` row count against `MaxMCPImportIssues`
(`IssuesOverCap`) — bounding the *server* and *issue* reads themselves to
one row past each cap (`LIMIT cap+1`) so detecting an over-cap condition
never itself requires reading an unbounded number of rows.
`MaxMCPImportIssues` is `MaxNonBundledMCPServers + 1`, not merely
`MaxNonBundledMCPServers`: the latter already bounds the most named,
ordinary per-entry refusals a single `mcp.json` document can ever name at
once (`ImportJSON`'s own `maxMCPImportEntries` refuses a document naming
more than that, wholesale, before any entry is processed at all — see
above), but `ReimportConfiguredJSON` can still fold in one *more* row on
top of every one of those — the bounded `"_document"` entry
`recordDocumentRefusal` adds when a later, whole-run failure interrupts an
otherwise fully-processed document (see above). Without the `+ 1`, a
single, entirely legitimate run that both names the maximum number of
entries and is later interrupted this way — 256 ordinary refusals plus one
`_document` entry, 257 rows from one honest `ReplaceMCPImportIssues`
write — would have tripped `IssuesOverCap` and degraded the whole registry
for an outcome that did nothing wrong at all. All three
should be unreachable in ordinary operation now that every write path
enforces the matching limit, but none is asserted to be *impossible*
forever: they protect against a future regression that reintroduces an
unguarded write path, and against upgrading a database that predates one
of these limits being universally enforced. Rather than refuse the whole
call, `ListMcpServers` keeps the registry *manageable* whenever any one of
the three trips: every server row (bundled and non-bundled alike) is still
returned — bounded to `MaxMCPRegistryServers` entries when the row count
itself is what is over cap, in full otherwise — an operator retains
enough to identify and delete whichever server is responsible, but every
server's own `Tools` list is left completely empty, and the response's
explicit `registry_degraded`/`registry_degradation_reason` fields explain
why, rather than either attempting to read, let alone marshal and send, a
schema-heavy result sized against an unbounded aggregate, or overloading
the per-entry `Unsupported` list with a systemic, non-per-entry notice
(there is deliberately no reserved `"_registry"` (or similarly special)
name in `Unsupported` for this: a real mcp.json entry named `"_registry"`
is refused through the ordinary synthetic-invalid-entry-name path — see
"Invalid or reserved mcp.json entry names" below — so it can never collide
with this systemic signal). `DeleteMcpServer` itself never reads
`MCPRegistrySnapshot` at all, so it keeps working normally even while any
of the three is set; deleting the offending server cascade-deletes its
tool rows (`tools.mcp_server_id` is declared `ON DELETE CASCADE`) or its
own row (for `ServersOverCap`), which is normally enough to bring the
aggregate/count back under budget, at which point the very next
`ListMcpServers` call recovers full, non-degraded listing automatically —
no separate "clear the flag" operation, migration, or restart is needed.
Because the MCP registry feature this whole document describes has not
shipped in a release yet, no real deployment's database can already carry
rows written by the since-fixed, unbounded `UpsertTools` path or by a
write path that predates the server-count/issue-count caps, so no
destructive migration to truncate or reconcile a pre-existing oversized
aggregate or row count was required here — an operator upgrading a
database that somehow does carry one instead sees the degraded-but-usable
listing above and can recover from it entirely through the ordinary
list/delete/reimport UI, rather than needing direct database access.

Invalid or reserved mcp.json entry names: an entry whose own key fails
`validateMCPServerName` — either because it does not match the name
pattern, or because it names one of TuringAgent's reserved bundled/pseudo
servers — is never recorded, persisted, or returned under that raw
key/name at all. The key an mcp.json entry is filed under is exactly as
untrusted as any other value in the document — it might not even have
been intended as a name (a bearer token or other secret pasted into the
wrong JSON slot, for instance) — so `ImportJSON` records the refusal under
a bounded, synthetic, per-document-deterministic label instead
(`"_invalid_server_1"`, `"_invalid_server_2"`, ... in the same sorted-by-
name order entries are already processed in) with one fixed, generic
reason that never distinguishes "invalid" from "reserved" and never
echoes the entry's own name. This is the one place a raw rejected key
could otherwise have reached the in-memory report, `mcp_import_issues`,
the `ReimportMcpJson`/`ListMcpServers` RPC responses, and the Flutter UI.

Deleting a server writes a local import tombstone, so an unchanged
`mcp.json` cannot silently recreate it on the next reimport; the file path
keeps refusing a tombstoned name. An explicit in-app Register of that same
name is the user's own consent: it atomically clears the tombstone in the
same transaction and does not require a new name. Registering over a name
that still has a live row, or over a bundled name, is refused either way.
`DeleteMCPServer` reads the row, checks its tier, writes the tombstone, and
removes the row all inside its own single transaction, and returns the
exact record it just deleted — read from inside that same transaction —
rather than the service layer needing a separate pre-read of the row
before calling it: a pre-read-then-delete pair would leave a race window
between "read" and "the transaction that actually deletes," which
returning the deleted record from inside that one transaction closes
entirely. That returned record's name and tier are what the service layer
records in a post-commit `mcp.server.deleted` audit entry, even though the
row itself is gone by the time that entry is written.

Token rotation is write-only for non-bundled servers: a new bearer replaces
the sealed value, an empty bearer clears it, and a bundled server refuses
rotation outright. A nonempty token still requires `TURING_INTEGRATION_KEY`
to seal. Because a prior Up/Down liveness observation was made under the
credential rotation is replacing or clearing, rotation atomically resets
liveness to unknown/empty in the same transaction as the sealed-token
update — a status-write failure rolls back the token change too, so a
rotated (or cleared) token can never be left paired with a stale liveness
reading taken under the credential it just replaced. No response, log line,
registry-change event, or audit row ever carries the plaintext token or its
ciphertext; audit rows record only the server name and whether a token is
now configured.

A nonempty token is also refused outright — before it is ever sealed,
persisted, or audited — if it appears verbatim anywhere in the server's
own name or canonical URL, including that URL's independently re-decoded
path (catching a token containing a character `url.URL.String()` would
otherwise percent-encode, such as a quote or backslash, differently from
how the token itself reads). A name and a canonical URL are both public:
returned in every list/register/rotate response and recorded in every
audit row for that server, so a token equal to (or contained in) either
one can never actually be secret regardless of how carefully it is
sealed — anyone who can see the server's name or URL already has it. This
one check is shared by registration and file import (both funnel through
`validateServerDefinition`) and mirrored, separately, by rotation
(`RotateMcpServerToken` compares the *new* token against the *existing*
row's own name/url, since a rotation never sets a new name or URL of its
own); every path refuses with the same fixed, generic reason, naming
neither which of the two matched nor how, the same way this document's
own token-in-tool-metadata and unsupported-header-name checks already do.
This intentionally may refuse a short, coincidentally-ambiguous token
that happens to share characters with an unrelated name/URL substring —
the secrecy invariant wins over that inconvenience.

A per-server credential lock — one `sync.RWMutex` per server id, created on
first use and keyed by that id — fences a rotation against a concurrent call
or discovery for the *same* server: `CallTool` and enable-time discovery hold
it for reading from immediately before they (re-)read the server's current
sealed token through their own network call and liveness/tool-status
recording (deliberately never across an unbounded wait such as caller-side
approval enforcement, which always runs first), and a rotation holds it for
writing across its own read/seal/atomic-replace/liveness-reset. This is
scoped per server, not a single lock shared by the whole registry: an
in-flight call or discovery against one server never blocks a rotation, or a
call, against a completely different one. A server's lock entry is removed
once that server is deleted, so the map's steady-state size tracks the
registry's own row count rather than growing across register/delete cycles
over a long-running process's lifetime; a rotation request naming a server id
that was never real does not create an entry for it either; a lock object a
goroutine already obtained keeps working safely even if its map entry is
removed moments later, since a deleted server's id is never reused.

That last guarantee has one more edge DeleteMcpServer's own cleanup alone
cannot close: DeleteMcpServer forgets a server's entry the instant its own
delete commits, but `CallTool`, enable-time discovery, and
`RotateMcpServerToken` may each not call into the lazily-creating lock
lookup for that same server until sometime *after* that — if any of them
does, the lookup reinstates a brand-new entry for an id DeleteMcpServer
will never forget again, which would otherwise leak permanently rather
than the map's size actually tracking the registry's own row count the
way the paragraph above describes. All three close this the same way:
once their own post-lock re-read of the server discovers it no longer
exists, each removes its own lock entry itself — but only if it is still
the exact object that call installed or found, never a different one
some other goroutine has since installed for the same id, so this can
never race away a lock a concurrent, legitimate use still holds.

Enabling any non-bundled server — local-container or remote-URL — performs a
bounded `tools/list` liveness discovery as part of that call. A server with
no configured endpoint (`url == ""` — the shape a legacy migration-0016
placeholder starts in, and stays in until an operator supplies a real one via
import or explicit registration) can never be enabled at all: the call
refuses with `FailedPrecondition` before it mutates anything, so nothing is
notified, audited, or contacted over the network. Before this check existed,
enabling such a placeholder would flip its enabled bit first and only then
fail discovery against the empty URL, leaving a server that was enabled —
and so whose stale, pre-registry tool snapshot could look available to a
client — despite never having a real endpoint. The Flutter MCPs page mirrors
this: a non-bundled server's enable switch is itself disabled while its `url`
is empty, with a tooltip explaining that an endpoint must be configured
first; adding or registering a server directly remains the one path a mobile
operator (who cannot edit `mcp.json`) uses to give a placeholder a real
endpoint. For a remote-URL server this is a real, explicit enable-time
network request to
the configured endpoint, sending the configured bearer if one is set. That
is separate from per-run consent: invoking a remote tool during a run still
requires the caller to prepare, and the run to acknowledge, a signed
per-run egress decision naming the endpoint and the tool-argument and
tool-result categories, before any call is dispatched. Registration and
`mcp.json` import never contact an endpoint on their own while a server
stays disabled — only an explicit enable does, and only at that moment.

A successful discovery reconciles the server's live tools and schemas: tools
no longer reported are marked absent, newly reported tools are seeded through
`DefaultPolicyFor` (`approval_required` unless already known safe), and an
operator's edited policy on a tool that is still present is left untouched. A
failed discovery leaves the enabled state exactly as the operator set it,
marks the server down with a bounded, bearer-redacted status message, and
preserves whatever tool snapshot the last successful discovery produced.
Enable/disable, discovery outcome, registration (including whether it
adopted a placeholder), token rotation, deletion, and a tool policy change are
all audited; the audit payload and any status text never carry a token. Each
of these actions is also readable back through the audit read API
([Action allowlist](architecture/audit-read-api.md#action-allowlist)): that
API is itself default-deny, so these records only surface at all because each
action has an explicit, reviewed, typed field rule — `mcp.server.registered`
discloses the server name, tier, and URL and whether it adopted a placeholder;
`.enabled`/`.disabled` disclose the name, tier, and whether/whether-succeeded
discovery was attempted; `.token_rotated`/`.token_cleared` disclose the name
and whether a token is now configured (never the token); `.deleted` discloses
the name and tier; and `.tool_policy_changed` discloses the server name, tool
name, and the new canonical policy string. No MCP audit record — through this
API or otherwise — ever exposes a raw stored payload, a bearer token, or its
sealed/ciphertext form.

Peer-controlled MCP errors and results are scrubbed of the registered bearer
before they can cross the internal RPC boundary or reach liveness state, tool
events, audit, or persisted result summaries.

All four bundled backend services run as explicit non-root users. Compose makes every
root filesystem read-only, drops all Linux capabilities without adding any
back, and sets `no-new-privileges`. Writable storage is allowlisted:

| Service | Runtime identity | Writable storage |
|---|---|---|
| `turing-orchestrator` | Validated host UID/GID | `data/` at `/app/data`; `skills/` at `/skills` |
| `turing-agent-runtime-general` | Image user `turing-agent-runtime` (UID/GID 1000) | None |
| `turing-mcp-system` | Image user `mcp-system` (UID/GID 1000) | None |
| `turing-mcp-files` | Validated host UID/GID | `sandbox/` at `/sandbox` |

No service receives a writable temporary filesystem. Each container replaces
Docker's default writable `/dev/shm` with a 64 KiB read-only, `nosuid`, `nodev`,
and `noexec` tmpfs. The static security guard
decodes the complete Compose service map, rejects unresolved `include` or
`extends` inheritance and unknown service/build keys, applies this mount
allowlist, and rejects root or missing users, writable roots, capability
additions, incomplete capability drops, missing `no-new-privileges`, device or
deploy-resource mappings, namespace sharing, supplementary groups,
image overrides or Dockerfile-declared volumes, host aliases, and unapproved
secrets. A Docker-gated companion check builds the four images and rejects
volumes inherited through their base-image metadata; run it with
`TURING_DOCKER_SECURITY_LIVE=1 go test -tags sqlite_fts5 ./turing-backend/tests -run TestBuiltBackendImagesDeclareNoWritableVolumes -count=1`.
The guard also
pins each service to its reviewed build context and Dockerfile, keeps both
networks project-scoped, and permits only one orchestrator public port on the
fixed loopback bind; MCP ports are exposed only inside their assigned network.
The orchestrator configures SQLite `temp_store=MEMORY`, so sorts and transient
b-trees do not require write access to `/tmp`; durable SQLite files and WAL
artifacts remain under `/app/data`.
The bundled servers bound request bodies and configure header, read, write, and idle
HTTP timeouts. `mcp-system` accepts at most 1 MiB per request; `mcp-files`
allows the worst-case escaped 512 KiB mutation envelope (about 3.1 MiB). Both
cap encoded responses at 1 MiB. Responses are ordinary bounded JSON responses,
not open-ended streams, so a finite write timeout is appropriate.

Both services accept JSON-RPC 2.0 `tools/list` and `tools/call` requests at
`/mcp`. Malformed tool arguments use `-32602`; operational tool failures use
`-32000`; an unknown JSON-RPC method uses `-32601`. Request IDs are echoed in
responses.

## Catalog discovery and filtering

`mcp-system` advertises four `safe` tools:

| Tool | Arguments | Result |
|---|---|---|
| `system.health` | none | `{ "ok": true, "service": "turing-mcp-system" }` |
| `system.time` | none | `{ "iso": string, "unixMs": number, "timezone": "UTC" }` |
| `system.echo` | optional `text: string`, at most 65,536 Unicode characters | `{ "text": string }` |
| `system.info` | none | `{ "os": string, "arch": string, "runtime": string }` |

`system.info` deliberately omits environment variables.

`mcp-files` advertises only the five callable tools:

| Tool | Policy |
|---|---|
| `files.list` | `safe` |
| `files.search` | `safe` |
| `files.read` | `safe` |
| `files.create` | `approval_required` |
| `files.update` | `approval_required` |

`files.delete` and `files.move` are not advertised. Their dispatcher cases
remain defensive and return `tool disabled` if called directly.

## Session provenance and withdrawal

Every `files.*` call carries a server-issued provenance capability in
`params._meta.provenanceToken`, outside model-controlled `arguments`. The
capability is short-lived and binds the agent, session, run, deletion
generation, tool, canonical argument hash, and logical path. `mcp-files`
verifies it before and after filesystem I/O. Approval-gated writes also carry
the existing single-use approval token.

Before a mutation reaches the filesystem, the internal approval-consumption
RPC reserves an orchestrator-owned `sandbox_artifacts` row. New files are
written beneath `sessions/<session>/runs/<run>/files/` and default to
`delete_on_session_delete`; a crash between reservation and finalization still
leaves an enumerable record. A post-write internal finalizer marks the manifest
ready. A session deletion that races this flow either rejects the write before
I/O or records it for cleanup and returns failure rather than success.

`retain_legacy_unowned` is the only retention exception: an existing
pre-provenance sandbox-root file that a session updates is recorded but never
deleted with that session. It is not session content and no new retained/shared
policy is implied. Cleanup uses the existing mcp-files listener's dedicated
`/internal/session-cleanup` route, authenticated by
`TURING_MCP_FILES_CLEANUP_TOKEN`; it is not advertised to models or clients
and accepts only a
strictly scoped session namespace request. Public audit records opaque artifact
identity, policy, state, and error class, never path or content.

Bundled discovery follows paginated `tools/list` responses in order, with limits of
100 pages, 10,000 tools, and 4 MiB of aggregate encoded descriptors. It
validates names, descriptions, object-rooted input schemas, and duplicate
names across servers. Catalog entries with policy `disabled` are filtered out
of both model definitions and runtime lookup. Policies `safe` and
`approval_required` remain callable; an unknown or non-string policy makes
discovery fail closed.

The 64 KiB `files.read` content cap and 65,536-character `system.echo` cap are
chosen so their worst-case `encoding/json` escaping fits the runtime client's
1 MiB per-response limit. Even with a maximum-length escaped file path, ten
worst-case results fit the runtime's 4 MiB aggregate tool-result budget.

## File request and result shapes

Paths are sandbox-relative and limited to 4,096 input bytes, 255 bytes per
component, and 64 components. Unix absolute paths, volume-qualified paths, and
any cleaned `..` component are rejected rather than rewritten. Unknown
arguments and wrong types are rejected rather than ignored. These are byte
limits; tool schemas document them rather than misrepresenting them with JSON
Schema `maxLength`, which counts characters. Required paths and search queries
advertise nonblank string constraints.

### `files.list`

- Arguments: optional `path` (defaults to `"."`) and optional integer `limit`
  (default 200, range 1–1000).
- Result:
  `{ "items": [{ "name": string, "isDir": bool }], "truncated": bool }`.
- Scanning is bounded to 4,000 directory entries. Internal staging names are
  omitted. Encoded collection data is capped at 384 KiB. `truncated` is true
  when the requested result limit, scan budget, or result budget prevents an
  exhaustive listing.

### `files.search`

- Arguments: optional `path` (defaults to `"."`), required non-empty `query`,
  and optional integer `limit` (default 50, range 1–200).
- A regular file may be supplied as the starting path.
- Result fields are:
  - `matches`: `{path, snippet}` entries;
  - `truncated`: a result or traversal budget stopped the search;
  - `incomplete`: one or more traversal/read/close failures occurred;
  - `errors`, `errorCount`, and `errorDetailsTruncated`;
  - `entriesVisited`, `directoryEntriesRead`, `directoriesScanned`,
    `filesScanned`, `skippedFiles`, and `bytesScanned`.
- Search budgets are 2,000 visited entries, 1,000 opened files, 8 MiB of
  aggregate reads, and 512 KiB per file. At most 20 error details are returned.
  Matches and error details share a 384 KiB encoded collection budget.
  Symlinks and non-UTF-8 files are skipped. Snippets contain the first match
  with up to 40 bytes of context on each side, adjusted to valid UTF-8
  boundaries.

### `files.read`

- Arguments: required non-empty `path` and optional integer `maxBytes`
  (default and maximum 64 KiB, range 1 byte–64 KiB).
- Result:
  `{ "path": string, "content": string, "truncated": bool, "bytesRead": number }`.
- Reads return at most `maxBytes`, so larger files remain inspectable through a
  bounded prefix with `truncated: true`. Returned content must be UTF-8 and is
  trimmed to a valid UTF-8 boundary. `bytesRead` reports the opened file's full
  length.

### `files.create` and `files.update`

- `create` arguments: required `path` and `content`.
- `update` arguments: required `path` and `content`, plus optional
  `expectedHash` matching exactly `sha256:[0-9a-f]{64}`.
- Content is limited to 512 KiB by bytes.
- Content schemas describe the byte limit without a misleading
  character-based `maxLength`.
- Result: `{ "path": string, "sha256": "sha256:<hex>" }`.
- `create` is exclusive and never overwrites an existing path.
- `update` rejects non-regular files and files with no write permission bit.
  When supplied, `expectedHash` validates the observed content before approval
  and again before replacement.

## Descriptor-relative confinement

The configured root is made absolute and canonicalized once when possible.
Every operation then normalizes the relative input and walks from an open root
directory descriptor:

1. Each directory component is opened with `openat`, `O_DIRECTORY`, and
   `O_NOFOLLOW`.
2. Leaf files are opened relative to the verified parent descriptor with
   `O_NOFOLLOW`.
3. Type and identity checks use `fstat`/`fstatat`; mutations use
   descriptor-relative `linkat`, `renameat`, and `unlinkat`.
4. Directory batches are read as names, then classified with
   descriptor-relative `fstatat(..., AT_SYMLINK_NOFOLLOW)`. This avoids
   `ReadDir`'s path-based unknown-type fallback and never follows a symlink.
5. Search performs a bounded descriptor-relative breadth-first traversal.

This avoids check/use races caused by repeatedly resolving absolute path
strings. A parent renamed concurrently remains the directory represented by
the already-open descriptor; replacing it with a symlink does not redirect
the operation outside the sandbox. Reserved, case-folded
`.turing-create-*` and `.turing-update-*` path components are rejected so
callers cannot address staging files.

Long walks check request cancellation between descriptor opens, directory
creation, and synchronization work. Creation synchronizes the containing
directory and each required ancestor before success; ancestor descriptors are
reopened and closed one at a time rather than retained for the full path depth.

The sandbox restricts path reachability; it is not a substitute for operating
system permissions, container isolation, or careful mount selection.

## Approval ordering

The runtime and file server both enforce ordering:

1. The runtime posts a BEFORE tool-call beacon to the orchestrator.
2. A deny stops immediately. For `APPROVAL_REQUIRED`, the runtime waits for an
   approved token; it does not call MCP while approval is pending.
3. The runtime calls MCP with the JWT in `params._meta.approvalToken`, outside
   `arguments`, so the token is not included in the argument hash.
4. `mcp-files` validates arguments and performs only non-mutating precondition
   checks (for example, existence/type/permission and optional current-hash
   checks) before approval consumption.
5. The server requires `TURING_APPROVAL_JWT_SECRET` at startup. It verifies the
   HS256 signature, `typ == "JWT"`, `iss == "turing.orchestrator"`, and expiry
   (`exp <= now` is expired), then binds `aud` to `mcp-files`, `sub` to the
   bearer-derived agent, `tool` to the requested tool, and `args_hash` to
   canonical JSON of the exact arguments.
6. It synchronously consumes the JWT `jti` through the orchestrator's
   `ApprovalService.ConsumeApproval`. Only `APPROVAL_STATUS_CONSUMED`
   succeeds; replay/not-approved maps to `FailedPrecondition`.
7. Only after successful consumption can file content or namespace state be
   mutated. A later write failure does not restore the single-use approval.
8. The runtime posts an AFTER beacon with the result or failure.

### Caller-side enforcement for non-bundled servers

A third-party server does not know Turing's approval JWT format and never
receives `TURING_APPROVAL_CONSUMER_TOKEN`. Giving it that identity would let it
consume approvals for calls it did not make.

For local-container and remote tiers, the runtime sends the approved
`approval_id` back to the orchestrator over its existing least-privilege
internal connection. The orchestrator verifies the run, server, tool and
canonical argument hash, atomically consumes the approval, and only then sends
JSON-RPC to the registered endpoint. The signed approval JWT is not forwarded
on this path.

This preserves argument binding and single use, but the enforcement point is
different and the guarantee is narrower. Bundled `mcp-files` rejects a forged
direct request itself. A third-party server would not; the guarantee holds
because its registered endpoint and sealed bearer are usable only through the
orchestrator proxy. A process that can reach or authenticate to that server by
some other route is outside this guarantee.

The orchestrator's own "server" check above compares more than the server's
name. `tool_calls.mcp_server_id` (`schema/0018_mcp_approval_identity.sql`)
records, at the moment a tool call is first recorded, the *id* of whichever
`mcp_servers` row currently owns that call's server name — never the name
alone — and `ConsumeApprovalForThirdParty` requires the caller's live-resolved
server id (`CallTool` passes the id it just re-read the server row by) to
equal that stored binding exactly, in addition to run/name/tool/args. This
closes a gap a name-only check would leave open: a server name can be freely
reused after its original row is deleted (`DeleteMcpServer`) and a different
server explicitly registered under that same name, and an approval created
and approved against the original server must not stay consumable against
the new one merely because the two happen to share a name. The foreign key
is `ON DELETE SET NULL`, not `ON DELETE CASCADE` — deleting a server severs
this binding without deleting the tool-call history of a run that already
called it — and a `NULL` binding (whether from that deletion, or from a
legacy `tool_calls` row the 0018 migration's one-time backfill could not
resolve to any then-current server) fails closed: it can only ever match a
caller-supplied id that is itself empty, the permanent state of the two
orchestrator-owned pseudo-servers ("skills", "integrations", neither of
which is ever backed by a real `mcp_servers` row).

The same caller-side rule covers the orchestrator-owned `integrations`
pseudo-server. `github.create_comment` cannot be made safe, and every
`approval_required` integration call—including reads—consumes the
argument-bound approval at the orchestrator before dispatch. The runtime can
discover and dispatch integration tools through the internal service facet but
cannot enumerate, create, revoke, or delete connections; the public client can
manage connections but cannot reach dispatch.

Integration credentials are AES-256-GCM-sealed at rest and opened once per
call in the orchestrator. Plaintext exists only in the provider-call stack
frame, travels only in GitHub's `Authorization` header, and is never placed in
a URL, event, audit row, log, error, or tool result. Provider HTTP uses the
shared public-address resolver and no-redirect client, so a private DNS answer
or redirect cannot move the header away from the pinned `api.github.com`
destination.

Immediately before HTTP dispatch, the proxy rechecks that the run is still
execution-active, the session is not being withdrawn, the server and tool are
still present/enabled, and (for remote tiers) the endpoint and tool remain in
the run-owned egress decision. That final check is the dispatch linearization
point. A cancellation committed after it observes an already in-flight call;
as with a bundled write after approval consumption, it does not retroactively
restore the consumed approval.

`CallRegisteredMcpTool` — the gRPC-facing wrapper `CallTool`'s own internal
`map[string]any` result feeds — checks the fully-built response against
`maxMCPToolResultWireBytes` (4 MiB, mirroring `internal/app`'s own
`maxGRPCMessageSize`) using `proto.Size`, before ever returning it. A
vendor's raw `tools/call` JSON-RPC result is already bounded at the HTTP
layer (`maxMCPResponseBytes`, 1 MiB) before this package ever sees it, but
that bound is on the raw JSON text, not on what `structpb.NewStruct`
converts it into: a JSON number array converts to a repeated
`google.protobuf.Value`, each carrying a fixed 8-byte double plus its own
framing, the same ~5.5x adversarial expansion already measured for a tool
*schema* (see the aggregate-budget accounting above) — enough that a
`maxMCPResponseBytes`-sized result, comfortably within the 1 MiB HTTP
bound, can still convert to a protobuf message well past the 4 MiB gRPC
send cap by itself. A result whose converted size exceeds the cap is
refused with a fixed, generic `ResourceExhausted` status that never
echoes the result's own content or the server's bearer token, before
gRPC's own send path would otherwise refuse it. This check lives only in
`CallRegisteredMcpTool`'s own response path; `CallTool` itself — whose
`map[string]any` result the runtime persists directly into
tool-call/message history, never through a marshaled
`CallRegisteredMcpToolResponse` — is untouched by it.

Human approval comments and denial reasons are durable decision evidence. The
orchestrator stores them in separate nullable columns in the same transaction as
the decision. The existing proto3 scalar fields do not expose presence, so an
omitted human field and an explicitly empty one both persist as `""`; `NULL`
means that a non-human path, such as an unattended approval or expiration, never
carried that field. Non-empty rationale must be valid UTF-8 and is limited to
4096 bytes.

Human decision audit payloads use an allowlist: `toolName` plus the matching
`comment` or `reason`. The audit copy is truncated on a UTF-8 boundary to 512
bytes and gets a `commentTruncated` or `reasonTruncated` marker when shortened.
Approval tokens, JTIs, argument hashes, and tool arguments are never copied into
this payload. Deleting the session cascades the approval row and replaces
correlated audit payloads, including rationale, with `{"scrubbed":true}`.

The canonical argument digest is `sha256:<hex>` over Go `encoding/json`
output with HTML escaping disabled and the trailing newline removed. Map keys
are deterministically sorted by the encoder.

The current file bearer maps to `general_assistant`; approval JWTs must use
that subject. Approval consumption uses the internal gRPC bearer and has its
own 10-second timeout. The default approval lifetime is 65 seconds, the runtime
wait bound is 71 seconds, an individual MCP request is bounded to 30 seconds,
and the whole tool lifecycle retains a 180-second timeout.

## Atomic creation and replacement

Both mutation paths stage content in a randomly named file in the destination
directory, write it completely, `fsync` it, publish it atomically, and
`fsync` the directory. `create` publishes with `linkat` so an existing target
cannot be overwritten. `update` revalidates the original device/inode and,
when requested, its hash before publishing with `renameat`.

`update` preserves only the target's ordinary `0777` permission bits. It
deliberately strips setuid, setgid, and sticky bits from the replacement.
Atomic replacement creates a new inode whose ownership follows the MCP
process and parent-directory creation rules. It therefore does not preserve
the old inode number, old owner/group, old POSIX ACL, extended attributes,
file flags, hard-link identity, or other inode metadata. Attempting to copy
these attributes is nonportable and can reintroduce privilege or race hazards;
callers that require such metadata must manage it outside this
content-oriented tool.

The process-wide normalized path lock serializes `read`, `create`, and `update`
operations made by cooperating `mcp-files` callers in that server process.
Accordingly, `expectedHash` acts as compare-and-replace protection among those
cooperating calls. It is not globally atomic against a host process that writes
the sandbox directly: such a writer does not take the lock and can modify the
target after the final hash/identity validation but before `renameat`. Callers
must not treat `expectedHash` as coordination with non-cooperating host
writers.

## Bind-mount identities and host security systems

Every backend image defines a non-root user. The standalone orchestrator,
agent-runtime, `mcp-system`, and `mcp-files` identities use UID/GID 1000.
Repository Compose overrides the orchestrator and `mcp-files` bind-mount writers
with the validated current host UID/GID so `data/`, `skills/`, and `sandbox/`
remain writable without broadening their permissions. The runtime and
`mcp-system` retain their fixed image identities because they have no writable
mount.

`scripts/init.sh` accepts only canonical positive UID/GID values for the
current process and rejects root, invalid, or out-of-range identities before it
mutates the sandbox or `.env`. It always rewrites `HOST_UID` and `HOST_GID` to
the current host values; manual or stale values are not supported. It rejects a
pre-existing sandbox or skills symlink, creates real mode-`0700` sandbox,
skills, and data directories independently of the caller's umask, and checks
that the sandbox root and existing nested
directories/files are owned, accessible, and not group/world-writable. Symlink
entries are not followed. Data and configured SQLite files must have safe types
and ownership; owned database files are restricted to mode `0600`. A
pre-existing `.env` must be a regular non-symlink file before the script changes
its mode or content. The script reports unsafe legacy content and exits; it
never recursively runs `chmod` or `chown`.

All repository launch scripts delegate to `scripts/compose.sh`. That wrapper
revalidates the current non-root identity and supplies it directly to Compose,
overriding both exported and `.env` `HOST_UID`/`HOST_GID` values. Direct
`docker compose` invocation is unsupported because shell variables take
precedence over `.env` and can bypass that preflight. Immediately before a
launch, the wrapper also verifies that `sandbox/`, `skills/`, and `data/` are
real, owned, restrictive directories and that SQLite files are regular, owned,
mode-`0600` files.

Rootless Docker and daemon `userns-remap` translate container IDs through
daemon-specific subordinate-ID mappings. A host UID copied into Compose is
therefore not guaranteed to map back to that same host account, and bind-mount
writes may fail. Configure the daemon/user namespace and mount ownership
together, or use a pre-provisioned named volume; do not solve mapping failures
by running the service as root or making the sandbox world-writable.

On SELinux-enforcing hosts, DAC ownership can be correct while the bind mount
is still denied by its label. The shared Compose file intentionally does not
append `:Z`, because private relabeling is platform- and deployment-specific
and can be harmful for shared host paths. Apply an appropriate persistent
label, or use a local Compose override with `:Z` for a private mount or `:z`
for a deliberately shared mount. Never relabel an unrelated or sensitive host
directory merely to make it available to the agent.

Keeping the token outside `arguments` is what allows the approval to bind to
a hash of `arguments` without that hash including the token itself (see
"Approval JWT validation" below).

### Authentication and agent identity

All requests must include `Authorization: Bearer <MCP_FILES_TOKEN_GENERAL>`.
The middleware in `internal/auth/auth.go` enforces this and, on success,
returns the **agent identity** that the rest of the request will be bound to.

In v1.0 there is a single token mapped to a single agent:

```go
// v1.0 has one runtime/MCP token for the general assistant; v1.1 should
// replace this with a token-to-agent map.
return "general_assistant", nil
```

That `general_assistant` value is what flows downstream into approval JWT
verification (`sub` claim) — so widening the agent map without also updating
the orchestrator's JWT signer would invalidate approvals.

An empty configured token is treated as rejection, identical to `mcp-system`.

## Sandbox model

The sandbox is the security heart of `mcp-files`. Every path-bearing tool
normalizes its relative input, rejects absolute/traversing/reserved paths, and
walks from an open root descriptor without following symlinks.

### Sandbox root

The sandbox root is configured by `FILES_SANDBOX_ROOT` (default `/sandbox`).
The standalone image runs as its fixed non-root UID/GID 1000. For the local
Compose workflow, `scripts/init.sh` records the host account as `HOST_UID` and
`HOST_GID`, and `scripts/compose.sh` requires and reinjects that current
identity so the process can write to the host-owned `sandbox/` bind mount.
Unset or stale identity values are not accepted; the sandbox is not made
world-writable.
On construction, the root is processed in two steps:

1. `filepath.Abs(root)` — make it absolute.
2. `filepath.EvalSymlinks(abs)` — canonicalize through any symlinks on the
   parent chain. If `EvalSymlinks` fails, the original absolute path is kept.

Step 2 matters because, without it, environments where the parent path
traverses a symlink — macOS (`/var` is a symlink to `/private/var`),
container bind mounts, automount points — will compare a non-canonical root
against a canonicalized resolved target, and every legitimate call will
register as a false-positive "escape". The plan as originally written did
only `filepath.Abs`; the implementation does both. This is a **spec
correction** worth flagging: tests on macOS will not pass against the
plan's literal pseudocode, but they do pass against the shipped resolver.

### Per-call descriptor walk

For a caller-supplied `path`:

1. Reject an absolute/volume-qualified path, `..`, empty required path, reserved
   staging component, or a byte/component/depth overflow.
2. Open the configured root as a directory descriptor.
3. Open each directory component relative to the preceding descriptor with
   `O_DIRECTORY|O_NOFOLLOW`.
4. Open or inspect the leaf relative to the verified parent with `O_NOFOLLOW`
   and descriptor-relative type/identity checks.
5. Publish and clean up through `linkat`, `renameat`, and `unlinkat`, never by
   reconstructing a trusted absolute leaf path.

Two consequences:

- Traversal (`"../../etc/passwd"`) is rejected during normalization, while a
  symlinked parent or leaf fails the no-follow descriptor open.
- `files.create` may create missing parent directories, reopening and
  synchronizing one verified descriptor at a time.

### Search and symlinks

`files.search` uses a bounded descriptor-relative breadth-first walk. Directory
entry names are classified with `fstatat(..., AT_SYMLINK_NOFOLLOW)`, including
when `ReadDir` reports `DT_UNKNOWN`; symlinks are skipped and never opened.
Regular files are opened relative to their verified parent and bounded by the
per-file and aggregate read budgets.

The result: symlink-bearing entries cannot produce search hits, and search
cannot leak content from outside the sandbox even if a symlink was somehow
walked. `TestSearchRejectsSymlinkEscape` exercises this path.

## Read limits and content rules

`files.read` enforces three layers of size and content discipline:

- **Default cap:** `defaultReadBytes = 64 * 1024` (64 KiB).
- **Maximum honored cap:** 64 KiB. Larger regular files return a bounded prefix
  and `truncated: true`; `bytesRead` still reports the full opened-file size.
- **UTF-8 enforcement:** the returned prefix must be valid UTF-8. Binary data
  is rejected rather than exposed to the model.
- **Truncation hygiene:** when truncation does fire, the content is sliced to
  the byte cap and then trailing bytes are trimmed off until the result is
  again valid UTF-8. This prevents partial code points from being returned
  to the model as garbled glyphs.
- **`truncated` flag:** the response includes `truncated: true` whenever the
  cap fired. Callers should treat the content as a prefix when this flag is
  set.

## Approval JWT validation

`files.create` and `files.update` are the only mutating tools, and both are
gated by an approval JWT that the orchestrator signs. The JWT verifier lives
in `internal/approval/jwt.go` and is invoked before any mutation. Non-mutating
precondition checks may run first; **none of the verification steps fails open**.

### Algorithm

- Only **HS256** is accepted. The token header's `alg` field is parsed and
  compared *before* the signature is verified and *before* any payload claim
  is parsed. Tokens with any other `alg` (including `none`, `RS256`, etc.) are
  rejected with `invalid token algorithm`.
- The signing input is `base64url(header) + "." + base64url(payload)`.
- The signature is recomputed with `hmac.New(sha256.New, secret)` and compared
  against the third token segment using `hmac.Equal` (constant-time
  comparison). Mismatch returns `invalid signature`.
- Only after the signature checks out does the verifier decode and unmarshal
  the payload.

### Time bound

The decoded payload's `exp` claim is compared to `time.Now().Unix()`. If
`exp <= now`, the verifier returns `token expired`. There is no skew
tolerance.

### Bound claims

After the structural and time checks pass, the consumer enforces four claim
bindings:

| Claim       | Required value                              | Threat addressed                                  |
|-------------|---------------------------------------------|---------------------------------------------------|
| `aud`       | `"mcp-files"`                               | Token issued for another service is not accepted. |
| `sub`       | `agentID` returned by `AgentFromBearer`     | Approval issued to a different agent is not accepted. |
| `tool`      | `params.name` of the JSON-RPC call          | An approval for `files.update` cannot be replayed as `files.create`, and vice versa. |
| `args_hash` | `sha256(canonical JSON of arguments)`       | An approval bound to one set of arguments cannot be replayed with different arguments. |

`TestValidateRejectsMismatchedApprovalBinding` exercises each of these four
bindings independently.

### Canonical argument hashing

The `args_hash` is computed as:

```
sha256:<hex>  where  hex = sha256(canonicalJSON(arguments))
```

`canonicalJSON` is Go's `encoding/json` encoder output, with
`SetEscapeHTML(false)` and the trailing newline trimmed. It is
deterministic on a `map[string]any` because Go's encoder sorts map keys.

There is a parity test, `TestCanonicalArgsHashMatchesTypeScriptFixture`,
that pins the hash for `{"B": 1, "a": 2}` to:

```
sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161
```

This fixture is the source of truth for v1.0. The Go orchestrator signer must
reproduce the same canonical-JSON byte sequence so its hash matches. If the
orchestrator emits a different hash (different key ordering, different HTML
escaping, different whitespace), every approval will fail with
`approval args_hash does not match call`.

### Single-use consume

After the four claim checks pass, the verifier calls the orchestrator's gRPC
`ApprovalService.ConsumeApproval` method with the JWT's unique identifier
(`jti`). This is what makes approvals single-use.

- Address: `${ORCHESTRATOR_GRPC_ADDR}`. The default is
  `turing-orchestrator:3001`.
- Metadata: `authorization: Bearer ${TURING_APPROVAL_CONSUMER_TOKEN}`.
- Response handling:
  - `APPROVAL_STATUS_CONSUMED` — the JWT was unused; the verifier returns
    success and the write proceeds.
  - `FailedPrecondition` — the JWT was already used, or the underlying
    approval record was never approved. The verifier returns
    `approval already consumed or not approved` and the write is aborted.
  - Any other gRPC error — generic failure: `approval consume failed: <error>`.
    The write is aborted.

The write happens only after consume returns `consumed` and its reservation
identifies the server-derived artifact path. mcp-files finalizes that reservation
after I/O and rechecks the session capability. In other words, a
successful consume is the act of marking the JWT used, and any failure path
after that point (write error, etc.) does not roll consume back. This is
intentional: a partially completed write is still a "used" approval.

The internal gRPC server authorizes each caller by which of two registered
tokens its bearer matches, not by anything the caller claims about itself.
`TURING_APPROVAL_CONSUMER_TOKEN` (held only by the bundled `mcp-files`
consumer) is authorized for
`ApprovalService.ConsumeApproval`, `FinalizeSandboxArtifact`, and
`CheckSessionCapability`. `TURING_RUNTIME_TOKEN` (held by
`agent-runtime-go`) is authorized for that method plus
`ApprovalService.GetApprovalForRuntime`, `RuntimeService.ConnectWorker`, and
`SessionService.ListMessages`/`SearchMessages`. The two tokens must differ —
the orchestrator refuses to start otherwise — so a compromised `mcp-files`
cannot present the runtime's token to claim a job or read conversation
history, and a compromised runtime cannot pose as a different service's
approval consumer.

`TestValidateRejectsConsumeReplayConflict` asserts the `FailedPrecondition`
path.

### Failure-mode ordering

The verification pipeline is, in order:

1. Wrong segment count or non-base64url segments — `invalid token`.
2. Wrong `alg` — `invalid token algorithm`.
3. Invalid signature — `invalid signature`.
4. Missing or unexpected `iss` — `invalid token issuer`.
5. Expired (`exp <= now`) — `token expired`.
6. `aud != "mcp-files"` — `invalid approval audience`.
7. `sub != agentID` — `approval subject does not match agent`.
8. `tool != params.name` — `approval tool does not match call`.
9. `args_hash` mismatch — `approval args_hash does not match call`.
10. Consume returns gRPC `FailedPrecondition` — `approval already consumed or not approved`.
11. Consume returns any other gRPC error — `approval consume failed: <error>`.

None of these branches fall through to the I/O path.

## Failure modes and security rationale

A concise mapping from rejection to threat:

- **Path traversal (`..` or absolute paths).** Input normalization rejects the
  request before descriptor traversal begins.
- **Symlink confused deputy.** Directory and leaf opens use no-follow,
  descriptor-relative operations; search classifies entries without following.
- **Oversized read DoS.** Reads return at most a 64 KiB prefix; search also has
  per-file, aggregate-byte, entry, and file-count budgets.
- **Binary / non-UTF-8 leakage to the model.** The bounded returned prefix must
  be valid UTF-8. Truncation trims trailing partial code points.
- **Approval replay across tools.** `tool` claim must equal the JSON-RPC
  method's `name` argument.
- **Approval replay across arguments.** `args_hash` claim must equal the
  canonical-JSON SHA-256 of the call's `arguments`.
- **Approval reuse.** Single-use `jti` consume against the orchestrator;
  `FailedPrecondition` aborts the write.
- **Token-to-agent mismatch.** `sub` claim must equal the agent identity
  the bearer mapped to. v1.0 has only one agent, but the binding is in
  place for v1.1.
- **No-token "open" misconfiguration.** Empty configured bearer in either
  service is treated as rejection rather than as "everyone allowed".
- **Non-HS256 token substitution (`alg: "none"`, `alg: "RS256"`).** Header
  `alg` is checked before signature verification or claim parse.
- **Disabled mutating tools (`delete`, `move`).** Returned as `tool disabled`
  by the dispatcher; cannot be enabled without a code change.

## Runtime / orchestrator integration

Flutter consumes four public event types:
`tool.call.started`, `tool.call.completed`, `tool.call.failed`, and
`tool.call.denied`. Their payload contract uses camelCase keys and contains
`toolCallId`, `toolName`, optional `serverName`, and `error` only on failed or
denied events. Provider IDs, arguments, status, duration, and result summaries
remain in persisted tool/audit state rather than this UI payload.

### Agent runtime

- Holds one bearer token per MCP server: `MCP_SYSTEM_TOKEN_GENERAL` and
  `MCP_FILES_TOKEN_GENERAL`.
- Routes JSON-RPC requests to `http://turing-mcp-system:7100/mcp` and
  `http://turing-mcp-files:7110/mcp` on the internal Docker network. Both
  base URLs are configurable through `MCP_SYSTEM_BASE_URL` and
  `MCP_FILES_BASE_URL`, with those Docker-network URLs as defaults.
- For approval-gated tools, attaches the orchestrator-issued JWT to
  `params._meta.approvalToken`, not to `params.arguments`.
- Treats any HTTP non-2xx from an MCP server as a hard error (e.g.
  `MCP HTTP 401`) rather than as a tool result.
- Lists enabled non-bundled servers over the internal registry RPC and invokes
  them through `CallRegisteredMcpTool`; it never receives their endpoint bearer.
- Refreshes and re-reports its capability snapshot when server enablement,
  discovery or tool policy changes.

### Orchestrator

The orchestrator implements the signing side and the gRPC consume method that
the Files MCP verifier calls.

#### Dynamic tool discovery, registry and policy

The runtime connects directly only to bundled MCP servers. The orchestrator
discovers local third-party servers and proxies all non-bundled calls. The
runtime reports the combined snapshot in `RuntimeWorkerReady`; each entry
contains only `server_name`, `tool_name`, and the JSON argument schema. Policy
is never accepted from the runtime; the orchestrator remains authoritative.

`RuntimeWorkerReady.tool_discovery_status` makes the snapshot semantics
explicit:

- `TOOL_DISCOVERY_STATUS_COMPLETE` means `tools` is authoritative, including
  when it is empty.
- `TOOL_DISCOVERY_STATUS_FAILED` rejects the worker before admission. A failed
  discovery therefore cannot fall back to a permissive compatibility catalog.
- `TOOL_DISCOVERY_STATUS_UNSPECIFIED` is reserved for runtimes that predate
  discovery. An admitted legacy worker's reported ready-message tools are authoritative
  when present; only a ready message with no tools uses the explicit capability profile's
  fallback list, and an empty fallback means no tools. A modern capability registration
  without a discovery callback reports `COMPLETE` with an authoritative empty set rather
  than selecting legacy behavior.

The orchestrator validates a complete snapshot before admitting the worker,
rejecting blank identities, missing or invalid schemas, and duplicate
`(server_name, tool_name)` pairs. It reconciles the union of all active
workers' snapshots into the SQLite `tools` registry. Re-reporting updates the
schema and enabled state without overwriting an operator-set policy; tools no
longer present in the active union are disabled rather than deleted. On a
fresh database with no connected worker, `ListTools` returns an empty list.

Policy lookup always uses the exact `(server_name, tool_name)` pair. Known v1
read-only tools seed as `safe`, files mutations seed as
`approval_required`, and unknown tools seed as `approval_required` rather
than `safe`. `files.delete` and `files.move` remain permanently disabled.
Once discovery initializes the registry, absent or disabled tools are denied;
they cannot fall through to legacy defaults. Authorization is also scoped to
the authenticated worker connection, so one worker cannot borrow a capability
reported only by another worker even though `ListTools` exposes their union.

The shipped runtime sends `COMPLETE` after every successful discovery,
including a successful empty result, and `FAILED` when a required discovery
attempt fails.

For a run using a remote model or remote MCP server, the orchestrator freezes
the selected `server/tool` names and every remote MCP endpoint into the one-time
egress decision and job. The runtime filters its local registry to that exact
set before serializing tool schemas; a missing selected tool fails the run
rather than widening or substituting the set. Remote MCP tool arguments and
results are disclosed categories. The proxy refuses a remote call whose server,
endpoint and tool are absent from the run-owned decision. Egress controls do
not replace tool policy or approval.

JWT signing requirements:

- Algorithm: **HS256** only. The header must be `{"alg":"HS256","typ":"JWT"}`.
- Secret: the same `TURING_APPROVAL_JWT_SECRET` that the Files MCP verifier
  is configured with. The MCP server has no way to discover any other secret.
- Claims:
  - `aud`: `"mcp-files"`.
  - `sub`: the agent identity (currently always `"general_assistant"`).
  - `tool`: the exact `name` from the JSON-RPC `tools/call`
    (e.g. `"files.create"`).
  - `args_hash`: `sha256:<hex>` of `canonicalJSON(arguments)` (see below).
  - `jti`: a unique identifier per approval. Must be passed as
    `ConsumeApproval.approval_id`.
  - `exp`: the configured approval expiry (65 seconds by default).
  - `iss`: exactly `"turing.orchestrator"`; the verifier rejects any other
    value. `iat` is included for audit context but is not a separate gate.

Canonical-hash parity:

- The Go verifier's `canonicalJSON` is `encoding/json` output with
  `SetEscapeHTML(false)` and the trailing newline trimmed. Go's encoder
  sorts map keys.
- The Go signer must produce byte-identical canonical JSON. The fixture
  `{"B": 1, "a": 2}` must hash to
  `sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161`.
  Mismatches here will cause every approval to fail at the `args_hash`
  check, even though signatures, audience, subject, and tool are correct.

Consume method requirements:

- RPC: `ApprovalService.ConsumeApproval`.
- Request: `approval_id` is the JWT `jti`.
- Auth: gRPC metadata `authorization: Bearer ${TURING_APPROVAL_CONSUMER_TOKEN}`. The
  service must stay on the internal gRPC port (not published to the host).
- Semantics:
  - First call for a given `jti` that corresponds to an approved request
    returns `APPROVAL_STATUS_CONSUMED`.
  - Subsequent calls for the same `jti`, or calls for a `jti` whose
    underlying approval was never granted, return gRPC `FailedPrecondition`.
    The MCP server maps that to `approval already consumed or not approved`
    and aborts the write.
  - Any other gRPC error is treated by the MCP server as a generic failure
    and aborts the write.

### v1.1 follow-ups intentionally deferred

- **Token-to-agent map.** Today, `mcp-files`'s `AgentFromBearer` returns
  the hard-coded string `"general_assistant"` for any holder of
  `MCP_FILES_TOKEN_GENERAL`. v1.1 should replace this with a real map so
  multiple agents can have distinct identities. Until that lands, the
  orchestrator must always set `sub: "general_assistant"` in approvals
  destined for `mcp-files`. There is an inline comment in
  `internal/auth/auth.go` to that effect.
- **`files.delete` and `files.move`.** They remain permanently disabled and
  are not advertised. Re-enabling them is a code change plus a policy decision.

The repository-root `.dockerignore` excludes Git/agent state, `.env` and key
material, runtime data, sandbox contents, dependency caches, and generated
build/test output from root-context backend image builds. Required Go modules
and generated protobuf sources remain in the context. Because Compose builds
`mcp-system` from its module subdirectory, that module has its own
`.dockerignore` with equivalent credential and build-artifact exclusions.

## Local verification

The two services have Go test suites that exercise the server entrypoints,
sandbox, read limits, approval verifier, and auth middleware:

```sh
go test -tags sqlite_fts5 ./.github/workflows -count=1
go test -tags sqlite_fts5 ./turing-backend/scripts -count=1
(cd turing-backend/mcp-system && go test -race ./... -count=1 && go vet ./... && go build ./...)
(cd turing-backend/mcp-files && go test -race ./... -count=1 && go vet ./... && go build ./cmd/server)
golangci-lint run --config .golangci.yml --build-tags sqlite_fts5 ./... ./.github/workflows
(cd turing-backend/mcp-files && golangci-lint run --config ../../.golangci.yml ./...)
(cd turing-backend/mcp-system && golangci-lint run --config ../../.golangci.yml ./...)
```

The Docker smoke path is optional and requires Docker plus its configured
model endpoint:

```sh
cd turing-backend
./scripts/smoke.sh
```
