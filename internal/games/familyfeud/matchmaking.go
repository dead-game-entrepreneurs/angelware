package familyfeud

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// SLUS-214.46 0x1964d8: common seven fields, name, two game integers.
// The reader at 0x1965a8 consumes up to 16 name bytes, including the NUL.
func ValidateSession(info []bitbuf.Field) error {
	if len(info) != 10 {
		return fmt.Errorf("Family Feud session has %d fields, want 10", len(info))
	}
	for i, size := range []int{25, 8, 16} {
		if info[i].Type != bitbuf.Blob || len(info[i].Bytes) != size {
			return fmt.Errorf("invalid Family Feud session blob")
		}
	}
	for _, f := range info[3:7] {
		if f.Type != bitbuf.I32 || int32(f.Int) < 0 || f.Int > 2 {
			return fmt.Errorf("invalid Family Feud slot count")
		}
	}
	if info[4].Int > info[3].Int || info[6].Int > info[5].Int || info[3].Int+info[5].Int != 2 {
		return fmt.Errorf("invalid Family Feud slot allocation")
	}
	if info[7].Type != bitbuf.String || len(info[7].Bytes) > 15 || bytes.IndexByte(info[7].Bytes, 0) >= 0 {
		return fmt.Errorf("invalid Family Feud session name")
	}
	if info[8].Type != bitbuf.I32 || info[9].Type != bitbuf.I32 {
		return fmt.Errorf("invalid Family Feud session integer")
	}
	return nil
}

// 0x19c650 requests query kind 2, maximum 20, required public openings 1.
// Other linked query classes are not used by the traced menu path.
func Search(query []bitbuf.Field, rooms []services.Candidate) []services.Candidate {
	if len(query) != 3 {
		return nil
	}
	for _, f := range query {
		if f.Type != bitbuf.I32 {
			return nil
		}
	}
	kind, limit, value := int32(query[0].Int), int32(query[1].Int), int32(query[2].Int)
	if kind != 2 || limit <= 0 || value < 1 || value > 2 {
		return nil
	}
	var out []services.Candidate
	for _, room := range rooms {
		info := room.Info
		if ValidateSession(info) != nil || info[3].Int <= info[4].Int {
			continue
		}
		if int32(info[3].Int-info[4].Int) < value {
			continue
		}
		out = append(out, room)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out[:min(len(out), int(limit), 20)]
}
