package storage

import (
	"context"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"

	ruleservice "predixaai-backend/services/rule-service"
)

// TestMigrationsFS_ContainsExpectedFiles verifies that the embedded migrations FS
// is non-empty and contains the known required migration files.
// This is a compile-time + runtime contract test: if a migration file is removed
// or the embed path breaks, this test fails immediately.
func TestMigrationsFS_ContainsExpectedFiles(t *testing.T) {
	entries, err := fs.Glob(ruleservice.MigrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("fs.Glob failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("embedded migrations FS is empty — embed path is likely broken")
	}

	required := []string{
		"001_create_db_connections.sql",
		"006_create_machine_units.sql",
		"008_create_ui_rules.sql",
		"009_add_machine_unit_timestamp_column.sql",
	}
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		// entries are like "migrations/001_create_db_connections.sql"
		set[e[len("migrations/"):]] = true
	}
	for _, name := range required {
		if !set[name] {
			t.Errorf("required migration %q not found in embedded FS; found: %v", name, entries)
		}
	}
}

// TestMigrationsFS_FilesAreSorted verifies that the embedded file names are already
// in lexicographic order so RunMigrations applies them in the right sequence.
func TestMigrationsFS_FilesAreSorted(t *testing.T) {
	entries, err := fs.Glob(ruleservice.MigrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("fs.Glob failed: %v", err)
	}
	sorted := make([]string, len(entries))
	copy(sorted, entries)
	sort.Strings(sorted)
	for i := range entries {
		if entries[i] != sorted[i] {
			t.Errorf("entries not in sorted order at index %d: got %q want %q", i, entries[i], sorted[i])
		}
	}
}

// TestMigrationsFS_FilesAreReadable verifies that every embedded SQL file can be
// read and is non-empty.
func TestMigrationsFS_FilesAreReadable(t *testing.T) {
	entries, err := fs.Glob(ruleservice.MigrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatalf("fs.Glob failed: %v", err)
	}
	for _, entry := range entries {
		content, err := ruleservice.MigrationsFS.ReadFile(entry)
		if err != nil {
			t.Errorf("failed to read %s: %v", entry, err)
			continue
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			t.Errorf("migration file %s is empty", entry)
		}
	}
}

// TestRunMigrations_Idempotent verifies that running migrations twice does not
// return an error (all migrations use IF NOT EXISTS guards).
// Requires TEST_DATABASE_URL or DATABASE_URL to be set; skipped otherwise.
func TestRunMigrations_Idempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL not set")
	}

	store, err := NewStore(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// First run
	if err := store.RunMigrations(context.Background()); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	// Second run — must be idempotent
	if err := store.RunMigrations(context.Background()); err != nil {
		t.Errorf("second RunMigrations (idempotency check) failed: %v", err)
	}
}

