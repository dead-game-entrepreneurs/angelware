package stacked

import (
	"bytes"
	"context"
	"fmt"
	"math"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// 0x279028 writes context, board and the object at 0x215da8: I64 rating,
// then String/U32/U64 columns. 0x279158 reads an I32 count with no total.
// The row reader (0x215f50) requires signed markers for numeric columns.
func Stats() services.Handler {
	read := services.Stats(services.StatsOptions{ParseRead: parseStatsRead, EncodeReply: statsReply})
	return func(ctx context.Context, c *services.Conn, t services.Task) []byte {
		if t.ID == 3 || t.ID == 4 {
			return read(ctx, c, t)
		}
		if t.ID != 1 {
			return services.ErrorReply(t, 1)
		}
		f, err := t.Args.Fields()
		if err != nil {
			return services.ErrorReply(t, 1)
		}
		row, err := decodeStats(f)
		if err != nil {
			c.Log().Warn("invalid Stacked stats write", "err", err)
			return services.ErrorReply(t, 1)
		}
		// bdStatsInfo::serialize uses INT64_MAX for an unchanged rating.
		// New metadata records (including boards 19/20) start with score zero.
		if row.Rating == math.MaxInt64 {
			old, err := c.Store().StatsFor(ctx, row.Board, []uint64{c.UserID()}, false)
			if err != nil {
				return services.ErrorReply(t, 1)
			}
			row.Rating = 0
			if len(old) != 0 {
				row.Rating = old[0].Rating
			}
		}
		if err := c.Store().WriteStats(ctx, c.UserID(), row); err != nil {
			c.Log().Error("save Stacked stats", "err", err)
			return services.ErrorReply(t, 1)
		}
		c.Log().Debug("Stacked stats written", "board", row.Board, "columns", len(f)-3)
		return services.Reply(t, nil)
	}
}

func decodeStats(f []bitbuf.Field) (store.StatsRow, error) {
	if len(f) < 3 || len(f) > 131 || f[0].Type != bitbuf.U8 || f[0].Int != 0 || f[1].Type != bitbuf.I32 || int32(f[1].Int) <= 0 || f[2].Type != bitbuf.I64 {
		return store.StatsRow{}, fmt.Errorf("invalid Stacked statistics header")
	}
	for _, col := range f[3:] {
		switch col.Type {
		case bitbuf.U32, bitbuf.U64:
		case bitbuf.String:
			if len(col.Bytes) > 31 || bytes.IndexByte(col.Bytes, 0) >= 0 {
				return store.StatsRow{}, fmt.Errorf("invalid Stacked statistics string")
			}
		default:
			return store.StatsRow{}, fmt.Errorf("unsupported Stacked statistics column %d", col.Type)
		}
	}
	return store.StatsRow{Board: int32(f[1].Int), Rating: int64(f[2].Int), Values: []int32{}, Extra: bitbuf.NewWriter(true).Fields(f[3:]).Bytes()}, nil
}

func parseStatsRead(task uint8, f []bitbuf.Field) (services.StatsRead, error) {
	bad := func() (services.StatsRead, error) {
		return services.StatsRead{}, fmt.Errorf("invalid Stacked statistics query")
	}
	if len(f) < 3 || f[0].Type != bitbuf.U8 || f[0].Int != 0 || f[1].Type != bitbuf.I32 || int32(f[1].Int) <= 0 || f[2].Type != bitbuf.U64 {
		return bad()
	}
	switch task {
	case 3:
		if len(f) != 3 {
			return bad()
		}
		return services.ParseStatsRead(3, f)
	case 4:
		if len(f) != 5 || f[3].Type != bitbuf.U64 || f[4].Type != bitbuf.I64 || f[3].Int > math.MaxInt32 || int64(f[4].Int) <= 0 || f[4].Int > 100 {
			return bad()
		}
		return services.ParseStatsRead(5, f)
	default:
		return bad()
	}
}

func statsReply(t services.Task, rows []store.RankedRow, _ int) []byte {
	f := []bitbuf.Field{bitbuf.Int(bitbuf.I32, uint64(len(rows)))}
	for _, row := range rows {
		r, err := bitbuf.NewReader(row.Extra)
		if err != nil {
			return services.ErrorReply(t, 1)
		}
		cols, err := r.Fields()
		if err != nil {
			return services.ErrorReply(t, 1)
		}
		f = append(f, bitbuf.Int(bitbuf.U64, row.UserID), bitbuf.Int(bitbuf.I64, uint64(row.Rating)))
		for _, col := range cols {
			switch col.Type {
			case bitbuf.U32:
				col.Type = bitbuf.I32
			case bitbuf.U64:
				col.Type = bitbuf.I64
			}
			f = append(f, col)
		}
	}
	return services.Reply(t, f)
}
