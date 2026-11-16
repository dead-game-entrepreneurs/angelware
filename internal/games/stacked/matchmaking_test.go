package stacked

import (
	"math"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

func TestMatchmakingReplyUsesRetailReadMarkersWithoutMutatingRooms(t *testing.T) {
	a, b := room(), room()
	a[7].Int = math.MaxUint32
	b[7].Int = 123
	input := append(append([]bitbuf.Field(nil), a...), b...)
	payload := matchmakingReply(services.Task{ID: 5, SignedStatus: true}, 2, input)
	r, _ := bitbuf.NewReader(payload)
	f, err := r.Fields()
	if err != nil || len(f) != 5+72 || f[4].Type != bitbuf.I32 || f[4].Int != 2 {
		t.Fatal("wrong search response envelope")
	}
	for n, value := range []uint64{math.MaxUint32, 123} {
		start := 5 + n*36
		for _, col := range f[start+3 : start+35] {
			if col.Type != bitbuf.I32 {
				t.Fatal("unsigned marker rejected by native 0x210598")
			}
		}
		if f[start+7].Int != value || f[start+35].Type != bitbuf.String {
			t.Fatal("room values changed during response conversion")
		}
	}
	if ValidateSession(input[:36]) != nil || ValidateSession(input[36:]) != nil {
		t.Fatal("response conversion mutated stored room request types")
	}
}

func room() []bitbuf.Field {
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
	return append(f, bitbuf.Bytes(bitbuf.String, []byte("table")))
}

func TestBrowseVisibilityAndUnknownFilters(t *testing.T) {
	full, private, malformed := room(), room(), room()
	full[3].Int = 0
	private[3].Int, private[4].Int, private[5].Int = 0, 0, 8
	malformed[0].Bytes = make([]byte, 25)
	q := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 1), bitbuf.Int(bitbuf.I32, 0)}
	rooms := []services.Candidate{{Info: room(), Order: 2}, {Info: full}, {Info: private}, {Info: malformed}, {Info: room(), Order: 1}}
	got := Search(q, rooms)
	if len(got) != 1 || got[0].Order != 1 {
		t.Fatal("visibility/order/limit incorrect")
	}
	q[0].Int = 99
	if len(Search(q, rooms)) != 0 {
		t.Fatal("unknown filter was silently broadened")
	}
}

func TestRetailQuickGameQuery(t *testing.T) {
	// Captured after signup: service 5 task 5, context zero,
	// I32 kind 7, I32 limit 35, U32 flag 0, U32 zero.
	q := []bitbuf.Field{bitbuf.Int(bitbuf.I32, 7), bitbuf.Int(bitbuf.I32, 35), bitbuf.Int(bitbuf.U32, 0), bitbuf.Int(bitbuf.U32, 0)}
	open, other, full := room(), room(), room()
	other[10].Int = 1
	full[3].Int = 0
	rooms := []services.Candidate{{Info: other, Order: 1}, {Info: full, Order: 2}, {Info: open, Order: 3}}
	got := Search(q, rooms)
	if len(got) != 1 || got[0].Order != 3 {
		t.Fatal("Quick Game flag or available-seat filtering failed")
	}
	q[2].Int = 1
	got = Search(q, rooms)
	if len(got) != 1 || got[0].Order != 1 {
		t.Fatal("Quick Game ignored the requested flag")
	}
	q[3].Int = 1
	if len(Search(q, rooms)) != 0 {
		t.Fatal("unmapped query suffix was ignored")
	}
	bad := room()
	bad[3].Int = 9
	if ValidateSession(bad) == nil {
		t.Fatal("accepted more free seats than capacity")
	}
}

func TestDetailedSearchAppliesEveryCriterion(t *testing.T) {
	// Independent table from the builder/serializer offsets, including the
	// distinct stake fields used by the flag-zero and flag-one menu branches.
	for _, tc := range []struct {
		kind uint64
		cols []int
	}{
		{2, []int{10, 31}},
		{3, []int{10, 12}},
		{4, []int{10, 7}},
		{5, []int{10, 7, 31}},
		{6, []int{10, 12, 31}},
	} {
		f := room()
		f[10].Int, f[7].Int, f[12].Int, f[31].Int = 1, 4, 2000, 2
		q := []bitbuf.Field{bitbuf.Int(bitbuf.I32, tc.kind), bitbuf.Int(bitbuf.I32, 35)}
		for _, col := range tc.cols {
			q = append(q, bitbuf.Int(bitbuf.U32, f[col].Int))
		}
		q = append(q, bitbuf.Int(bitbuf.U32, 0))
		rooms := []services.Candidate{{Info: f, Order: 1}}
		if len(Search(q, rooms)) != 1 {
			t.Fatalf("kind %d did not match its selected criteria", tc.kind)
		}
		for i := range tc.cols {
			old := q[i+2].Int
			q[i+2].Int = old ^ 1
			if len(Search(q, rooms)) != 0 {
				t.Fatalf("kind %d ignored criterion %d", tc.kind, i)
			}
			q[i+2].Int = old
		}
		q[2].Type = bitbuf.I32
		if len(Search(q, rooms)) != 0 {
			t.Fatalf("kind %d accepted the wrong scalar type", tc.kind)
		}
	}
}
