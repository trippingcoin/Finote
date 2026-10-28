// Package migrations runs embedded, forward-only migrations through Goose.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql sql
var files embed.FS

const migrationLockID int64 = 732418590121

func Apply(ctx context.Context, db *sql.DB) error {
	if err := importLegacyHistory(ctx, db); err != nil {
		return fmt.Errorf("import legacy migration history: %w", err)
	}
	provider, err := newProvider(db)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func newProvider(db *sql.DB) (*goose.Provider, error) {
	// Preserve original SQL bytes and checksums. Goose, not this adapter, handles
	// execution order, per-migration transactions, versioning and recovery.
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	legacy := make([]*goose.Migration, 0, len(names))
	for _, name := range names {
		prefix, _, _ := strings.Cut(name, "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || version < 1 || version > 5 {
			return nil, fmt.Errorf("invalid legacy migration: %s", name)
		}
		body, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		legacy = append(legacy, goose.NewGoMigration(version, &goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, string(body))
			return err
		}}, nil))
	}
	source, err := fs.Sub(files, "sql")
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(migrationLockID), lock.WithLockTimeout(1, 30))
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, source,
		goose.WithGoMigrations(legacy...), goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
}
