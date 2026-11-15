package server

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func multiTCP(t *testing.T) (*TCP, *store.MemoryDB) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessions := session.NewRegistry()
	db := store.NewMemoryDB()
	tcp := &TCP{Auth: map[uint32]*auth.Service{}, Lobby: &gateway.Server{Sessions: sessions, Applications: map[string]gateway.Application{}, Log: log}, Log: log}
	for _, d := range []game.Definition{svr2007.Definition(), outrun2006.Definition()} {
		r, err := d.New(game.Options{Store: db.ForGame(d.Key), Sessions: sessions, AuthMode: auth.Verify, Log: log})
		if err != nil {
			t.Fatal(err)
		}
		tcp.Auth[d.TitleIDs[0]] = r.Auth
		tcp.Lobby.Applications[d.Key] = r.Lobby
	}
	return tcp, db
}
func TestSharedAccountWorksAcrossGamesWithoutSharingRooms(t *testing.T) {
	s, db := multiTCP(t)
	if _, code := status(t, dial(t, s).roundTrip(signupFor(svr2007.TitleID, "same-name", "shared-password"))); code != 700 {
		t.Fatal("signup failed")
	}
	if _, code := status(t, dial(t, s).roundTrip(signupFor(outrun2006.TitleID, "same-name", "other-password"))); code != 707 {
		t.Fatalf("duplicate shared account status %d, want 707", code)
	}
	wwe := loginGatewayFor(t, s, svr2007.TitleID, "same-name", "shared-password")
	outrun := loginGatewayFor(t, s, outrun2006.TitleID, "same-name", "shared-password")
	// OutRun's service 8 bootstrap has no WWE profile side effects.
	outrun.task(8, 1, 1, bitbuf.Int(bitbuf.U8, 0))
	// Session data from the PS2 serializer: shared base plus game flag, two names,
	// and thirteen menu fields. The same user's WWE connection cannot retrieve it.
	info := []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16)), bitbuf.Int(bitbuf.I32, 6), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Bytes(bitbuf.String, []byte("same-name")), bitbuf.Bytes(bitbuf.String, []byte("race"))}
	for range 13 {
		info = append(info, bitbuf.Int(bitbuf.I32, 0))
	}
	args := append([]bitbuf.Field{bitbuf.Int(bitbuf.U8, 0)}, info...)
	r := outrun.task(5, 2, 1, args...)
	count, _ := r.U32()
	id, _ := r.Field()
	key, _ := r.Field()
	if count != 1 || len(id.Bytes) != 8 || len(key.Bytes) != 16 {
		t.Fatal("OutRun room was not created")
	}
	r = wwe.task(5, 3, 4, bitbuf.Int(bitbuf.U8, 0), id)
	if count, _ := r.U32(); count != 0 {
		t.Fatal("WWE retrieved an OutRun room")
	}
	r = wwe.task(5, 4, 5, bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 1))
	if count, _ := r.U32(); count != 0 {
		t.Fatal("WWE search returned an OutRun room")
	}
	// A distinct guest can find and fetch that room inside OutRun.
	if _, code := status(t, dial(t, s).roundTrip(signupFor(outrun2006.TitleID, "guest", "guest-password"))); code != 700 {
		t.Fatal("guest signup")
	}
	guest := loginGatewayFor(t, s, outrun2006.TitleID, "guest", "guest-password")
	query := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 25)}
	for range 13 {
		query = append(query, bitbuf.Int(bitbuf.I32, 0x7fffffff))
	}
	r = guest.task(5, 5, 5, query...)
	if count, _ := r.U32(); count != 1 {
		t.Fatal("guest did not find OutRun room")
	}
	found, _ := r.Fields()
	if len(found) != 23 || !bytes.Equal(found[1].Bytes, id.Bytes) {
		t.Fatal("OutRun session reply layout changed")
	}
	// OutRun's six unsigned stats columns and 64-bit ghost ID survive storage and readback.
	row := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 42), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 9000)}
	for i := range 6 {
		row = append(row, bitbuf.Int(bitbuf.U32, uint64(i+10)))
	}
	row = append(row, bitbuf.Int(bitbuf.I64, 0xfedcba9876543210))
	outrun.task(4, 6, 1, row...)
	r = outrun.task(4, 7, 5, bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 42), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 10))
	count, _ = r.U32()
	total, _ := r.U32()
	fields, _ := r.Fields()
	if count != 1 || total != 1 || len(fields) != 11 || fields[1].Type != bitbuf.U64 || fields[10].Type != bitbuf.I64 || fields[10].Int != 0xfedcba9876543210 {
		t.Fatalf("OutRun stats reply: %v", fields)
	}
	if count, _ := db.ForGame(svr2007.Key).StatsTotal(context.Background(), 42); count != 0 {
		t.Fatal("stats crossed games")
	}
	// Bootstrap query must not masquerade as WWE CreateProfile.
	_, code := outrun.taskStatus(8, 8, 1, bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Bytes(bitbuf.Blob, make([]byte, 48)))
	if code == 0 {
		t.Fatal("OutRun accepted WWE's profile shape")
	}
}
func TestUnknownAuthTitleRejected(t *testing.T) {
	s, _ := multiTCP(t)
	client, server := net.Pipe()
	defer client.Close()
	go s.handle(context.Background(), server)
	client.SetDeadline(time.Now().Add(time.Second))
	if _, err := client.Write(loginFor(999999, "unknown")); err != nil {
		t.Fatal(err)
	}
	if _, err := frame.Read(bufio.NewReader(client)); err != io.EOF {
		t.Fatalf("unknown title: %v", err)
	}
}
