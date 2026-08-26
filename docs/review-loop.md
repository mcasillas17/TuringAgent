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
