package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectArchiveAndPMOOrchestrationMigrations(t *testing.T) {
	for _, name := range []string{
		"800_project_archive.up.sql",
		"800_project_archive.down.sql",
		"801_pmo_orchestration.up.sql",
		"801_pmo_orchestration.down.sql",
	} {
		readMigrationFile(t, name)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("integration test requires Postgres at DATABASE_URL")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to Postgres: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire Postgres connection: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `
		CREATE TEMP TABLE project (id uuid PRIMARY KEY);
		CREATE TEMP TABLE pmo_sync_config (id uuid PRIMARY KEY);
	`); err != nil {
		t.Fatalf("create temporary pre-migration tables: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "800_project_archive.up.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "801_pmo_orchestration.up.sql")

	if _, err := conn.Exec(ctx, `SELECT archived_at, archived_by FROM project LIMIT 0`); err != nil {
		t.Fatalf("query project archive columns: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT orchestration_squad_id, orchestration_issue_id FROM pmo_sync_config LIMIT 0`); err != nil {
		t.Fatalf("query PMO orchestration columns: %v", err)
	}

	applyMigrationFile(t, ctx, conn.Conn(), "801_pmo_orchestration.down.sql")
	applyMigrationFile(t, ctx, conn.Conn(), "800_project_archive.down.sql")

	for _, query := range []string{
		`SELECT archived_at FROM project LIMIT 0`,
		`SELECT orchestration_squad_id FROM pmo_sync_config LIMIT 0`,
	} {
		if _, err := conn.Exec(ctx, query); err == nil {
			t.Fatalf("query unexpectedly succeeded after rollback: %s", query)
		}
	}
}
