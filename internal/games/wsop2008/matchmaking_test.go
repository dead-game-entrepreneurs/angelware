package wsop2008

import (
	"bytes"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/crypto"
	"github.com/princebyyourlogic/angelware/internal/services"
)

func room() []bitbuf.Field {
	f := []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16))}
	for range 17 {
		f = append(f, bitbuf.Int(bitbuf.I32, 0))
	}
	f[3].Int = 8
	f[4].Int = 1
	return append(f, bitbuf.Bytes(bitbuf.String, []byte("table")), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 1))
}
func TestValidateRetailLayoutAndBounds(t *testing.T) {
	if err := ValidateSession(room()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func([]bitbuf.Field) []bitbuf.Field{
		"truncated":      func(f []bitbuf.Field) []bitbuf.Field { return f[:22] },
		"too many seats": func(f []bitbuf.Field) []bitbuf.Field { f[3].Int = 256; return f },
		"overfull":       func(f []bitbuf.Field) []bitbuf.Field { f[4].Int = 9; return f },
		"wrong scalar":   func(f []bitbuf.Field) []bitbuf.Field { f[12].Type = bitbuf.U32; return f },
		"name overflow":  func(f []bitbuf.Field) []bitbuf.Field { f[20].Bytes = bytes.Repeat([]byte{'x'}, 64); return f },
		"embedded nul":   func(f []bitbuf.Field) []bitbuf.Field { f[20].Bytes = []byte("x\x00y"); return f },
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateSession(change(room())) == nil {
				t.Fatal("accepted malformed room")
			}
		})
	}
}
func TestBrowseExcludesFullPrivateAndMalformedRooms(t *testing.T) {
	public, full, private, bad := room(), room(), room(), room()
	full[4].Int = full[3].Int
	private[3].Int = 0
	private[4].Int = 0
	private[5].Int = 8
	private[6].Int = 1
	bad[20].Type = bitbuf.Blob
	rooms := []services.Candidate{{Info: public, Order: 2}, {Info: full, Order: 3}, {Info: private, Order: 4}, {Info: bad, Order: 5}, {Info: room(), Order: 1}}
	query := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0)}
	got := Search(query, rooms)
	if len(got) != 1 || got[0].Order != 1 {
		t.Fatal("visibility/order/limit incorrect")
	}
	query[0].Int = 99
	if len(Search(query, rooms)) != 0 {
		t.Fatal("accepted unknown query")
	}
}
func TestAccountIDMatchesSDKLowercaseASCII(t *testing.T) {
	if AccountID([]byte("PokerUser")) != dwcrypto.UserID([]byte("pokeruser")) {
		t.Fatal("wrong account identity")
	}
}

func TestRetailUnfilteredNineFieldQuery(t *testing.T) {
	// Captured from SLUS-216.86 after login: service 5 task 5, context 0,
	// query kind 1, count 20, six wildcard fields, compatibility flag 1.
	q := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 20)}
	for range 6 {
		q = append(q, bitbuf.Int(bitbuf.I32, 0x7fffffff))
	}
	q = append(q, bitbuf.Int(bitbuf.I32, 1))
	open, full, private, incompatible := room(), room(), room(), room()
	full[4].Int = full[3].Int
	private[3].Int = 0
	private[4].Int = 0
	private[5].Int = 8
	incompatible[22].Int = 0
	rooms := []services.Candidate{{Info: full}, {Info: private}, {Info: incompatible}, {Info: open, Order: 2}, {Info: room(), Order: 1}}
	got := Search(q, rooms)
	if len(got) != 2 || got[0].Order != 1 || got[1].Order != 2 {
		t.Fatal("retail unfiltered query missed public compatible rooms")
	}
	for i := 2; i < 8; i++ {
		restricted := append([]bitbuf.Field(nil), q...)
		restricted[i].Int = 0
		if len(Search(restricted, rooms)) != 0 {
			t.Fatalf("silently broadened unsupported filter %d", i)
		}
	}
}
