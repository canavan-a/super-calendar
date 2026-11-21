package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every migration file that hasn't been recorded yet, in
// filename order. Only pure database/sql + SQLite is used here rather than a
// migration framework, since the schema is small and this avoids a
// dependency for a handful of files.
func Migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var already string
		err := conn.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE name = ?`, name).Scan(&already)
		if err == nil {
			continue // already applied
		}
		if err != sql.ErrNoRows {
			return err
		}

		content, err := migrationFiles.ReadFile(path.Join("migrations", name))
		if err != nil {
			return err
		}
		up, _, _ := strings.Cut(string(content), "-- +goose Down")
		up = strings.Replace(up, "-- +goose Up", "", 1)

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(up) {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements splits a migration on ";", except inside a
// "-- +goose StatementBegin" … "-- +goose StatementEnd" block (a trigger body
// has semicolons of its own), which stays one statement.
func splitStatements(sqlText string) []string {
	sqlText = stripComments(sqlText)
	var out []string
	add := func(stmt string) {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			out = append(out, stmt)
		}
	}
	for i, part := range strings.Split(sqlText, "-- +goose StatementBegin") {
		if i > 0 {
			block, rest, _ := strings.Cut(part, "-- +goose StatementEnd")
			add(block)
			part = rest
		}
		for _, stmt := range strings.Split(part, ";") {
			add(stmt)
		}
	}
	return out
}

// stripComments drops "--" comments (which may contain ";"), keeping the
// goose markers. Migrations don't put "--" inside string literals.
func stripComments(sqlText string) string {
	lines := strings.Split(sqlText, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "-- +goose") {
			continue
		}
		if j := strings.Index(line, "--"); j >= 0 {
			lines[i] = line[:j]
		}
	}
	return strings.Join(lines, "\n")
}
