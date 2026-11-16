package wsop2008

import (
	"bytes"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func TestRetailScoreOnlySubmissionAndResult(t *testing.T) {
	// Retail end-of-game write: context 0, flag 0, board 11, user 0, signed score 0.
	f := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 11), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.I64, 0)}
	row, err := decodeStats(f)
	if err != nil || row.Board != 11 || row.Rating != 0 || len(row.Values) != 0 || row.Values == nil {
		t.Fatalf("retail score: %+v %v", row, err)
	}
	f[4].Int = ^uint64(0) - 4
	row, err = decodeStats(f)
	if err != nil || row.Rating != -5 {
		t.Fatal("signed score was lost")
	}
	for _, bad := range [][]bitbuf.Field{f[:4], append(append([]bitbuf.Field(nil), f...), bitbuf.Int(bitbuf.I32, 7))} {
		if _, err := decodeStats(bad); err == nil {
			t.Fatal("accepted wrong game's columns")
		}
	}
	bad := append([]bitbuf.Field(nil), f...)
	bad[3].Int = 123
	if _, err := decodeStats(bad); err == nil {
		t.Fatal("accepted targeted stats write")
	}
	out := encodeStats(store.RankedRow{UserID: 42, Name: string(bytes.Repeat([]byte{'x'}, 70)), Rank: 2, Rating: row.Rating})
	if len(out) != 4 || out[1].Type != bitbuf.I64 || int64(out[1].Int) != -5 || out[2].Int != 2 || len(out[3].Bytes) != 63 {
		t.Fatalf("score-only result: %v", out)
	}
}
func TestRetailLeaderboardQueries(t *testing.T) {
	// Captured default range read: board 8, user 0, first rank 1, count 25.
	f := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 25)}
	q, err := parseStatsRead(5, f)
	if err != nil || q.Board != 8 || q.Position != 1 || q.Limit != 25 {
		t.Fatalf("range query: %+v %v", q, err)
	}
	f[2].Int = 42
	q, err = parseStatsRead(5, f)
	if err != nil || q.User != 42 {
		t.Fatal("lost player ranking pivot")
	}
	bulk := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 8), bitbuf.Int(bitbuf.U32, 2), bitbuf.Int(bitbuf.U64, 42), bitbuf.Int(bitbuf.U64, 43)}
	q, err = parseStatsRead(4, bulk)
	if err != nil || len(q.Users) != 2 {
		t.Fatal("bulk ranking query rejected")
	}
	bulk[2].Int = 3
	if _, err := parseStatsRead(4, bulk); err == nil {
		t.Fatal("accepted inconsistent player count")
	}
}
