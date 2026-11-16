package wsop2008

import (
	"fmt"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// SLUS-216.86 0x1fa960/0x1fa980 delegate to the SDK's score-only codecs
// 0x34c1f8/0x34bf08. The two trailing in-memory UI fields are not serialized.
func StatsOptions() services.StatsOptions {
	return services.StatsOptions{DecodeWrite: decodeStats, EncodeRow: encodeStats, ParseRead: parseStatsRead}
}
func decodeStats(f []bitbuf.Field) (store.StatsRow, error) {
	if len(f) != 5 || f[0].Type != bitbuf.U8 || f[0].Int != 0 || f[1].Type != bitbuf.U8 || f[2].Type != bitbuf.I32 || int32(f[2].Int) <= 0 || f[3].Type != bitbuf.U64 || f[3].Int != 0 || f[4].Type != bitbuf.I64 {
		return store.StatsRow{}, fmt.Errorf("invalid WSOP score submission")
	}
	return store.StatsRow{Flag: uint8(f[1].Int), Board: int32(f[2].Int), Rating: int64(f[4].Int), Values: []int32{}}, nil
}
func encodeStats(r store.RankedRow) []bitbuf.Field {
	name := r.Name
	if len(name) > 63 {
		name = name[:63]
	}
	return []bitbuf.Field{bitbuf.Int(bitbuf.U64, r.UserID), bitbuf.Int(bitbuf.I64, uint64(r.Rating)), bitbuf.Int(bitbuf.U64, r.Rank), bitbuf.Bytes(bitbuf.String, []byte(name))}
}
func parseStatsRead(task uint8, f []bitbuf.Field) (services.StatsRead, error) {
	bad := func() (services.StatsRead, error) {
		return services.StatsRead{}, fmt.Errorf("invalid WSOP leaderboard query")
	}
	if len(f) < 2 || f[0].Type != bitbuf.U8 || f[0].Int != 0 || f[1].Type != bitbuf.I32 || int32(f[1].Int) <= 0 {
		return bad()
	}
	switch task {
	case 3:
		if len(f) != 3 || f[2].Type != bitbuf.U64 {
			return bad()
		}
	case 4:
		if len(f) < 3 || f[2].Type != bitbuf.U32 || f[2].Int > 100 || f[2].Int != uint64(len(f)-3) {
			return bad()
		}
		for _, v := range f[3:] {
			if v.Type != bitbuf.U64 {
				return bad()
			}
		}
	case 5:
		if len(f) != 5 || f[2].Type != bitbuf.U64 || f[3].Type != bitbuf.U64 || f[4].Type != bitbuf.I64 {
			return bad()
		}
	default:
		return bad()
	}
	return services.ParseStatsRead(task, f)
}
