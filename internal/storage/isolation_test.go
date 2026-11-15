package store

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestMemoryGameIsolation(t *testing.T) { exerciseGameIsolation(t, NewMemoryDB()) }
func TestPostgresGameIsolation(t *testing.T) {
	url := os.Getenv("ANGELWARE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ANGELWARE_TEST_DATABASE_URL not set")
	}
	db, err := OpenPostgres(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exerciseGameIsolation(t, db)
}
func exerciseGameIsolation(t *testing.T, db Database) {
	t.Helper()
	ctx := context.Background()
	a, b := db.ForGame("isolation-a"), db.ForGame("isolation-b")
	for _, s := range []Store{a, b} {
		s.DeleteAccount(ctx, 7)
		s.DeleteAccount(ctx, 8)
	}
	must(t, a.CreateAccount(ctx, Account{UserID: 7, Name: "same-name", PasswordTiger: []byte{42}}))
	must(t, a.CreateAccount(ctx, Account{UserID: 8, Name: "friend", PasswordTiger: []byte{}}))
	if err := b.CreateAccount(ctx, Account{UserID: 7, Name: "same-name", PasswordTiger: []byte{99}}); !errors.Is(err, ErrExists) {
		t.Fatal("duplicate unified account accepted")
	}
	for i, s := range []Store{a, b} {
		must(t, s.SetProfile(ctx, 7, "private", Profile{Version: 1, Payload: []byte{byte(i)}}))
		must(t, s.WriteStats(ctx, 7, StatsRow{Board: 1, Rating: int64(10 + i)}))
		must(t, s.WriteStats(ctx, 8, StatsRow{Board: 1, Rating: 5}))
		must(t, s.AddFriendship(ctx, 7, 8))
		_, err := s.AddFriendRequest(ctx, 8, 7, nil)
		must(t, err)
		must(t, s.SetBlock(ctx, 7, 8, uint8(i+1)))
	}
	for i, s := range []Store{a, b} {
		account, err := s.Account(ctx, 7)
		must(t, err)
		if len(account.PasswordTiger) != 1 || account.PasswordTiger[0] != 42 {
			t.Fatal("shared credential directory differs by game")
		}
		profile, err := s.Profile(ctx, 7, "private")
		must(t, err)
		if profile.Payload[0] != byte(i) {
			t.Fatal("profile crossed games")
		}
		friends, err := s.Friends(ctx, 7)
		must(t, err)
		if len(friends) != 1 || friends[0].Name != "friend" {
			t.Fatal("social join crossed games")
		}
		requests, err := s.FriendRequestsTo(ctx, 7)
		must(t, err)
		if len(requests) != 1 || requests[0].Name != friends[0].Name {
			t.Fatal("request join crossed games")
		}
		blocks, err := s.Blocks(ctx, 7)
		must(t, err)
		if len(blocks) != 1 || blocks[0].Level != uint8(i+1) {
			t.Fatal("blocks crossed games")
		}
		for _, asc := range []bool{false, true} {
			rows, err := s.StatsFrom(ctx, 1, 1, 20, asc)
			must(t, err)
			want := uint64(7)
			if asc {
				want = 8
			}
			if len(rows) != 2 || rows[0].UserID != want {
				t.Fatalf("rank order or scope: %+v", rows)
			}
		}
	}
	mid, err := a.Enqueue(ctx, Message{From: 7, To: 8, Type: 1})
	must(t, err)
	must(t, b.Ack(ctx, 8, mid))
	inbox, err := a.Inbox(ctx, 8, 0, 20)
	must(t, err)
	if len(inbox) != 1 {
		t.Fatal("cross-game ack removed notification")
	}
	if inbox, err = b.Inbox(ctx, 8, 0, 20); err != nil || len(inbox) != 0 {
		t.Fatal("mailbox crossed games")
	}
	fid, err := a.CreateFile(ctx, File{Owner: 7, Name: "ghost", Visible: true, Data: []byte{1}})
	must(t, err)
	if _, err = b.File(ctx, fid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-game file read: %v", err)
	}
	if changed, err := b.UpdateFile(ctx, 7, fid, []byte{2}, nil, false); err != nil || changed {
		t.Fatal("cross-game file update")
	}
	must(t, b.DeleteFile(ctx, 7, fid))
	if _, err = a.File(ctx, fid); err != nil {
		t.Fatal("cross-game file delete")
	}
	// Gameplay mutations stay local even for a shared account.
	must(t, a.DeleteProfile(ctx, 7))
	must(t, a.DeleteStats(ctx, 7, 1))
	must(t, a.RemoveFriendship(ctx, 7, 8))
	if _, err = b.Profile(ctx, 7, "private"); err != nil {
		t.Fatal("profile deletion crossed games")
	}
	if count, err := b.StatsTotal(ctx, 1); err != nil || count != 2 {
		t.Fatal("stats deletion crossed games")
	}
	if friends, err := b.Friends(ctx, 7); err != nil || len(friends) != 1 {
		t.Fatal("social deletion crossed games")
	}
	must(t, a.SetPassword(ctx, 7, []byte{99}))
	if got, err := b.Account(ctx, 7); err != nil || got.PasswordTiger[0] != 99 {
		t.Fatal("password update is not shared")
	}
	// Authenticated account deletion intentionally removes the global identity.
	must(t, a.DeleteAccount(ctx, 7))
	for _, s := range []Store{a, b} {
		if _, err := s.Account(ctx, 7); !errors.Is(err, ErrNotFound) {
			t.Fatal("shared identity survived account deletion")
		}
		if _, err := s.Profile(ctx, 7, "private"); !errors.Is(err, ErrNotFound) {
			t.Fatal("account-owned profile survived deletion")
		}
	}
	must(t, a.DeleteAccount(ctx, 8))

}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
