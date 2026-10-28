package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"finote/internal/assistant"
	"finote/internal/storage/postgres"

	_ "github.com/lib/pq"
)

func TestUpgradePreservesExpensesAndRegistersExistingOwners(t *testing.T) {
	db := migrationTestDB(t)
	old := fstest.MapFS{}
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "005" {
			continue
		}
		data, err := files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: data}
	}
	ctx := context.Background()
	if err = applyLegacyForTest(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO payments(id,user_id,amount,category,comment,spent_at,created_at,updated_at)
 VALUES('00000000-0000-4000-8000-000000000001','10',400,'еда','кофе',now(),now(),now())`); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal("migration must be idempotent", err)
	}
	var amount float64
	var version int64
	var name string
	if err = db.QueryRow("SELECT amount,version FROM payments").Scan(&amount, &version); err != nil || amount != 400 || version != 1 {
		t.Fatal(amount, version, err)
	}
	if err = db.QueryRow("SELECT display_name FROM users WHERE id='10'").Scan(&name); err != nil || name != "" {
		t.Fatal(name, err)
	}
	if _, err = db.Exec("UPDATE payments SET amount=500"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT version FROM payments").Scan(&version); err != nil || version != 2 {
		t.Fatal(version, err)
	}
	if _, err = db.Exec("DELETE FROM payments"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

// Reproduce the released migration journal, without keeping a legacy runner in production.
func applyLegacyForTest(ctx context.Context, db *sql.DB, source fs.FS) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE schema_migrations(name TEXT PRIMARY KEY,checksum TEXT NOT NULL,applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	names, err := fs.Glob(source, "*.sql")
	if err != nil {
		return err
	}
	for _, name := range names {
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)", name, fmt.Sprintf("%x", sha256.Sum256(body))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated FINOTE_TEST_DATABASE_URL required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}

	schema := fmt.Sprintf("upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })

	return db
}

func TestFreshAndConcurrentGooseStartup(t *testing.T) {
	db := migrationTestDB(t)
	errors := make(chan error, 3)
	var workers sync.WaitGroup
	for range 3 {
		workers.Add(1)
		go func() { defer workers.Done(); errors <- Apply(context.Background(), db) }()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM goose_db_version WHERE is_applied").Scan(&count); err != nil || count != 6 {
		t.Fatal(count, err)
	}
	var legacy bool
	if err := db.QueryRow("SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&legacy); err != nil || legacy {
		t.Fatal("new databases must only use Goose", err)
	}
}

func TestRejectDamagedLegacyHistory(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"checksum", "UPDATE schema_migrations SET checksum='modified' WHERE name='001_initial.sql'"},
		{"gap", "DELETE FROM schema_migrations WHERE name='002_indexes.sql'"},
		{"unknown", "INSERT INTO schema_migrations(name,checksum) VALUES('999_unknown.sql','unknown')"},
		{"schema", "ALTER TABLE users DROP COLUMN display_name"},
		{"trigger", "DROP TRIGGER payments_version ON payments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := migrationTestDB(t)
			if err := applyLegacyForTest(context.Background(), db, files); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if err := Apply(context.Background(), db); err == nil {
				t.Fatal("damaged legacy database accepted")
			}
			var created bool
			if err := db.QueryRow("SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&created); err != nil || created {
				t.Fatal("failed import must be atomic", err)
			}
		})
	}
}

func TestLegacyDialogsAndRepliesSurviveUpgrade(t *testing.T) {
	db := migrationTestDB(t)
	ctx := context.Background()
	if err := applyLegacyForTest(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	// Payloads and receipts have the exact shape written by the released code.
	_, err := db.Exec(`INSERT INTO telegram_dialogs VALUES('123','10','{"intent":{"action":"draft_create","category":"еда","comment":"кофе"},"choices":null}',now()+interval '10 minutes');
 INSERT INTO telegram_context VALUES('123','10','{"ids":["00000000-0000-4000-8000-000000000001"],"selected":"00000000-0000-4000-8000-000000000001"}',now()+interval '10 minutes');
 INSERT INTO telegram_updates VALUES('123',1,'Старый ответ');
 INSERT INTO telegram_outbox(bot_id,update_id,chat_id,response) VALUES('123',1,10,'Старый ответ');`)
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := postgres.New(db)
	service := assistant.New(nil, store, time.UTC)
	reply, err := service.Handle(ctx, assistant.Update{BotID: "123", UserID: "10", ID: 2}, "400,50")
	if err != nil || !strings.Contains(reply, "Расход добавлен") {
		t.Fatal(reply, err)
	}
	reply, err = service.Handle(ctx, assistant.Update{BotID: "123", UserID: "10", ID: 2}, "400,50")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM payments").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	queued, ok, err := store.NextReply(ctx, "123")
	if err != nil || !ok || queued.UpdateID != 1 || queued.Text != "Старый ответ" {
		t.Fatal(queued, ok, err)
	}
	// Restarting Goose does not rerun the old functions or import the journal again.
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM goose_db_version WHERE version_id=5 AND is_applied").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
