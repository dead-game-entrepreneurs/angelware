package services

import (
	"context"
	"fmt"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

// Stats, lobby service 4 (senders LBYM 0x6e2c8..0x6ecb8, lobby tasks 0x22..0x29
// and WriteDna):
//
//	1 write(u8, u8 flag, i32 board, u64 0, i64 rating, i32 values...)
//	2 delete(u8, i32 board, u64 user): user 0 is the caller; sent per board of a
//	  group, e.g. when a player no longer holds a belt (prestige boards 19-26)
//	3 read players(u8, i32 board, u64 user)
//	4 read players(u8, i32 board, u32 n, n x u64 user)
//	5 read range(u8, i32 board, u64 user, u64 position, i64 count)
//	  user != 0: rows around that player; user == 0: rows from a 1-based position
//	6 read filtered(u8, u64, object): answered with no rows
//
// Read results (0x6ef98, list 0xbf778): u32 count, u32 total rows on the board
// (0x6f6e0, GetReadStatsTotalEntries), then rows of u64 user, i64 rating, u64 rank,
// string name (0x6f328), followed by the board's columns exactly as the game
// wrote them (board 1: 12 x i32, 10: 32, 19 and 23: 6, score-only boards: none).
const (
	statsWrite      = 1
	statsDelete     = 2
	statsReadUser   = 3
	statsReadUsers  = 4
	statsReadRange  = 5
	statsReadFilter = 6
	statsMaxRows    = 100
)

func (c *Conn) stats(ctx context.Context, t Task, options StatsOptions) []byte {
	if t.ID >= statsReadUser && t.ID <= statsReadFilter {
		return c.statsRead(ctx, t, options)
	}
	if t.ID == statsDelete {
		return c.statsDelete(ctx, t)
	}
	if t.ID != statsWrite {
		return Reply(t, nil)
	}
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 5 {
		c.log.Warn("stats write did not parse", "err", err, "fields", len(fields))
		return Reply(t, nil)
	}
	if options.DecodeWrite == nil {
		return ErrorReply(t, 1)
	}
	row, err := options.DecodeWrite(fields)
	if err != nil {
		c.log.Warn("invalid stats row", "err", err)
		return ErrorReply(t, 1)
	}
	if err := c.srv.Store.WriteStats(ctx, c.sess.UserID, row); err != nil {
		c.log.Error("save stats", "err", err)
	}
	c.log.Debug("stats written", "board", row.Board, "rating", row.Rating, "values", len(row.Values))
	return Reply(t, nil)
}

func (c *Conn) statsRead(ctx context.Context, t Task, options StatsOptions) []byte {
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 2 {
		if err != nil {
			c.log.Warn("stats read did not parse", "task", t.ID, "err", err)
		}
		return statsReply(t, nil, 0, options)
	}
	var q StatsRead
	if options.ParseRead != nil {
		q, err = options.ParseRead(t.ID, fields)
	} else {
		q, err = ParseStatsRead(t.ID, fields)
	}
	if err != nil {
		return ErrorReply(t, 1)
	}
	q.Limit = min(max(q.Limit, 0), statsMaxRows)
	board := q.Board
	st := c.srv.Store
	var rows []store.RankedRow
	switch {
	case q.Users != nil:
		rows, err = st.StatsFor(ctx, board, q.Users, options.ascending(board))
	case q.User != 0:
		rows, err = st.StatsAround(ctx, board, q.User, q.Limit, options.ascending(board))
	default:
		rows, err = st.StatsFrom(ctx, board, q.Position, q.Limit, options.ascending(board))
	}
	if err != nil {
		c.log.Error("read stats", "board", board, "err", err)
		return statsReply(t, nil, 0, options)
	}
	total, err := st.StatsTotal(ctx, board)
	if err != nil {
		c.log.Error("count stats", "board", board, "err", err)
	}
	c.log.Debug("stats read", "task", t.ID, "board", board, "rows", len(rows), "total", total)
	return statsReply(t, rows, total, options)
}

// statsReply is the read result: u32 count, u32 total, rows.
func statsReply(t Task, rows []store.RankedRow, total int, options StatsOptions) []byte {
	out := []bitbuf.Field{bitbuf.Int(bitbuf.U32, uint64(total))}
	for _, r := range rows {
		if options.EncodeRow != nil {
			out = append(out, options.EncodeRow(r)...)
		}
	}
	return ListReply(t, uint32(len(rows)), out)
}

// statsDelete removes the caller's row on a board. A player can only delete
// their own rows.
func (c *Conn) statsDelete(ctx context.Context, t Task) []byte {
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 3 {
		c.log.Warn("stats delete did not parse", "err", err)
		return Reply(t, nil)
	}
	board, user := int32(fields[1].Int), fields[2].Int
	if user != 0 && user != c.sess.UserID {
		c.log.Warn("stats delete for another player refused", "board", board)
		return Reply(t, nil)
	}
	if err := c.srv.Store.DeleteStats(ctx, c.sess.UserID, board); err != nil {
		c.log.Error("delete stats", "board", board, "err", err)
	}
	c.log.Info("stats deleted", "board", board)
	return Reply(t, nil)
}

// StatsOptions supplies a game's board ordering and row codecs.
type StatsOptions struct {
	Ascending   func(int32) bool
	DecodeWrite func([]bitbuf.Field) (store.StatsRow, error)
	EncodeRow   func(store.RankedRow) []bitbuf.Field
	ParseRead   func(uint8, []bitbuf.Field) (StatsRead, error)
}

func (o StatsOptions) ascending(board int32) bool { return o.Ascending != nil && o.Ascending(board) }
func Stats(options StatsOptions) Handler {
	return func(ctx context.Context, c *Conn, t Task) []byte { return c.stats(ctx, t, options) }
}

type StatsRead struct {
	Board           int32
	Users           []uint64
	User            uint64
	Position, Limit int
}

// ParseStatsRead implements the common SDK's user/list/range queries. Games may
// replace it when an SDK service or custom query differs.
func ParseStatsRead(task uint8, fields []bitbuf.Field) (StatsRead, error) {
	q := StatsRead{Position: 1, Limit: statsMaxRows}
	if len(fields) < 2 {
		return q, fmt.Errorf("short stats query")
	}
	q.Board = int32(fields[1].Int)
	args := fields[2:]
	switch task {
	case statsReadUser, statsReadUsers:
		q.Users = []uint64{}
		for _, f := range args {
			if f.Type == bitbuf.U64 {
				q.Users = append(q.Users, f.Int)
			}
		}
	case statsReadRange:
		if len(args) < 3 {
			return q, fmt.Errorf("short range query")
		}
		q.User = args[0].Int
		q.Position = int(args[1].Int)
		q.Limit = int(int64(args[2].Int))
	case statsReadFilter:
		q.Users = []uint64{}
	default:
		return q, fmt.Errorf("unsupported stats query")
	}
	return q, nil
}
