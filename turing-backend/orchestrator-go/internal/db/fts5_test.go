package db

import (
	"context"
	"testing"
)

func TestFTS5IsCompiledIn(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.ExecContext(context.Background(),
		`CREATE VIRTUAL TABLE probe USING fts5(content)`)
	if err != nil {
		t.Fatalf("FTS5 not available (missing -tags sqlite_fts5?): %v", err)
	}
}
