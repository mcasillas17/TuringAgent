# Needs-you queue

The Needs-you queue (UX-002) is one orchestrator-computed list of everything
that waits on the owner, across conversations. UX-002a ships the projection
and the API; UX-002b ships the app's destination (see [The app](#the-app)). The design is section 8 of the
[superagent adoption spec](../superpowers/specs/2026-11-01-turing-superagent-adoption-design.md).

## Stage table

`internal/attention` is the projection: a pure, closed, first-match-wins
table that classifies one subject's conditions.

| Order | Stage | Source today | Row leaves when |
| --- | --- | --- | --- |
| 1 | `person/approval` | A pending approval whose expiry is in the future, on any run. A delegated task's is listed under its parent conversation. | It is decided, expires, or its conversation is deleted. |
| 2 | `person/consent` | A delegated task still queued with `egress_consent = 'pending'`. | The owner allows or declines, or the task stops. |
| 3 | `person/blocked` | An enabled automation whose latest occurrence failed or was blocked: a failed run, a cancelled run the owner did not cancel, or an `automation.remote_egress_blocked` record newer than its last run. | A later run succeeds or is still going, the automation is disabled or deleted, the failed run's conversation is deleted, or the owner dismisses that occurrence. Never by age. |
| 4 | `person/deletion` | An owner's deletion receipt (no parent) held `failed_external` and retryable. | It completes or stops being retryable. |
| 5 | `person/proposal` | None until PRO-001. | — |

A row whose source's stage does not hold for it (today: an approval whose
expiry cannot be read) is listed as `unavailable` in its source's place, so
an empty list is always a fact. Suspended watches (stage 3) and proposals
(stage 5) are PRO-001's sources; `repository.attentionSources` is where they
are added.

## `AttentionService`

Public only. It is not registered on the internal server, so the runtime and
the approval consumer are refused.

- `ListAttention(page)` returns up to 100 rows (`page.limit` 1–100, 0 means
  100). A row carries its stage, source kind, `since`, the conversation's id
  and title, and the ids its action needs; an automation row adds its name,
  `occurrence_key` and a closed `cause` code, and a deletion row its failure
  class. It never carries tool arguments, message content, task text or
  transcripts.
- `DismissAttention(automation_id, occurrence_key)` hides one automation's
  occurrence. It is refused with `FailedPrecondition` unless the key is the
  automation's current, failing, latest occurrence, and with `NotFound` for
  anything that is not an automation (approvals, consents and deletions
  cannot be dismissed).

### Order and paging

Rows sort by stage rank, then `since` descending, then source kind
(approval, delegation, automation, deletion, watch, proposal), then source id
ascending. The page token is base64url JSON holding the first page's
watermark (its read time) and the last row's sort key; a token that does not
decode, or names an unknown stage or kind, is `InvalidArgument`. Each source
compares its stage rank with the cursor's first (an earlier stage reads
nothing, a later one everything), and within the cursor's stage compares
`since` before kind. Later pages leave out rows whose `since` is after the
watermark; the next first page shows them. A pass therefore shows no row
twice, never reorders rows, and skips nothing that qualified at the watermark
and still qualifies with the same sort key. A row committed late with a
`since` at or before the watermark (an approval's `created_at` is taken
before its transaction) is shown by a later page when it sorts after the
cursor and otherwise by the next first page, at most once either way.

## Occurrence key and dismissal

An automation's latest occurrence is keyed by its latest
`automation.remote_egress_blocked` audit id newer than `last_run_at`, else by
`last_run_id`. Run and audit ids carry distinct prefixes. Migration 0031 adds
`automations.attention_dismissed_occurrence`; stage 3 skips an automation
while its key equals it, so the next failure, with or without a run, raises
the row again.

## Hint

`SubscribeSessionUpdates` carries `TURING_EVENT_TYPE_ATTENTION_CHANGED`, with
no session, run, sequence, time or payload. The bus derives one from every
`approval.*`, `delegation.*`, `agent.run.completed|failed|cancelled` and
`session.deleted` event (it cannot tell an automation's run from a chat's, so
every run ending hints). Transitions that publish no event call
`Bus.PublishAttentionChanged`: a scheduler fire that queues a run, records a
remote-egress block or disables an unreadable schedule; enabling, disabling,
editing or deleting an automation; a dismissal; and a deletion that begins,
or whose receipt starts or stops being retryable or changes its cause. Hints
coalesce per subscriber.
Approval expiry publishes nothing; the app refetches on resume.

## The app

UX-002b is the app's **Needs you** destination
(`turing-client/turing_app/lib/features/attention/`).

- **One queue, owned by the shell.** `AttentionQueue` holds the rows of the
  pages read in the current pass. The shell owns it because the badge beside
  the destination is the first page, read whether or not the page is open.
  It reads on four triggers: the shell starting, the page opening, an
  `attention.changed` hint on the shell's existing updates stream, and the
  app resuming. A row's action finishing is a fifth. The updates stream
  dropping adds two more, because hints sent while it was down are lost:
  the first reconnect attempt after a drop reads, and so does a stream that
  comes back and stays up after several attempts. Attempts in between do
  not, so a stream that keeps failing never turns its backoff into polling.
  Requests are coalesced: at most one read starts per second, and never two
  at once. Nothing reads on a timer of its own. A refetch starts a fresh
  pass from the first page and reads as many pages as the last pass had, so
  a row reached with **Load more** stays on screen. Pages of two passes are
  never mixed. Load more asked during a read runs when that read ends, and
  a failed Load more is reported beside it, not as a stale list. The shell
  rebuilds only when the badge changes.
- **Badge.** The badge is the first page's row count, shown as "100+" when
  that page has a next page, so no second count query is needed. It reads
  "!" when the latest read failed and no row is known, because not knowing
  is not the same as nothing waiting. The empty state takes no space: with
  nothing waiting, the destination is not in the rail at all (it stays while
  it is open). A backend without `AttentionService` (`unimplemented`) gets
  no destination. On the compact layout the menu button carries the badge,
  since the rail is hidden in the drawer there.
- **Rows.** Each row is a summary built only from the `ListAttention`
  response: stage, the conversation's title (or the automation's name),
  `since`, a closed cause code put into words, and its actions. Titles and
  names render as plain `Text`, never markdown or links. The mapper turns a
  stage or kind this build does not know, or a row missing an id its action
  needs, into `unavailable`, which is listed with no action except **Open
  chat**.
- **Expanding mounts the existing flow, one row at a time.** An approval row
  makes one `GetApprovalDetails` read and hands it to the chat's
  `ApprovalCard`, so mounting the card does not read twice. The details must
  name the row's approval and run, and its conversation unless the row is a
  delegated task's (listed under the parent, while the approval is the
  task's own). A delegated task's row also reads `GetDelegation`, so the
  card names the specialist asking. A consent row makes one `GetDelegation`
  read for the task's `DelegationCard`, whose Allow… opens the chat's
  consent dialog. Expanding one row collapses any other, and a refetch that
  still lists the open row keeps its card mounted.
- **Actions go through the chat's RPCs**: `ApproveApproval` with the
  reviewed details, `DenyApproval`, `PrepareDelegationEgress` /
  `DecideDelegationEgress`, `DeleteSession` for **Retry deletion**, and
  `DismissAttention` with the row's `occurrence_key`, unchanged. Only a stage
  3 automation row offers Dismiss. The server re-checks each action, so a
  stale row's action is refused, not replayed. The refusal is shown in the
  server's words when it explains itself (`failed_precondition`,
  `not_found`, as a mapped code or a raw gRPC status), and the list is read
  again either way, so an expired row disappears. A refused approval also
  re-reads its review, as the chat does, since the preview may be what went
  stale. Retry deletion runs the shell's own deletion path, so its receipt
  bookkeeping and messages are the sidebar's.

## Cost

One read transaction per page and no writes. Approvals use
`idx_approvals_status`, consents the partial `idx_delegations_consent_pending`,
and each automation's latest-block lookup is one seek on the partial
`idx_audit_remote_egress_blocked(actor_id, created_at)`, shared with the
automation library's read. Tests pin the query plans and keep a page under
100 ms at 10,000 terminal runs, 2,000 completed deletions, 200 stale consents
and 100 pending obligations, and with 1,000 automations beside 100,000 blocks
of another.
