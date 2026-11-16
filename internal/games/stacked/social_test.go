package stacked

import (
	"testing"
	"time"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func TestOlderFriendOperations(t *testing.T) {
	ctx := bitbuf.Int(bitbuf.U8, 0)
	other := bitbuf.Int(bitbuf.U64, 42)
	for _, tc := range []struct{ wire, flag, want uint8 }{
		{2, 1, services.FriendAccept}, {2, 0, services.FriendReject},
		{8, 1, services.FriendInviteAccept}, {8, 0, services.FriendInviteReject},
		{5, 1, services.FriendBlock},
	} {
		op, args, err := decodeFriends(tc.wire, []bitbuf.Field{ctx, other, bitbuf.Int(bitbuf.U8, uint64(tc.flag))})
		if err != nil || op != tc.want || args[0].Type != other.Type || args[0].Int != other.Int {
			t.Fatalf("operation %d/%d: %d, %v", tc.wire, tc.flag, op, err)
		}
	}
	if _, _, err := decodeFriends(7, []bitbuf.Field{ctx, other, bitbuf.Bytes(bitbuf.Blob, make([]byte, 7))}); err == nil {
		t.Fatal("accepted truncated invitation")
	}
	if _, _, err := decodeFriends(2, []bitbuf.Field{ctx, other, bitbuf.Int(bitbuf.Bool, 1)}); err == nil {
		t.Fatal("accepted a Bool where this SDK sends U8")
	}
}

func TestOlderNotificationLayout(t *testing.T) {
	m := store.Message{ID: 1234, Type: 1, From: 42, FromName: "fixture", Created: time.Unix(1770000000, 0)}
	f := MessageFields(m)
	want := []uint8{bitbuf.I32, bitbuf.U64, bitbuf.I64, bitbuf.String, bitbuf.String, bitbuf.U64, bitbuf.String}
	if len(f) != len(want) {
		t.Fatal("wrong request length")
	}
	for i, typ := range want {
		if f[i].Type != typ {
			t.Fatalf("field %d: got %d, want %d", i, f[i].Type, typ)
		}
	}
	if f[1].Int != 1234 || f[2].Int != 1770000000 || f[5].Int != 42 || string(f[6].Bytes) != "fixture" {
		t.Fatal("wrong message header or sender")
	}
	m.Type, m.Data = 5, []byte("12345678")
	f = MessageFields(m)
	if len(f) != 8 || f[0].Int != 6 || f[7].Type != bitbuf.Blob || string(f[7].Bytes) != "12345678" {
		t.Fatal("session invitation did not use the native type and session object")
	}
	for _, typ := range []uint32{4, 6, 7, 8, 9, 10} {
		m.Type = typ
		if MessageFields(m) != nil {
			t.Fatalf("encoded unsupported factory %d", typ)
		}
	}
}
