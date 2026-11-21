# Needs-you queue

The Needs-you queue (UX-002) is one orchestrator-computed list of everything
that waits on the owner, across conversations. UX-002a ships the projection
and the API; UX-002b is the app. The design is section 8 of the
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

## Cost

One read transaction per page and no writes. Approvals use
`idx_approvals_status`, consents the partial `idx_delegations_consent_pending`,
and each automation's latest-block lookup is one seek on the partial
`idx_audit_remote_egress_blocked(actor_id, created_at)`, shared with the
automation library's read. Tests pin the query plans and keep a page under
100 ms at 10,000 terminal runs, 2,000 completed deletions, 200 stale consents
and 100 pending obligations, and with 1,000 automations beside 100,000 blocks
of another.
