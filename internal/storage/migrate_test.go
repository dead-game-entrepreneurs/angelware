package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLegacyMigrationPreservesWWE(t *testing.T) {
	url := os.Getenv("ANGELWARE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ANGELWARE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, url)
	must(t, err)
	defer root.Close()
	schema := fmt.Sprintf("legacy_test_%d", time.Now().UnixNano())
	_, err = root.Exec(ctx, "CREATE SCHEMA "+schema)
	must(t, err)
	defer root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	config, err := pgxpool.ParseConfig(url)
	must(t, err)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	must(t, err)
	defer pool.Close()
	sql, err := migrations.ReadFile("migrations/0001_legacy.sql")
	must(t, err)
	_, err = pool.Exec(ctx, string(sql))
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO accounts (user_id,name,password_tiger) VALUES (42,'legacy',decode('abcd','hex'));
 INSERT INTO profiles (user_id,public_a,public_b,public_data,private) VALUES (42,3,-2,decode('aaff','hex'),ARRAY[1,0,2,3]);
 INSERT INTO stats (user_id,board,flag,rating,vals) VALUES (42,1,0,100,ARRAY[5,6]);
 INSERT INTO friends (user_id,friend_id) VALUES (42,43);
 INSERT INTO storage_files (owner,name,flag,visible,data) VALUES (42,'legacy',true,true,decode('aaff','hex'))`)
	must(t, err)
	must(t, migrate(ctx, pool))
	must(t, migrate(ctx, pool))
	db := &PostgresDB{pool: pool}
	wwe := db.ForGame("svr2007")
	other := db.ForGame("outrun2006")
	account, err := wwe.Account(ctx, 42)
	must(t, err)
	if account.Name != "legacy" || string(account.PasswordTiger) != string([]byte{0xab, 0xcd}) {
		t.Fatal("account was not preserved")
	}
	record, err := wwe.Profile(ctx, 42, "public")
	must(t, err)
	var public struct {
		A, B int32
		Data []byte
	}
	must(t, json.Unmarshal(record.Payload, &public))
	if record.Version != 1 || public.A != 3 || public.B != -2 || string(public.Data) != string([]byte{0xaa, 0xff}) {
		t.Fatalf("public profile: %+v", public)
	}
	record, err = wwe.Profile(ctx, 42, "private")
	must(t, err)
	var private []int32
	must(t, json.Unmarshal(record.Payload, &private))
	if len(private) != 4 || private[2] != 2 {
		t.Fatal("private profile changed")
	}
	rows, err := wwe.StatsFrom(ctx, 1, 1, 10, false)
	must(t, err)
	if len(rows) != 1 || rows[0].Rating != 100 || len(rows[0].Values) != 2 {
		t.Fatal("stats changed")
	}
	friends, err := wwe.Friends(ctx, 42)
	must(t, err)
	if len(friends) != 1 || friends[0].UserID != 43 {
		t.Fatal("friends changed")
	}
	files, err := wwe.FilesByOwner(ctx, 42, true, 0, 10)
	must(t, err)
	if len(files) != 1 || !files[0].Flag {
		t.Fatal("files changed")
	}
	if account, err = other.Account(ctx, 42); err != nil || account.Name != "legacy" {
		t.Fatal("legacy identity unavailable to OutRun")
	}
	if _, err = other.Profile(ctx, 42, "private"); err != ErrNotFound {
		t.Fatal("legacy profile leaked to OutRun")
	}
	if total, err := other.StatsTotal(ctx, 1); err != nil || total != 0 {
		t.Fatal("legacy stats leaked to OutRun")
	}
}

func TestAccountUnificationRefusesConflictingPasswords(t *testing.T) {
	url := os.Getenv("ANGELWARE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ANGELWARE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, url)
	must(t, err)
	defer root.Close()
	schema := fmt.Sprintf("conflict_test_%d", time.Now().UnixNano())
	_, err = root.Exec(ctx, "CREATE SCHEMA "+schema)
	must(t, err)
	defer root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	config, err := pgxpool.ParseConfig(url)
	must(t, err)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	must(t, err)
	defer pool.Close()
	for _, name := range []string{"0001_legacy.sql", "0002_game_scopes.sql"} {
		sql, err := migrations.ReadFile("migrations/" + name)
		must(t, err)
		_, err = pool.Exec(ctx, string(sql))
		must(t, err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE angelware_migrations (version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now());
 INSERT INTO angelware_migrations (version) VALUES (1),(2);
 INSERT INTO accounts (game_id,user_id,name,password_tiger) VALUES ('svr2007',42,'same',decode('aa','hex')),('outrun2006',42,'same',decode('bb','hex'))`)
	must(t, err)
	if err = migrate(ctx, pool); err == nil {
		t.Fatal("conflicting passwords were silently merged")
	}
	var count, version int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count))
	must(t, pool.QueryRow(ctx, `SELECT max(version) FROM angelware_migrations`).Scan(&version))
	if count != 2 || version != 2 {
		t.Fatal("failed migration was not rolled back")
	}
}
