package stacked

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// 0x20f598/0x20fa60: seven common fields (33-byte CommonAddr), 25 U32s,
// three I32s, then a name with a 32-byte buffer including the terminator.
func ValidateSession(info []bitbuf.Field) error {
	if len(info) != 36 {
		return fmt.Errorf("Stacked session has %d fields, want 36", len(info))
	}
	for i, n := range []int{33, 8, 16} {
		if info[i].Type != bitbuf.Blob || len(info[i].Bytes) != n {
			return fmt.Errorf("invalid Stacked session blob")
		}
	}
	for i := 3; i < 35; i++ {
		want := uint8(bitbuf.U32)
		if i < 7 || i >= 32 {
			want = bitbuf.I32
		}
		if info[i].Type != want {
			return fmt.Errorf("invalid Stacked session integer %d", i)
		}
	}
	for _, f := range info[3:7] {
		if int32(f.Int) < 0 || f.Int > 255 {
			return fmt.Errorf("invalid Stacked slot count")
		}
	}
	// 0x20f340 copies native GameInfo's free/maximum counts (+0x28/+0x29)
	// into SDK slots 0/2. Slots 1/3 remain zero. A retail eight-seat table
	// with its host present therefore advertises 7,0,8,0, not max/used pairs.
	if info[4].Int != 0 || info[6].Int != 0 || info[3].Int > info[5].Int {
		return fmt.Errorf("invalid Stacked slot allocation")
	}
	name := info[35]
	if name.Type != bitbuf.String || len(name.Bytes) > 31 || bytes.IndexByte(name.Bytes, 0) >= 0 {
		return fmt.Errorf("invalid Stacked table name")
	}
	return nil
}

// 0x1f5bb0 builds the query classes at 0x1f8150..0x1f87a0 from the same
// object used by room serializer 0x20f598. Entries list the corresponding
// room fields, in query order. Every filtered class ends with U32 zero.
var queryRoomFields = map[uint64][]int{
	2: {10, 31},
	3: {10, 12},
	4: {10, 7},
	5: {10, 7, 31},
	6: {10, 12, 31},
	7: {10}, // retail Quick Game / default filtered search
}

// 0x210598 reads each game scalar with an I32 marker even though the host's
// serializer writes U32. Convert response copies only; stored rooms and search
// criteria retain the request layout. Create replies carry ID/key, not a room.
func matchmakingReply(t services.Task, count uint32, fields []bitbuf.Field) []byte {
	if t.ID != 4 && t.ID != 5 {
		return signedListReply(t, count, fields)
	}
	if len(fields) != int(count)*36 {
		return services.ErrorReply(t, 1)
	}
	out := append([]bitbuf.Field(nil), fields...)
	for start := 0; start < len(out); start += 36 {
		if ValidateSession(out[start:start+36]) != nil {
			return services.ErrorReply(t, 1)
		}
		for i := start + 7; i < start+32; i++ {
			out[i].Type = bitbuf.I32
		}
	}
	return signedListReply(t, count, out)
}

func Search(query []bitbuf.Field, rooms []services.Candidate) []services.Candidate {
	if len(query) < 2 || query[0].Type != bitbuf.I32 || query[1].Type != bitbuf.I32 || int32(query[1].Int) <= 0 {
		return nil
	}
	kind := query[0].Int
	var columns []int
	if kind == 1 {
		if len(query) != 3 || query[2].Type != bitbuf.I32 || query[2].Int != 0 {
			return nil
		}
	} else {
		columns = queryRoomFields[kind]
		if len(columns) == 0 || len(query) != len(columns)+3 || query[2].Int > 1 || query[len(query)-1].Int != 0 {
			return nil
		}
		for _, f := range query[2:] {
			if f.Type != bitbuf.U32 {
				return nil
			}
		}
	}
	var out []services.Candidate
	for _, room := range rooms {
		if ValidateSession(room.Info) != nil || room.Info[3].Int == 0 {
			continue
		}
		match := true
		for i, col := range columns {
			if room.Info[col].Int != query[i+2].Int {
				match = false
				break
			}
		}
		if match {
			out = append(out, room)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out[:min(len(out), int(query[1].Int), 35)]
}
