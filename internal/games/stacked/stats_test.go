package stacked

import (
	"math"
	"testing"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

func TestStatsColumnsKeepBitsAndUseSignedReadMarkers(t *testing.T) {
	f := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 20), bitbuf.Int(bitbuf.I64, 0), bitbuf.Int(bitbuf.U32, math.MaxUint32), bitbuf.Int(bitbuf.U64, math.MaxUint64), bitbuf.Bytes(bitbuf.String, []byte("player"))}
	row, err := decodeStats(f)
	if err != nil {
		t.Fatal(err)
	}
	b := statsReply(services.Task{ID: 3, SignedStatus: true}, []store.RankedRow{{UserID: 42, Rating: row.Rating, Extra: row.Extra}}, 100)
	r, _ := bitbuf.NewReader(b)
	got, err := r.Fields()
	// slot, sequence, I32 status, task, I32 count, UID, rating, columns.
	if err != nil || len(got) != 10 || got[2].Type != bitbuf.I32 || got[4].Type != bitbuf.I32 || got[4].Int != 1 || got[5].Type != bitbuf.U64 || got[5].Int != 42 || got[7].Type != bitbuf.I32 || got[7].Int != math.MaxUint32 || got[8].Type != bitbuf.I64 || got[8].Int != math.MaxUint64 || string(got[9].Bytes) != "player" {
		t.Fatalf("wrong older-SDK statistics reply: %v, %v", got, err)
	}
	f[3].Type = bitbuf.Blob
	if _, err := decodeStats(f); err == nil {
		t.Fatal("accepted unsupported column")
	}
}

func TestStatsReadUsesOlderRangeTask(t *testing.T) {
	f := []bitbuf.Field{bitbuf.Int(bitbuf.U8, 0), bitbuf.Int(bitbuf.I32, 19), bitbuf.Int(bitbuf.U64, 0), bitbuf.Int(bitbuf.U64, 1), bitbuf.Int(bitbuf.I64, 10)}
	q, err := parseStatsRead(4, f)
	if err != nil || q.Board != 19 || q.Position != 1 || q.Limit != 10 {
		t.Fatalf("range query: %+v, %v", q, err)
	}
	if _, err := parseStatsRead(5, f); err == nil {
		t.Fatal("accepted an unmapped query")
	}
}
