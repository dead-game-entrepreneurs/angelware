package svr2007

import (
	"fmt"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func StatsOptions() services.StatsOptions {
	return services.StatsOptions{DecodeWrite: decodeStats, EncodeRow: encodeStats}
}
func decodeStats(fields []bitbuf.Field) (store.StatsRow, error) {
	if len(fields) < 5 || fields[0].Type != bitbuf.U8 || fields[1].Type != bitbuf.U8 || fields[2].Type != bitbuf.I32 || fields[3].Type != bitbuf.U64 || fields[4].Type != bitbuf.I64 {
		return store.StatsRow{}, fmt.Errorf("invalid WWE stats header")
	}
	row := store.StatsRow{Flag: uint8(fields[1].Int), Board: int32(fields[2].Int), Rating: int64(fields[4].Int), Values: []int32{}}
	for _, f := range fields[5:] {
		if f.Type != bitbuf.I32 {
			return row, fmt.Errorf("invalid WWE stats column")
		}
		row.Values = append(row.Values, int32(f.Int))
	}
	return row, nil
}
func encodeStats(r store.RankedRow) []bitbuf.Field {
	name := r.Name
	if len(name) > 63 {
		name = name[:63]
	}
	out := []bitbuf.Field{bitbuf.Int(bitbuf.U64, r.UserID), bitbuf.Int(bitbuf.I64, uint64(r.Rating)), bitbuf.Int(bitbuf.U64, r.Rank), bitbuf.Bytes(bitbuf.String, []byte(name))}
	for _, v := range r.Values {
		out = append(out, bitbuf.Int(bitbuf.I32, uint64(uint32(v))))
	}
	return out
}
