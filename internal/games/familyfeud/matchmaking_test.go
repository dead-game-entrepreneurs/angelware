package familyfeud

import (
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

func testRoom(name string, used uint64) []bitbuf.Field {
	return []bitbuf.Field{
		bitbuf.Bytes(bitbuf.Blob, make([]byte, 25)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 8)), bitbuf.Bytes(bitbuf.Blob, make([]byte, 16)),
		bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, used), bitbuf.Int(bitbuf.I32, 0), bitbuf.Int(bitbuf.I32, 0),
		bitbuf.Bytes(bitbuf.String, []byte(name)), bitbuf.Int(bitbuf.I32, 5), bitbuf.Int(bitbuf.I32, 101),
	}
}

func TestPublicOpenSearchAndMalformedSchemas(t *testing.T) {
	rooms := []services.Candidate{{Info: testRoom("new", 1), Order: 2}, {Info: testRoom("full", 2), Order: 0}, {Info: testRoom("old", 1), Order: 1}}
	q := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 20), bitbuf.Int(bitbuf.I32, 1)}
	got := Search(q, rooms)
	if len(got) != 2 || got[0].Order != 1 || got[1].Order != 2 {
		t.Fatal("public-open query did not filter/order rooms")
	}
	q[1].Int = 1
	if got := Search(q, rooms); len(got) != 1 || got[0].Order != 1 {
		t.Fatal("result limit ignored")
	}
	q[2].Int = 2
	if len(Search(q, rooms)) != 0 {
		t.Fatal("minimum openings ignored")
	}
	for _, edit := range []func([]bitbuf.Field){
		func(r []bitbuf.Field) { r[0].Bytes = r[0].Bytes[:24] },
		func(r []bitbuf.Field) { r[7].Bytes = []byte("name\x00suffix") },
		func(r []bitbuf.Field) { r[7].Bytes = []byte("overlong room name") },
		func(r []bitbuf.Field) { r[8].Type = bitbuf.U32 },
		func(r []bitbuf.Field) { r[3].Int = 10 },
		func(r []bitbuf.Field) { r[4].Int = 3 },
	} {
		r := testRoom("valid", 1)
		edit(r)
		if ValidateSession(r) == nil {
			t.Fatal("malformed session accepted")
		}
	}
	for _, q := range [][]bitbuf.Field{
		{bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 20)},
		{bitbuf.Int(bitbuf.I32, 99), bitbuf.Int(bitbuf.I32, 20), bitbuf.Int(bitbuf.I32, 1)},
		{bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 0xffffffff), bitbuf.Int(bitbuf.I32, 1)},
		{bitbuf.Int(bitbuf.I32, 2), bitbuf.Int(bitbuf.I32, 20), bitbuf.Int(bitbuf.I32, 0)},
	} {
		if len(Search(q, rooms)) != 0 {
			t.Fatal("unsupported or malformed search returned rooms")
		}
	}
}
