package server

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/games/stacked"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/crypto"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func TestStackedFriendsMailboxAndInvitation(t *testing.T) {
	s, db := multiTCP(t)
	for _, name := range []string{"stacked-host", "stacked-guest"} {
		if _, code := status(t, dial(t, s).roundTrip(signupFor(stacked.TitleID, name, "password"))); code != 700 {
			t.Fatal("signup failed")
		}
	}
	host := loginGatewayFor(t, s, stacked.TitleID, "stacked-host", "password")
	guest := loginGatewayFor(t, s, stacked.TitleID, "stacked-guest", "password")
	host.signedStatus, guest.signedStatus = true, true
	ctx := bitbuf.Int(bitbuf.U8, 0)
	poll := bitbuf.Int(bitbuf.Bool, 0)
	hostID := dwcrypto.UserID([]byte("stacked-host"))
	guestID := dwcrypto.UserID([]byte("stacked-guest"))
	st := db.ForGame(stacked.Key)
	host.task(8, 1, 2, ctx, bitbuf.Int(bitbuf.U64, guestID))
	if _, code := host.taskStatus(8, 2, 2, ctx, bitbuf.Int(bitbuf.U64, 999)); code != 101 {
		t.Fatal("unknown proposal target did not return native user-not-found")
	}
	for range 2 {
		host.task(9, 3, 1, ctx, bitbuf.Int(bitbuf.U64, guestID))
	}
	f, err := guest.task(6, 4, 1, ctx, poll).Fields()
	if err != nil || len(f) != 8 || f[0].Type != bitbuf.I32 || f[0].Int != 1 || f[1].Type != bitbuf.I32 || f[1].Int != 1 || f[6].Int != hostID || string(f[7].Bytes) != "stacked-host" {
		t.Fatalf("older friend request or duplicate filtering failed: %v", err)
	}
	guest.task(9, 5, 2, ctx, bitbuf.Int(bitbuf.U64, hostID), bitbuf.Int(bitbuf.U8, 1))
	// Acceptance removes the proposal even if its separate ack was lost.
	f, err = guest.task(6, 6, 1, ctx, poll).Fields()
	if err != nil || len(f) != 1 || f[0].Int != 0 {
		t.Fatal("accepted proposal remained in the inbox")
	}
	f, err = guest.task(9, 7, 4, ctx).Fields()
	if err != nil || len(f) != 4 || f[0].Type != bitbuf.I32 || f[0].Int != 1 || f[1].Int != hostID || string(f[2].Bytes) != "stacked-host" || f[3].Type != bitbuf.I32 || f[3].Int != 1 {
		t.Fatal("older task 4 list or signed presence failed")
	}
	room := []byte("12345678")
	for range 2 {
		host.task(9, 8, 7, ctx, bitbuf.Int(bitbuf.U64, guestID), bitbuf.Bytes(bitbuf.Blob, room))
	}
	f, err = guest.task(6, 9, 1, ctx, poll).Fields()
	if err != nil || len(f) != 9 || f[0].Int != 1 || f[1].Int != 6 || f[8].Type != bitbuf.Blob || !bytes.Equal(f[8].Bytes, room) {
		t.Fatal("older invitation or duplicate filtering failed")
	}
	guest.task(9, 10, 8, ctx, bitbuf.Int(bitbuf.U64, hostID), bitbuf.Int(bitbuf.U8, 1))
	msgs, err := st.Inbox(context.Background(), guestID, 0, 10)
	if err != nil || len(msgs) != 0 {
		t.Fatal("answered invitation remained queued")
	}
	// This SDK cannot decode session answers or presence notifications.
	msgs, err = st.Inbox(context.Background(), hostID, 0, 10)
	if err != nil || len(msgs) != 1 || msgs[0].Type != 2 {
		t.Fatal("unsupported session answer was queued")
	}
	other, err := db.ForGame(svr2007.Key).Friends(context.Background(), guestID)
	if err != nil || len(other) != 0 {
		t.Fatal("Stacked friendship leaked into another game")
	}
}

func TestStackedCreateUserLoginStartsWithUnknownAccountCheck(t *testing.T) {
	s, _ := multiTCP(t)
	// The retail UI first tries login. 0x1f3c48 maps 704 to state 0x24,
	// which 0x1ffe68 uses to start signup; 701 only displays a generic error.
	r := dial(t, s).roundTrip(loginFor(stacked.TitleID, "new-stacked"))
	if _, code := status(t, r); r.Type != 0x0b || code != 704 {
		t.Fatal("missing account did not select Stacked's create-user flow")
	}
	if _, code := status(t, dial(t, s).roundTrip(signupFor(stacked.TitleID, "new-stacked", "password"))); code != 700 {
		t.Fatal("signup failed after checking availability")
	}
	c := loginGatewayFor(t, s, stacked.TitleID, "new-stacked", "password")
	c.signedStatus = true
	c.task(33, 1, 6, bitbuf.Int(bitbuf.U64, dwcrypto.UserID([]byte("new-stacked"))))
	if _, code := status(t, dial(t, s).roundTrip(signupFor(stacked.TitleID, "new-stacked", "other-password"))); code != 707 {
		t.Fatal("duplicate signup did not return username-exists")
	}
}

func TestStackedStartupMTTAndMailbox(t *testing.T) {
	s, db := multiTCP(t)
	if _, code := status(t, dial(t, s).roundTrip(signupFor(stacked.TitleID, "mtt-stacked", "password"))); code != 700 {
		t.Fatal("signup failed")
	}
	c := loginGatewayFor(t, s, stacked.TitleID, "mtt-stacked", "password")
	c.signedStatus = true
	uid := dwcrypto.UserID([]byte("mtt-stacked"))
	r := c.task(33, 1, 6, bitbuf.Int(bitbuf.U64, uid))
	f, err := r.Fields()
	if err != nil || len(f) != 1 || f[0].Type != bitbuf.U32 || f[0].Int != 0 {
		t.Fatal("MTT initialization requires an unsigned empty subscription count")
	}
	for _, filter := range []uint64{0, 2} {
		// Retail default and filtered Stacked Masters catalogue requests.
		q := []bitbuf.Field{bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1300000000), bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 100000000), bitbuf.Int(bitbuf.U32, filter), bitbuf.Int(bitbuf.U32, 1)}
		r = c.task(33, 8, 3, q...)
		f, err = r.Fields()
		if err != nil || len(f) != 1 || f[0].Type != bitbuf.U32 || f[0].Int != 0 {
			t.Fatal("MTT catalogue reply did not match the retail format")
		}
		if _, code := c.taskStatus(33, 9, 3, q[:5]...); code == 0 {
			t.Fatal("truncated MTT filter query was accepted")
		}
	}
	if _, code := c.taskStatus(33, 2, 6, bitbuf.Int(bitbuf.U64, uid+1)); code == 0 {
		t.Fatal("accepted another player's subscription lookup")
	}
	if _, code := c.taskStatus(33, 3, 0); code == 0 {
		t.Fatal("unimplemented tournament creation reported success")
	}
	ctx := bitbuf.Int(bitbuf.U8, 0)
	poll := bitbuf.Int(bitbuf.Bool, 0)
	r = c.task(6, 4, 1, ctx, poll)
	f, err = r.Fields()
	if err != nil || len(f) != 1 || f[0].Type != bitbuf.I32 || f[0].Int != 0 {
		t.Fatal("mailbox initialization requires a signed empty count")
	}
	st := db.ForGame(stacked.Key)
	id, err := st.Enqueue(context.Background(), store.Message{To: uid, From: uid, Type: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, code := c.taskStatus(6, 5, 1, ctx, poll); code == 0 {
		t.Fatal("unmapped pending notification was hidden")
	}
	msgs, err := st.Inbox(context.Background(), uid, 0, 10)
	if err != nil || len(msgs) != 1 || msgs[0].ID != id {
		t.Fatal("unmapped pending notification was consumed")
	}
	c.task(6, 6, 4, ctx, bitbuf.Int(bitbuf.I64, id))
	r = c.task(6, 7, 1, ctx, poll)
	f, err = r.Fields()
	if err != nil || len(f) != 1 || f[0].Int != 0 {
		t.Fatal("acknowledged message still present")
	}
}

func TestStackedMissingStatsInitializeAndSurviveReconnect(t *testing.T) {
	s, _ := multiTCP(t)
	if _, code := status(t, dial(t, s).roundTrip(signupFor(stacked.TitleID, "stats-stacked", "password"))); code != 700 {
		t.Fatal("signup failed")
	}
	c := loginGatewayFor(t, s, stacked.TitleID, "stats-stacked", "password")
	c.signedStatus = true
	ctx := bitbuf.Int(bitbuf.U8, 0)
	board := bitbuf.Int(bitbuf.I32, 20)
	// Retail login asks for boards 20 and 19 using task 3. No record triggers
	// the game's own initialization write rather than a fabricated server row.
	r := c.task(4, 1, 3, ctx, board, bitbuf.Int(bitbuf.U64, dwcrypto.UserID([]byte("stats-stacked"))))
	f, err := r.Fields()
	if err != nil || len(f) != 1 || f[0].Type != bitbuf.I32 || f[0].Int != 0 {
		t.Fatal("missing stats did not return an I32 zero count without a total")
	}
	c.task(4, 2, 1, ctx, board, bitbuf.Int(bitbuf.I64, 42), bitbuf.Int(bitbuf.U64, 2000))
	c.task(4, 3, 1, ctx, board, bitbuf.Int(bitbuf.I64, math.MaxInt64), bitbuf.Int(bitbuf.U64, 1500))
	c.conn.Close()
	c = loginGatewayFor(t, s, stacked.TitleID, "stats-stacked", "password")
	c.signedStatus = true
	r = c.task(4, 4, 3, ctx, board, bitbuf.Int(bitbuf.U64, dwcrypto.UserID([]byte("stats-stacked"))))
	f, err = r.Fields()
	if err != nil || len(f) != 4 || f[0].Type != bitbuf.I32 || f[0].Int != 1 || f[1].Type != bitbuf.U64 || f[2].Type != bitbuf.I64 || f[2].Int != 42 || f[3].Type != bitbuf.I64 || f[3].Int != 1500 {
		t.Fatalf("stored stats or unchanged rating failed: %v, %v", f, err)
	}
	// Older SDK task 4 is a range read (newer SDKs use task 5).
	r = c.task(4, 5, 4, ctx, board, bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 10))
	f, err = r.Fields()
	if err != nil || len(f) != 4 || f[0].Int != 1 {
		t.Fatal("older range task failed")
	}
}

func TestStackedSharedLoginAndRoomIsolation(t *testing.T) {
	s, _ := multiTCP(t)
	if _, code := status(t, dial(t, s).roundTrip(signupFor(svr2007.TitleID, "stacked-user", "shared-password"))); code != 700 {
		t.Fatal("signup failed")
	}
	host := loginGatewayFor(t, s, stacked.TitleID, "stacked-user", "shared-password")
	host.signedStatus = true
	host.task(8, 1, 1, bitbuf.Int(bitbuf.U8, 0))
	f := []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, make([]byte, 33)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16))}
	for range 4 {
		f = append(f, bitbuf.Int(bitbuf.I32, 0))
	}
	f[3].Int, f[5].Int = 7, 8
	for range 25 {
		f = append(f, bitbuf.Int(bitbuf.U32, 0))
	}
	for range 3 {
		f = append(f, bitbuf.Int(bitbuf.I32, 0))
	}
	f = append(f, bitbuf.Bytes(bitbuf.String, []byte("test table")))
	r := host.task(5, 2, 1, append([]bitbuf.Field{bitbuf.Int(bitbuf.U8, 0)}, f...)...)
	count, err := r.Field()
	id, _ := r.Field()
	key, _ := r.Field()
	if err != nil || count.Type != bitbuf.I32 || count.Int != 1 || len(id.Bytes) != 8 || len(key.Bytes) != 16 {
		t.Fatal("Stacked room creation failed")
	}
	// Exercise a second gateway connection using the shared account.
	viewer := loginGatewayFor(t, s, stacked.TitleID, "stacked-user", "shared-password")
	viewer.signedStatus = true
	r = viewer.task(5, 3, 4, bitbuf.Int(bitbuf.U8, 0), id)
	count, err = r.Field()
	info, err := r.Fields()
	if err != nil || count.Type != bitbuf.I32 || count.Int != 1 || len(info) != 36 || len(info[0].Bytes) != 33 || info[35].Type != bitbuf.String {
		t.Fatal("Stacked room did not survive its wire round trip")
	}
	for _, scalar := range info[3:35] {
		if scalar.Type != bitbuf.I32 {
			t.Fatal("retail room parser requires signed markers in server replies")
		}
	}
	r = viewer.task(5, 5, 5, bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 7), bitbuf.Int(bitbuf.I32, 35), bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 0))
	count, err = r.Field()
	if err != nil || count.Type != bitbuf.I32 || count.Int != 1 {
		t.Fatal("retail Quick Game query did not find the hosted table")
	}
	wwe := loginGatewayFor(t, s, svr2007.TitleID, "stacked-user", "shared-password")
	r = wwe.task(5, 4, 4, bitbuf.Int(bitbuf.U8, 0), id)
	if count, _ := r.U32(); count != 0 {
		t.Fatal("Stacked room leaked into WWE")
	}
}
