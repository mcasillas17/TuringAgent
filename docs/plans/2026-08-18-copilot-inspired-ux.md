# Copilot-Inspired UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gap between what TuringAgent *is* — a local assistant that routes to a model you chose, runs real tools, and remembers earlier conversations — and what its client *shows*, which is a list of rows all reading "New chat" beside a text field. The reference is the GitHub Copilot desktop app's **information architecture**, not its features: a landing surface you can type into, a settings modal you can search, a composer that says what it is talking to, a sidebar that groups rather than lists, and a footer that tells you the backend is alive.

**Hard constraint:** TuringAgent is **not** coding-focused. Every borrowed pattern below is written as *Copilot slot → TuringAgent content*, and the ones with no honest content are refused by name in "What we are not borrowing". Nothing here plans a repo, a branch, a pull request, a worktree, or a diffstat.

**Tech Stack:** Flutter (`turing-client/turing_app`) for Tasks 1–6, Go 1.23 (`orchestrator-go`) for Task 2 and the deferred Task 8. **No proto change is required for Tasks 1–7.**

---

## Baseline correction — read this before anything else

This plan was commissioned against a description of branch `feature/claude/client-redesign` (PR #43) that does not match the code. Verified at `bdbfb42` (branch tip) and `be2c8c9` (`origin/main`):

- **PR #43 is already squash-merged into `main`** as `be2c8c9`. The branch carries **two further unmerged commits** — `fb2434f` (markdown answers) and `bdbfb42` (provider picker moved into Settings). The branch tip is therefore `main` + those two commits, and it is the baseline this plan targets.
- **There is no `lib/ui/shell/shell_destination.dart` and no `lib/features/workspace/workspace_pages.dart`.** No file in `lib/` outside `lib/generated/` mentions Skills, Integrations, MCPs, Automations or Agents as destinations. The one place those words appear is `test/ui/responsive_shell_backend_test.dart:39-42`, which asserts `Devices`/`Stats`/`Integrations` are **absent** — the redesign deliberately *deleted* those destinations because they were placeholders for things `docs/VISION.md` refuses.
- **There is no compact/drawer layout, and no 840px breakpoint.** `grep -rn "840\|LayoutBuilder\|MediaQuery\|Drawer\|breakpoint" lib` (excluding `lib/generated/`) returns nothing. `ResponsiveShell.build` is an unconditional `Row` with a hard-coded 268px sidebar (`responsive_shell.dart:49,185-201`). The class name is aspirational. **This is Task 1, not a given.**
- **The backend does not auto-name a session.** `CreateSession` stores whatever title it is handed (`repository/sessions.go:45-56`); the client always hands it the literal string `'New chat'` (`responsive_shell.dart:83`); there is no rename or update RPC in `proto/turing/v1/sessions.proto:94-104`, and `grep -rn "UPDATE sessions" turing-backend/orchestrator-go/internal` returns nothing at all. **This is Task 2.**

Do not "re-add" the destinations the redesign removed. Every destination must lead somewhere real — that is the rule `responsive_shell.dart:13-23` was written to enforce, and it is why `Automations`, `Skills` and `Plugins` are refused below rather than stubbed.

---

## Why this, and why now

**The client cannot tell you what it is talking to.** `getConfig()` is implemented end-to-end — orchestrator (`service/sessions/service.go:137-147`) through the Dart stub (`grpc_client.dart:95-115`), returning each provider's `enabled` flag and `defaultModel`. **Nothing in `lib/` calls it.** The only callers are four test fakes. So the backend knows it is running `qwen2.5:7b` on Ollama, and the user is shown a hint that says `Ask Turing...`. Copilot's composer chip (`GPT-5.6 Sol`) is the cheapest correct fix, and here it carries something Copilot's does not need to: **whether this request leaves the machine**, which is commitment #1's whole subject.

**Every conversation is called "New chat".** `responsive_shell.dart:83` is the only creation path and it hard-codes the title. The sidebar (`responsive_shell.dart:330-343`) is a flat `ListView.builder` of those identical rows. Copilot's sidebar groups sessions under a heading you can scan; ours cannot be scanned at all, because there is nothing to distinguish one row from the next. Grouping the list is worthless until the rows have names, which is why Task 2 comes before Task 3.

**The list is ordered by a column nothing writes.** `ListSessions` orders by `updated_at DESC` (`repository/sessions.go:59`), and `updated_at` is set exactly once, at insert (`repository/sessions.go:51`). Reply to a month-old conversation and it stays at the bottom. `idx_sessions_updated` (`0001_initial.sql:19`) indexes a frozen column.

**The composer goes quiet during the part that takes time.** `_sending` is cleared the moment the `sendMessage` RPC resolves (`chat_screen.dart:934-940`), which is when the run is *queued* — not when it finishes. For the whole generation the send button shows a plain send icon (`chat_screen.dart:1103-1112`). Copilot shows a spinner and a stop button for exactly this window. Under commitment #2 ("a run that retries, stalls, or draws on old context says so") a run that is *working* should say so too — the run-visibility notices (#33) fixed the exceptional cases and left the normal one silent.

**Settings is two text fields.** `settings_screen.dart:44-100` is a `ListView` with Backend URL, API key, a provider dropdown and Save. Meanwhile `ListTools` (`service.go:154-168`) returns every discovered tool with its server and policy, `ListAgents` (`service.go:149-152`) returns the agent roster, and `HealthService` (`health.proto`) returns liveness and version — none of which any screen reads. Copilot's two-pane searchable settings modal is the single highest value-per-line borrow available, because the content it needs **already exists on the wire**.

**Nothing invites a first message.** `_EmptyState` (`responsive_shell.dart:508-559`) shows a mark, two sentences and a "New chat" button. You must click, wait for `createSession`, and only then get a text field. Copilot's landing view puts the composer *first* and the session is created by sending. That is one fewer step for the most common action in the app.

---

## What we are borrowing, and what it becomes here

Every row states the TuringAgent content explicitly. Rows with no honest content are in the next section.

| Copilot slot | TuringAgent content | Cost | Task |
|---|---|---|---|
| Landing view: mark + large composer | A composer on the no-conversation surface; sending creates the session | cheap | 5 |
| Placeholder hinting affordances | `Ask Turing anything — it can read and write files in the sandbox, and remembers earlier conversations` | cheap | 5 |
| Three suggestion cards with category chips | Prompts that exercise tools that actually exist: `files.list`/`files.read` (Files), `system.info`/`system.time` (System), cross-session recall (Memory) | cheap | 5 |
| Composer model chip (`GPT-5.6 Sol`) | Provider + `defaultModel` from `getConfig()` — e.g. `Ollama · qwen2.5:7b` | cheap | 6 |
| Composer `Medium · 400K` (effort · context) | **The numbers have no equivalent** — but the slot does: a locality badge, `On this machine` / `Leaves this machine`, driven by the stored provider | cheap | 6 |
| Composer spinner + stop while running | Spinner keyed to the run, not the RPC. **Stop is refused for now** — see below | spinner cheap | 6 |
| Settings as a searchable two-pane modal | Same shape, groups fed by existing RPCs: General, Model providers (`GetConfig`), Tools (`ListTools`), Agents (`ListAgents`), About (`HealthService.Version`) | medium | 4 |
| Title + grey-description rows | Direct borrow, no translation | cheap | 4 |
| App-version card | `HealthService.Version` → version + schema version. **The updater is refused** | cheap | 4 |
| Theme card with preview | A three-way ThemeMode row (System / Light / Dark) with a two-swatch preview from `AppPalette`. **Named theme packs refused** | cheap | 4 |
| `Sessions` header with filter + `+` | `Conversations` header with `+` and a sort/filter affordance | cheap | 3 |
| Project folders (`TuringCare`, `ScoreArc`) | **No projects exist.** Honest equivalent: time buckets — Today / Yesterday / Previous 7 days / Earlier — from `Session.updatedAt` | cheap | 3 |
| Session titles that describe the work | First-message-derived title, written server-side | medium (backend) | 2 |
| Git-branch badge on a session row | **No branches.** Honest equivalent: a state badge — *running*, *waiting on your approval*, *last run failed* | expensive (backend) | 8 (deferred) |
| Titlebar showing the active session name | A slim header above the transcript with the conversation title | cheap | 3 |
| Account footer (avatar, name, gear) | **Single-user by design** (`VISION.md` open question 2). Honest equivalent: a backend status footer — connected / disconnected, version, provider locality | cheap | 7 |
| `Up next` (recent PRs and issues) | **Things waiting on you** — approvals pending a decision, runs that failed unattended | expensive (backend + proto) | 8 (deferred) |
| Right-panel toggle / session sub-step tree | A run's steps *already* render inline as tool cards and run notices (`chat_screen.dart:744-846,664-682`). A sidebar tree would be a second source of truth for the same events | — | refused, see below |

## What we are not borrowing, and why

Each of these is refused on the record so the next plan does not re-litigate it.

- **`Autopilot` mode.** Copilot's mode selector offers a setting that skips confirmation. `docs/VISION.md` invariant: *"Every mutation is approved, argument-bound, and single-use. New mutating capability inherits the existing approval flow; it does not get its own weaker one."* A mode that bypasses approvals voids commitment #1 and #2 simultaneously. **Refused permanently, not deferred.** If a mode selector is ever wanted, its axes must be something other than "ask me less".
- **`Automations` destination.** Scheduled or triggered runs do not exist. Building them means a scheduler owning dispatch, which collides with the invariant *"The orchestrator owns durable state and control flow"* — not fatally, but it is a whole feature, not a UX borrow. Adding the destination without the feature re-creates exactly the placeholder problem PR #43 deleted.
- **`Skills` / `Plugins` / `Model providers` as separate *sidebar* destinations.** Tools and providers are real, but they are **read-only configuration**, not places you work. They belong in the settings modal (Task 4), which is where this plan puts them. A sidebar destination implies you go there to do something.
- **`My work`.** No assigned work, no tasks, no inbox. Nothing to put in it.
- **Composer `+` (attachments).** `files.*` operate on the server-side sandbox (`turing-backend/sandbox/`), and `mcp-files` has no upload path. A `+` that can only browse a directory on another machine is a lie about what the button does. Revisit if a sandbox file browser is ever built.
- **`#` (issues) and `&` (sessions) entity pickers.** `#` has no referent. `&` is closer — referencing an earlier conversation — but recall is *automatic and attributed after the fact* (#33), so a picker would be a second, manual recall mechanism competing with the one that works. `/` for a small command set (`/new`, `/search`, `/settings`) is honest and cheap, but it is a keyboard affordance rather than an IA change; noted as a follow-up, not planned here.
- **Run / editor split-buttons in the titlebar.** There is no code to run and no editor to launch.
- **A stop button.** Wanted, and the right long-term answer — but `CancelRun`/`CancelRunWithEvent` exist only inside the orchestrator (`repository/runs.go`), with **no RPC** on any service in `proto/turing/v1/`. A stop button is a proto change plus a service method plus a decision about what the transcript shows afterwards. It belongs with Task 8, not smuggled into Task 6. Task 6 ships the spinner only, and the spinner must not imply a stop exists.
- **The sidebar session→sub-step tree.** The steps are already rendered, in the transcript, where their ordering relative to the answer carries meaning. Mirroring them into the sidebar means two renderings of the same event stream that can disagree — and `_isHistoricalRunEvent` (`chat_screen.dart:737-743`) already suppresses replayed run events, so the sidebar copy would be empty on reopen while the transcript copy was merely quiet. Refused as designed; a right-hand run-detail panel is a separate proposal that must first answer "what does it show on reopen?".
- **Storage location row (change / copy / reveal in Finder).** Data lives in `turing-backend/data/` **inside a container**, on the orchestrator's filesystem. The client cannot change it, cannot open it, and cannot even read the path without a new RPC that exposes a server path to a client — a small but real information leak for zero user benefit.
- **"Automatically check for updates" and `Check for updates`.** There is no updater, and adding one means background network egress, which the invariant *"no feature may introduce background egress"* forbids by default. The version card (Task 4) shows what you are running; it does not phone anyone.
- **Named theme packs with preview thumbnails (`Fox`).** Light and dark are the only palettes (`app_colors.dart:19-33`), and `AppColors`' own doc argues colour must carry meaning rather than decoration. A theme *row* is worth having; a theme *store* is yak-shaving.
- **`Find in workspace position` dropdown.** No workspace, no such control.
- **`Extend your experience` / `Read documentation` link.** Opening a URL is egress, and there is no doc site. If it ever points at `docs/` on disk it can come back.

---

## The mobile problem, stated once

`turing-client/turing_app` carries `ios/` and `android/` targets and configures launcher icons for both (`pubspec.yaml:93-99`), so mobile is an intended destination. But:

- **The shell has no small-screen behaviour at all.** At an iPhone width of 390 logical pixels, `responsive_shell.dart:185-201` gives 268px to the sidebar and 121px to the conversation. The app is unusable on a phone today.
- **CI never builds or tests a mobile target.** `.github/workflows/ci.yml:193-216` runs `flutter pub get`, `flutter analyze` and `flutter test` — no `flutter build ios`/`apk`. The only platform-specific test is `test/platform/macos_entitlements_test.dart`. Widget tests are the only mobile signal available, so **every task below states its phone layout as a widget test at a phone viewport**, not as a manual check.
- **A phone cannot reach the backend today, and this plan does not change that.** `docker-compose.yml:36` publishes the orchestrator at `${ORCHESTRATOR_PUBLIC_BIND_HOST:-127.0.0.1}:3000`, and `VISION.md` names widening that bind host *"the single easiest way to break commitment #1 by accident"*. So the mobile work here is about **layout correctness on small windows and tablets**, which is testable and safe. Shipping to a phone is a separate decision about the trust boundary — see Open questions.

Convention adopted by this plan, and asserted in Task 1's tests: **compact is `< 840` logical pixels wide**, matching Material 3's medium breakpoint. One constant, one place.

---

## Task 1: Make the shell survive a narrow window

Everything after this depends on there being a compact layout to describe. Ship it alone.

**Files:** `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`; `test/ui/responsive_shell_backend_test.dart`.

- [ ] **Step 1: Write the failing tests.** At a phone viewport (`tester.view.physicalSize = Size(390, 844)`, `devicePixelRatio = 1`):
  - the sidebar is **not** laid out beside the conversation — assert the conversation surface occupies effectively the full width (`tester.getSize(find.byType(ChatScreen))`, or the empty-state finder, has width within a pixel of 390);
  - a menu affordance exists and opening it reveals the conversation list (`find.text('Existing chat')` is absent before the tap and present after);
  - selecting a conversation from the drawer **closes it** and swaps the conversation in place — the drawer must not stay over the thing you just chose;
  - at 1200×800 the existing desktop assertions still pass unchanged (the current tests at `responsive_shell_backend_test.dart:15-61,63-...` already pin this; do not weaken them).

- [ ] **Step 2: Run, confirm failure.** All four phone assertions must fail today; the desktop ones must already pass.

- [ ] **Step 3: Implement.**
  - Add `static const double _compactBreakpoint = 840;` beside `_sidebarWidth` (`responsive_shell.dart:49`).
  - Wrap `build`'s body in a `LayoutBuilder`. Wide: today's `Row`, untouched. Compact: a `Scaffold` whose `drawer` is the **same `_Sidebar` widget** — it is already a `StatelessWidget` taking a `width` (`responsive_shell.dart:228-353`), so pass the drawer's width and change nothing else. Do not fork the sidebar into two widgets; a second copy will drift.
  - `onSelect` must close the drawer when compact. Route it through one callback so the wide layout keeps its current no-op behaviour rather than branching inside `_SessionTile`.
  - The compact `Scaffold` needs an `AppBar` for the drawer handle. Give it the active conversation's title — this is Copilot's titlebar slot, and on a phone it is the only place the title fits. Task 3 gives the wide layout its own header.

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — hard-code the breakpoint to `0` and confirm the phone tests fail.

- [ ] **Step 5:** `flutter analyze` and `flutter test`. **Commit.**

> Deliberately **not** in scope: `SearchScreen` and `SettingsScreen` are already pushed as full-screen routes (`responsive_shell.dart:150-160,163-176`), which is correct on a phone and acceptable on desktop. Task 4 changes the settings one.

## Task 2: Give conversations names that mean something (backend)

**Why here:** grouping and scanning a list of identical rows is pointless. This is the prerequisite for Task 3, and it is the only Go work in the shippable set.

**Files:** `turing-backend/orchestrator-go/internal/repository/jobs.go` (`EnqueueUserMessage`, `:202-258`); tests alongside the existing enqueue tests. **No schema change** — `sessions.title` is already nullable (`db/schema/0001_initial.sql:12-18`). **No proto change** — the title already ships on `Session` (`sessions.proto:10-16`) and `ListSessions` already returns it.

- [ ] **Step 1: Write the failing tests.**
  - enqueuing the **first** user message into a session whose title is `NULL` or `'New chat'` sets the title to a derived summary of that message;
  - enqueuing a **second** message does **not** change the title (the derivation runs once);
  - a session whose title the user set to something else is **never** overwritten (once a rename affordance exists, this is the test that stops it being clobbered — write it now, before it can regress);
  - `updated_at` moves forward on **every** enqueue, first or not;
  - a title is derived from the message text only, is trimmed and length-capped, and collapses newlines — assert the exact output for a long multi-line input so the rule cannot drift silently.

- [ ] **Step 2: Run, confirm failure.** The `updated_at` test is the one that proves the current column is dead.

- [ ] **Step 3: Implement**, inside `EnqueueUserMessage`'s existing transaction — it already computes `next` (`jobs.go:215`), and `next == 1` is exactly "this is the first message". Two statements, both in the tx:
  - `UPDATE sessions SET title = ? WHERE id = ? AND (title IS NULL OR title = '' OR title = 'New chat')` — guarded so it is idempotent and cannot overwrite a user's own title. The `'New chat'` literal is the client's placeholder (`responsive_shell.dart:83`); keep the two in one named Go constant and reference it from the client's own constant in Task 3, so they cannot drift apart.
  - `UPDATE sessions SET updated_at = ? WHERE id = ?`, using the same `createdAt` the transaction already computed (`jobs.go:233`) — do not call `time.Now()` a second time inside a transaction that has gone to lengths to keep its timestamps monotonic (`jobs.go:218-234`).

  **Derive the title mechanically, not with the model.** First ~60 characters of the trimmed user text, newlines collapsed to spaces, cut on a word boundary, ellipsis if truncated. Reasons: an LLM call inside the enqueue transaction would hold a write lock on SQLite for the length of a model round-trip; it would spend the user's local model on chrome; and it would make the title non-deterministic, so no test could assert it. **Do not ask the model to name the chat.**

- [ ] **Step 4: Run, confirm pass; prove it discriminates** — drop the `AND (title IS NULL OR ...)` guard and confirm the "never overwritten" test fails.

- [ ] **Step 5:** `go test -tags sqlite_fts5 ./... -count=1` and `-race`. **Commit.**

**Client side of the same task:** `responsive_shell.dart:83` should create with an **empty** title rather than `'New chat'`, so the guard's `IS NULL` arm is the normal path and the placeholder literal exists in one language rather than two. The sidebar already falls back to `'Untitled chat'` for an empty title (`responsive_shell.dart:380-382`), so the pre-first-message state stays readable. Widget test: a session with an empty title renders `Untitled chat`, and `createSession` is called with no title.

> **Not in scope:** letting the user rename a conversation. That needs an `UpdateSession` RPC and therefore a proto change; the guard above is written so it lands cleanly later. Say so in the PR rather than half-building it.

## Task 3: A conversation list you can scan

**Files:** `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`; `test/ui/responsive_shell_backend_test.dart`.

Copilot groups sessions under project folders. We have no projects, so the honest grouping axis is **time**, which `Session.updatedAt` already carries (`models/session.dart:1-21`) and which Task 2 makes truthful.

- [ ] **Step 1: Write the failing tests.**
  - a fake returning sessions dated today, yesterday, four days ago and two months ago renders four headers — `Today`, `Yesterday`, `Previous 7 days`, `Earlier` — in that order, each above its own rows;
  - a bucket with no sessions renders **no** header (an empty `Yesterday` is noise);
  - the header row above the list reads `Conversations` and carries the `+` new-chat action;
  - **desktop:** the conversation pane has a header showing the active conversation's title, and it changes when you select another;
  - **phone:** the same title is in the `AppBar` from Task 1 and there is **no** second header — assert the title is found exactly once at 390px, and exactly once at 1200px. This is the assertion that catches the desktop design being dropped onto a phone.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Replace the flat `ListView.builder` (`responsive_shell.dart:330-343`) with a bucketed build over the same list — sessions arrive already ordered by `updated_at DESC` (`repository/sessions.go:59`), so bucketing is a single forward pass, not a sort. Keep `_SessionTile` exactly as it is; the hover-reveal delete (`responsive_shell.dart:418-425`) is deliberate and its test still applies.
  - Move the "New chat" `FilledButton` (`responsive_shell.dart:288-300`) into the section header as a `+` icon action, per Copilot. **Keep a full-width button in the empty state** — with no conversations, a `+` in a header nobody is looking at is not an invitation.
  - Bucket boundaries are computed from a `DateTime.now()` that must be injectable, or the tests are date-dependent and will fail one day in January. Take an optional `DateTime Function()` on the shell defaulting to `DateTime.now`.

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — return all sessions with the same timestamp and confirm the multi-header test fails.

- [ ] **Step 5:** `flutter analyze`, `flutter test`. **Commit.**

> **Deliberately not doing:** the filter/sort glyph beside Copilot's `Sessions` header. With one grouping axis there is nothing to switch to, and a control with one option is worse than none. It comes back when there is a second axis (see Task 8's state badges).

## Task 4: Settings as a searchable two-pane modal

The highest value-per-line change in this plan, because the content already exists on the wire and no screen reads it.

**Files:** `turing-client/turing_app/lib/features/settings/settings_screen.dart` (grown into a directory: `settings_screen.dart` plus `settings_sections.dart`), `lib/networking/api_client.dart`, `lib/networking/grpc_client.dart`, `lib/ui/shell/responsive_shell.dart`; tests in `test/features/settings/`.

**API surface to add — no proto change; all four RPCs already exist and are already generated in Dart:**

| Dart method on `TuringApi` | RPC | Already implemented? |
|---|---|---|
| `getConfig()` | `SessionService.GetConfig` | **yes** (`grpc_client.dart:95-115`), zero callers |
| `listTools()` | `SessionService.ListTools` (`sessions.proto:103`) | no Dart method; server done (`service.go:154-168`) |
| `listAgents()` | `SessionService.ListAgents` (`sessions.proto:102`) | no Dart method; server done (`service.go:149-152`) |
| `version()` | `HealthService.Version` (`health.proto:21-22`) | no Dart stub; `health.pbgrpc.dart` is generated and committed |

`TuringGrpcApi`'s constructor (`grpc_client.dart:69-90`) builds four service clients; adding `HealthServiceClient` on the same channel and options is three lines. **Every fake in `test/` implements `TuringApi` directly** (`responsive_shell_backend_test.dart:178`, `chat_screen_test.dart:7336`, `search_screen_test.dart:2507`, `widget_test.dart:111`), so each new abstract method breaks four fakes at compile time. That is the intended safety net, not an obstacle — do not add default implementations to dodge it.

- [ ] **Step 1: Write the failing tests.**
  - the modal opens over the shell (`find.byType(Dialog)` present, the conversation still in the tree behind it) at 1200px, and as a **full-screen route** at 390px — this is the mobile treatment, not a shrunken dialog;
  - the left pane lists the groups: `General`, `Model providers`, `Tools`, `Agents`, `About`. On the phone that same list is the **first screen**, and choosing one pushes its pane — a two-pane modal at 390px is unreadable;
  - typing `key` into `Search settings…` filters to the rows whose **title or description** matches, and hides the rest — assert an unrelated row disappears, not merely that a matching one is present;
  - a search with no matches says so rather than showing an empty pane;
  - `Model providers` renders one row per provider from a fake `getConfig()`, each with its `defaultModel`, and states plainly which one stays on the machine — reuse the existing sentence at `settings_screen.dart:75-79`, do not write a second one;
  - `Tools` renders `server · tool · policy` from a fake `listTools()`, and shows a specific message when the list is empty (tools are discovered at runtime, so empty means "the runtime has not reported yet", not "there are none");
  - `About` renders version and schema version from a fake `version()`, and renders a failure message rather than blank when the call rejects;
  - **the existing behaviour still holds:** saving backend URL and API key still calls `authStorage.save`, still calls `onSaved`, still surfaces a failure as a snackbar (`settings_screen.dart:108-132`), and the unconfigured-app path (`app.dart:79-84`) — which renders `SettingsScreen` as the whole app with no shell behind it — still works. That path has no modal to be inside, so `SettingsScreen` must keep rendering standalone.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.**
  - Add the three API methods and the health stub. Return plain `Map`/`List` shapes consistent with `getConfig()`'s existing style (`grpc_client.dart:95-115`) rather than introducing new model classes for read-only display data.
  - Build a `SettingsRow` primitive: bold title, grey description sentence, trailing control. **This is the whole Copilot pattern** — a title without its explanatory sentence is the thing being fixed. Make `description` a required parameter so a row cannot ship without one.
  - Search filters over `(title + description)` of every registered row, across all groups. Keep the row registry a plain `List<SettingsSection>` data structure so search is a filter over data, not a walk over widgets.
  - `General`: backend URL, API key, theme (a three-way `ThemeMode` segmented control driven by `ThemeLogic().mode`, `logic/theme_logic.dart`, with a two-swatch preview from `AppPalette`). The theme toggle currently lives in the sidebar footer (`responsive_shell.dart:436-476`); **move it, do not duplicate it** — Task 7 rebuilds that footer.
  - `About`: version card, plus the plain statement of where data lives and what deletion does. No update check, no path picker.
  - Open it from the shell as a modal on wide, a route on compact — the same `LayoutBuilder` decision Task 1 established. One breakpoint constant, shared.

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — make search match on title only and confirm the description-match test fails.

- [ ] **Step 5:** `flutter analyze`, `flutter test`. **Commit.**

> **Merge note:** this task lands on top of `bdbfb42`, which moved the provider picker into Settings. Keep `ModelProviderSelector` (`features/chat/model_provider_selector.dart`) as the control inside the `Model providers` group — it is 29 lines and already tested (`test/features/model_provider_selector_test.dart`).

## Task 5: A landing view you can type into

**Files:** `turing-client/turing_app/lib/ui/shell/responsive_shell.dart` (`_EmptyState`, `:508-559`); `test/ui/responsive_shell_backend_test.dart`.

- [ ] **Step 1: Write the failing tests.**
  - with no conversation selected there is a text field on screen, and typing into it and submitting calls `createSession` **then** `sendMessage` with that text — one gesture, not two;
  - if `createSession` fails, the typed text is **not lost** and a failure is shown. `_newConversation` today swallows this into a snackbar (`responsive_shell.dart:79-95`) with nothing to lose; once text is involved, losing it is a defect;
  - three suggestion cards render, each with its category chip, and tapping one puts its text in the composer rather than sending it immediately — a suggestion is a starting point, not a command;
  - **phone:** the three cards stack vertically and the composer stays reachable above the keyboard; assert no horizontal overflow at 390px (pump with a `MediaQuery` `viewInsets.bottom` and assert no `RenderFlex` overflow exception was recorded);
  - **desktop:** the same content is centred within a max width rather than stretched across a 1400px window.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Replace `_EmptyState`'s button with a composer plus cards. The three suggestions must map to capability that exists — draw from the actual tool registry, not from imagination:
  - `"What files are in the sandbox, and what's in the newest one?"` — chip **Files** (`files.list`, `files.read`, `mcp-files/internal/tools/files.go:705-710`)
  - `"What did I tell you about this before?"` — chip **Memory** (cross-session FTS recall, #33)
  - `"What operating system and Go runtime is the backend on?"` — chip **System** (`system.info`, `mcp-system/internal/tools/system.go:34`)

  Nothing here promises the internet, a repo, a schedule, or a file on the user's desktop. If a suggestion cannot be satisfied by a tool in `ListTools`, it does not ship. Consider driving the chips off a real `listTools()` call once Task 4 has added it, so a suggestion for a tool the runtime has not reported is hidden rather than broken — decide this during implementation; a hard-coded trio is acceptable for v1 if the tests pin the strings.

- [ ] **Step 4: Run, confirm pass; prove it discriminates** — make the composer send without creating a session and confirm the ordering test fails.

- [ ] **Step 5:** `flutter analyze`, `flutter test`. **Commit.**

## Task 6: A composer that says what it is talking to

**Files:** `turing-client/turing_app/lib/features/chat/chat_screen.dart` (composer, `:1087-1116`; `_composerCopy`, `:1012-1017`; `_sending`, `:918-971`), `lib/ui/shell/responsive_shell.dart` (passes `modelProvider` already, `:220-224`); `test/features/chat_screen_test.dart`.

- [ ] **Step 1: Write the failing tests.**
  - the composer shows the provider and model from a fake `getConfig()` — e.g. `Ollama · qwen2.5:7b`;
  - with the provider set to `ollama` it reads `On this machine`; with `openai_compatible` it reads `Leaves this machine`. **Assert both strings**, and assert the second is *not* shown for Ollama. This row is a privacy claim; a wrong one is a commitment-#1 falsification, which is rule 1 of `VISION.md`'s priority list;
  - if `getConfig()` rejects, the chip degrades to the provider name alone and does **not** invent a model name;
  - a run in progress shows a busy indicator, and it persists **after** `sendMessage` resolves — clearing on `agent.run.completed`/`failed`/`cancelled`, not on the RPC. This is the whole point;
  - **phone:** at 390px the chip row wraps or truncates rather than overflowing, and the send button stays hit-testable;
  - **desktop:** the chip row sits inside the composer's footer, not as a separate band above it.

- [ ] **Step 2: Run, confirm failure.** The busy-persistence test is the one that fails hardest today.

- [ ] **Step 3: Implement.**
  - Fetch config once at shell level and pass it down, alongside the existing `modelProvider` (`responsive_shell.dart:53,220-224`). Do not call `getConfig()` per `ChatScreen` — the screen is keyed by session (`responsive_shell.dart:216-218`) and rebuilds on every switch.
  - Add a `_running` flag driven by the event stream, set on `agent.run.started`/`queued` and cleared on the three terminal types. `_applyEvent` (`chat_screen.dart:607-663`) is the single switch; the terminal handlers already exist (`_applyRunFailed:685`, `_applyRunCancelled:720`). **Leave `_sending` alone** — it guards `_composerDisabled` (`chat_screen.dart:192-195`) against double-send and has its own carefully argued lifecycle. `_running` is presentation only; it must not disable the composer.
  - `_composerCopy`'s ordering doc (`chat_screen.dart:1000-1017`) explains why each state wins; if `_running` gains a copy string, extend that doc rather than reordering the checks.
  - **The busy indicator must not look like a stop button** and must not be tappable. There is no cancel RPC; an affordance that does nothing is worse than none.

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — clear `_running` in `sendMessage`'s success path and confirm the persistence test fails.

- [ ] **Step 5:** `flutter analyze`, `flutter test`. **Commit.**

## Task 7: A footer that says the backend is alive

**Files:** `turing-client/turing_app/lib/ui/shell/responsive_shell.dart` (`_SidebarFooter`, `:436-476`); `test/ui/responsive_shell_backend_test.dart`.

Copilot's footer is an account card. TuringAgent has no accounts — `VISION.md` open question 2 records that single-user is an assumption, not an oversight. The honest content for that slot is **the state of the thing the whole app depends on**.

- [ ] **Step 1: Write the failing tests.**
  - a healthy fake renders a connected indicator and the backend version from `version()`;
  - a rejecting fake renders a disconnected state naming the configured backend URL, with a route into Settings — a user whose backend is down needs the URL field, not a shrug;
  - the theme toggle is **gone** from the footer (it moved to Settings in Task 4) — assert it is absent here and present there, so the two tasks cannot both drop it;
  - **phone:** the footer is inside the drawer, pinned to its bottom, and does not eat conversation space;
  - **desktop:** unchanged position at the bottom of the sidebar.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Poll nothing. Call `version()` once when the shell mounts and again when settings change (`onSettingsChanged` already fans out, `responsive_shell.dart:163-177`). A heartbeat is a background network call on a loop, which is exactly the shape the egress invariant is suspicious of, and the event stream already reports its own health through `_streamEnded` (`chat_screen.dart:523-541`).

- [ ] **Step 4: Run, confirm pass; prove it discriminates.**

- [ ] **Step 5:** `flutter analyze`, `flutter test`. **Commit.**

---

## Backend and proto work, called out separately

Tasks 1, 3, 4, 5, 6, 7 are **client-only**. Task 2 is **Go-only, no proto, no schema**. That leaves one genuinely expensive borrow, which is **not** part of this plan's shippable set:

### Task 8 (deferred, needs its own plan): session state badges and "Waiting on you"

Copilot's git-branch badge and its `Up next` section both answer *"which of these needs me?"*. The honest TuringAgent answers are **an approval waiting for a decision** and **a run that failed while you were elsewhere**. Both are real, both are commitment-#2 material, and neither is reachable from the client today:

- **`ListSessions` returns no run state.** `Session` (`sessions.proto:10-16`) carries `status`, which is the `'active'|'archived'` column (`0001_initial.sql:15`), not run state. A badge needs a new field or a new RPC — **a proto change, regeneration with the pinned toolchain, and `tools/proto/check.sh` byte comparison.**
- **There is no "list pending approvals" read path.** Approvals reach the client only as `approval.requested` events on a session subscription the client must already be watching (`events.proto:10-32`). A cross-session view of what is waiting is a new query and a new RPC.
- **`_isHistoricalRunEvent` (`chat_screen.dart:737-743`) suppresses replayed run events**, and `VISION.md` already records the consequence: *"a past failed or cancelled run can still surface as an unexplained empty turn."* Any badge asserting "this conversation failed" must be derived from **persisted run rows**, not from the event stream the client watches — otherwise the badge and the transcript will disagree, and the transcript will be the one that looks broken.

Estimated shape: one proto change, one repository query, one service method, four fakes updated, plus a decision about what a badge shows for a session with both a failure *and* a pending approval. Write it as its own plan. Do not start it inside this one.

**Also deferred, also proto:** a stop button (`CancelRun` has no RPC), and renaming a conversation (`UpdateSession` does not exist). Task 2's title guard is written so the rename lands without a migration.

---

## Verification

Run from the repo root after each task. Client-only tasks still need the full matrix before the PR, because `flutter analyze` is not in CI's blocking set in the way the Go linters are.

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint cache clean
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

`golangci-lint cache clean` is not in CLAUDE.md's matrix but is required here: a stale cache has twice produced false positives citing sibling worktrees.

`tools/proto/check.sh` should be a **no-op** for every task in this plan. If it reports a diff, something in the shippable set has drifted into proto territory and must be pulled out.

Plus CLAUDE.md's required pre-push subagent review, covering unit-test coverage explicitly.

### Seeing it in the real client (once Tasks 1–7 land)

- [ ] Bring the stack up (`cd turing-backend && ./scripts/dev.sh`) with Ollama running; `flutter run -d macos`.
- [ ] Send a first message and confirm the sidebar row renames itself from `Untitled chat` to the message summary (Task 2).
- [ ] Reply to the oldest conversation and confirm it moves into `Today` (Task 2 + 3).
- [ ] Confirm the composer chip reads `Ollama · qwen2.5:7b` and `On this machine`; switch to the OpenAI-compatible provider in Settings and confirm the badge flips (Task 6). **Screenshot both for the PR** — this is the claim most worth evidencing.
- [ ] Open Settings, search for `key`, confirm unrelated rows disappear; confirm `Tools` lists real discovered tools (Task 4).
- [ ] Resize the macOS window below 840px and confirm the sidebar becomes a drawer without a layout exception in the console (Task 1).
- [ ] Stop the orchestrator and confirm the footer says disconnected and names the URL (Task 7).
- [ ] Tear the stack down.

---

## Open questions for the user

1. **Is a phone a real destination, or is "mobile" just narrow windows?** This plan makes the layout correct below 840px and testable, which is safe and useful on its own. But a phone on your LAN cannot reach `127.0.0.1:3000`, and widening `ORCHESTRATOR_PUBLIC_BIND_HOST` is the thing `VISION.md:29` names as the easiest accidental breach of commitment #1. If phones are real, the trust boundary needs a decision (LAN bind + a stronger client credential than one shared bearer key) **before** any more mobile UX is designed.
2. **Do you want folders?** Task 3 groups by time because time is the only axis that exists. Copilot's project folders are genuinely useful, and the TuringAgent equivalent would be user-created collections — but that is a new table, a new RPC, and drag-and-drop, and it competes with Task 8. Worth it, or is time enough?
3. **Should suggestion cards be generated from `ListTools`, or hard-coded?** Generated cannot promise a tool that is not there; hard-coded reads better. Task 5 assumes hard-coded with a note; say if you want the other.
4. **The composer's third slot.** Copilot puts reasoning effort and context budget there. Neither exists here. The tool-iteration cap (`maxToolIterations`, already emitted in a run notice) is the nearest thing to a "budget" the system has — is surfacing it in the composer useful, or is it internal detail a user cannot act on?

## Self-review checklist

- Baseline verified against the actual branch tip, and the three false premises in the brief are corrected on the record ✓
- Every borrowed pattern names its TuringAgent content; every refused one names why ✓
- `Autopilot` mode refused against a named `VISION.md` invariant, not on taste ✓
- No plan item mentions repos, branches, pull requests, worktrees or diffstats ✓
- Tasks 1–7 need **no** proto change; the one that does is separated out and deferred ✓
- Every task states both a desktop and a phone behaviour, each as an assertion ✓
- The 840px breakpoint is one constant, established in Task 1 and reused ✓
- Ordering is causal: names (2) before grouping (3); breakpoint (1) before everything ✓
- Titles are derived mechanically, so they are deterministic, testable, and do not spend the local model or hold a write lock ✓
- The privacy badge (Task 6) has both its true and its false case asserted ✓
- The busy indicator ships without a stop button, and is explicitly non-tappable, because no cancel RPC exists ✓
- The theme toggle is moved, not duplicated, and a test in each of Tasks 4 and 7 pins that ✓
- Every new `TuringApi` method deliberately breaks the four existing fakes at compile time ✓
- No background polling introduced; the egress invariant is not touched ✓
