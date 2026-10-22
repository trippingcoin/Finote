package migrations

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/lib/pq"
	"io/fs"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"
)

func TestUpgradePreservesExpensesAndRegistersExistingOwners(t *testing.T) {
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated FINOTE_TEST_DATABASE_URL required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("upgrade_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
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
	defer db.Close()
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
	if err = apply(ctx, db, old); err != nil {
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
