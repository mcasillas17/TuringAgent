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
