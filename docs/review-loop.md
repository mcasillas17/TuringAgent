# Review loop — memory / Obsidian brain

A record of the formal multi-model review rounds run against the memory work on
`mcasillas17-implement-turing-brain`, and what each round changed.

One entry per round: who reviewed, what they found, and whether the finding was
accepted and fixed or rejected with a reason. A finding that is rejected stays
on the page with the reason, because "we looked at this and decided against it"
and "nobody looked" are different facts and only the second one is a gap.

Rounds 1–4 predate this file; what they changed is in the branch's commit
history, one commit per finding, and the tests those commits added are the
durable record. This file starts at round 5.

## Round 5

Reviewers: Grok, GPT-5.6 Terra, Claude Opus 5, Claude Opus 4.8.
Outcome: three findings, all accepted and fixed. Terra and Opus 5 found nothing.

### Grok — the inbox sweep let go of a claim on a note that was still there

`sweepVaultInbox` re-decided every candidate row under the lock a decision
holds, and then released every reservation beside them on the strength of a
worklist assembled before any of it ran. So the two halves could disagree about
the same file: a note that came back between the walk and the sweep kept its
row and lost the manifest entry that named it, and a standing `profile_applying`
claim kept its row and lost the same. A reservation is the only durable record
that a session left bytes in the user's vault — no cleaner can find what nothing
names, so the outcome was an untracked claim about the user, in the user's own
vault, that nothing in the system could withdraw.

Accepted. Each release is now re-decided the way a retirement already was:
under the same per-candidate decision lock, on what is true while it is held.
The path must still be absent from the inbox — read through the same confined
reader every other caller uses — and no pending or applying proposal may still
name it. No transaction is opened before the lock and one candidate is locked at
a time, so the sweep cannot wedge against the decision it is coordinating with.

Tests: `memory_reconcile_reservation_test.go`. Both re-checks are separately
mutation-covered — deleting either one fails a test that the other does not
cover — and the release-when-nothing-holds-it case is pinned so the fix cannot
be "keep everything".

### Opus 4.8 — candidate reads answered for a vault nobody could open

A proposal's words live in the user's inbox. The row beside one keeps a copy so
a decision can be audited against what Turing originally wrote; it is not a
second inbox to read from. Every candidate read served that copy the moment the
vault was not attached — the whole listing, the filtered one and the single
fetch — with the reason still saying nothing was wrong. The user was shown text
nobody could confirm was still in their vault, above buttons whose token the
server would then compare against a file it could not open.

Accepted. One rule now covers every surface: a proposal whose file could not be
read keeps its identity, path, lifecycle and reason, and loses its text and its
token. That covers a vault that is not attached at all and a single file the
vault refused. The proposal stays listed rather than being refused outright,
because taking the whole inbox off the page over an unreachable folder tells the
user nothing is waiting when something is.

Tests: `service/memory/candidate_unavailable_test.go` and
`app/memory_unavailable_test.go`, which starts a real app over a database whose
vault has gone away.

### Opus 4.8 — the client read an unnamed problem as a healthy proposal

Whether a proposal had been shown whole was decided by listing the four things
the client knows can go wrong, so anything else passed — including a reason a
newer server invents, which this build decodes as "the server did not say". A
proposal nobody had read arrived with a Promote button and a compare-and-set
token made of bytes the page never displayed.

Accepted. The test is a whitelist: only NONE, the server saying in a value this
build understands that nothing is wrong. What is offered when a proposal is not
whole narrowed with it — a rejection removes the file, so it needs the vault. It
stays on offer where this build knows the vault is open and only the contents
defeated the reader, and is withdrawn where the vault is out of reach or the
reason has no name here. The card says it cannot decide instead, except where a
named reason already puts a truer sentence on it.

Tests: `test/models/memory_unknown_reason_test.dart`, driven off raw wire bytes
carrying an enum value no released server sends, plus the inbox-review widget
tests.

## Round 6

Reviewers: Grok, GPT-5.6 Terra, Claude Opus 5, Claude Opus 4.8.
Outcome: two findings, both accepted and fixed. Grok and Terra found nothing.

### Opus 5 — a proposal that came back unreadable lost the row that named it

A profile edit is a proposal to rewrite the user's own description of
themselves. Round 4 stopped the sweep from consuming one the user had moved into
`beliefs/` by hand, by reading `kind: profile_edit` back out of the moved file.
That reads the file's own frontmatter — so a file whose frontmatter no longer
parses, and one past the 512 KB read ceiling, arrive carrying no kind and no
identity at all. The sweep saw a candidate row naming an empty inbox path,
called it an orphan, deleted it, and released the reservation beside it.

What that leaves is the outcome the manifest exists to prevent, and the same one
Grok's round-5 finding was about from the other direction: a claim about the
user, in the user's own vault, that nothing names, no cleaner can find and no
decision can retract.

Accepted. The retention now falls back to the identity this server minted into
the file's name. It travels with the file exactly as the frontmatter does, and
it is the one part of the file this server rather than its contents is the
author of, so nothing here trusts bytes it could not parse. It is checked
against the profile edits on the books rather than believed on its own: a name
matching no proposal retains nothing, and a belief candidate the user moved is
still swept, because that move is the promotion the primitive would have
performed and finishing it is the crash-heal the pass is there for.

A name carrying no minted identity is the ambiguous case rather than the
negative one. It widens to every profile edit still on the books — the same
answer a `beliefs/` folder nobody could list already gets, for the same reason.
Retaining too much costs one more pass; consuming one costs the user a proposal
in their vault that nothing says was ever proposed.

The page carries the rest. The file is listed under beliefs as an error, never
indexed and never searchable. The proposal stays on the inbox with its text and
its compare-and-set token withheld, so no decision is offered against a path the
file is no longer at. And the error line now names the inbox path the proposal
is still tracked at, said off the minted name alone, so "move it back" is an
instruction rather than a guess. Nothing in that sentence is the file's
contents — a file nobody could read has no contents to quote.

Tests: `memory_reconcile_unclassified_test.go` for the malformed and the
over-limit move, for the unrelated proposal that is still retired beside them,
for the uncorrelatable name that widens the answer, for the belief candidate
that is still swept, and for the ordinary decision resuming once the user moves
the file back; `service/memory/misplaced_unclassified_test.go` for what the page
says on both sides of it; `memoryfiles/inbox_identity_test.go` for the shape of
a name this server will read an identity out of. Five separate mutations are
covered: dropping the correlation, dropping the widening, retaining everything,
trusting the name without checking it against a proposal, and dropping the
move-back sentence each fail a test the others do not.

### Opus 4.8 — the pinned-snapshot mapping test named some of the fields

`TestMapJobCarriesTheFrozenPinnedSnapshot` asserted the persona's id, body and
hash. Dropping `DisplayName` from the mapping left the whole runtime package
green: the name the user gave their persona could fall off every queued job and
nothing would have said so. The snapshot the test built also used one string for
both the persona id and its display name, so even an assertion would not have
told a mapping that carries both from one that carries the id twice.

Accepted. Distinct values now, and the display name asserted. Beside it, a guard
that reads the fields off the descriptor rather than off a list somebody has to
remember to extend: every field of both snapshot messages is filled with
something nothing else could have produced, and every one has to arrive set. A
field added to the wire and not to the mapping fails there — and the profile
snapshot having no display name to carry is now something a test says rather
than something it silently omits.

Tests: `service/runtime/memory_job_test.go`. Mutation-covered field by field —
dropping each of `persona_id`, `display_name`, `profile_id`, `body`,
`content_hash` and `withheld` fails a test that the previous file passed.

## Round 7

Reviewers: Grok, GPT-5.6 Terra, Claude Opus 5, Claude Opus 4.8.
Outcome: five findings, all accepted and fixed. Grok found two.

### Grok — withdrawal was owed to a filesystem pass that may never run

Deleting a conversation removes the evidence rows that ground the beliefs it
produced; the cascade does that transactionally. What marked the belief itself
withdrawn was the reconcile pass that runs *afterwards*, outside the
transaction, over the whole vault — and that pass refuses outright on a vault
past the scan bound, on one it cannot enumerate, and on one that is not attached
at all. The rows go either way, because removing them is the half of the promise
the user asked for and it is never held hostage to a folder.

Between those two facts sat a claim about the user whose every conversation they
had deleted: still `managed` in the index, still returned by search, and still
readable by identity — which is exactly the door round 3 closed for a model that
saw a belief before its conversation went. It is not a withdrawal if the only
thing that performs it is a pass that may never run.

Accepted. The index status is now written inside the deletion transaction, for
exactly the notes this conversation was the last support for. "Last support" is
asked of the rows rather than of the file: a belief two conversations ground
does not lose its grounding because one of them was deleted, and withdrawing it
early would take a memory the user accepted away over a conversation they were
entitled to remove. The file's own frontmatter is still the completion pass's
job — that is a write into the user's folder, and it is not something to hold a
deletion open for — but the half search and reads answer from commits with the
cascade or not at all.

Tests: `session_delete_withdrawal_test.go`, over a vault deliberately pushed past
the index bound so the completion provably cannot run; for the belief a second
conversation still supports, which stays until the last one goes; for a belief
one conversation cited twice, which is one source and not two; for the retry
after an unfinished completion, which withdraws nothing a second time; for a
note already withdrawn with its citations still linked, which is left untouched
rather than re-written and re-logged; and for the rollback, where the barrier
reports what it withdrew so the test says the withdrawal happened before it says
the failure took it back. Four separate mutations are covered.

### Grok — a walk that refused answered with an empty inbox

`readVault` returned early on a scan failure with no candidates at all, so the
whole page and the unfiltered listing told the user nothing was waiting on them
while pending proposals sat in their vault. A vault one note over the index
bound, or one folder the walk could not enumerate, was enough — and "there is
nothing to decide about you" is the one answer that is not true.

Accepted. The failure now falls back to the rule every read with no whole-vault
walk behind it already uses: the rows are listed, and each proposal is read once
through the confined reader. A folder too large to index is not a file nobody can
open, and that one read is the same read the decision performs — so the page
shows the words the decision will be checked against, or it shows nothing and
says why. The page, the unfiltered listing and the single fetch now agree.

Beside it, a proposal the walk merely did not get to no longer claims the file is
gone. Absence means the file is not there only over a listing that finished;
over one that did not, nobody looked, and the page says the folder could not be
read instead.

Tests: `service/memory/list_state_walk_failure_test.go` — over-bound vault on
both listings, list and fetch asserted token for token, an inbox nobody can open,
and the incomplete-walk classification.

### Terra — an unknown note availability read as a findable belief

`MemoryNote.isIndexable` accepted the server saying nothing is wrong *and* the
server not saying. The second is also what this build decodes any reason a newer
server invents into, so a note nobody could account for rendered as an ordinary
belief — and the line that would have told the user their memory is not being
found was suppressed by the very condition that made it unfindable. Round 5 made
the same test on a proposal a whitelist; the note beside it was still a
blacklist.

Accepted, and widened to the status while there. The server's own search
predicate answers from `managed` and `unmanaged` and from nothing else, so the
page now says a note is findable on exactly those terms: a withdrawn belief is
kept and no longer implied to be searchable, a status from a newer build claims
nothing, and a reason with no name here gets the "the server did not say"
sentence rather than a blank line.

Tests: `test/models/memory_unknown_note_test.dart`, driven off raw wire bytes
carrying an enum value no released server sends in the reason field and in the
status field, plus a whitelist assertion over every value of both; and two
inbox-page widget tests for the unnamed reason and the withdrawn note.

### Opus 5 — the page explained a refusal with a token nobody sent

A compare-and-set token on screen is there to explain a refusal: the user is told
this applies only while the document still matches X, the server refuses because
it does not, and the sentence only helps if X is what was sent. Round 4 made a
composed profile result keep the token it was composed against, precisely so a
re-read cannot re-aim an unsaved edit at a newer document. The card beside it
went on printing whichever profile hash was read most recently, and the persona
and profile editors printed the document's newest hash while a save carries the
one the editor was loaded at.

So the page showed the true state of affairs — the save will be refused — and
named the wrong number for it. A user comparing what they were shown against what
the server complained about would find they do not match, and nothing on the page
would explain why.

Accepted. The proposal card now names the token its own apply will carry, and
each editor names the version it is holding. An untouched editor still follows
the document, because a re-read does adopt it.

Tests: `test/ui/memory_cas_display_test.dart` — an edited result and both dirty
editors after the vault moves underneath them, each asserting the number on the
screen and the number in the request are the same one, plus the untouched editor
that must still follow.

### Opus 4.8 — unparseable bytes were served as the proposal, out of the row

Round 5 established one rule for a proposal whose file could not be read:
identity, path and lifecycle kept, text and token withheld. The parse-failure
branch of the walk-backed overlay predated it and did the opposite — it served
the database row's copy of the text and the row's hash. The row is Turing's
record of what it wrote before the user opened the file; the file is what the
user is looking at. So the card showed text the file no longer says, above a
token taken over bytes nobody could parse, and the decision compares that token
against the file and refuses. Two of the four surfaces did this and two did not.

Accepted. One rule now covers a proposal nobody could read whatever defeated the
reader — an absent vault, a refused file, or bytes that are not a note. The parse
detail stays, because it is what tells the user which file to open and fix, and
nothing in it is the file's contents. The rejection makes no claim about bytes
and still works, which matters most here: it is the only way out a proposal in
this state has.

Tests: `service/memory/malformed_body_withheld_test.go`, asserting all four
surfaces — the whole page, the unfiltered listing, the filtered listing and the
single fetch — against one corrupted proposal, and asserting the identity the row
owns survives on each.

## Round 8

Reviewers: Grok, GPT-5.6 Terra, Claude Opus 5, Claude Opus 4.8.
Outcome: seven findings. Six accepted and fixed, one rejected with a reason.
Grok and Terra found nothing.

### Opus 5 — the card read its token before the build that re-aimed it

Round 7 gave a composed profile result a token that travels with the words, so
a re-read cannot silently re-aim an unsaved edit at a newer document. The card
asked for the token and the editor separately, in that order, and the re-seed
happens inside the second call. So on the one build where an untouched result
was re-composed from a newer profile — the build where the number changes — the
line said the version the *previous* build composed against while the apply
carried the new one.

Both cases the page owns had been reasoned about; the ordering between them had
not. It is the ordinary failure of a rule enforced by call order: correct
everywhere except the moment it describes.

Accepted, structurally. The editor and its token come back from one call, which
re-seeds first and answers with what it just decided, and the apply carries the
token the card was handed rather than looking it up again when the button is
pressed. There is no second call to order.

Tests: `test/ui/memory_cas_display_test.dart` — an untouched result after the
profile moves, asserting the number on screen and the number in the request are
the same one, beside the edited-result case that must not move.

### Opus 4.8 — the proposal half of an apply had no such rule

An apply carries two compare-and-set tokens: one against profile.md, which the
result is an edit of, and one against the proposal, which the result is an
acceptance of. Only the first had been given a home that travels with the text.
The second was read off whichever listing was newest.

A proposal is a file in the user's inbox and they can rewrite it in Obsidian
between composing a result and applying it. The apply then said "I read this and
I accept it" about a claim about them that nobody had read, and the server
accepted it, because the newer proposal is exactly what the request named.

Accepted, under one rule for both: an untouched result is re-composed and adopts
the pair it was composed from; an edited one keeps its words, its profile
version and its proposal. The card names both and the apply sends the two it
named, so where either has moved the server refuses and the user is shown the
new claim before deciding on it.

Tests: `test/ui/memory_cas_display_test.dart` — an edited result after the
proposal and the profile both move, asserting the proposal token on screen and
in the request, and the untouched result that must follow the newer proposal.

### Opus 4.8 — an editor that could not keep what it was given

The two authored documents stay live while a save is in flight on purpose: the
user keeps typing, the editor is still there when the answer comes back, and the
newer words stay theirs. Round 4 pinned that. The resulting-profile editor
beside a proposal inherited the liveness and cannot honour the promise: a
successful apply decides the proposal, the card leaves the page and the editor
is disposed with it, so anything typed after the button was pressed was neither
sent nor kept anywhere the user could find it.

Accepted. The field is closed while the page is busy, and rendered disabled
rather than merely ignoring input — the state is true, a screen reader can
report it, and a field that cannot keep what it takes must not take it. The
decision buttons beside it were already closed for that window.

Tests: `test/ui/memory_apply_test.dart` — a delayed apply, asserting the field
refuses input while it is in flight, that a keystroke aimed at it lands nowhere,
that the document sent is the one that was reviewed, and that the editor opens
again once the request has answered.

### Opus 4.8 — a rejection could delete the file that replaced the one decided

A rejection deletes the proposal the user read. The unlink named a name, and the
entry under that name between the check and the unlink is not necessarily the
file that was checked: Obsidian, a sync client and Turing's own writer all
replace a file by writing a new one beside it and renaming over the top, so the
window is the ordinary way this vault gets written to rather than an exotic
race. A newer claim about the user that landed in it was deleted by a decision
about the older one, and the user was told their rejection succeeded.

Accepted. The deletion is two steps. The candidate is opened first and that
descriptor is the identity everything afterwards is held to; the entry is then
detached from its name in one atomic rename into a private staging name inside
the same confined directory — the link-and-stage discipline every create here
uses, run backwards. What was detached is compared against the opened descriptor
and, where the decision named bytes, against those bytes again, before anything
is unlinked: an editor saving in place keeps the inode, so identity alone would
call the user's own newer words unchanged.

Where it is not the same file, nothing is deleted. It goes back under its own
name with link semantics that refuse to overwrite. An unreferenced file somebody
can still find is recoverable and a deleted one is not. The hashless door keeps
its reason for existing — a proposal nobody can parse, or open at all, is still
removable on the identity of the entry that was inspected — and Turing's own
tidying keeps the plain idempotent unlink, still confined to the inbox and still
unreachable as a user's rejection.

Tests: `memoryfiles/remove_identity_test.go` — a barrier standing at both
moments another writer can be at, asserting a replacement survives a rejection
of what it replaced, that the same holds for the hashless malformed path, that
an in-place rewrite is refused, and that the ordinary paths still delete and
leave no staging behind. Deleting either check fails a test the other does not
cover.

Two residuals of that fix are now closed. The first: when the name had been
taken meanwhile, the bytes were left under the private staging name and the
refusal merely said where they were. That name is dot-prefixed, and the vault
walk skips dot entries on purpose — so an unread claim about the user sat on
disk but on no page, in a directory the user was never told to look in. It is
now moved instead to a visible confined name the server mints,
`recovered-inbox-draft-<ULID>.md`: linked with the same no-clobber semantics,
fsynced, and only then unstaged, so a crash at any point leaves the bytes under
at least one name. The name is deliberately uncorrelatable to any candidate row,
so the next scan surfaces it as an unmanaged draft the user can read, keep or
delete — the recovery is something they can act on rather than something they
are told about. The refusal itself is bounded and says only that a draft was
recovered and where; it never carries the text, because a refusal is logged and
the whole point is that these bytes are the user's.

The second: only the taken-name case was ever exercised, so a link failing for
any other reason — a full disk, a lost permission, a vanished directory — was
untested on a path whose job is to not lose data. The link is now behind a seam
a test supplies, addressed by the name being linked to, so a test can fail the
restore alone or fail everything. Under either, nothing is unlinked, the state
left behind is deterministic, and the error reaches the user. The cleaner still
goes nowhere near this: it neither detaches nor links, and the staging prefix
stays reserved and unnameable from outside.

Tests: `memoryfiles/remove_recovery_test.go` and
`service/memory/rejection_recovery_state_test.go` — the rescue under a contested
name, the recovered draft appearing in the next scan and in `ListMemoryState`
correlated to nothing, an outright link failure losing neither the bytes nor the
error, the refusal clipped on a rune boundary, the staging prefix still skipped
and still refused as a name, the cleaner's plain unlink untouched, and the
ordinary rejections still leaving neither staging nor a draft behind.

### Opus 4.8 — a challenge for a run no worker could execute

The route a prepare validated was built before anything had looked at where the
frozen tools go, so it said "local model, no egress decision" — which a worker
built before egress decisions existed satisfies. The tool snapshot was then
taken from that same pre-decision candidate set, a remote MCP server or an
integration turned up in it, and the challenge went out. The send that follows
freezes a decision onto the job, and dispatch will only hand it to a worker that
can validate one. So the user consented to sending their words to a named
destination, and the run went into a queue nothing would take it out of.

Accepted. The route is rebuilt and validated a second time, once the egress
decision requirement is known and the tool snapshot is frozen — and the slice
that is validated is the slice the challenge signs, so nothing can move between
the two. A challenge that goes out is a promise the run can happen; where no
connected worker can execute it the caller gets the routing refusal, which names
what is missing, instead of a consent dialog for a run that cannot happen. The
first pass stays, documented as provisional: an unknown model or a tool nobody
has is a better answer early than one arrived at after reading the vault.

Tests: `service/chat/local_model_remote_tool_route_test.go` — a local model with
a remote MCP tool and with an integration, against a worker advertising the
literal pre-memory decision version, asserting the prepare and the send both
refuse with the routing detail that names the version; against a current worker,
asserting the challenge is issued and the tools it signs are the ones that were
validated; and a purely local run that still needs no decision.

### Opus 4.8 — a disclosed tier nobody named, rendered as the one before it

Every other enum on this client is decoded through `decodeClosedEnum`, because a
closed enum keeps the last value the parser *recognised*: a newer value sent
after a known one leaves the known one sitting in the field while what the
server meant is filed away as unknown. The tier on a memory egress disclosure
was read straight off the field.

So a tier this build has never heard of arrived on the consent dialog as
whichever tier the bytes before it named — "Persona", under "Memory pinned into
this run", which tells a person the exact words of their persona document are
already in the prompt they are about to send to a remote model. The one screen
whose whole job is to be exact about what leaves the machine was inventing the
claim.

Accepted. It decodes like the rest. An unnamed tier is unspecified, which the
dialog already renders as "Memory" and lists under what the tools can reach
rather than what is pinned — honest about the row, silent about the tier.

Tests: `test/networking/memory_api_test.dart`, driven off raw wire bytes
carrying a recognised PERSONA followed by a value no released server sends,
asserting the mapped tier and that the row is not counted as pinned; and a
dialog test pinning the label and the heading such a row gets.

### Opus 4.8 — jobs queued under the previous decision version. Rejected

A job enqueued before the memory bump keeps its decision through migration 0019,
and after the upgrade no worker will execute it: the runtime's shape check
refuses a decision whose version is not the current one, and the run ends
terminally. The finding proposed rescuing those runs — accepting the older
version, or bringing the stored decision forward.

Rejected, and the behaviour is unchanged. The plan's acceptance item is "version
skew fails closed at dispatch — the literal pre-bump number", and migration 0019
keeps such a row exactly as recorded, with an empty memory snapshot fingerprint,
precisely because a consent given before memory existed disclosed no memory and
must never be retroactively credited with any. Both alternatives are worse than
the failure: executing the run would carry the user's persona and profile under
a consent that disclosed neither, and rewriting the version would forge that
consent with this server's own signature. A typed, terminal, never-retried
refusal is the honest third answer — the person is told, and the way forward is
sending the message again under a disclosure they actually read.

What was missing was the record. The outcome was implied by a constant
comparison and a migration comment, and a decision nobody wrote down is
indistinguishable from an accident the next reader helpfully fixes. So the
rejection is now falsifiable rather than assumed:
`agent-runtime-go/internal/agent/egress_version_skew_test.go` holds a job
carrying the literal pre-bump version refused while the same job at the current
version passes, and asserts the run it produces is one named, terminal,
never-retried failure and nothing else; and
`docs/architecture/remote-egress-policy.md` now says which case a migration
terminalizes and which is refused at dispatch, beside the TUR-003 paragraph it
is easy to mistake for.
