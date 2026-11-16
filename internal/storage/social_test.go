package store

import (
	"context"
	"os"
	"testing"
)

func TestMemoryResolveNotifications(t *testing.T) {
	exerciseResolveNotifications(t, NewMemoryDB())
}

func TestPostgresResolveNotifications(t *testing.T) {
	url := os.Getenv("ANGELWARE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ANGELWARE_TEST_DATABASE_URL not set")
	}
	db, err := OpenPostgres(context.Background(), url)
	must(t, err)
	defer db.Close()
	exerciseResolveNotifications(t, db)
}

func exerciseResolveNotifications(t *testing.T, db Database) {
	t.Helper()
	ctx := context.Background()
	a, b := db.ForGame("resolve-a"), db.ForGame("resolve-b")
	for _, s := range []Store{a, b} {
		for _, m := range []Message{
			{To: 71, From: 72, Type: 1},
			{To: 71, From: 72, Type: 1},
			{To: 71, From: 72, Type: 5},
			{To: 71, From: 73, Type: 1},
			{To: 72, From: 71, Type: 1},
		} {
			_, err := s.Enqueue(ctx, m)
			must(t, err)
		}
	}
	must(t, a.ClearMessages(ctx, 71, 72, 1))
	must(t, a.ClearMessages(ctx, 71, 72, 1)) // idempotent
	msgs, err := a.Inbox(ctx, 71, 0, 100)
	must(t, err)
	if len(msgs) != 2 || msgs[0].Type != 5 || msgs[1].From != 73 {
		t.Fatal("resolution removed unrelated notifications or kept duplicates")
	}
	msgs, err = a.Inbox(ctx, 72, 0, 100)
	must(t, err)
	if len(msgs) != 1 {
		t.Fatal("resolution crossed recipients")
	}
	msgs, err = b.Inbox(ctx, 71, 0, 100)
	must(t, err)
	if len(msgs) != 4 {
		t.Fatal("resolution crossed game scope")
	}
}
