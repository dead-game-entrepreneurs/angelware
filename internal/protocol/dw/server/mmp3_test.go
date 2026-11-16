package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/games/wsop2008"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/crypto"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

type mmpClient struct {
	t           *testing.T
	conn        net.Conn
	reader      *bufio.Reader
	key         []byte
	seed        uint32
	transaction uint64
	pushes      []frame.Frame
}

func dialMMP3(t *testing.T, s *TCP) *mmpClient {
	t.Helper()
	a, b := net.Pipe()
	go s.handle(context.Background(), b)
	a.SetDeadline(time.Now().Add(3 * time.Second))
	t.Cleanup(func() { a.Close() })
	return &mmpClient{t: t, conn: a, reader: bufio.NewReader(a)}
}
func (c *mmpClient) exchange(out []byte) frame.Frame {
	c.t.Helper()
	// Write in small fragments to exercise classification of a partial u32 header.
	for i := 0; i < len(out); {
		n := min(3, len(out)-i)
		if _, err := c.conn.Write(out[i : i+n]); err != nil {
			c.t.Fatal(err)
		}
		i += n
	}
	for {
		f, err := frame.MMP3.Read(c.reader)
		if err != nil {
			c.t.Fatal(err)
		}
		if !f.KeepAlive && f.Type == 2 {
			c.pushes = append(c.pushes, f)
			continue
		}
		return f
	}
}
func mmpSignup(name, password string) []byte {
	const seed = 123
	block := make([]byte, 96)
	copy(block, []byte{0xde, 0xad, 0xbd, 0xef})
	copy(block[4:68], name)
	copy(block[68:], dwcrypto.Tiger([]byte(password)))
	enc, _ := dwcrypto.Encrypt(signupKey, dwcrypto.SeedIV(seed), block)
	return frame.MMP3.Plain(0, bitbuf.NewWriter(true).U32(seed).U32(wsop2008.TitleID).Raw(make([]byte, 8)).Raw(enc).Bytes())
}
func openMMP3(t *testing.T, s *TCP, name, password string) *mmpClient {
	t.Helper()
	a := dialMMP3(t, s)
	var uid [8]byte
	binary.LittleEndian.PutUint64(uid[:], wsop2008.AccountID([]byte(name)))
	f := a.exchange(frame.MMP3.Plain(10, bitbuf.NewWriter(true).U32(42).U32(wsop2008.TitleID).Raw(uid[:]).Bytes()))
	r, code := status(t, f)
	if code != 700 || f.Type != 11 {
		t.Fatalf("MMP3 login status %d type %d", code, f.Type)
	}
	seed, _ := r.U32()
	enc, _ := r.Raw(128)
	ticket, _ := r.Raw(128)
	block, err := dwcrypto.Decrypt(dwcrypto.Tiger([]byte(password)), dwcrypto.SeedIV(seed), enc)
	if err != nil || !bytes.Equal(block[:4], []byte{0xde, 0xad, 0xbd, 0xef}) {
		t.Fatal("wrong password ticket")
	}
	g := dialMMP3(t, s)
	g.key = bytes.Clone(block[97:121])
	hello := bitbuf.NewWriter(true).U32(binary.LittleEndian.Uint32(block[5:])).U32(0).Raw(ticket)
	if _, err := g.conn.Write(append([]byte{180, 0, 0, 0, 0, 0x3c, 0, 0}, frame.MMP3.Plain(7, hello.Bytes())...)); err != nil {
		t.Fatal(err)
	}
	return g
}
func (c *mmpClient) task(service, id uint8, args ...bitbuf.Field) *bitbuf.Reader {
	c.t.Helper()
	payload := bitbuf.NewWriter(true).U8(id).Fields(args).Raw(make([]byte, 4)).Bytes()
	plain := append([]byte{0, 0, 0, 0, service}, payload...)
	plain = append(plain, bytes.Repeat([]byte{byte(c.seed)}, (8-len(plain)%8)%8)...)
	mac := hmac.New(sha1.New, c.key)
	mac.Write(plain[5:])
	copy(plain, mac.Sum(nil)[:4])
	ct, _ := dwcrypto.Encrypt(c.key, dwcrypto.SeedIV(c.seed), plain)
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(ct)+5))
	out = append(out, 1)
	out = binary.LittleEndian.AppendUint32(out, c.seed)
	c.seed++
	f := c.exchange(append(out, ct...))
	if f.Type != 1 {
		c.t.Fatal("expected task reply")
	}
	r, err := bitbuf.NewReader(f.Payload)
	if err != nil {
		c.t.Fatal(err)
	}
	txn, err := r.Field()
	c.transaction++
	if err != nil || txn.Type != bitbuf.U64 || txn.Int != c.transaction {
		c.t.Fatalf("transaction %+v %v", txn, err)
	}
	code, err := r.U32()
	if err != nil || code != 0 {
		c.t.Fatalf("task error %d %v", code, err)
	}
	echoed, err := r.U8()
	if err != nil || echoed != id {
		c.t.Fatal("task echo mismatch")
	}
	return r
}

// pokerRoomFields follows the retail session serializer, including its 13
// custom integers and trailing compatibility flag.
func pokerRoomFields() []bitbuf.Field {
	info := []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16)), bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0)}
	for range 13 {
		info = append(info, bitbuf.Int(bitbuf.I32, 0))
	}
	return append(info, bitbuf.Bytes(bitbuf.String, []byte("Poker table")), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 1))
}

func TestMMP3SharedListenerAccountsAndRoomLifecycle(t *testing.T) {
	s, db := multiTCP(t)
	// A mixed-case WSOP signup hashes the lowercase identity but keeps its display.
	if _, code := status(t, dialMMP3(t, s).exchange(mmpSignup("PokerUser", "shared-pass"))); code != 700 {
		t.Fatalf("signup %d", code)
	}
	if _, code := status(t, dial(t, s).roundTrip(signupFor(svr2007.TitleID, "pokeruser", "different-pass"))); code != 707 {
		t.Fatalf("shared duplicate %d", code)
	}
	host := openMMP3(t, s, "POKERUSER", "shared-pass")
	wwe := loginGatewayFor(t, s, svr2007.TitleID, "pokeruser", "shared-pass")
	host.task(8, 1, bitbuf.Int(bitbuf.U8, 0))
	// A quiet in-game lobby sends zero-length keepalives. Echo them and keep
	// the authenticated connection (and its later task transactions) alive.
	for range 2 {
		if f := host.exchange([]byte{0, 0, 0, 0}); !f.KeepAlive {
			t.Fatal("keepalive was not echoed")
		}
	}

	info := pokerRoomFields()
	r := host.task(5, 1, append([]bitbuf.Field{bitbuf.Int(bitbuf.U8, 0)}, info...)...)
	if f := host.exchange([]byte{0, 0, 0, 0}); !f.KeepAlive {
		t.Fatal("host disconnected during keepalive")
	}

	count, _ := r.U32()
	id, _ := r.Field()
	key, _ := r.Field()
	if count != 1 || len(id.Bytes) != 8 || len(key.Bytes) != 16 {
		t.Fatal("room creation failed")
	}
	if _, code := status(t, dialMMP3(t, s).exchange(mmpSignup("guest", "guest-pass"))); code != 700 {
		t.Fatal("guest signup")
	}
	guest := openMMP3(t, s, "guest", "guest-pass")
	// Use the nine-field unfiltered query observed from the retail laptop.
	query := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 20)}
	for range 6 {
		query = append(query, bitbuf.Int(bitbuf.I32, 0x7fffffff))
	}
	query = append(query, bitbuf.Int(bitbuf.I32, 1))
	r = guest.task(5, 5, query...)
	count, _ = r.U32()
	found, _ := r.Fields()
	if count != 1 || len(found) != 23 || !bytes.Equal(found[1].Bytes, id.Bytes) {
		t.Fatal("WSOP search did not find room")
	}
	r = wwe.task(5, 1, 4, bitbuf.Int(bitbuf.U8, 0), id)
	if count, _ := r.U32(); count != 0 {
		t.Fatal("WSOP room crossed game boundary")
	}
	if _, err := db.ForGame(wsop2008.Key).Profile(context.Background(), wsop2008.AccountID([]byte("PokerUser")), "public"); err == nil {
		t.Fatal("bootstrap created profile")
	}
	host.task(5, 3, bitbuf.Int(bitbuf.U8, 0), id)
	r = guest.task(5, 4, bitbuf.Int(bitbuf.U8, 0), id)
	if count, _ := r.U32(); count != 0 {
		t.Fatal("room survived deletion")
	}
}

func TestFirstFrameBindsProtocolToTitleAndTicket(t *testing.T) {
	s, _ := multiTCP(t)
	for _, p := range []frame.Protocol{frame.Legacy, frame.MMP3} {
		other := frame.MMP3
		title := svr2007.TitleID
		if p == frame.MMP3 {
			other = frame.Legacy
			title = wsop2008.TitleID
		}
		_, ticket := s.Lobby.Sessions.Issue("test", title, 1, "test", p)
		s.Lobby.Applications["test"] = s.Lobby.Applications[svr2007.Key]
		for _, wire := range [][]byte{
			other.Plain(10, bitbuf.NewWriter(true).U32(42).U32(title).Raw(make([]byte, 8)).Bytes()),
			other.Plain(7, bitbuf.NewWriter(true).U32(42).U32(0).Raw(ticket).Bytes()),
		} {
			if _, err := s.readFirst(bufio.NewReader(bytes.NewReader(wire))); err == nil {
				t.Fatalf("accepted wrong wire protocol %d", other)
			}
		}
	}
	// A peer cannot force a 2 GB allocation with a u32 length prefix.
	if _, err := s.readFirst(bufio.NewReader(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0x7f, 0, 10}))); err == nil {
		t.Fatal("oversized first frame accepted")
	}
}

func TestMMP3RetailPreambleLeavesQueuedTasks(t *testing.T) {
	s, _ := multiTCP(t)
	_, ticket := s.Lobby.Sessions.Issue(wsop2008.Key, wsop2008.TitleID, 123, "test", frame.MMP3)
	hello := frame.MMP3.Plain(7, bitbuf.NewWriter(true).U32(123).U32(42).Raw(ticket).Bytes())
	if len(hello) != 144 {
		t.Fatalf("retail hello size %d", len(hello))
	}
	// Retail capture: eight raw introduction bytes, a 144-byte hello, and
	// three 25-byte encrypted tasks can arrive in a single TCP segment.
	wire := append([]byte{180, 0, 0, 0, 0, 0x3c, 0, 0}, hello...)
	task := append([]byte{21, 0, 0, 0, 1, 0, 0, 0, 0}, make([]byte, 16)...)
	for range 3 {
		wire = append(wire, task...)
	}
	r := bufio.NewReader(bytes.NewReader(wire))
	first, err := s.readFirst(r)
	if err != nil || first.Protocol != frame.MMP3 || first.Type != 7 || !bytes.Equal(first.Payload, hello[6:]) {
		t.Fatalf("retail intro rejected: %v", err)
	}
	for range 3 {
		f, err := frame.MMP3.Read(r)
		if err != nil || !f.Encrypted || len(f.Payload) != 16 {
			t.Fatalf("queued task consumed by intro: %v", err)
		}
	}
	wire[0] = 181
	if _, err := s.readFirst(bufio.NewReader(bytes.NewReader(wire))); err == nil {
		t.Fatal("accepted unknown SDK preamble")
	}
}

func (c *mmpClient) push() []bitbuf.Field {
	c.t.Helper()
	var f frame.Frame
	if len(c.pushes) > 0 {
		f = c.pushes[0]
		c.pushes = c.pushes[1:]
	} else {
		var err error
		f, err = frame.MMP3.Read(c.reader)
		if err != nil {
			c.t.Fatal(err)
		}
	}
	if f.Type != 2 {
		c.t.Fatalf("expected push, got %d", f.Type)
	}
	r, err := bitbuf.NewReader(f.Payload)
	if err != nil {
		c.t.Fatal(err)
	}
	fields, err := r.Fields()
	if err != nil {
		c.t.Fatal(err)
	}
	return fields
}

func TestMMP3FriendsNotificationsMailboxAndIsolation(t *testing.T) {
	s, db := multiTCP(t)
	for _, name := range []string{"poker-a", "poker-b"} {
		if _, code := status(t, dialMMP3(t, s).exchange(mmpSignup(name, "test-password"))); code != 700 {
			t.Fatal("signup failed")
		}
	}
	a, b := openMMP3(t, s, "poker-a", "test-password"), openMMP3(t, s, "poker-b", "test-password")
	aid, bid := wsop2008.AccountID([]byte("poker-a")), wsop2008.AccountID([]byte("poker-b"))
	contextField := bitbuf.Int(bitbuf.U8, 0)
	a.task(8, 1, contextField)
	b.task(8, 1, contextField)
	a.task(9, 1, contextField, bitbuf.Int(bitbuf.U64, bid))
	push := b.push()
	// Verified WSOP type-1 decoder: U32 type, U64 time/id, U32 flags,
	// bool, U64 sender, name, U16 length, optional blob.
	if len(push) != 8 || push[0].Type != bitbuf.U32 || push[0].Int != 1 || push[5].Int != aid || string(push[6].Bytes) != "poker-a" || push[7].Type != bitbuf.U16 || push[7].Int != 0 {
		t.Fatalf("friend request push: %v", push)
	}
	id := push[2]
	requests, err := db.ForGame(svr2007.Key).FriendRequestsTo(context.Background(), bid)
	if err != nil || len(requests) != 0 {
		t.Fatal("friend request crossed game boundary")
	}
	r := b.task(9, 19, contextField)
	count, _ := r.U32()
	fields, _ := r.Fields()
	if count != 1 || len(fields) != 2 || fields[0].Int != aid {
		t.Fatal("missing pending friend request")
	}
	r = b.task(6, 1, contextField, bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 100), bitbuf.Int(bitbuf.Bool, 0), bitbuf.Int(bitbuf.Bool, 1))
	count, _ = r.U32()
	fields, _ = r.Fields()
	if count != 1 || len(fields) != 8 || fields[2].Int != id.Int {
		t.Fatal("mailbox did not retain friend notification")
	}
	// Retail WSOP accepts without acknowledging the original mailbox message.
	b.task(9, 2, contextField, bitbuf.Int(bitbuf.U64, aid))
	accepted := a.push()
	if len(accepted) != 7 || accepted[0].Int != 2 || accepted[5].Int != bid {
		t.Fatal("friend acceptance notification invalid")
	}
	// Repair already-stored notifications left by an older server as well.
	if _, err := db.ForGame(wsop2008.Key).Enqueue(context.Background(), store.Message{From: aid, To: bid, Type: 1}); err != nil {
		t.Fatal(err)
	}
	r = b.task(6, 1, contextField, bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 100), bitbuf.Int(bitbuf.Bool, 0), bitbuf.Int(bitbuf.Bool, 1))
	if count, _ := r.U32(); count != 0 {
		t.Fatal("accepted friend request reappeared in mailbox")
	}
	r = b.task(9, 19, contextField)
	if count, _ := r.U32(); count != 0 {
		t.Fatal("accepted friend request remained pending")
	}
	for _, item := range []struct {
		client *mmpClient
		other  uint64
	}{{a, bid}, {b, aid}} {
		r = item.client.task(9, 5, contextField)
		count, _ = r.U32()
		fields, _ = r.Fields()
		if count != 1 || len(fields) != 3 || fields[0].Int != item.other || fields[2].Type != bitbuf.Bool || fields[2].Int != 1 {
			t.Fatal("friend list or online presence invalid")
		}
	}
	// WSOP's type-5 decoder (0x348610) reads the sender, an eight-byte
	// session blob and a U16 optional-data length. Accepting it uses task 9,
	// and the recipient then resolves that session with matchmaking task 4.
	r = a.task(5, 1, append([]bitbuf.Field{contextField}, pokerRoomFields()...)...)
	count, _ = r.U32()
	roomID, _ := r.Field()
	if count != 1 || len(roomID.Bytes) != 8 {
		t.Fatal("inviter room creation failed")
	}
	a.task(9, 8, contextField, bitbuf.Int(bitbuf.U64, bid), roomID)
	invitation := b.push()
	if len(invitation) != 9 || invitation[0].Int != 5 || invitation[5].Int != aid || invitation[7].Type != bitbuf.Blob || !bytes.Equal(invitation[7].Bytes, roomID.Bytes) || invitation[8].Type != bitbuf.U16 || invitation[8].Int != 0 {
		t.Fatalf("game invitation notification: %v", invitation)
	}
	// Clicking Invite again must not create another pending notification.
	a.task(9, 8, contextField, bitbuf.Int(bitbuf.U64, bid), roomID)
	inbox, err := db.ForGame(wsop2008.Key).Inbox(context.Background(), bid, 0, 100)
	if err != nil || len(inbox) != 1 || inbox[0].ID != invitation[2].Int {
		t.Fatal("repeated invitation was not deduplicated")
	}
	b.task(9, 9, contextField, bitbuf.Int(bitbuf.U64, aid))
	answer := a.push()
	if len(answer) != 7 || answer[0].Int != 6 || answer[5].Int != bid {
		t.Fatalf("invitation acceptance notification: %v", answer)
	}
	if inbox, err := db.ForGame(wsop2008.Key).Inbox(context.Background(), bid, 0, 100); err != nil || len(inbox) != 0 {
		t.Fatal("accepted invitation remained in mailbox")
	}
	// Repeated acceptance or invitation to the same accepted table is a no-op.
	b.task(9, 9, contextField, bitbuf.Int(bitbuf.U64, aid))
	a.task(9, 8, contextField, bitbuf.Int(bitbuf.U64, bid), roomID)
	b.task(8, 1, contextField)
	if len(a.pushes) != 0 || len(b.pushes) != 0 {
		t.Fatal("repeated invitation/answer emitted another push")
	}
	r = a.task(6, 1, contextField, bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 100), bitbuf.Int(bitbuf.Bool, 0), bitbuf.Int(bitbuf.Bool, 1))
	count, _ = r.U32()
	fields, _ = r.Fields()
	if count != 2 || len(fields) != 14 || fields[0].Int != 2 || fields[7].Type != bitbuf.U32 || fields[7].Int != 6 {
		t.Fatal("friend/invitation answers misaligned in mailbox")
	}
	r = b.task(5, 4, contextField, invitation[7])
	count, _ = r.U32()
	fields, _ = r.Fields()
	if count != 1 || len(fields) != 23 || !bytes.Equal(fields[1].Bytes, roomID.Bytes) {
		t.Fatal("invitation did not resolve the hosted room")
	}
	// A different table may be invited immediately; rejecting it also clears
	// the mailbox without requiring a separate message acknowledgement.
	r = a.task(5, 1, append([]bitbuf.Field{contextField}, pokerRoomFields()...)...)
	_, _ = r.U32()
	nextRoom, _ := r.Field()
	a.task(9, 8, contextField, bitbuf.Int(bitbuf.U64, bid), nextRoom)
	nextInvite := b.push()
	if !bytes.Equal(nextInvite[7].Bytes, nextRoom.Bytes) {
		t.Fatal("new table invitation was incorrectly suppressed")
	}
	b.task(9, 10, contextField, bitbuf.Int(bitbuf.U64, aid))
	if rejected := a.push(); len(rejected) != 7 || rejected[0].Int != 7 {
		t.Fatal("invitation rejection notification invalid")
	}
	if inbox, err := db.ForGame(wsop2008.Key).Inbox(context.Background(), bid, 0, 100); err != nil || len(inbox) != 0 {
		t.Fatal("rejected invitation remained pending")
	}
	b.conn.Close()
	offline := a.push()
	if len(offline) != 7 || offline[0].Int != 10 || offline[5].Int != bid {
		t.Fatal("missing offline presence")
	}
}

func TestMMP3PokerRankingsAndGameIsolation(t *testing.T) {
	s, db := multiTCP(t)
	for _, name := range []string{"rank-a", "rank-b"} {
		if _, code := status(t, dialMMP3(t, s).exchange(mmpSignup(name, "test-password"))); code != 700 {
			t.Fatal("signup failed")
		}
	}
	a, b := openMMP3(t, s, "rank-a", "test-password"), openMMP3(t, s, "rank-b", "test-password")
	aid, bid := wsop2008.AccountID([]byte("rank-a")), wsop2008.AccountID([]byte("rank-b"))
	zero := bitbuf.Int(bitbuf.U8, 0)
	for _, item := range []struct {
		client *mmpClient
		score  uint64
	}{{a, 125}, {b, 250}} {
		item.client.task(4, 1, zero, zero, bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.I64, item.score))
	}
	// The client's list reader consumes count, total, then exactly four fields/row.
	r := a.task(4, 5, zero, bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 25))
	count, _ := r.U32()
	total, _ := r.U32()
	f, _ := r.Fields()
	if count != 2 || total != 2 || len(f) != 8 || f[0].Int != bid || f[1].Type != bitbuf.I64 || f[1].Int != 250 || f[2].Int != 1 || string(f[3].Bytes) != "rank-b" || f[4].Int != aid || f[6].Int != 2 {
		t.Fatalf("ranking order/format: %v", f)
	}
	r = a.task(4, 3, zero, bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.U64, aid))
	count, _ = r.U32()
	total, _ = r.U32()
	f, _ = r.Fields()
	if count != 1 || total != 2 || len(f) != 4 || f[0].Int != aid || f[2].Int != 2 {
		t.Fatal("individual ranking incorrect")
	}
	if total, err := db.ForGame(svr2007.Key).StatsTotal(context.Background(), 8); err != nil || total != 0 {
		t.Fatal("poker ranking crossed game boundary")
	}
	// A zero-valued end-of-game submission is a real row, not a missing result.
	a.task(4, 1, zero, zero, bitbuf.Int(bitbuf.I32, 11), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.I64, 0))
	r = a.task(4, 5, zero, bitbuf.Int(bitbuf.I32, 11), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 25))
	count, _ = r.U32()
	r.U32()
	f, _ = r.Fields()
	if count != 1 || len(f) != 4 || f[1].Int != 0 {
		t.Fatal("zero score was not stored")
	}
}
