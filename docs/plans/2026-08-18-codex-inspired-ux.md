# Codex-Inspired UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Borrow the *information architecture* of the OpenAI Codex desktop app — a permission state you can see, settings organised as searchable groups of explained cards, honest empty states, an onboarding checklist made of real facts, and a sidebar you can actually read — and translate each into something TuringAgent can honestly claim. Plus the thing that blocks all of it: **the client has no responsive layout at all**, so none of this survives a phone.

**Architecture:** Almost entirely client-side. The one genuinely valuable thing the client does not surface — the tool policy set — is already on the wire (`ListTools`, `sessions.proto:103`), so surfacing it costs **no proto change**. Two backend items are called out separately: session auto-naming (Go only, no proto) and *editing* a tool policy (proto change + pinned regeneration; last, and optional).

**Tech Stack:** Flutter (`turing-client/turing_app`), Go 1.23 `orchestrator-go` for Tasks 6 and 9.

**Baseline:** written against `feature/claude/client-redesign` (PR #43, tip `bdbfb42`). Every line reference below is on that branch, and **that is not the same as `main`**: `main` at `be2c8c9` is the squash-merge of only the *first* of that branch's three commits ("make the app one conversation surface"). The two still unmerged — markdown assistant rendering, and moving the provider picker into Settings — are the source of `chat_screen.dart`'s `_EmptyConversation` and `_assistantMarkdown`, and of `auth_storage.dart`'s `readModelProvider`/`saveModelProvider`. Those three are cited below, so **land or rebase onto #43 before starting**; against `main` alone their line numbers do not resolve.

---

## Correction to the brief (read this first)

The task description of PR #43 does not match the branch. Verified against `git diff origin/main...origin/feature/claude/client-redesign`, the branch touches 18 files and:

- **There is no `lib/ui/shell/shell_destination.dart` and no `lib/features/workspace/workspace_pages.dart`.** Neither path exists on the branch. `find turing-client/turing_app/lib -type f` returns no `workspace/` directory at all.
- **There are no Skills / Integrations / MCPs / Automations / Agents destinations.** The branch did the *opposite*: it deleted the old five-item rail because three destinations were placeholders for features `docs/VISION.md` refuses, and its own test now asserts they are gone (`test/ui/responsive_shell_backend_test.dart:37-41`).
- **There is no backend auto-naming.** `_newConversation` hard-codes `createSession(title: 'New chat')` (`responsive_shell.dart:83`), and no `UPDATE sessions` statement exists anywhere in `orchestrator-go/internal/repository`. Every session in the sidebar is literally titled "New chat" forever.
- **There is no compact/drawer layout below 840px.** `grep -rn "MediaQuery\|LayoutBuilder\|Drawer\|840" turing-client/turing_app/lib` (excluding `generated/`) returns **zero matches**. `ResponsiveShell` is a fixed `Row` with a hard-coded 268px sidebar (`responsive_shell.dart:49, 185-202`). The name is aspirational.

This plan is written against what is actually there. The consequence is that **mobile is not a refinement of an existing compact mode — it does not exist yet**, which is why Task 1 comes first.

## Current state, verified

**Shell.** `ResponsiveShell` (`ui/shell/responsive_shell.dart:26`) renders `Row[ _Sidebar(268), VerticalDivider, Expanded(chat) ]` (`:183-203`). The sidebar is: product row with a search icon (`:261-285`), a `New chat` button (`:286-301`), a flat `ListView` of every session (`:330-343`), and a footer with a theme toggle and a settings icon (`:436-476`). Search and Settings both `Navigator.push` a full route (`:150-161`, `:163-178`).

**Sessions are unreadable.** Titles are set once at creation and never updated, so the list is N rows reading "New chat". Worse, `ListSessions` orders by `updated_at DESC` (`repository/sessions.go:59`) but nothing ever writes `updated_at` after the `INSERT` (`sessions.go:51`) — so the order is really *creation* order, and a conversation you returned to yesterday sinks below one you opened once and abandoned.

**Permissions are invisible.** The substance is all there and all real:
- Policies are `safe` / `approval_required` / `disabled` (`service/tools/policy.go:5-11`), seeded per tool (`service/tools/defaults.go:16-26`), with **unknown tools defaulting to `approval_required`** (`defaults.go:37`) and `files.delete`/`files.move` permanently disabled (`defaults.go:40-42`).
- `ListTools` returns every enabled tool with its policy (`sessions.proto:82-92, 103`; `service/sessions/service.go:154-181`).
- `GetConfig` returns per-provider enablement + default model, `approvals_enabled`, `files_mcp_enabled` (`sessions.proto:68-74`; `service/sessions/service.go:137-148`).

And **none of it reaches the user.** `TuringApi` (`networking/api_client.dart:8-55`) has no `listTools` and no `listAgents`. `getConfig` is declared (`:9`) and implemented (`grpc_client.dart:95-113`) but has **no caller in `lib/`** — every hit outside `generated/` is a test fake. The only time a user ever learns a tool needs permission is when an `ApprovalCard` (`features/approvals/approval_card.dart`) appears mid-run, reading `Approval requested: <toolName>` with no statement of what the standing policy is.

**Settings.** `SettingsScreen` (`features/settings/settings_screen.dart`) is a `ListView` of three raw controls — backend URL, API key, model provider — inside a `Scaffold` titled `Project Turing Settings` (`:45-99`). No groups, no search, no explanatory sentences except one on the provider (`:75-79`). It doubles as the first-run screen (`app.dart:79-84`).

**Composer.** A bare `TextField` + send `IconButton` in a `Row` (`chat_screen.dart:1087-1116`), hint `Ask Turing...` (`:1099`). No context row, no chips. The model provider is passed in from Settings (`chat_screen.dart:97`) and never displayed.

**Empty states are already honest**, and that is the one Codex pattern this repo got right first: an empty sidebar says "No conversations yet." (`responsive_shell.dart:315-328`), an empty pane says "Ask Turing anything" (`:508-559`), a selected-but-empty conversation says "What can I help with?" (`chat_screen.dart:1373-1415`).

## What is borrowed, what is refused

Every Codex pattern from the two screenshots, with its TuringAgent translation. **Refusals are load-bearing — do not quietly implement one later.**

| Codex | Verdict | TuringAgent equivalent |
|---|---|---|
| `⚠ Full access` composer chip | **Borrow the chip, refuse the mode** | A chip stating the *standing* policy summary, derived from `ListTools`. "Full access" itself is refused: VISION's invariant *"Every mutation is approved, argument-bound, and single-use"* has no opt-out, so a mode that skips approvals cannot exist. Task 4. |
| `Permissions` settings group | **Borrow** | Per-tool policy, each row a title + plain-language sentence + control. The three toggles become *per tool*, and the riskiest choice on offer is "ask every time", not "never ask". Tasks 5, 9. |
| Settings as full-screen searchable grouped cards | **Borrow** | Groups: General, Model, Permissions, Tools & servers, Data. `← Back to app`, `Search settings…`. Task 5. |
| Empty state renders with "No chats" rather than vanishing | **Borrow and extend** | Already true for sessions. Extend to settings groups: a group empty because `files_mcp_enabled` is false must say so, not disappear. Task 5, Step 4. |
| Onboarding ring, `Getting started 1 of 6` | **Borrow** | Six items each checkable from data already on the wire — no invented state. Task 7. |
| Four suggestion cards | **Borrow, gated on reality** | Cards must be gated on `ListTools`/`GetConfig` so a card never offers a tool that is disabled or a server that is not configured. Task 8. |
| Context chip row above the composer | **Borrow one chip of three** | Keep the privacy-bearing one: provider + model (`Ollama · qwen2.5:7b`, from `GetConfig`). Refuse the project chip and the `main` branch chip. Task 4. |
| Session titles that read as content | **Borrow** | Auto-name from the first user message, deterministically. Task 6. |
| `Projects` folder tree | **Refuse the tree, borrow the grouping** | `sessions` has no parent column and no user has asked for folders; inventing a schema for it is unpaid work. Group by recency instead, which `updated_at` already supports once Task 6 makes it true. Task 3. |
| `Recents` section | **Borrow** | Falls out of Task 3's grouping. |
| Workspace switcher (`Codex ⌄`) | **Refuse** | One user, one machine, one backend key (VISION open question 2 explicitly assumes single-user). There is no second workspace to switch to. The backend URL is a settings field, not an identity. |
| Bell / notifications | **Refuse** | There is no notification store. Run notices are in-transcript and are *suppressed on reopen* by the replay watermark (`chat_screen.dart:737-742`) — a bell would either be permanently empty or fabricate a history that does not exist. |
| `Pull requests`, `Sites`, `Worktrees`, `Git`, `Environments`, `Hooks` | **Refuse** | TuringAgent is not coding-focused. No equivalent exists and none should be invented. |
| `Scheduled` | **Refuse** | Background runs on a timer are unbudgeted: they need their own answer to "how does this not phone home?" and would be the first thing to run without a user present. Not in VISION's deferral list at all. |
| `Plugins` in primary nav | **Refuse the nav slot, borrow the content** | MCP servers are real, but there is no add/remove/configure RPC, so a top-level destination would be a read-only list pretending to be an action. It becomes the **Tools & servers** settings group. Task 5. |
| `Voice`, microphone, waveform submit | **Refuse** | VISION deferral 6 — voice is the furthest-out item and needs its own privacy answer first. |
| `Pets`, `Usage & billing`, `Account ↗`, `Import`, `Browser`, `Computer use` | **Refuse** | No account, no billing, no remote surface. `Computer use` is barred by the sandbox invariant. |
| `5.6 Sol High` (model + reasoning effort) | **Borrow half** | Model name is real (`GetConfig.providers[].default_model`). There is no reasoning-effort concept; refuse that half rather than inventing a dial. |
| `Speed` dropdown ("across chats, subagents, and compaction") | **Refuse** | There are no subagents (one agent, VISION "What is true today") and no compaction. |

## Design decisions (locked)

1. **A chip that states a permission must be derived from the policy the backend will actually enforce**, never from a client-side preference. If `ListTools` fails, the chip says it could not read the policy — it never falls back to a cheerful default. Showing a *wrong* permission state is rule 1 in VISION's "How we decide what is next": wrong beats missing.
2. **No permission mode weakens approvals.** The only policy transitions Task 9 may offer are `approval_required ⇄ disabled`. Promoting a tool to `safe` from the UI, or any "don't ask again", is out of scope permanently, not deferred.
3. **Every settings row is title + explanatory sentence + right-aligned control**, and the sentence says what the setting *does to your data*, not what it is called. This is the single most transferable thing in Screenshot B.
4. **Nothing renders that cannot be verified from a response.** Onboarding items, suggestion cards and server lists are all derived from `GetConfig`/`ListTools`/`listSessions`. No hard-coded "6 steps" that stays at 6 when a step is unreachable.
5. **Mobile is a first-class layout, not a squeeze.** Below the breakpoint the sidebar becomes a `Drawer` and the conversation is the whole screen — it does not become a 120pt column.
6. **Titles are derived, not generated.** Auto-naming truncates the first user message. It does **not** ask a model: that spends a run on naming, adds a failure mode to every first message, and VISION commitment 3 targets 7B-class models where the naming call is as likely to fail as the answer.

## Known limits (state these; do not claim otherwise)

- **The permission chip is a summary, and summaries lose information.** Policies are per tool; one chip cannot say "files.create asks, system.time does not". The chip must therefore be a *link into* the Permissions group, and its copy must be true for the whole set (see Task 4, Step 3) rather than describing the strictest or the loosest tool.
- **`ListTools` returns only `enabled = 1` tools** (`repository/tools.go:54-60`). A tool that has been discovered and later disappeared keeps a row but is excluded. So "Tools & servers" lists what is *live now*, and must say so — it is not a history.
- **`files.delete` and `files.move` are permanently disabled in code** (`defaults.go:40-42`), not in the database, so they will never appear in `ListTools` at all. The Permissions group must not imply the list is exhaustive of everything the agent could ever do.
- **Recency grouping is only as good as `updated_at`.** Until Task 6 writes it, grouping by it groups by creation. Task 3 must land after Task 6 or it will silently mislabel.
- **Settings search matches the strings the client itself ships.** It cannot find a tool by name until `ListTools` has resolved; while that future is pending the Permissions group must show a loading state, not "no results".
- **Onboarding progress does not persist and should not.** It is recomputed from live responses every time. A checklist that remembers being complete while the thing it checked has since broken is exactly the false-state failure VISION rule 1 names.

---

## Task 1: Give the shell a real breakpoint

**Nothing else in this plan is safe to build until this exists**, because every subsequent task adds chrome to a shell that currently cannot fit on a phone at all.

**Files:** `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`; `test/ui/responsive_shell_backend_test.dart`.

- [ ] **Step 1: Write the failing tests.** The existing suite already sizes the viewport (`responsive_shell_backend_test.dart:18-21`), so follow that convention.
  - at 1200×800 the sidebar is visible inline and there is no menu button;
  - at 390×844 (iPhone-class) the sidebar is **not** in the tree, a menu affordance is, and tapping it opens a `Drawer` containing the session list;
  - at 390×844, selecting a session from the drawer closes the drawer and shows that conversation full-width;
  - the breakpoint constant is asserted at both sides of the boundary (839 → compact, 841 → expanded), so an off-by-one cannot ship silently.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Wrap `build`'s `Row` (`responsive_shell.dart:183-203`) in a `LayoutBuilder`. Extract the existing `_Sidebar` unchanged and host it either inline or as `Scaffold.drawer`. Keep `_sidebarWidth` (`:49`) for the expanded case and add `static const double _compactBreakpoint = 840;`.
  - The `Scaffold` must move *inside* the layout branch or gain a `key`, so switching branches does not drop `ScaffoldMessenger` state mid-toast (`_toast`, `:146-148`).
  - `_conversation` is unchanged; the `ValueKey(sessionId)` (`:218`) must survive the branch switch or rotating a phone rebuilds the transcript.
- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — set the breakpoint to `0` and confirm the compact tests fail.
- [ ] **Step 5: Commit.**

**Desktop:** unchanged — identical to today. **Mobile:** the conversation is the whole screen; the sidebar is a drawer behind a leading menu button.

## Task 2: Settings as a surface, not a dialog

Splits from Task 5 so the container lands before the content and the diffs stay readable.

**Files:** `lib/features/settings/settings_screen.dart`, `lib/ui/shell/responsive_shell.dart`; `test/features/settings/settings_screen_test.dart` (new).

- [ ] **Step 1: Write the failing tests** — settings shows a `← Back to app` affordance; on a 1200-wide viewport the nav rail and the content pane are both visible; on a 390-wide viewport only the nav list is visible until a group is chosen, and choosing one shows that group with a back affordance to the nav.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Keep `_openSettings`'s `Navigator.push` (`responsive_shell.dart:163-178`) — it is already a full route, which is what Screenshot B is. Replace the `AppBar` title (`settings_screen.dart:97`) with the back link, and split the body into a left nav + right scrollable pane.
  - Preserve `embedded` (`settings_screen.dart:16, 92-94`) and the first-run path (`app.dart:79-84`): on first run the user has no backend, so **only the General group may render** — every other group depends on an RPC that cannot succeed yet. A test must pin this.
- [ ] **Step 4: Run, confirm pass.**
- [ ] **Step 5: Commit.**

**Desktop:** two panes, nav rail ~230px. **Mobile:** a two-level push — the nav list, then a group. Do not shrink the two-pane layout; at 390pt a 230px rail leaves nothing.

## Task 3: A sidebar you can read

**Depends on Task 6** — do not land this first.

**Files:** `lib/ui/shell/responsive_shell.dart`; `test/ui/responsive_shell_backend_test.dart`.

- [ ] **Step 1: Write the failing tests** — sessions are grouped under `Today` / `Previous 7 days` / `Older` headers by `Session.updatedAt`; a group with no sessions is **not** rendered (unlike Codex's empty *projects*, an empty date bucket carries no information — the honesty rule is about things the user created, not derived buckets); the all-empty case still shows the existing "No conversations yet." (`responsive_shell.dart:315-328`).
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement** in the `ListView.builder` at `:330-343`. Group in a pure function so it is unit-testable without a widget.
- [ ] **Step 4: Run, confirm pass; prove it discriminates.**
- [ ] **Step 5: Commit.**

**Desktop and mobile:** identical — the drawer hosts the same widget.

## Task 4: Surface the permission state where the user acts

The single highest-value borrow: TuringAgent has the substance and shows none of it.

**Files:** `lib/networking/api_client.dart`, `lib/networking/grpc_client.dart`, `lib/models/` (a `ToolDescriptor` model), `lib/features/chat/chat_screen.dart`, `lib/ui/shell/responsive_shell.dart`; tests in `test/networking/grpc_client_test.dart` and `test/features/chat_screen_test.dart`.

**No proto change.** `ListTools` already exists (`sessions.proto:103`) and is already implemented (`service/sessions/service.go:154-168`). This is Dart-side wiring only.

- [ ] **Step 1: Write the failing tests.**
  - `TuringApi.listTools` maps `ToolDescriptor` including all three policies and `TOOL_POLICY_UNSPECIFIED` (which `toProtoToolPolicy`'s `default` branch really can return, `service.go:178`);
  - with tools whose policies include at least one `approval_required`, the composer renders a chip stating the agent asks before it acts, and it is tappable through to the Permissions group;
  - with **every** tool `safe`, the chip states that instead — the copy is derived, not fixed;
  - when `listTools` **rejects**, the chip says the permission state could not be read, and specifically does **not** render the reassuring copy. This is the discriminating test.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Add `Future<List<ToolDescriptor>> listTools()` to `TuringApi` (`api_client.dart:8-55`) and to `TuringGrpcApi` beside `getConfig` (`grpc_client.dart:95`). Fetch once in the shell and pass down, so a chip does not fire an RPC per rebuild.
  - Chip copy is a pure function of the policy multiset. Suggested: any `approval_required` → `Asks before it acts`; all `safe` → `Read-only tools`; unreadable → `Permissions unavailable`. Pin the exact strings in tests.
  - Colour it with `AppColors.warning` (`constants/app_colors.dart:39`) only in the unreadable case. The default state is not a warning — the default is *correct*, and colouring it amber would train the user to ignore amber.
- [ ] **Step 4:** add the second chip: provider + model from `getConfig` (`grpc_client.dart:95-113`), e.g. `Ollama · qwen2.5:7b`. Test that an OpenAI-compatible provider renders differently — this chip's whole justification is that it makes "this conversation is leaving the machine" visible, which is commitment #1.
- [ ] **Step 5: Run, confirm pass. Commit.**

**Desktop:** a chip row above the input, as in Screenshot A. **Mobile:** the two chips do not fit beside a 390pt composer. Put them in a **single scrollable row above the input at reduced size**, and never wrap to two lines — the composer must not grow taller as chips are added. Test this at 390pt explicitly.

## Task 5: The Permissions and Tools groups

**Files:** `lib/features/settings/settings_screen.dart` (+ new files under `lib/features/settings/`); `test/features/settings/`.

- [ ] **Step 1: Write the failing tests.**
  - the Permissions group lists every tool from `listTools`, grouped by `serverName`, each row with tool name, a plain sentence for its policy, and a control;
  - the sentence for `approval_required` states that a signed, single-use approval is required for that exact call — the argument-binding is the property worth stating and it is real (`CLAUDE.md`, mcp-files approval flow);
  - the group states plainly that it lists only tools that are live now, and that `files.delete`/`files.move` are disabled in the build (`defaults.go:40-42`);
  - when `getConfig().filesMcpEnabled` is false, the files server renders with an explicit "not configured" placeholder rather than being omitted — the Codex "No chats" borrow;
  - the Tools & servers group lists servers from the same response, with a count, and says nothing about adding one (there is no RPC);
  - settings search filters group titles, row titles **and** tool names, and an empty result says so.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Controls in this task are **read-only** — status text, not switches. Task 9 makes them editable, and shipping a dead switch is worse than shipping a label.
- [ ] **Step 4: Move the existing three settings into groups** — backend URL + API key into General, provider into Model (keeping its existing privacy sentence, `settings_screen.dart:75-79`), theme out of the sidebar footer (`responsive_shell.dart:448-463`) into an Appearance row. Keep the footer toggle too; it is a one-tap thing people use, and Codex keeps `Appearance` in settings without removing the quick control.
- [ ] **Step 5: Run, confirm pass. Commit.**

**Desktop:** cards in a scrollable right pane. **Mobile:** one group per screen, full width, cards edge-to-edge with no side margin; the right-aligned control drops **below** the sentence when the row is under ~340pt wide, rather than squeezing the text to two words. Pin that with a 390pt test.

## Task 6 (backend): Name a session from its first message

**This is the expensive kind — Go, not Dart — but there is no proto change.** `Session.title` already exists (`sessions.proto:12`) and `ListSessions` already returns it (`service/sessions/service.go:183-192`).

**Files:** `turing-backend/orchestrator-go/internal/repository/jobs.go`; tests alongside the existing enqueue tests.

- [ ] **Step 1: Write the failing tests.**
  - enqueuing the **first** user message into a session whose title is empty sets the title from that message;
  - enqueuing into a session whose title was set explicitly at creation does **not** overwrite it;
  - the **second** message never changes the title;
  - a long message is truncated at a fixed length on a word boundary, with the exact expected string asserted;
  - a message of only whitespace/newlines leaves the title unset rather than writing a blank one;
  - `updated_at` is advanced on every enqueue, not only the first.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement** inside `EnqueueUserMessage`'s existing transaction (`repository/jobs.go:202-242`). `next` is already computed at `:215`, so `next == 1` identifies the first user message with no extra query. Add one `UPDATE sessions SET title = COALESCE(NULLIF(title, ''), ?), updated_at = ? WHERE id = ?` — the `COALESCE`/`NULLIF` makes "don't overwrite" a database property rather than a Go branch that can drift.
  - **The client must stop poisoning this.** `responsive_shell.dart:83` sends `title: 'New chat'`, which is a non-empty title and would defeat the guard. Change it to `createSession()` with no title and let the backend fill it. `_SessionTile` already falls back to `'Untitled chat'` for an empty title (`:380-382`), so the intermediate state is already handled — add a client test pinning that the create call sends no title.
- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — drop the `NULLIF` and confirm the "does not overwrite" test fails.
- [ ] **Step 5: Commit.**

**Desktop and mobile:** identical. This is the change that makes the sidebar legible on a phone, where you can see about eight rows.

## Task 7: Getting started, from real facts

**Files:** new `lib/features/onboarding/`; `lib/ui/shell/responsive_shell.dart`; tests.

- [ ] **Step 1: Write the failing tests.** Six items, each derived:
  1. backend URL and API key saved — `authStorage` (`networking/auth_storage.dart:6-8`);
  2. the backend answers — `getConfig` resolves;
  3. a model provider is enabled — `getConfig().providers[].enabled` (`grpc_client.dart:98-107`);
  4. tools were discovered — `listTools` is non-empty;
  5. approvals are armed — `getConfig().approvalsEnabled` (`sessions.proto:72`, true iff `ApprovalJWTSecret != ""`, `service.go:145`);
  6. a first conversation exists — `listSessions` is non-empty.

  Tests: the ring reads `2 of 6` when exactly two hold; it reads `6 of 6` and the block **disappears** when all hold; an item whose RPC failed reads as *unknown*, not as done; the count never exceeds the number of items actually evaluated.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement** as a pure function from `(config, tools, sessions, storage)` to a list of `(label, done)`, plus a small widget. The purity is the point — the whole failure mode here is a checklist that drifts from what it claims to check.
- [ ] **Step 4: Run, confirm pass. Commit.**

**Desktop:** sidebar footer above the theme/settings row, as in Screenshot A. **Mobile:** the drawer footer is scarce and the drawer is closed most of the time, so it would never be seen. Render it instead **inside the empty conversation pane** (`chat_screen.dart:1373-1415`) below the "What can I help with?" copy — the one screen a new user is guaranteed to be looking at.

## Task 8: Suggestion cards that cannot lie

**Files:** `lib/ui/shell/responsive_shell.dart` (`_EmptyState`, `:508-559`), `lib/features/chat/chat_screen.dart` (`_EmptyConversation`, `:1373-1415`); tests.

Codex's four cards are coding-shaped and every one of them is refused. The TuringAgent set is derived from what the registry actually reports:

| Card | Rendered only when |
|---|---|
| "Find something in your sandbox files" | `files.search` or `files.list` is in `listTools` |
| "Ask about an earlier conversation" | `listSessions` returns more than one session — recall has nothing to draw on otherwise |
| "Check on this machine" | `system.info` or `system.health` is in `listTools` |
| "Write or summarise something" | always — needs no tool |

- [ ] **Step 1: Write the failing tests** — a card whose tool is absent from `listTools` does **not** render; with an empty registry only the last card renders; tapping a card fills the composer with its prompt rather than sending it (the user must be able to edit before committing to a run).
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Reuse `AppColors`' semantic colours (`constants/app_colors.dart:37-40`) for the icons; do not add new palette entries for decoration — the palette doc is explicit that colour carries meaning (`app_colors.dart:8-11`).
- [ ] **Step 4: Run, confirm pass. Commit.**

**Desktop:** a single row of up to four cards, as in Screenshot A. **Mobile:** a 2×2 grid at ≥360pt and a single column below that. Four cards in a row at 390pt is ~90pt each and the two-line labels become unreadable — do not do it.

## Task 9 (backend + proto): Make a policy editable — LAST, and optional

**This is the only proto change in the plan.** Everything above ships without it. Do not start it until Tasks 4 and 5 are merged and the read-only surface has been used.

**Files:** `proto/turing/v1/sessions.proto`, regenerated `gen/` + `turing-client/turing_app/lib/generated/`, `orchestrator-go/internal/repository/tools.go`, `internal/service/sessions/service.go`, client.

- [ ] **Step 1:** Add `rpc SetToolPolicy(SetToolPolicyRequest) returns (SetToolPolicyResponse);` to `SessionService` (`sessions.proto:94-104`). Request carries `server_name`, `tool_name`, `ToolPolicy policy`.
- [ ] **Step 2: Regenerate with the pinned toolchain and commit the output** — protoc 34.1, protoc-gen-go 1.36.11, protoc-gen-go-grpc 1.6.2, Dart `protoc_plugin` 22.5.0 via `tools/proto/generate.sh`. `tools/proto/check.sh` compares bytes in CI.
- [ ] **Step 3: Write the failing tests before implementing the handler.**
  - `approval_required → disabled` succeeds and `GetToolPolicy` (`repository/tools.go:80-95`) reflects it;
  - `disabled → approval_required` succeeds;
  - **`→ safe` is refused with `InvalidArgument`** — decision 2. This is the test that stops a future contributor quietly widening the API into an approvals bypass;
  - `→ TOOL_POLICY_UNSPECIFIED` is refused;
  - an unknown server/tool pair is `NotFound`, not a silent insert;
  - a change writes an audit row, so the record shows who loosened what and when.
- [ ] **Step 4:** implement; add the write path to `repository/tools.go`. Note `UpsertTools` deliberately preserves existing policies across rediscovery (`repository/tools.go:19-20`), so a user's choice survives a restart for free — assert that with a test rather than assuming it.
- [ ] **Step 5:** client: turn Task 5's read-only rows into controls; confirm before disabling a tool, and say what stops working.
- [ ] **Step 6: Commit.**

**Desktop and mobile:** the control is a two-state segmented control, not a switch. A switch implies an off/on axis where "on" would read as "more permission"; the two states here are both restrictive.

## Task 10: See it in the real client

- [ ] **Step 1:** `cd turing-backend && ./scripts/init.sh && ./scripts/dev.sh` with Ollama running; `flutter run -d macos`.
- [ ] **Step 2:** Verify the permission chip against a real registry, then stop the mcp-files container and confirm the Permissions group says the server is unavailable rather than silently shortening.
- [ ] **Step 3:** Send a first message in a new session and confirm the sidebar title changes from "Untitled chat" to the message.
- [ ] **Step 4:** `flutter run -d <ios simulator>` (or resize the macOS window under 840pt) and walk the drawer, the chip row, the settings two-level push, and the suggestion grid. Screenshot both widths for the PR.
- [ ] **Step 5:** Tear the stack down.

---

## Deliberately not doing

- **Anything in the refusal column above**, in particular: a `Full access` mode, notifications, scheduling, voice, projects-as-folders, and every coding destination. VISION refuses the mutation bypass (invariant: *every mutation is approved, argument-bound, and single-use*), the sandbox escape (`Computer use`), and defers voice (deferral 6).
- **A model-generated session title.** Decision 6.
- **Reconnecting the event stream**, even though the drop notice is the most visible broken thing in the client (`chat_screen.dart:322-323`, and `TuringEventSource` never reconnects). It is a real gap and it is not a UX-borrowing task; it deserves its own plan.
- **Rendering historical tool cards and run notices on reopen.** The replay watermark suppresses them (`chat_screen.dart:737-742`) and VISION lists this as a known gap. Making the transcript whole is a separate, larger change.
- **Renaming `ResponsiveShell`.** After Task 1 the name is finally accurate.
- **Any new palette or type scale.** PR #43 just built one (`constants/app_colors.dart`, `app.dart:112-223`); this plan consumes it.

## Open questions for the owner

1. **Should the permission chip be tappable through to Settings, or should Permissions get its own sheet from the composer?** The chip-to-settings jump leaves the conversation, which is exactly the navigation cost PR #43 removed. A sheet is more work but keeps the conversation on screen.
2. **Is Task 9 wanted at all?** The policy set is currently owned entirely by the orchestrator, which is a defensible position — the user cannot loosen what they cannot reach. Making it editable is the only step in this plan that *adds* a way to reduce safety, even in the narrow `approval_required → disabled` direction. It is last and optional for that reason.
3. **iOS/Android are configured directories but is either actually a target?** `turing-client/turing_app/ios` and `android` exist, but VISION says "Clients: **One** (Flutter, macOS-focused)". The mobile work in Tasks 1–8 is worth doing regardless — it is also what makes a narrow desktop window work — but if a phone is genuinely a target then the bearer-key trust boundary (VISION open question 1) becomes urgent, because the key would then live on a device that leaves the house.
4. **Where should this plan live?** `docs/superpowers/plans/` holds the 15 existing plans, and VISION calls that directory an **archive** rather than a backlog. This one is filed under `docs/plans/` per instruction, creating a second location. Worth deciding once.

---

## Verification

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter test )
tools/proto/check.sh
golangci-lint cache clean
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

`tools/proto/check.sh` only matters for Task 9; every other task leaves `proto/` untouched, and a diff there from Tasks 1–8 means something went wrong.

Plus CLAUDE.md's required pre-push subagent review, covering unit-test coverage explicitly.

## Self-review checklist

- Every borrowed pattern names its TuringAgent content; every refused one names why ✓
- The one pattern with the most substance behind it (permissions) is the one with the most weight, and it needs **no proto change** ✓
- `Full access` is refused on an invariant, not deferred, and Task 9 has a test that enforces the refusal ✓
- Nothing renders that is not derived from a real response; failure renders as *unknown*, never as reassurance ✓
- Every UI task states desktop **and** mobile behaviour separately, and mobile is never "the desktop layout, smaller" ✓
- Task 1 comes first because there is currently no responsive layout at all — verified, not assumed ✓
- Task 3 is explicitly ordered after Task 6, because `updated_at` is not written today ✓
- The one proto change is isolated, last, and optional ✓
- Empty states extend the honesty the repo already has rather than replacing it ✓
- The brief's description of PR #43 is corrected up front with the commands that disprove it ✓
