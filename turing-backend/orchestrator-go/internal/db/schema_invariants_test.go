package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
)

type schemaTablePolicyKind string

const (
	schemaTableIndependent       schemaTablePolicyKind = "independent"
	schemaTableCascadeOwned      schemaTablePolicyKind = "cascade_owned"
	schemaTableFTSProjection     schemaTablePolicyKind = "fts_projection"
	schemaTableScrubbedException schemaTablePolicyKind = "scrubbed_exception"
)

type schemaTablePresence int

const (
	schemaTableRequired schemaTablePresence = iota
	schemaTableOptional
)

type schemaTablePolicy struct {
	table         string
	kind          schemaTablePolicyKind
	presence      schemaTablePresence
	sourceTable   string
	deleteTrigger string
	rationale     string
}

var approvedScrubbedExceptionTables = map[string]struct{}{
	"audit_logs": {},
}

var ftsProjectionDeleteChecks = map[string]func(context.Context, *DB) error{
	"messages_fts": validateMessagesFTSDeleteBehavior,
}

var currentSchemaTablePolicies = []schemaTablePolicy{
	{
		table:     "schema_migrations",
		kind:      schemaTableIndependent,
		rationale: "Schema migration metadata is system state, not user-derived content.",
	},
	{
		table:     "settings",
		kind:      schemaTableIndependent,
		rationale: "Settings are user-managed source configuration with their own lifecycle.",
	},
	{
		table:     "sessions",
		kind:      schemaTableIndependent,
		rationale: "Sessions are ownership roots whose deletion drives conversation cascades.",
	},
	{table: "messages", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "agent_runs", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "agent_run_steps", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "jobs", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "events", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{
		table:     "tools",
		kind:      schemaTableIndependent,
		rationale: "Tools are a discovered capability catalog, not user-derived memory.",
	},
	{table: "tool_calls", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "approvals", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{
		table:     "audit_logs",
		kind:      schemaTableScrubbedException,
		rationale: "Session deletion scrubs payload content before retaining minimal action evidence.",
	},
	{
		table:         "messages_fts",
		kind:          schemaTableFTSProjection,
		sourceTable:   "messages",
		deleteTrigger: "messages_fts_ad",
	},
	{
		table:     "skill_settings",
		kind:      schemaTableIndependent,
		rationale: "Skill enablement is user-managed source configuration with its own lifecycle.",
	},
	{
		table:     "skill_capability_grants",
		kind:      schemaTableIndependent,
		rationale: "Capability grants are user-managed consent records with explicit revocation.",
	},
	{
		table:     "legacy_skill_export_recovery",
		kind:      schemaTableIndependent,
		presence:  schemaTableOptional,
		rationale: "Legacy skill rows are migration recovery state retained until explicit operator cleanup.",
	},
	{
		table:     "external_agents",
		kind:      schemaTableIndependent,
		rationale: "External agents are user-managed routing destinations, not conversation-derived state.",
	},
	{table: "session_external_agent", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{
		table:     "integration_connections",
		kind:      schemaTableIndependent,
		rationale: "Integration connections are user-managed consent records with explicit revocation.",
	},
	{
		table:     "automations",
		kind:      schemaTableIndependent,
		rationale: "Automations are user-authored schedules that intentionally survive session deletion.",
	},
	{table: "automation_allowed_tools", kind: schemaTableCascadeOwned, sourceTable: "automations"},
	{table: "automation_runs", kind: schemaTableCascadeOwned, sourceTable: "agent_runs"},
	{table: "send_message_idempotency", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
}

func TestDerivedStateSchemaPoliciesCoverCurrentSchema(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies); err != nil {
		t.Fatal(err)
	}
}

func TestDerivedStateSchemaPoliciesCoverMigrationRecoverySchema(t *testing.T) {
	ctx := context.Background()
	database := databaseThrough0010(t, ctx)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO skills (id, name, instructions, created_at, updated_at)
		VALUES ('skill_recovery_policy', 'Recovery policy', 'Retain until verified.', datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMigrationsWithSkillsRoot(ctx, database, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies); err != nil {
		t.Fatal(err)
	}
}

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

func TestDerivedStateSchemaRejectsUnclassifiedTable(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE unclassified_memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}

	err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies)
	if err == nil || !strings.Contains(err.Error(), `application table "unclassified_memories" has no derived-state policy`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want unclassified table", err)
	}
}

func TestDerivedStateSchemaDoesNotCollapseDistinctNonASCIIIdentifiers(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE "Å" (id TEXT PRIMARY KEY);
		CREATE TABLE "å" (id TEXT PRIMARY KEY);
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "Å",
		kind:      schemaTableIndependent,
		rationale: "Uppercase non-ASCII identifier for this regression.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `application table "å" has no derived-state policy`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want distinct non-ASCII table rejection", err)
	}
}

func TestDerivedStateSchemaRequiresScrubbedExceptionJustification(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "audit_logs" {
			policies[index].rationale = ""
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `scrubbed exception "audit_logs" requires an explicit justification`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing scrubbed-exception justification", err)
	}
}

func TestDerivedStateSchemaRejectsUnapprovedScrubbedException(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_tombstones (
			id TEXT PRIMARY KEY,
			payload TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "memory_tombstones",
		kind:      schemaTableScrubbedException,
		rationale: "A future scrubber will remove payload content.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `scrubbed exception "memory_tombstones" is not approved`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want unapproved scrubbed exception", err)
	}
}

func TestDerivedStateSchemaRejectsScrubbedExceptionAsProvenanceSource(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE audit_projections (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL REFERENCES audit_logs(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "audit_projections",
		kind:        schemaTableCascadeOwned,
		sourceTable: "audit_logs",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`derived table "audit_projections" cannot use scrubbed exception "audit_logs" as its provenance source`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsApprovedScrubbedExceptionAsProvenanceSourceAfterReclassification(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE audit_projections (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL REFERENCES audit_logs(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "audit_logs" {
			policies[index].kind = schemaTableIndependent
			policies[index].rationale = "Incorrectly reclassified for this regression."
		}
	}
	policies = append(policies, schemaTablePolicy{
		table:       "audit_projections",
		kind:        schemaTableCascadeOwned,
		sourceTable: "audit_logs",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`derived table "audit_projections" cannot use scrubbed exception "audit_logs" as its provenance source`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want approved scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionOverScrubbedException(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "messages_fts" {
			policies[index].sourceTable = "AUDIT_LOGS"
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`derived table "messages_fts" cannot use scrubbed exception "AUDIT_LOGS" as its provenance source`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want FTS scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsMixedCaseSourceOnScrubbedExceptionPolicy(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "audit_logs" {
			policies[index].sourceTable = "AUDIT_LOGS"
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`derived table "audit_logs" cannot use scrubbed exception "AUDIT_LOGS" as its provenance source`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want scrubbed-policy source rejection", err)
	}
}

func TestDerivedStateSchemaRequiresApprovedScrubbedExceptionPolicy(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "audit_logs" {
			policies[index].kind = schemaTableIndependent
			policies[index].rationale = "Incorrectly reclassified for this regression."
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`approved scrubbed exception "audit_logs" must use the scrubbed_exception policy`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want approved exception policy rejection", err)
	}
}

func TestDerivedStateSchemaRejectsIndependentTableReferencingScrubbedException(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL REFERENCES audit_logs(id),
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "memory_facts",
		kind:      schemaTableIndependent,
		rationale: "Incorrectly classified for this regression.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`table "memory_facts" has a foreign-key path to scrubbed exception "audit_logs"`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want structural scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsAdditionalNullableScrubbedProvenance(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
			audit_id TEXT REFERENCES audit_logs(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "sessions",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`table "memory_facts" has a foreign-key path to scrubbed exception "audit_logs"`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want additional scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsMixedCaseScrubbedProvenance(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL REFERENCES AUDIT_LOGS(id),
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "memory_facts",
		kind:      schemaTableIndependent,
		rationale: "Incorrectly classified for this regression.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`table "memory_facts" has a foreign-key path to scrubbed exception "audit_logs"`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want case-insensitive scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsTransitiveMixedCaseScrubbedProvenance(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_index (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL REFERENCES AUDIT_LOGS(id)
		);
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			index_id TEXT NOT NULL REFERENCES memory_index(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		);
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies,
		schemaTablePolicy{
			table:       "memory_facts",
			kind:        schemaTableCascadeOwned,
			sourceTable: "memory_index",
		},
		schemaTablePolicy{
			table:     "memory_index",
			kind:      schemaTableIndependent,
			rationale: "Incorrectly classified for this regression.",
		},
	)

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`table "memory_facts" has a foreign-key path to scrubbed exception "audit_logs"`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want transitive scrubbed-source rejection", err)
	}
}

func TestDerivedStateSchemaAcceptsTransitiveSourceCascade(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_message_id TEXT NOT NULL REFERENCES MESSAGES(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "SeSsIoNs",
	})

	if err := validateDerivedStateSchema(ctx, database, policies); err != nil {
		t.Fatalf("validateDerivedStateSchema rejected transitive source cascade: %v", err)
	}
}

func TestDerivedStateSchemaRejectsNullableCompositeSourceCascade(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_sources (
			tenant_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			PRIMARY KEY (tenant_id, source_id)
		);
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_tenant_id TEXT NOT NULL,
			source_id TEXT,
			content TEXT NOT NULL,
			FOREIGN KEY (source_tenant_id, source_id)
				REFERENCES memory_sources(tenant_id, source_id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies,
		schemaTablePolicy{
			table:     "memory_sources",
			kind:      schemaTableIndependent,
			rationale: "Synthetic composite source for this regression.",
		},
		schemaTablePolicy{
			table:       "memory_facts",
			kind:        schemaTableCascadeOwned,
			sourceTable: "memory_sources",
		},
	)

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), "no ON DELETE CASCADE path with non-null source columns") {
		t.Fatalf("validateDerivedStateSchema error = %v, want nullable composite source rejection", err)
	}
}

func TestDerivedStateSchemaAcceptsNonNullCompositeSourceCascade(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_sources (
			tenant_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			PRIMARY KEY (tenant_id, source_id)
		);
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_tenant_id TEXT NOT NULL,
			source_id TEXT NOT NULL,
			content TEXT NOT NULL,
			FOREIGN KEY (source_tenant_id, source_id)
				REFERENCES memory_sources(tenant_id, source_id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies,
		schemaTablePolicy{
			table:     "memory_sources",
			kind:      schemaTableIndependent,
			rationale: "Synthetic composite source for this regression.",
		},
		schemaTablePolicy{
			table:       "memory_facts",
			kind:        schemaTableCascadeOwned,
			sourceTable: "memory_sources",
		},
	)

	if err := validateDerivedStateSchema(ctx, database, policies); err != nil {
		t.Fatalf("validateDerivedStateSchema rejected non-null composite source: %v", err)
	}
}

func TestDerivedStateSchemaForeignKeyCyclesTerminate(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE cycle_a (
			id TEXT PRIMARY KEY,
			b_id TEXT NOT NULL REFERENCES cycle_b(id) ON DELETE CASCADE
		);
		CREATE TABLE cycle_b (
			id TEXT PRIMARY KEY,
			a_id TEXT NOT NULL REFERENCES cycle_a(id) ON DELETE CASCADE
		);
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies,
		schemaTablePolicy{
			table:       "cycle_a",
			kind:        schemaTableCascadeOwned,
			sourceTable: "sessions",
		},
		schemaTablePolicy{
			table:     "cycle_b",
			kind:      schemaTableIndependent,
			rationale: "Synthetic cycle for this regression.",
		},
	)

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `derived table "cycle_a" has no ON DELETE CASCADE path`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want finite missing-cascade rejection", err)
	}
}

func TestDerivedStateSchemaRequiresIndependentTableJustification(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "settings" {
			policies[index].rationale = ""
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `independent table "settings" requires an explicit justification`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing independent-table justification", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionWithoutDeleteTrigger(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, "DROP TRIGGER messages_fts_ad"); err != nil {
		t.Fatal(err)
	}

	err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies)
	if err == nil || !strings.Contains(err.Error(), `FTS projection "messages_fts" is missing delete trigger "messages_fts_ad"`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing FTS delete trigger", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionWithDisabledDeleteTrigger(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		DROP TRIGGER messages_fts_ad;
		CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages WHEN 0 BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content)
			VALUES ('delete', old.rowid, old.content);
		END;
	`); err != nil {
		t.Fatal(err)
	}

	err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies)
	if err == nil || !strings.Contains(err.Error(), `FTS projection "messages_fts" retained its probe after source deletion`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want nonfunctional FTS delete trigger", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionWithMalformedDeleteTrigger(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		DROP TRIGGER messages_fts_ad;
		CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
			SELECT old.rowid;
		END;
	`); err != nil {
		t.Fatal(err)
	}

	err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies)
	if err == nil || !strings.Contains(err.Error(), `does not remove projection rows transactionally`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want malformed FTS delete trigger", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionWithoutExternalContentSource(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		DROP TRIGGER messages_fts_ai;
		DROP TRIGGER messages_fts_ad;
		DROP TRIGGER messages_fts_au;
		DROP TABLE messages_fts;
		CREATE VIRTUAL TABLE messages_fts USING fts5(content);
	`); err != nil {
		t.Fatal(err)
	}

	err := validateDerivedStateSchema(ctx, database, currentSchemaTablePolicies)
	if err == nil || !strings.Contains(err.Error(), `must use "messages" as external content keyed by rowid`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing FTS external-content source", err)
	}
}

func TestDerivedStateSchemaAcceptsMixedCaseFTSPolicyIdentifiers(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "messages_fts" {
			policies[index].table = "MESSAGES_FTS"
			policies[index].sourceTable = "MESSAGES"
			policies[index].deleteTrigger = "MESSAGES_FTS_AD"
		}
	}

	if err := validateDerivedStateSchema(ctx, database, policies); err != nil {
		t.Fatalf("validateDerivedStateSchema rejected mixed-case FTS policy identifiers: %v", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionOverDistinctNonASCIIIdentifier(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE "Å" (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL
		);
		CREATE TABLE "å" (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL
		);
		CREATE VIRTUAL TABLE unicode_fts USING fts5(
			content,
			content='å',
			content_rowid='rowid'
		);
		CREATE TRIGGER unicode_fts_ad AFTER DELETE ON "å" BEGIN
			INSERT INTO unicode_fts(unicode_fts, rowid, content)
			VALUES ('delete', old.rowid, old.content);
		END;
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies,
		schemaTablePolicy{
			table:     "Å",
			kind:      schemaTableIndependent,
			rationale: "Distinct uppercase non-ASCII source for this regression.",
		},
		schemaTablePolicy{
			table:     "å",
			kind:      schemaTableIndependent,
			rationale: "Distinct lowercase non-ASCII source for this regression.",
		},
		schemaTablePolicy{
			table:         "unicode_fts",
			kind:          schemaTableFTSProjection,
			sourceTable:   "Å",
			deleteTrigger: "unicode_fts_ad",
		},
	)

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`FTS projection "unicode_fts" must use "Å" as external content keyed by rowid`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want distinct non-ASCII FTS source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsUnknownPolicyKind(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	for index := range policies {
		if policies[index].table == "settings" {
			policies[index].kind = "unchecked"
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `application table "settings" has unsupported derived-state policy kind "unchecked"`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want unsupported policy kind", err)
	}
}

func TestDerivedStateSchemaRejectsDuplicatePolicy(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "settings",
		kind:      schemaTableIndependent,
		rationale: "Duplicate policy for this regression.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`application table "settings" has duplicate derived-state policies`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want duplicate policy rejection", err)
	}
}

func TestDerivedStateSchemaRejectsPolicyForAbsentTable(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "never_created",
		kind:      schemaTableIndependent,
		rationale: "Absent table policy for this regression.",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`derived-state policy names absent application table "never_created"`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want absent table policy rejection", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionThatIsNotVirtual(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_fts (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:         "memory_fts",
		kind:          schemaTableFTSProjection,
		sourceTable:   "messages",
		deleteTrigger: "memory_fts_ad",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`FTS projection "memory_fts" is not a virtual table`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want non-virtual FTS rejection", err)
	}
}

func TestDerivedStateSchemaRejectsFTSProjectionWithoutBehavioralDeleteCheck(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_sources (
			id INTEGER PRIMARY KEY,
			content TEXT NOT NULL
		);
		CREATE VIRTUAL TABLE memory_fts USING fts5(
			content,
			content='memory_sources',
			content_rowid='rowid'
		);
		CREATE TRIGGER memory_fts_ad AFTER DELETE ON memory_sources BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, content)
			VALUES ('delete', old.rowid, old.content);
		END;
	`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:     "memory_sources",
		kind:      schemaTableIndependent,
		rationale: "Synthetic FTS source for this regression.",
	})
	policies = append(policies, schemaTablePolicy{
		table:         "memory_fts",
		kind:          schemaTableFTSProjection,
		sourceTable:   "memory_sources",
		deleteTrigger: "memory_fts_ad",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(
		err.Error(),
		`FTS projection "memory_fts" has no behavioral delete check`,
	) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing behavioral check rejection", err)
	}
}

func TestDerivedStateSchemaRejectsMissingDeclaredSource(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL REFERENCES missing_sources(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "missing_sources",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `declares absent source table "missing_sources"`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want absent source", err)
	}
}

func TestDerivedStateSchemaRejectsSelfAsDeclaredSource(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL REFERENCES memory_facts(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "memory_facts",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `cannot declare itself as its source`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want self-source rejection", err)
	}
}

func TestDerivedStateSchemaRejectsNullableSourceCascade(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_message_id TEXT REFERENCES messages(id) ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "messages",
	})

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), "no ON DELETE CASCADE path with non-null source columns") {
		t.Fatalf("validateDerivedStateSchema error = %v, want nullable source rejection", err)
	}
}

func TestDerivedStateSchemaAcceptsImplicitPrimaryKeyReference(t *testing.T) {
	ctx := context.Background()
	database := openMigratedInvariantDB(t, ctx)
	if _, err := database.ExecContext(ctx, `
		CREATE TABLE memory_facts (
			id TEXT PRIMARY KEY,
			source_message_id TEXT NOT NULL REFERENCES messages ON DELETE CASCADE,
			content TEXT NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	policies := append([]schemaTablePolicy(nil), currentSchemaTablePolicies...)
	policies = append(policies, schemaTablePolicy{
		table:       "memory_facts",
		kind:        schemaTableCascadeOwned,
		sourceTable: "messages",
	})

	if err := validateDerivedStateSchema(ctx, database, policies); err != nil {
		t.Fatalf("validateDerivedStateSchema rejected implicit primary-key reference: %v", err)
	}
}

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

func validateDerivedStateSchema(ctx context.Context, database *DB, policies []schemaTablePolicy) error {
	policyByTable := make(map[string]schemaTablePolicy, len(policies))
	for _, policy := range policies {
		tableName := canonicalSQLiteIdentifier(policy.table)
		if _, exists := policyByTable[tableName]; exists {
			return fmt.Errorf("application table %q has duplicate derived-state policies", policy.table)
		}
		policyByTable[tableName] = policy
	}

	tableTypes, err := applicationSchemaTableTypes(ctx, database)
	if err != nil {
		return err
	}
	tableNames := make([]string, 0, len(tableTypes))
	for tableName := range tableTypes {
		tableNames = append(tableNames, tableName)
	}
	sort.Strings(tableNames)
	for _, tableName := range tableNames {
		if _, classified := policyByTable[tableName]; !classified {
			return fmt.Errorf("application table %q has no derived-state policy", tableName)
		}
	}
	for _, policy := range policies {
		if _, exists := tableTypes[canonicalSQLiteIdentifier(policy.table)]; !exists &&
			policy.presence != schemaTableOptional {
			return fmt.Errorf("derived-state policy names absent application table %q", policy.table)
		}
	}

	for _, policy := range policies {
		if policy.sourceTable == "" {
			continue
		}
		sourceTable := canonicalSQLiteIdentifier(policy.sourceTable)
		sourcePolicy, sourceClassified := policyByTable[sourceTable]
		_, sourceApproved := approvedScrubbedExceptionTables[sourceTable]
		if sourceApproved ||
			(sourceClassified && sourcePolicy.kind == schemaTableScrubbedException) {
			return fmt.Errorf(
				"derived table %q cannot use scrubbed exception %q as its provenance source",
				policy.table,
				policy.sourceTable,
			)
		}
	}
	for table := range approvedScrubbedExceptionTables {
		policy, classified := policyByTable[canonicalSQLiteIdentifier(table)]
		if !classified || policy.kind != schemaTableScrubbedException {
			return fmt.Errorf(
				"approved scrubbed exception %q must use the scrubbed_exception policy",
				table,
			)
		}
	}
	scrubbedExceptionTables := make(map[string]struct{}, len(approvedScrubbedExceptionTables))
	for table := range approvedScrubbedExceptionTables {
		scrubbedExceptionTables[canonicalSQLiteIdentifier(table)] = struct{}{}
	}
	for _, policy := range policies {
		if policy.kind == schemaTableScrubbedException {
			scrubbedExceptionTables[canonicalSQLiteIdentifier(policy.table)] = struct{}{}
		}
	}
	for _, policy := range policies {
		tableName := canonicalSQLiteIdentifier(policy.table)
		if _, exists := tableTypes[tableName]; !exists {
			continue
		}
		if _, scrubbed := scrubbedExceptionTables[tableName]; scrubbed {
			continue
		}
		scrubbedAncestor, err := findForeignKeyAncestor(
			ctx,
			database,
			policy.table,
			scrubbedExceptionTables,
			nil,
		)
		if err != nil {
			return err
		}
		if scrubbedAncestor != "" {
			return fmt.Errorf(
				"table %q has a foreign-key path to scrubbed exception %q",
				policy.table,
				scrubbedAncestor,
			)
		}
	}

	for _, policy := range policies {
		switch policy.kind {
		case schemaTableIndependent:
			if strings.TrimSpace(policy.rationale) == "" {
				return fmt.Errorf(
					"independent table %q requires an explicit justification",
					policy.table,
				)
			}
		case schemaTableCascadeOwned:
			sourceTable := canonicalSQLiteIdentifier(policy.sourceTable)
			if sourceTable == canonicalSQLiteIdentifier(policy.table) {
				return fmt.Errorf(
					"derived table %q cannot declare itself as its source",
					policy.table,
				)
			}
			if _, exists := tableTypes[sourceTable]; !exists {
				return fmt.Errorf(
					"derived table %q declares absent source table %q",
					policy.table,
					policy.sourceTable,
				)
			}
			hasCascadePath, err := tableHasCascadePath(ctx, database, policy.table, sourceTable, nil)
			if err != nil {
				return err
			}
			if !hasCascadePath {
				return fmt.Errorf(
					"derived table %q has no ON DELETE CASCADE path with non-null source columns to declared source %q",
					policy.table,
					policy.sourceTable,
				)
			}
		case schemaTableFTSProjection:
			if tableTypes[canonicalSQLiteIdentifier(policy.table)] != "virtual" {
				return fmt.Errorf("FTS projection %q is not a virtual table", policy.table)
			}
			if err := validateExternalContentFTSProjection(ctx, database, policy); err != nil {
				return err
			}
		case schemaTableScrubbedException:
			if strings.TrimSpace(policy.rationale) == "" {
				return fmt.Errorf(
					"scrubbed exception %q requires an explicit justification",
					policy.table,
				)
			}
			if _, approved := approvedScrubbedExceptionTables[canonicalSQLiteIdentifier(policy.table)]; !approved {
				return fmt.Errorf("scrubbed exception %q is not approved", policy.table)
			}
		default:
			return fmt.Errorf(
				"application table %q has unsupported derived-state policy kind %q",
				policy.table,
				policy.kind,
			)
		}
	}
	return nil
}

func validateExternalContentFTSProjection(
	ctx context.Context,
	database *DB,
	policy schemaTablePolicy,
) error {
	var tableSQL string
	if err := database.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ? COLLATE NOCASE`,
		policy.table,
	).Scan(&tableSQL); err != nil {
		return fmt.Errorf("inspect FTS projection %q: %w", policy.table, err)
	}
	compactTableSQL := normalizeSQLiteSQLForComparison(tableSQL)
	expectedContent := "content='" + canonicalSQLiteIdentifier(policy.sourceTable) + "'"
	if !strings.Contains(compactTableSQL, expectedContent) ||
		!strings.Contains(compactTableSQL, "content_rowid='rowid'") {
		return fmt.Errorf(
			"FTS projection %q must use %q as external content keyed by rowid",
			policy.table,
			policy.sourceTable,
		)
	}

	var triggerSQL string
	if err := database.QueryRowContext(
		ctx,
		`SELECT sql FROM sqlite_schema WHERE type = 'trigger' AND name = ? COLLATE NOCASE`,
		policy.deleteTrigger,
	).Scan(&triggerSQL); err != nil {
		return fmt.Errorf(
			"FTS projection %q is missing delete trigger %q: %w",
			policy.table,
			policy.deleteTrigger,
			err,
		)
	}
	compactTriggerSQL := normalizeSQLiteSQLForComparison(triggerSQL)
	expectedDeleteTarget := "afterdeleteon" + canonicalSQLiteIdentifier(policy.sourceTable)
	expectedDeleteCommand := "insertinto" + canonicalSQLiteIdentifier(policy.table) +
		"(" + canonicalSQLiteIdentifier(policy.table) + ",rowid,content)" +
		"values('delete',old.rowid,old.content)"
	if !strings.Contains(compactTriggerSQL, expectedDeleteTarget) ||
		!strings.Contains(compactTriggerSQL, expectedDeleteCommand) {
		return fmt.Errorf(
			"FTS projection %q delete trigger %q does not remove projection rows transactionally",
			policy.table,
			policy.deleteTrigger,
		)
	}
	checkDeleteBehavior, exists := ftsProjectionDeleteChecks[canonicalSQLiteIdentifier(policy.table)]
	if !exists {
		return fmt.Errorf("FTS projection %q has no behavioral delete check", policy.table)
	}
	return checkDeleteBehavior(ctx, database)
}

func validateMessagesFTSDeleteBehavior(ctx context.Context, database *DB) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin messages_fts delete probe: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const (
		sessionID = "__schema_invariant_fts_session__"
		messageID = "__schema_invariant_fts_message__"
		token     = "schemainvariantftsprobe"
	)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sessions (id, title, status, created_at, updated_at)
		VALUES (?, 'schema invariant probe', 'active', datetime('now'), datetime('now'))`,
		sessionID,
	); err != nil {
		return fmt.Errorf("insert messages_fts probe session: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, 'user', ?, 'text', 1, datetime('now'))`,
		messageID,
		sessionID,
		token,
	); err != nil {
		return fmt.Errorf("insert messages_fts probe message: %w", err)
	}
	var indexed int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH ?`,
		token,
	).Scan(&indexed); err != nil {
		return fmt.Errorf("query inserted messages_fts probe: %w", err)
	}
	if indexed != 1 {
		return fmt.Errorf("FTS projection %q did not index its probe", "messages_fts")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, messageID); err != nil {
		return fmt.Errorf("delete messages_fts probe source: %w", err)
	}
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH ?`,
		token,
	).Scan(&indexed); err != nil {
		return fmt.Errorf("query deleted messages_fts probe: %w", err)
	}
	if indexed != 0 {
		return fmt.Errorf("FTS projection %q retained its probe after source deletion", "messages_fts")
	}
	return nil
}

func normalizeSQLiteSQLForComparison(sqlText string) string {
	return canonicalSQLiteIdentifier(strings.Join(strings.Fields(sqlText), ""))
}

func canonicalSQLiteIdentifier(identifier string) string {
	canonical := []byte(identifier)
	for index, value := range canonical {
		if value >= 'A' && value <= 'Z' {
			canonical[index] = value + ('a' - 'A')
		}
	}
	return string(canonical)
}

func applicationSchemaTableTypes(ctx context.Context, database *DB) (map[string]string, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA table_list")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tableTypes := make(map[string]string)
	for rows.Next() {
		var schemaName, tableName, tableType string
		var columnCount, withoutRowID, strict int
		if err := rows.Scan(
			&schemaName,
			&tableName,
			&tableType,
			&columnCount,
			&withoutRowID,
			&strict,
		); err != nil {
			return nil, err
		}
		if schemaName != "main" ||
			strings.HasPrefix(tableName, "sqlite_") ||
			(tableType != "table" && tableType != "virtual") {
			continue
		}
		tableTypes[canonicalSQLiteIdentifier(tableName)] = tableType
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tableTypes, nil
}

func findForeignKeyAncestor(
	ctx context.Context,
	database *DB,
	table string,
	candidates map[string]struct{},
	visited map[string]bool,
) (string, error) {
	table = canonicalSQLiteIdentifier(table)
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[table] {
		return "", nil
	}
	visited[table] = true

	parentTables, err := foreignKeyParentTables(ctx, database, table)
	if err != nil {
		return "", err
	}
	for _, parentTable := range parentTables {
		if _, candidate := candidates[parentTable]; candidate {
			return parentTable, nil
		}
		ancestor, err := findForeignKeyAncestor(ctx, database, parentTable, candidates, visited)
		if err != nil {
			return "", err
		}
		if ancestor != "" {
			return ancestor, nil
		}
	}
	return "", nil
}

func foreignKeyParentTables(ctx context.Context, database *DB, table string) ([]string, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("inspect foreign keys for %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	parentSet := make(map[string]struct{})
	for rows.Next() {
		var id, sequence int
		var referencedTable, fromColumn, onUpdate, onDelete, match string
		var toColumn sql.NullString
		if err := rows.Scan(
			&id,
			&sequence,
			&referencedTable,
			&fromColumn,
			&toColumn,
			&onUpdate,
			&onDelete,
			&match,
		); err != nil {
			return nil, fmt.Errorf("scan foreign key for %q: %w", table, err)
		}
		parentSet[canonicalSQLiteIdentifier(referencedTable)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate foreign keys for %q: %w", table, err)
	}
	parentTables := make([]string, 0, len(parentSet))
	for parentTable := range parentSet {
		parentTables = append(parentTables, parentTable)
	}
	sort.Strings(parentTables)
	return parentTables, nil
}

func tableHasCascadePath(
	ctx context.Context,
	database *DB,
	table string,
	sourceTable string,
	visited map[string]bool,
) (bool, error) {
	table = canonicalSQLiteIdentifier(table)
	sourceTable = canonicalSQLiteIdentifier(sourceTable)
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[table] {
		return false, nil
	}
	visited[table] = true

	cascadeParents, err := cascadeDeleteParentTables(ctx, database, table)
	if err != nil {
		return false, err
	}
	for _, referencedTable := range cascadeParents {
		if referencedTable == sourceTable {
			return true, nil
		}
		hasCascadePath, err := tableHasCascadePath(
			ctx,
			database,
			referencedTable,
			sourceTable,
			visited,
		)
		if err != nil {
			return false, err
		}
		if hasCascadePath {
			return true, nil
		}
	}
	return false, nil
}

func cascadeDeleteParentTables(ctx context.Context, database *DB, table string) ([]string, error) {
	nonNullColumns, err := explicitlyNonNullTableColumns(ctx, database, table)
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("inspect foreign keys for %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	type cascadeForeignKey struct {
		parent                  string
		allSourceColumnsNonNull bool
	}
	cascadeForeignKeys := make(map[int]*cascadeForeignKey)
	for rows.Next() {
		var id, sequence int
		var referencedTable, fromColumn, onUpdate, onDelete, match string
		var toColumn sql.NullString
		if err := rows.Scan(
			&id,
			&sequence,
			&referencedTable,
			&fromColumn,
			&toColumn,
			&onUpdate,
			&onDelete,
			&match,
		); err != nil {
			return nil, fmt.Errorf("scan foreign key for %q: %w", table, err)
		}
		if strings.EqualFold(onDelete, "CASCADE") {
			foreignKey, exists := cascadeForeignKeys[id]
			if !exists {
				foreignKey = &cascadeForeignKey{
					parent:                  canonicalSQLiteIdentifier(referencedTable),
					allSourceColumnsNonNull: true,
				}
				cascadeForeignKeys[id] = foreignKey
			}
			foreignKey.allSourceColumnsNonNull =
				foreignKey.allSourceColumnsNonNull && nonNullColumns[fromColumn]
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate foreign keys for %q: %w", table, err)
	}
	var cascadeParents []string
	for _, foreignKey := range cascadeForeignKeys {
		if foreignKey.allSourceColumnsNonNull {
			cascadeParents = append(cascadeParents, foreignKey.parent)
		}
	}
	sort.Strings(cascadeParents)
	return cascadeParents, nil
}

func explicitlyNonNullTableColumns(
	ctx context.Context,
	database *DB,
	table string,
) (map[string]bool, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA table_info("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return nil, fmt.Errorf("inspect columns for %q: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	nonNullColumns := make(map[string]bool)
	for rows.Next() {
		var columnID, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(
			&columnID,
			&columnName,
			&columnType,
			&notNull,
			&defaultValue,
			&primaryKey,
		); err != nil {
			return nil, fmt.Errorf("scan column for %q: %w", table, err)
		}
		nonNullColumns[columnName] = notNull == 1
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns for %q: %w", table, err)
	}
	return nonNullColumns, nil
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
