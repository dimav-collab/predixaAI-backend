package storage

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	ruleservice "predixaai-backend/services/rule-service"
)

// RunMigrations applies every *.sql file from the embedded migrations FS in
// lexicographic order. All migrations use IF NOT EXISTS guards so re-running
// is idempotent for schema-level changes.
func (s *Store) RunMigrations(ctx context.Context) error {
	entries, err := fs.Glob(ruleservice.MigrationsFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)
	for _, entry := range entries {
		content, err := ruleservice.MigrationsFS.ReadFile(entry)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry, err)
		}
		if _, err := s.Pool.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry, err)
		}
	}
	return nil
}
