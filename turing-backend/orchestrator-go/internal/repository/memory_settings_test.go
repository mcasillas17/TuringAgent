package repository

import (
	"context"
	"errors"
	"testing"
)

// Memory is on out of the box. The absence of a row is a real answer — a fresh
// install has never been asked — and it must not be confused with "off".
func TestMemoryEnabledDefaultsOn(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	enabled, err := repo.MemoryEnabled(ctx)
	if err != nil {
		t.Fatalf("MemoryEnabled: %v", err)
	}
	if !enabled {
		t.Fatalf("memory is off on a fresh database, want on")
	}
	var rows int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key = ?`, "memory_enabled").Scan(&rows); err != nil {
		t.Fatalf("count settings rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("reading the toggle wrote %d settings rows, want a read to stay a read", rows)
	}
}

// The setter reports whether anything actually changed, because the caller
// that has to notify a registry must not announce a change that never happened.
func TestSetMemoryEnabledReportsAndPersistsTheChange(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	changed, err := repo.SetMemoryEnabled(ctx, false)
	if err != nil {
		t.Fatalf("SetMemoryEnabled(false): %v", err)
	}
	if !changed {
		t.Fatalf("turning memory off reported no change")
	}
	changed, err = repo.SetMemoryEnabled(ctx, false)
	if err != nil {
		t.Fatalf("SetMemoryEnabled(false) again: %v", err)
	}
	if changed {
		t.Fatalf("setting the toggle to what it already was reported a change")
	}
	enabled, err := repo.MemoryEnabled(ctx)
	if err != nil {
		t.Fatalf("MemoryEnabled: %v", err)
	}
	if enabled {
		t.Fatalf("memory is on after being turned off")
	}

	// A second repository over the same database is what a restart looks like.
	if enabled, err := New(database).MemoryEnabled(ctx); err != nil || enabled {
		t.Fatalf("after a restart MemoryEnabled = (%v, %v), want (false, nil)", enabled, err)
	}

	changed, err = repo.SetMemoryEnabled(ctx, true)
	if err != nil {
		t.Fatalf("SetMemoryEnabled(true): %v", err)
	}
	if !changed {
		t.Fatalf("turning memory back on reported no change")
	}
}

// Tools re-report themselves on every start. A registry refresh is about which
// tools exist, and it must never decide for the user whether memory is on.
func TestToolReportingDoesNotOverwriteTheMemoryToggle(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	if _, err := repo.SetMemoryEnabled(ctx, false); err != nil {
		t.Fatalf("SetMemoryEnabled(false): %v", err)
	}
	for range 2 {
		if err := repo.UpsertTools(ctx, []DiscoveredTool{{
			ServerName: "memory",
			ToolName:   "memory.search",
			SchemaJSON: `{"type":"object"}`,
			Policy:     "safe",
		}}); err != nil {
			t.Fatalf("UpsertTools: %v", err)
		}
	}
	enabled, err := repo.MemoryEnabled(ctx)
	if err != nil {
		t.Fatalf("MemoryEnabled: %v", err)
	}
	if enabled {
		t.Fatalf("re-reporting the memory tools turned the toggle back on")
	}
}

// A settings value is stored data, and stored data is not trusted. A value
// that is not one of the two booleans is reported rather than guessed at.
func TestMemoryEnabledRefusesACorruptStoredValue(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()

	if _, err := database.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, updated_at) VALUES (?, ?, ?)
	`, "memory_enabled", `"maybe"`, now()); err != nil {
		t.Fatalf("seed corrupt setting: %v", err)
	}
	if _, err := repo.MemoryEnabled(ctx); !errors.Is(err, ErrMemorySettingCorrupt) {
		t.Fatalf("MemoryEnabled error = %v, want ErrMemorySettingCorrupt", err)
	}
	// Setting it heals the row rather than leaving the user stuck.
	if _, err := repo.SetMemoryEnabled(ctx, true); err != nil {
		t.Fatalf("SetMemoryEnabled(true): %v", err)
	}
	if enabled, err := repo.MemoryEnabled(ctx); err != nil || !enabled {
		t.Fatalf("MemoryEnabled after healing = (%v, %v), want (true, nil)", enabled, err)
	}
}
