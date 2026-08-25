package repository

import (
	"context"
	"database/sql"
	"errors"
)

// memoryEnabledSettingKey is the one settings row that says whether Turing may
// remember anything at all. It is deliberately not seeded at install time: a
// missing row means "never asked", which reads as on, and writing a row on
// first read would make a later default change silently unreachable for every
// existing user.
const memoryEnabledSettingKey = "memory_enabled"

const (
	memorySettingTrue  = "true"
	memorySettingFalse = "false"
)

// ErrMemorySettingCorrupt reports a stored toggle that is neither boolean. The
// value is not echoed: it is stored data, and an error message is not a safe
// place to print a row's contents.
var ErrMemorySettingCorrupt = errors.New("stored memory setting is not a boolean")

// MemoryEnabled reports whether memory is on. A missing row is on, because
// memory ships enabled and the user has not turned it off.
func (r *Repository) MemoryEnabled(ctx context.Context) (bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	enabled, err := memoryEnabledTx(ctx, tx)
	if err != nil {
		return false, err
	}
	return enabled, tx.Commit()
}

// SetMemoryEnabled writes the toggle and reports whether it actually moved, so
// a caller that has to republish the memory tools can tell a real change from
// a repeated one. The read and the write share one transaction: two callers
// flipping the toggle at once must not both be told they were the one who
// changed it.
func (r *Repository) SetMemoryEnabled(ctx context.Context, enabled bool) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	previous, err := memoryEnabledTx(ctx, tx)
	// A corrupt stored value is not a reason to refuse the write that repairs
	// it. It is a change by definition, because nothing valid was there.
	changed := true
	switch {
	case errors.Is(err, ErrMemorySettingCorrupt):
	case err != nil:
		return false, err
	default:
		changed = previous != enabled
	}

	value := memorySettingFalse
	if enabled {
		value = memorySettingTrue
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
	`, memoryEnabledSettingKey, value, now()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func memoryEnabledTx(ctx context.Context, q rowQuerier) (bool, error) {
	var value string
	err := q.QueryRowContext(ctx,
		`SELECT value_json FROM settings WHERE key = ?`, memoryEnabledSettingKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	switch value {
	case memorySettingTrue:
		return true, nil
	case memorySettingFalse:
		return false, nil
	default:
		return false, ErrMemorySettingCorrupt
	}
}
