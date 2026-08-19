# MEM-001 Memory Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a documented memory threat model and a CI schema invariant that prevents user-derived state from escaping source deletion.

**Architecture:** Keep enforcement in a focused DB invariant test rather than adding a production startup refusal. The test classifies every application-owned ordinary or virtual SQLite table, validates cascading ownership for derived tables, validates the cascade-equivalent external-content FTS trigger, and permits only named scrubbed exceptions with written justification.

**Tech Stack:** Go 1.23, SQLite/FTS5 through `github.com/mattn/go-sqlite3`, Go `testing`, Markdown architecture documentation.

---

### Task 1: Add the failing derived-table regression

**Files:**
- Create: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`

- [ ] **Step 1: Write the synthetic invalid-table test**

Create a migrated in-memory database, add a derived-text table whose source
foreign key omits `ON DELETE CASCADE`, classify it as cascade-owned, and call
the not-yet-implemented guard:

```go
func TestDerivedStateSchemaRejectsTableWithoutSourceCascade(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_summaries (
			id TEXT PRIMARY KEY,
			source_message_id TEXT NOT NULL REFERENCES messages(id),
			summary TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}

	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_summaries",
		kind:        schemaTableCascadeOwned,
		sourceTable: "messages",
	})
	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), "no ON DELETE CASCADE path") {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing cascade", err)
	}
}
```

- [ ] **Step 2: Run the test and preserve the red result**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run TestDerivedStateSchemaRejectsTableWithoutSourceCascade -count=1
```

Expected: compilation fails because the schema policy types and guard do not
exist yet. Do not add implementation before observing this failure.

### Task 2: Implement the exhaustive schema contract

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`

- [ ] **Step 1: Define the policy model and complete manifest**

Add four policy kinds:

```go
type schemaTablePolicyKind string

const (
	schemaTableIndependent       schemaTablePolicyKind = "independent"
	schemaTableCascadeOwned      schemaTablePolicyKind = "cascade_owned"
	schemaTableFTSProjection     schemaTablePolicyKind = "fts_projection"
	schemaTableScrubbedException schemaTablePolicyKind = "scrubbed_exception"
)

type schemaTablePolicy struct {
	table         string
	kind          schemaTablePolicyKind
	sourceTable   string
	deleteTrigger string
	justification string
}

var currentSchemaTablePolicies = []schemaTablePolicy{
	{table: "schema_migrations", kind: schemaTableIndependent},
	{table: "settings", kind: schemaTableIndependent},
	{table: "sessions", kind: schemaTableIndependent},
	{table: "messages", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "agent_runs", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "agent_run_steps", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "jobs", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "events", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "tools", kind: schemaTableIndependent},
	{table: "tool_calls", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "approvals", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{
		table:         "audit_logs",
		kind:          schemaTableScrubbedException,
		justification: "Session deletion scrubs payload content before retaining minimal action evidence.",
	},
	{table: "messages_fts", kind: schemaTableFTSProjection, sourceTable: "messages", deleteTrigger: "messages_fts_ad"},
	{table: "skills", kind: schemaTableIndependent},
	{table: "session_skills", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "external_agents", kind: schemaTableIndependent},
	{table: "session_external_agent", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "integration_connections", kind: schemaTableIndependent},
	{table: "automations", kind: schemaTableIndependent},
	{table: "automation_allowed_tools", kind: schemaTableCascadeOwned, sourceTable: "automations"},
	{table: "automation_runs", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
}
```

Add the migrated-database helper used by every invariant test:

```go
func openMigratedInvariantDB(t *testing.T, ctx context.Context) *DB {
	t.Helper()
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	return database
}
```

- [ ] **Step 2: Implement exhaustive table discovery**

Read `PRAGMA table_list`, keep `main` entries of type `table` or `virtual`,
ignore names beginning with `sqlite_`, and ignore rows of type `shadow`.
Reject duplicate policies, unclassified live application tables, and policies
for absent tables:

```go
if _, ok := policyByTable[tableName]; !ok {
	return fmt.Errorf("application table %q has no derived-state policy", tableName)
}
```

This is the mechanism that forces future migrations to make their memory and
deletion semantics explicit.

- [ ] **Step 3: Validate transitive cascading provenance**

For every `cascade_owned` policy, recursively read
`PRAGMA foreign_key_list(<table>)`. Traverse only edges whose `on_delete`
value is `CASCADE`, stop on the declared source, and protect the traversal
with a visited set. Return:

```go
return fmt.Errorf(
	"derived table %q has no ON DELETE CASCADE path to declared source %q",
	policy.table,
	policy.sourceTable,
)
```

An ordinary foreign key with SQLite's default `NO ACTION` must not satisfy the
contract.

- [ ] **Step 4: Validate FTS and scrubbed exceptions**

For `messages_fts`, require a virtual-table declaration with
`content='messages'` and `content_rowid='rowid'`. Require the named trigger to
be an `AFTER DELETE ON messages` trigger whose body issues the FTS5 delete
command with `old.rowid` and `old.content`.

For `scrubbed_exception`, reject blank justification:

```go
if strings.TrimSpace(policy.justification) == "" {
	return fmt.Errorf("scrubbed exception %q requires an explicit justification", policy.table)
}
```

- [ ] **Step 5: Add positive and defensive invariant tests**

Add:

```go
func TestDerivedStateSchemaPoliciesCoverCurrentSchema(t *testing.T)
func TestDerivedStateSchemaRejectsUnclassifiedTable(t *testing.T)
func TestDerivedStateSchemaRequiresScrubbedExceptionJustification(t *testing.T)
```

The first applies all migrations and expects no error. The second creates a
new ordinary table without adding a policy and expects `no derived-state
policy`. The third clears `audit_logs`' justification in a copied policy slice
and expects `requires an explicit justification`.

- [ ] **Step 6: Run and format the DB tests**

Run:

```bash
gofmt -w turing-backend/orchestrator-go/internal/db/schema_invariants_test.go
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db -count=1
```

Expected: all DB package tests pass, including the synthetic invalid fixture.

### Task 3: Connect the contract to project documentation

**Files:**
- Modify: `docs/VISION.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`
- Verify: `docs/architecture/memory-governance.md`

- [ ] **Step 1: Update the North Star invariants and current-state wording**

Add a permanent invariant stating that durable derived state carries
source-linked cascading provenance, with only a scrubbed audit tombstone
exception. Link `docs/architecture/memory-governance.md`. Keep the current
state explicit that curated memory is still absent.

- [ ] **Step 2: Refine MEM-001's roadmap contract without declaring an open change landed**

Add the architecture document and the named DB invariant test as the contract
artifacts that must exist in repository history before downstream tasks may
treat MEM-001 as satisfied. Do not mark the roadmap item complete or claim an
unmerged PR is deployed.

- [ ] **Step 3: Check documentation for overclaims and placeholders**

Run:

```bash
rg -n 'TBD|TODO|FIXME|physical erasure|secure_delete|WAL|scrubbed' \
  docs/VISION.md \
  docs/architecture/memory-governance.md \
  docs/architecture/2026-08-18-personal-agent-audit.md
git diff --check
```

Expected: no placeholders in the new contract; physical-erasure language
states limits rather than guarantees.

### Task 4: Reconcile, review, verify, and publish

**Files:**
- Review all files changed from `origin/main`

- [ ] **Step 1: Commit the implementation and documentation**

```bash
git add \
  turing-backend/orchestrator-go/internal/db/schema_invariants_test.go \
  docs/VISION.md \
  docs/architecture/2026-08-18-personal-agent-audit.md \
  docs/architecture/memory-governance.md \
  docs/superpowers/plans/2026-08-19-mem-001-memory-governance.md
git commit -m "test(db): enforce derived-state provenance" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

- [ ] **Step 2: Merge latest `origin/main` normally**

```bash
git fetch origin main
git merge --no-edit origin/main
```

Resolve conflicts by preserving both landed main behavior and the MEM-001
contract. Never reset, rebase shared commits, or force-push.

- [ ] **Step 3: Complete iterative full-diff reviews**

Run Claude Opus 5 and GPT-5.6 Luna full-diff reviews in parallel. Apply every
valid correctness, contract, naming, and test-coverage finding, then repeat
fresh parallel reviews until both explicitly report no remaining feedback.
Record each round and result for the PR body.

Run a separate Claude Opus 4.8 repository-policy review over the full final
diff. Address all findings and rerun any review invalidated by a change.

- [ ] **Step 4: Run the complete repository verification matrix**

Invoke the repository `/verify` skill after the final code and final merge from
main. It must cover root Go test/race/build with `sqlite_fts5`, `mcp-files`
test/race/build, `mcp-system` test/race/build, Flutter analyze/test, proto
check, and all three `golangci-lint` commands.

- [ ] **Step 5: Push and create the focused PR**

Push normally, open a PR into `main`, apply `turing-roadmap`, and include the
MEM-001 scope, documentation, review rounds, and fresh verification evidence
in the PR body.

- [ ] **Step 6: Read live GitHub state and report**

Require the PR to be open and GitHub to report it mergeable/clean. Read all
six CI jobs and report each as successful or clearly pending; do not merge.
Send the coordinator the PR URL, head SHA, changed docs, review results,
verification matrix, mergeability/CI, and the fact that every derived-memory
task remains dependent on the MEM-001 PR merging.
