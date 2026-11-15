package outrun2006

import (
	"encoding/hex"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

func room() []bitbuf.Field {
	info := []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16)), bitbuf.Int(bitbuf.I32, 6), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0), bitbuf.Bytes(bitbuf.String, []byte("host")), bitbuf.Bytes(bitbuf.String, []byte("room"))}
	for i := range 13 {
		info = append(info, bitbuf.Int(bitbuf.I32, uint64(i)))
	}
	return info
}
func TestPS2SearchCriteriaAndCapacity(t *testing.T) {
	query := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 25)}
	for range 13 {
		query = append(query, bitbuf.Int(bitbuf.I32, 0x7fffffff))
	}
	rooms := []services.Candidate{{Info: room(), Order: 3}}
	if got := Search(query, rooms); len(got) != 1 {
		t.Fatal("retail wildcard query missed room")
	}
	for i := range 13 {
		q := append([]bitbuf.Field(nil), query...)
		q[i+2] = bitbuf.Int(bitbuf.I32, uint64(i))
		if len(Search(q, rooms)) != 1 {
			t.Fatalf("criterion %d failed exact match", i)
		}
		q[i+2].Int = 999
		if len(Search(q, rooms)) != 0 {
			t.Fatalf("criterion %d ignored", i)
		}
	}
	rooms[0].Info[4].Int = 6
	if len(Search(query, rooms)) != 0 {
		t.Fatal("full room listed")
	}
	rooms[0].Info[3].Int = 0
	rooms[0].Info[4].Int = 0
	if len(Search(query, rooms)) != 0 {
		t.Fatal("private-only room listed")
	}
	if len(Search(query[:2], rooms)) != 0 {
		t.Fatal("malformed query accepted")
	}
}

// Retail Quick Game request captured from SLUS-212.74. It contains no account,
// password, room ID or session key. Preserve the on-wire query as a regression.
func TestRetailQuickGameRequest(t *testing.T) {
	const packet = "07c020180503e0040000009c0c000080f3ffffff77feffffffceffffffdff9ffffff3bffffff7fe7ffffffeffcffffff9dffffffbff3ffffff77feffffffceffffffdff9ffffff3bffffff7f00000000000000"
	raw, err := hex.DecodeString(packet)
	if err != nil {
		t.Fatal(err)
	}
	r, err := bitbuf.NewReader(raw)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err = r.U8(); err != nil {
			t.Fatal(err)
		}
	}
	fields, err := r.Fields()
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 16 || fields[0].Type != bitbuf.U8 {
		t.Fatalf("retail arguments: %v", fields)
	}
	query := fields[1:]
	if query[0].Int != 1 || query[1].Int != 25 {
		t.Fatalf("retail query header: %v", query[:2])
	}
	for _, f := range query[2:] {
		if f.Type != bitbuf.I32 || f.Int != 0x7fffffff {
			t.Fatal("retail any criterion changed")
		}
	}
	if got := Search(query, []services.Candidate{{Info: room(), Order: 1}}); len(got) != 1 {
		t.Fatal("real Quick Game request did not match an open room")
	}
}
