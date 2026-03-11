package db

import (
	"path/filepath"
	"testing"
)

func TestRunMigrationsFallsBackToEmbeddedWhenDirMissing(t *testing.T) {
	tmp := t.TempDir()
	database, err := Open(filepath.Join(tmp, "agent.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = database.Close() }()

	missingDir := filepath.Join(tmp, "does-not-exist")
	if err := RunMigrations(database, missingDir); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}

	var count int
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='owners'`).Scan(&count); err != nil {
		t.Fatalf("owners table check error = %v", err)
	}
	if count != 1 {
		t.Fatalf("owners table not created")
	}

	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='agents'`).Scan(&count); err != nil {
		t.Fatalf("agents table check error = %v", err)
	}
	if count != 1 {
		t.Fatalf("agents table not created")
	}
}
