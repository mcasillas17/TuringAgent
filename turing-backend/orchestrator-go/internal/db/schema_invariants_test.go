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
		if _, exists := policyByTable[policy.table]; exists {
			return fmt.Errorf("application table %q has duplicate derived-state policies", policy.table)
		}
		policyByTable[policy.table] = policy
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
		if _, exists := tableTypes[policy.table]; !exists && policy.presence != schemaTableOptional {
			return fmt.Errorf("derived-state policy names absent application table %q", policy.table)
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
			if policy.sourceTable == policy.table {
				return fmt.Errorf(
					"derived table %q cannot declare itself as its source",
					policy.table,
				)
			}
			if _, exists := tableTypes[policy.sourceTable]; !exists {
				return fmt.Errorf(
					"derived table %q declares absent source table %q",
					policy.table,
					policy.sourceTable,
				)
			}
			hasCascadePath, err := tableHasCascadePath(ctx, database, policy.table, policy.sourceTable, nil)
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
			if tableTypes[policy.table] != "virtual" {
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
			if _, approved := approvedScrubbedExceptionTables[policy.table]; !approved {
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
		`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = ?`,
		policy.table,
	).Scan(&tableSQL); err != nil {
		return fmt.Errorf("inspect FTS projection %q: %w", policy.table, err)
	}
	compactTableSQL := normalizeSQLiteSQLForComparison(tableSQL)
	expectedContent := "content='" + strings.ToLower(policy.sourceTable) + "'"
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
		`SELECT sql FROM sqlite_schema WHERE type = 'trigger' AND name = ?`,
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
	expectedDeleteTarget := "afterdeleteon" + strings.ToLower(policy.sourceTable)
	expectedDeleteCommand := "insertinto" + strings.ToLower(policy.table) +
		"(" + strings.ToLower(policy.table) + ",rowid,content)" +
		"values('delete',old.rowid,old.content)"
	if !strings.Contains(compactTriggerSQL, expectedDeleteTarget) ||
		!strings.Contains(compactTriggerSQL, expectedDeleteCommand) {
		return fmt.Errorf(
			"FTS projection %q delete trigger %q does not remove projection rows transactionally",
			policy.table,
			policy.deleteTrigger,
		)
	}
	checkDeleteBehavior, exists := ftsProjectionDeleteChecks[policy.table]
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
	return strings.ToLower(strings.Join(strings.Fields(sqlText), ""))
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
		tableTypes[tableName] = tableType
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tableTypes, nil
}

func tableHasCascadePath(
	ctx context.Context,
	database *DB,
	table string,
	sourceTable string,
	visited map[string]bool,
) (bool, error) {
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
					parent:                  referencedTable,
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
