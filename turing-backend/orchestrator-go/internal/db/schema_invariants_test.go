package db

import (
	"context"
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
	{
		table:         "messages_fts",
		kind:          schemaTableFTSProjection,
		sourceTable:   "messages",
		deleteTrigger: "messages_fts_ad",
	},
	{table: "skills", kind: schemaTableIndependent},
	{table: "session_skills", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "external_agents", kind: schemaTableIndependent},
	{table: "session_external_agent", kind: schemaTableCascadeOwned, sourceTable: "sessions"},
	{table: "integration_connections", kind: schemaTableIndependent},
	{table: "automations", kind: schemaTableIndependent},
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
			policies[index].justification = ""
		}
	}

	err := validateDerivedStateSchema(ctx, database, policies)
	if err == nil || !strings.Contains(err.Error(), `scrubbed exception "audit_logs" requires an explicit justification`) {
		t.Fatalf("validateDerivedStateSchema error = %v, want missing scrubbed-exception justification", err)
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
		if _, exists := tableTypes[policy.table]; !exists {
			return fmt.Errorf("derived-state policy names absent application table %q", policy.table)
		}
	}

	for _, policy := range policies {
		switch policy.kind {
		case schemaTableIndependent:
		case schemaTableCascadeOwned:
			hasCascadePath, err := tableHasCascadePath(ctx, database, policy.table, policy.sourceTable, nil)
			if err != nil {
				return err
			}
			if !hasCascadePath {
				return fmt.Errorf(
					"derived table %q has no ON DELETE CASCADE path to declared source %q",
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
			if strings.TrimSpace(policy.justification) == "" {
				return fmt.Errorf(
					"scrubbed exception %q requires an explicit justification",
					policy.table,
				)
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
			"FTS projection %q delete trigger %q does not delete source rows transactionally",
			policy.table,
			policy.deleteTrigger,
		)
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
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdentifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cascadeParents []string
	for rows.Next() {
		var id, sequence int
		var referencedTable, fromColumn, toColumn, onUpdate, onDelete, match string
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
			return nil, err
		}
		if strings.EqualFold(onDelete, "CASCADE") {
			cascadeParents = append(cascadeParents, referencedTable)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cascadeParents, nil
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
