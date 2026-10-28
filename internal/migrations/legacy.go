package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3/database"
)

// One-time compatibility bridge, never a second migration runner. Refuse unknown,
// changed or incomplete history rather than assuming an existing DB is current.
func importLegacyHistory(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return err
	}
	var legacy, goose bool
	if err = tx.QueryRowContext(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL, to_regclass('goose_db_version') IS NOT NULL").Scan(&legacy, &goose); err != nil {
		return err
	}
	if !legacy || goose {
		return tx.Commit()
	}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT name,checksum FROM schema_migrations ORDER BY name")
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var name, checksum string
		if err = rows.Scan(&name, &checksum); err != nil {
			rows.Close()
			return err
		}
		if count >= len(names) || names[count] != name {
			rows.Close()
			return fmt.Errorf("unknown or incomplete legacy history at %s", name)
		}
		body, readErr := files.ReadFile(name)
		if readErr != nil {
			rows.Close()
			return readErr
		}
		if fmt.Sprintf("%x", sha256.Sum256(body)) != checksum {
			rows.Close()
			return fmt.Errorf("migration %s changed after application", name)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if err = validateLegacySchema(ctx, tx, count); err != nil {
		return err
	}
	store, err := database.NewStore(database.DialectPostgres, "goose_db_version")
	if err != nil {
		return err
	}
	if err = store.CreateVersionTable(ctx, tx); err != nil {
		return err
	}
	for version := 0; version <= count; version++ {
		if err = store.Insert(ctx, tx, database.InsertRequest{Version: int64(version)}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateLegacySchema(ctx context.Context, tx *sql.Tx, version int) error {
	checks := []struct {
		version int
		query   string
	}{
		{1, "SELECT id,user_id,amount,category,spent_at,comment,created_at,updated_at FROM payments LIMIT 0"},
		{1, "SELECT bot_id,update_id,response FROM telegram_updates LIMIT 0"},
		{1, "SELECT bot_id,user_id,payload,expires_at FROM telegram_dialogs LIMIT 0"},
		{3, "SELECT bot_id,update_id,chat_id,response,status,attempts,available_at,last_error FROM telegram_outbox LIMIT 0"},
		{4, "SELECT bot_id,user_id,payload,expires_at FROM telegram_context LIMIT 0"},
		{5, "SELECT version FROM payments LIMIT 0"},
		{5, "SELECT id,display_name,username,updated_at FROM users LIMIT 0"},
	}
	for _, check := range checks {
		if version < check.version {
			continue
		}
		rows, err := tx.QueryContext(ctx, check.query)
		if err != nil {
			return fmt.Errorf("legacy schema does not match version %d: %w", version, err)
		}
		rows.Close()
	}
	if version >= 5 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger WHERE tgrelid='payments'::regclass AND tgname IN ('payments_version','payments_user') AND NOT tgisinternal AND tgenabled='O'`).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			return fmt.Errorf("legacy schema is missing payment triggers")
		}
	}
	return nil
}
