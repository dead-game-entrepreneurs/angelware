package outrun2006

import (
	"encoding/binary"
	"fmt"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// SDK writer 0x159a70 and game row 0x122600: no WWE flag byte; rating is
// unsigned on the wire, followed by six u32 columns and optional i64 ghost ID.
func StatsOptions(ascendingBoards []int32) services.StatsOptions {
	ascending := map[int32]bool{}
	for _, board := range ascendingBoards {
		ascending[board] = true
	}
	return services.StatsOptions{Ascending: func(board int32) bool { return ascending[board] }, DecodeWrite: decodeStats, EncodeRow: encodeStats, ParseRead: parseStatsRead}
}
func decodeStats(f []bitbuf.Field) (store.StatsRow, error) {
	var row store.StatsRow
	if len(f) != 10 && len(f) != 11 {
		return row, fmt.Errorf("invalid OutRun stats length")
	}
	if f[0].Type != bitbuf.U8 || f[1].Type != bitbuf.I32 || f[2].Type != bitbuf.U64 || f[3].Type != bitbuf.U64 {
		return row, fmt.Errorf("invalid OutRun stats header")
	}
	row.Board = int32(f[1].Int)
	row.Rating = int64(f[3].Int)
	for _, v := range f[4:10] {
		if v.Type != bitbuf.U32 {
			return row, fmt.Errorf("invalid OutRun stats column")
		}
		row.Values = append(row.Values, int32(v.Int))
	}
	if len(f) == 11 {
		if f[10].Type != bitbuf.I64 {
			return row, fmt.Errorf("invalid ghost ID")
		}
		row.Extra = binary.LittleEndian.AppendUint64(nil, f[10].Int)
	}
	return row, nil
}
func encodeStats(r store.RankedRow) []bitbuf.Field {
	name := r.Name
	if len(name) > 63 {
		name = name[:63]
	}
	out := []bitbuf.Field{bitbuf.Int(bitbuf.U64, r.UserID), bitbuf.Int(bitbuf.U64, uint64(r.Rating)), bitbuf.Int(bitbuf.U64, r.Rank), bitbuf.Bytes(bitbuf.String, []byte(name))}
	for _, v := range r.Values {
		out = append(out, bitbuf.Int(bitbuf.I32, uint64(uint32(v))))
	}
	if len(r.Extra) == 8 {
		out = append(out, bitbuf.Int(bitbuf.I64, binary.LittleEndian.Uint64(r.Extra)))
	}
	return out
}
func parseStatsRead(task uint8, f []bitbuf.Field) (services.StatsRead, error) {
	if task != 6 {
		return services.ParseStatsRead(task, f)
	}
	// Custom query 0x122e18: i32 kind, i32 count, u32 board, u64 user,
	// u32 mode. The title's read UI supplies a user as the ranking pivot.
	if len(f) != 6 || f[0].Type != bitbuf.U8 || f[1].Type != bitbuf.I32 || f[2].Type != bitbuf.I32 || f[3].Type != bitbuf.U32 || f[4].Type != bitbuf.U64 || f[5].Type != bitbuf.U32 {
		return services.StatsRead{}, fmt.Errorf("invalid OutRun stats query")
	}
	return services.StatsRead{Board: int32(f[3].Int), User: f[4].Int, Position: 1, Limit: int(int32(f[2].Int))}, nil
}
