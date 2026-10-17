package migrations

import (
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed storage/*.sql frontier/*.sql index/*.sql
var FS embed.FS

// Run applies all pending goose migrations from the specified subdir ("storage", "frontier", or "index").
func Run(pool *pgxpool.Pool, subdir string) error {
	goose.SetBaseFS(FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrations: set dialect: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := goose.Up(db, subdir); err != nil {
		return fmt.Errorf("migrations: up %s: %w", subdir, err)
	}
	return nil
}
