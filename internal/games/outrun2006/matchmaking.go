package outrun2006

import (
	"fmt"
	"sort"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// SLUS-212.74: session serializer 0x121b80, query 0x129cb8, builder
// 0x1242b0. Thirteen menu criteria follow query kind/max; a negative menu
// criterion is serialized as INT_MAX (wildcard), in session field order.
func ValidateSession(info []bitbuf.Field) error {
	if len(info) != 23 {
		return fmt.Errorf("OutRun session has %d fields, want 23", len(info))
	}
	for i, f := range info[7:] {
		if i == 1 || i == 2 {
			if f.Type != bitbuf.String || len(f.Bytes) > 16 {
				return fmt.Errorf("invalid OutRun room name")
			}
			continue
		}
		if f.Type != bitbuf.I32 {
			return fmt.Errorf("invalid OutRun session integer")
		}
	}
	return nil
}
func Search(query []bitbuf.Field, rooms []services.Candidate) []services.Candidate {
	if len(query) != 15 || query[0].Type != bitbuf.I32 || query[0].Int != 1 || query[1].Type != bitbuf.I32 {
		return nil
	}
	limit := int(int32(query[1].Int))
	if limit <= 0 {
		return nil
	}
	limit = min(limit, 50)
	for _, f := range query[2:] {
		if f.Type != bitbuf.I32 {
			return nil
		}
	}
	var out []services.Candidate
	for _, room := range rooms {
		info := room.Info
		if ValidateSession(info) != nil {
			continue
		}
		if info[3].Int <= info[4].Int {
			continue
		}
		match := true
		for i, f := range query[2:] {
			if int32(f.Int) != 0x7fffffff && uint32(f.Int) != uint32(info[10+i].Int) {
				match = false
				break
			}
		}
		if match {
			out = append(out, room)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out[:min(len(out), limit)]
}
