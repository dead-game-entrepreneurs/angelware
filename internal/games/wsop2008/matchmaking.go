package wsop2008

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
)

// 0x1365d8/0x136878: seven common fields, thirteen I32s, name[64], two I32s.
func ValidateSession(info []bitbuf.Field) error {
	if len(info) != 23 {
		return fmt.Errorf("WSOP session has %d fields, want 23", len(info))
	}
	for i, size := range []int{25, 8, 16} {
		if info[i].Type != bitbuf.Blob || len(info[i].Bytes) != size {
			return fmt.Errorf("invalid WSOP session blob")
		}
	}
	for i, f := range info[3:] {
		if i+3 == 20 {
			if f.Type != bitbuf.String || len(f.Bytes) > 63 || bytes.IndexByte(f.Bytes, 0) >= 0 {
				return fmt.Errorf("invalid WSOP session name")
			}
		} else if f.Type != bitbuf.I32 {
			return fmt.Errorf("invalid WSOP session integer")
		}
	}
	// The SDK stores each count in one byte (0x341278).
	for _, f := range info[3:7] {
		if f.Int > 255 {
			return fmt.Errorf("invalid WSOP slot count")
		}
	}
	if info[4].Int > info[3].Int || info[6].Int > info[5].Int || info[3].Int+info[5].Int == 0 || info[3].Int+info[5].Int > 255 {
		return fmt.Errorf("invalid WSOP slot allocation")
	}
	return nil
}

// 0x17fcb0 constructs kinds 1 and 2. The retail unfiltered search uses kind 1,
// six wildcard criteria (0x7fffffff), and compatibility flag 1. Restrictive
// detailed filters remain unsupported; never silently broaden such a query.
func Search(query []bitbuf.Field, rooms []services.Candidate) []services.Candidate {
	if len(query) != 4 && len(query) != 9 {
		return nil
	}
	for _, f := range query {
		if f.Type != bitbuf.I32 {
			return nil
		}
	}
	if int32(query[1].Int) <= 0 {
		return nil
	}
	if len(query) == 4 {
		if query[0].Int != 2 || query[2].Int != 0 || query[3].Int != 0 {
			return nil
		}
	} else {
		if query[0].Int != 1 || query[8].Int != 1 {
			return nil
		}
		for _, f := range query[2:8] {
			if f.Int != 0x7fffffff {
				return nil
			}
		}
	}
	var out []services.Candidate
	for _, r := range rooms {
		if ValidateSession(r.Info) != nil || r.Info[3].Int <= r.Info[4].Int {
			continue
		}
		if len(query) == 9 && r.Info[22].Int != query[8].Int {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out[:min(len(out), int(query[1].Int), 20)]
}
