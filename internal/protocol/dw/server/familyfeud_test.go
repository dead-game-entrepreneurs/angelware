package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/games/familyfeud"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func guestGateway(t *testing.T, s *TCP) (*client, session.Session) {
	t.Helper()
	// Reusing a seed must still yield different temporary identities and keys.
	reply := dial(t, s).roundTrip(frame.Plain(0x10, bitbuf.NewWriter(false).U32(42).U32(familyfeud.TitleID).Bytes()))
	r, code := status(t, reply)
	if reply.Type != 0x11 || code != 700 {
		t.Fatalf("guest reply type=%x status=%d", reply.Type, code)
	}
	seed, err := r.U32()
	if err != nil || seed != 0 {
		t.Fatal("invalid guest reply seed")
	}
	block, err := r.Raw(128)
	if err != nil || !bytes.Equal(block[:4], []byte{0xde, 0xad, 0xbd, 0xef}) || binary.LittleEndian.Uint32(block[9:13]) != familyfeud.TitleID {
		t.Fatal("invalid guest auth ticket")
	}
	ticket, err := r.Raw(128)
	if err != nil {
		t.Fatal(err)
	}
	sess, ok := s.Lobby.Sessions.Redeem(ticket)
	if !ok || !sess.Guest || sess.Game != familyfeud.Key || sess.UserID == 0 || binary.LittleEndian.Uint64(block[17:25]) != sess.UserID || !bytes.Equal(block[97:121], sess.Key) {
		t.Fatal("guest ticket did not bind identity, game and session key")
	}
	g := dial(t, s)
	g.key = block[97:121]
	hello := bitbuf.NewWriter(false).U32(binary.LittleEndian.Uint32(block[5:9])).U32(seed).Raw(ticket)
	if _, err := g.conn.Write(frame.Plain(gateway.MsgHello, hello.Bytes())); err != nil {
		t.Fatal(err)
	}
	return g, sess
}

func familyRoom() []bitbuf.Field {
	return []bitbuf.Field{
		bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16)),
		bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0),
		bitbuf.Bytes(bitbuf.String, []byte("Test Family")), bitbuf.Int(bitbuf.I32, 5), bitbuf.Int(bitbuf.I32, 101),
	}
}

func TestFamilyFeudGuestLoginRoomsAndIsolation(t *testing.T) {
	s, db := multiTCP(t)
	host, a := guestGateway(t, s)
	guest, b := guestGateway(t, s)
	if a.UserID == b.UserID || bytes.Equal(a.Key, b.Key) || a.Token == b.Token {
		t.Fatal("anonymous clients share identity or credentials")
	}
	for _, sess := range []session.Session{a, b} {
		if _, err := db.ForGame(familyfeud.Key).Account(context.Background(), sess.UserID); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("anonymous login persisted a password account")
		}
	}
	query := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 20), bitbuf.Int(bitbuf.I32, 1)}
	if n, _ := guest.task(5, 0, 5, query...).U32(); n != 0 {
		t.Fatal("initial search was not empty")
	}
	info := familyRoom()
	r := host.task(5, 0, 1, append([]bitbuf.Field{bitbuf.Int(bitbuf.U8, 0)}, info...)...)
	n, _ := r.U32()
	id, _ := r.Field()
	key, _ := r.Field()
	if n != 1 || len(id.Bytes) != 8 || len(key.Bytes) != 16 {
		t.Fatal("create did not return room ID and key")
	}
	r = guest.task(5, 1, 5, query...)
	n, _ = r.U32()
	fields, err := r.Fields()
	if err != nil || n != 1 || len(fields) != 10 || !bytes.Equal(fields[1].Bytes, id.Bytes) || !bytes.Equal(fields[2].Bytes, key.Bytes) || string(fields[7].Bytes) != "Test Family" {
		t.Fatal("search did not return the Family Feud room layout")
	}
	if _, code := guest.taskStatus(8, 0, 1); code == 0 {
		t.Fatal("guest enabled an unregistered profile service")
	}
	if _, code := status(t, dial(t, s).roundTrip(signupFor(svr2007.TitleID, "family-test", "secret"))); code != 700 {
		t.Fatal("WWE signup")
	}
	for _, title := range []uint32{svr2007.TitleID, outrun2006.TitleID} {
		other := loginGatewayFor(t, s, title, "family-test", "secret")
		if n, _ := other.task(5, 0, 4, bitbuf.Int(bitbuf.U8, 0), id).U32(); n != 0 {
			t.Fatal("another title retrieved Family Feud's room/key")
		}
		if _, code := status(t, dial(t, s).roundTrip(frame.Plain(0x10, bitbuf.NewWriter(false).U32(42).U32(title).Bytes()))); code == 700 {
			t.Fatal("anonymous login bypassed a password title")
		}
	}
	info[1], info[2], info[4] = id, key, bitbuf.Int(bitbuf.I32, 2)
	host.task(5, 0, 2, append([]bitbuf.Field{bitbuf.Int(bitbuf.U8, 0)}, info...)...)
	if n, _ := guest.task(5, 0, 5, query...).U32(); n != 0 {
		t.Fatal("full room appeared in public-open search")
	}
	// A guest cannot delete another connection's room.
	guest.task(5, 0, 3, bitbuf.Int(bitbuf.U8, 0), id)
	if n, _ := guest.task(5, 0, 4, bitbuf.Int(bitbuf.U8, 0), id).U32(); n != 1 {
		t.Fatal("guest deleted the host's room")
	}
	host.task(5, 0, 3, bitbuf.Int(bitbuf.U8, 0), id)
	if n, _ := guest.task(5, 0, 4, bitbuf.Int(bitbuf.U8, 0), id).U32(); n != 0 {
		t.Fatal("deleted room remained visible")
	}
}

func TestMalformedGuestAuthCannotIssueTickets(t *testing.T) {
	s, _ := multiTCP(t)
	for _, payload := range [][]byte{
		bitbuf.NewWriter(false).U32(42).Bytes(),
		bitbuf.NewWriter(true).U32(42).U32(familyfeud.TitleID).Bytes(),
		bitbuf.NewWriter(false).U32(42).U32(familyfeud.TitleID).U8(1).Bytes(),
	} {
		a, b := net.Pipe()
		go s.handle(context.Background(), b)
		a.SetDeadline(time.Now().Add(time.Second))
		if _, err := a.Write(frame.Plain(0x10, payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := frame.Read(bufio.NewReader(a)); err != io.EOF {
			t.Fatalf("malformed guest request returned a ticket: %v", err)
		}
		a.Close()
	}
}
