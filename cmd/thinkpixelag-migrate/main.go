// Command thinkpixelag-migrate applies the immutable, forward-only schema.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	postgresadapter "github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/jackc/pgx/v5"
)

var version = "dev"
var revision = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv("THINKPIXELAG_DATABASE_URL"), os.DirFS("/migrations")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "thinkpixelag-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, databaseURL string, sources fs.FS) error {
	if databaseURL == "" {
		return errors.New("THINKPIXELAG_DATABASE_URL is required")
	}
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return errors.New("connect to PostgreSQL")
	}
	defer connection.Close(context.Background())
	migrator, err := postgresadapter.NewMigrator(ctx, connection, sources)
	if err != nil {
		return err
	}
	if err := migrator.Up(ctx); err != nil {
		return err
	}
	current, err := migrator.Version(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "thinkpixelag schema migrated version=%d application_version=%s revision=%s\n", current, version, revision)
	return err
}
