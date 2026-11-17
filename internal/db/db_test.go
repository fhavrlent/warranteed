package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Warranties linked to the same Wherehouse thing before a thing became one
// warranty's: the first keeps it, the others are unlinked.
func TestOneThingPerWarrantyMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	conn, err := raw.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := fs.Glob(migrations, "migrations/*.sql")
	sort.Strings(files)
	for i, f := range files {
		if strings.HasPrefix(strings.TrimPrefix(f, "migrations/"), "0025") {
			break
		}
		body, _ := migrations.ReadFile(f)
		if err := applyMigration(ctx, conn, i+1, string(body)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	conn.Close()
	for _, q := range []string{
		`INSERT INTO users (id, issuer, subject) VALUES (1, 'test', 'alice')`,
		`INSERT INTO warranties (id, user_id, space_id, product_name, purchase_date, wherehouse_thing) VALUES
			(1, 1, 1, 'A', '2025-01-01', 5), (2, 1, 1, 'B', '2025-01-01', 5), (3, 1, 1, 'C', '2025-01-01', 6)`,
		`INSERT INTO claims (id, warranty_id, title, reported_on, wherehouse_entry) VALUES (1, 2, 'Broken', '2025-02-01', 9)`,
	} {
		if _, err := raw.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got := map[int64]any{}
	rows, err := db.QueryContext(ctx, `SELECT id, wherehouse_thing FROM warranties ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		var thing sql.NullInt64
		rows.Scan(&id, &thing)
		got[id] = thing.Int64
	}
	rows.Close()
	if fmt.Sprint(got) != "map[1:5 2:0 3:6]" {
		t.Errorf("things = %v", got)
	}
	var entry sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT wherehouse_entry FROM claims WHERE id = 1`).Scan(&entry); err != nil || entry.Valid {
		t.Errorf("unlinked warranty's claim entry = %v, %v", entry, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE warranties SET wherehouse_thing = 5 WHERE id = 2`); err == nil {
		t.Error("a second warranty linked to a thing")
	}
}
