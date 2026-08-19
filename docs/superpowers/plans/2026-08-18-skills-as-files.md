# Skills as Files Implementation Plan

Replaces the storage and selection model shipped in #45. Skills become
`SKILL.md` files in a folder, and the model chooses which apply instead of the
user attaching them by hand. Decided after checking OpenClaw, Hermes and Scout,
all of which do it this way; see `2026-08-18-skills.md` for that comparison.

This deletes more than it adds. Read the removal list before the build list.

## Why change something that works

Rows in SQLite cannot be version-controlled, edited in a real editor, copied
from someone else, or read without a database client. For a local-first tool
whose whole point is that your things stay yours, that is the wrong container.
Files also make the ecosystem's `SKILL.md` format available to us rather than
being a dialect of one.

## What is removed

- `skills` and `session_skills` tables (migration `0011`).
- `SkillService`'s writing half: create, update, delete, attach, detach.
- `SessionSkillsBar` and its picker — there is nothing to attach.
- The editor inside `SkillsPage`. Skills are edited in your editor now.

Keep: the enqueue-time snapshot. Freezing what a run was told to do is
independent of where the text came from, and it is what stops an edit
rewriting a reply already in flight.

## Layout, identity, and what the database still holds

The ecosystem's shape, confirmed from Hermes' and OpenClaw's own docs — a
folder per skill, not a flat pile of markdown:

```
turing-backend/skills/
└── writing/              <- category
    └── tone/             <- the skill; this path is its identity
        ├── SKILL.md
        └── references/   <- optional, addressable by skill_view
```

Mounted into the orchestrator at `/skills`, exactly as `../sandbox:/sandbox` is
today, and created by `init.sh`. This only needs deciding because we run in
Docker; OpenClaw, Hermes and Scout run natively and use a home directory.

**Category is the parent folder, not frontmatter.** That is where the
`category` in `skills_list()` comes from.

**Identity is the path from the skills root** — `writing/tone`. Neither system
has an `id` field; the folder name is the identifier. The folder name alone is
not enough for us because `writing/tone` and `email/tone` can both exist, so
the relative path is the id.

Frontmatter carries `name` and `description` (both required), and may carry
`version`, `author`, `license`. A skill folder may hold more than `SKILL.md` —
`scripts/`, `references/` — which is what Hermes' third disclosure level
addresses.

**Renaming a folder creates a new skill.** Its enabled flag resets to off.
This is a consequence of matching the ecosystem rather than inventing an id
field, and it is the right behaviour anyway: a renamed skill is a different
thing, and re-enabling it is one click.

### The database keeps exactly one column

`(skill_id, enabled)`. Nothing else.

Name, description and category are read from the files. Copying them into the
database would create a second source of truth that drifts the moment someone
edits a description — a stale answer with no error to notice. Parsing a few
hundred small markdown files is milliseconds; there is nothing to cache.

Reconciliation runs at startup and when the Skills page opens. **A file that
appears is disabled by default**, so dropping a folder into the mount cannot
silently start influencing answers. A file that disappears leaves its row
harmlessly; the row is keyed by a path that no longer resolves.

## The selection problem, which is the real risk

OpenClaw and Scout let the model pick because they assume a frontier model.
The default here is `qwen2.5:7b`. A 7B model choosing tools is unreliable, and
a skill that silently fails to apply is worse than one that was never offered.

So selection is **progressive disclosure**, Hermes' shape, with one deliberate
difference.

Hermes has three levels, and its index is itself a tool call:

- `skills_list()` → `{name, description, category}` per skill, ~3k tokens for
  their bundled set
- `skill_view(name)` → the full body, only once the agent wants it
- `skill_view(name, path)` → a single reference file inside a skill

Because the index is a tool, it costs nothing until the agent decides to look.
That is the right design for a model that reliably decides. Ours is
`qwen2.5:7b`, which may never think to look at all — and skills that exist but
are never consulted are worse than no skills, because the user believes they
are in effect.

**So the index is injected, not fetched.** One line per skill, `name` and
`description` from the frontmatter. The cost is smaller than Hermes' 3k figure
suggests: roughly 15 tokens per skill, so ten skills is about 150 tokens. It
only becomes worth reconsidering in the hundreds — at which point the fetch
model earns its keep and this should be revisited with real numbers from
Telemetry rather than a guess.

Both are provided:

1. Every request carries the **enabled** index. Enabling is curation, so the
   set is small by construction — this is what makes injection affordable and
   removes the dependency on a 7B deciding to look.
2. `skills_list()` exists anyway, returning `{name, description, category}`
   for enabled skills. It costs nothing until called, and it is the path that
   still works when a library outgrows injection.
3. `skill_view(id)` returns a body; `skill_view(id, path)` a reference file
   inside the skill folder.

**Degrades in the safe direction.** A model too weak to select gets an
unhelpful answer, not a wrong action — the tool only reads a file the user
wrote for this purpose.

An escape hatch for when the model still does not pick: naming a skill
explicitly in the message forces its body in, no tool call needed. That covers
the 7B case without making the common path depend on it.

## Build order

1. **Loader** — read `/skills/*.md`, parse frontmatter (`name`, `description`,
   optional stable `id`), body is the instructions. Malformed file: skipped
   and reported in the list with its parse error, never silently absent.
2. **Identity** — the `id` in frontmatter if present, else the filename stem.
   Renaming a file with an `id` keeps its identity; renaming one without
   changes it. Say so in the UI rather than pretending renames are free.
3. **Index into the request** — replaces `skillsSystemMessage`'s current job.
4. **`skill_view` tool** — reads only from the skills root, path-escape
   refused like mcp-files does. Not approval-gated: reading a file the user
   wrote for this purpose is not a mutation.
5. **`SkillService` becomes read-only** — list skills, read one. Same proto
   file, fewer RPCs.
6. **`SkillsPage` becomes a browser** — name, description, body, parse errors,
   and the folder path so you know where to put files.
7. **One-time export** — on first run after the migration, write existing DB
   skills out as files, then drop the tables. Nobody loses a skill they wrote.

## What this costs, stated plainly

- **You stop knowing exactly which skills applied.** That was the property the
  per-chat model bought, and it is being traded for scale and portability.
  Telemetry should record which skills a run actually loaded, or this becomes
  unauditable.
- **A skill can now be loaded into a conversation routed off-machine.** Same
  as today, but the set is no longer one the user hand-picked. The routing
  disclosure named in the previous plan becomes more important, not less.
- **Automations inherit the whole library, not an attached subset.** An
  unattended run can pull any skill. Worth deciding whether an automation
  should be able to restrict that, the way it restricts tools.

## Verification

Loader: valid skill folder, missing frontmatter, unreadable `SKILL.md`, a
folder with no `SKILL.md` at all, two skills of the same name under different
categories, path escape, empty root, root absent entirely.

Identity and enablement: a new folder arrives disabled; enabling then renaming
the folder leaves the new path disabled and does not resurrect the old row;
editing a description changes what `skills_list()` returns without any
database write. Index: bounded size,
excludes bodies. `skill_view`: refuses paths outside the root, returns a body
by id and by filename stem. Export: every DB skill lands as a readable file
with its name and instructions intact, and is idempotent on a second run.
Client: browser renders parse errors, and says where the folder is.
