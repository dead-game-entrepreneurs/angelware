package store

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

// RankedRow is one leaderboard row as the game reads it: the player, their
// score, their rank on the board and the board's extra columns.
type RankedRow struct {
	UserID uint64
	Name   string
	Rating int64
	Rank   uint64 // 1-based position on the board (unique; ties ordered by user id)
	Values []int32
	Extra  []byte
}

// Leaderboards ranks one game's board in the order selected by its codec.
// Positions are unique; tied ratings are ordered by user ID.
type Leaderboards interface {
	// StatsFor returns the rows of the given players (missing players are skipped).
	StatsFor(ctx context.Context, board int32, users []uint64, ascending bool) ([]RankedRow, error)
	// StatsFrom returns up to limit rows starting at a 1-based position.
	StatsFrom(ctx context.Context, board int32, position, limit int, ascending bool) ([]RankedRow, error)
	// StatsAround returns up to limit rows centred on a player; empty if they have no row.
	StatsAround(ctx context.Context, board int32, user uint64, limit int, ascending bool) ([]RankedRow, error)
	// StatsTotal is the number of rows on a board.
	StatsTotal(ctx context.Context, board int32) (int, error)
}

// aroundStart is the 1-based position a window of limit rows centred on pos starts at.
func aroundStart(pos, limit int) int {
	return max(pos-limit/2, 1)
}

// --- in-memory -------------------------------------------------------------

// ranked returns a board's rows in leaderboard order with ranks filled in.
func (m *Memory) ranked(board int32, ascending bool) []RankedRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.accounts.mu.Lock()
	defer m.accounts.mu.Unlock()
	var rows []RankedRow
	for k, r := range m.stats {
		if int32(k[1]) == board {
			rows = append(rows, RankedRow{UserID: uint64(k[0]), Name: m.accounts.values[uint64(k[0])].Name,
				Rating: r.Rating, Values: r.Values, Extra: r.Extra})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Rating != rows[j].Rating {
			if ascending {
				return rows[i].Rating < rows[j].Rating
			}
			return rows[i].Rating > rows[j].Rating
		}
		return rows[i].UserID < rows[j].UserID
	})
	for i := range rows {
		rows[i].Rank = uint64(i + 1)
	}
	return rows
}

func (m *Memory) StatsFor(_ context.Context, board int32, users []uint64, ascending bool) ([]RankedRow, error) {
	want := map[uint64]bool{}
	for _, u := range users {
		want[u] = true
	}
	var out []RankedRow
	for _, r := range m.ranked(board, ascending) {
		if want[r.UserID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *Memory) StatsFrom(_ context.Context, board int32, position, limit int, ascending bool) ([]RankedRow, error) {
	rows := m.ranked(board, ascending)
	start := max(position, 1) - 1
	if start >= len(rows) {
		return nil, nil
	}
	return rows[start:min(start+limit, len(rows))], nil
}

func (m *Memory) StatsAround(ctx context.Context, board int32, user uint64, limit int, ascending bool) ([]RankedRow, error) {
	for i, r := range m.ranked(board, ascending) {
		if r.UserID == user {
			return m.StatsFrom(ctx, board, aroundStart(i+1, limit), limit, ascending)
		}
	}
	return nil, nil
}

func (m *Memory) StatsTotal(_ context.Context, board int32) (int, error) {
	return len(m.ranked(board, false)), nil
}

// --- PostgreSQL --------------------------------------------------------------

func (p *Postgres) StatsTotal(ctx context.Context, board int32) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM stats WHERE game_id = $2 AND board = $1`, board, p.game).Scan(&n)
	return n, err
}

// boardSQL ranks one board: pos is the row's position, which is also its rank.
const boardSQL = `
	SELECT s.user_id, coalesce(a.name, '') AS name, s.rating, s.vals, s.extra,
	       row_number() OVER (ORDER BY CASE WHEN $3 THEN s.rating END ASC, CASE WHEN NOT $3 THEN s.rating END DESC, s.user_id) AS pos
	FROM stats s LEFT JOIN accounts a ON a.user_id = s.user_id
	WHERE s.game_id = $2 AND s.board = $1`

func (p *Postgres) StatsFor(ctx context.Context, board int32, users []uint64, ascending bool) ([]RankedRow, error) {
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = int64(u)
	}
	return p.rankedRows(ctx, `SELECT user_id, name, rating, vals, extra, pos FROM (`+boardSQL+`) b
		WHERE user_id = ANY($4) ORDER BY pos`, board, p.game, ascending, ids)
}

func (p *Postgres) StatsFrom(ctx context.Context, board int32, position, limit int, ascending bool) ([]RankedRow, error) {
	return p.rankedRows(ctx, `SELECT user_id, name, rating, vals, extra, pos FROM (`+boardSQL+`) b
		WHERE pos >= $4 ORDER BY pos LIMIT $5`, board, p.game, ascending, max(position, 1), limit)
}

func (p *Postgres) StatsAround(ctx context.Context, board int32, user uint64, limit int, ascending bool) ([]RankedRow, error) {
	var pos int
	err := p.pool.QueryRow(ctx, `SELECT pos FROM (`+boardSQL+`) b WHERE user_id = $4`,
		board, p.game, ascending, int64(user)).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // no row on this board: nothing to centre on
	} else if err != nil {
		return nil, err
	}
	return p.StatsFrom(ctx, board, aroundStart(pos, limit), limit, ascending)
}

func (p *Postgres) rankedRows(ctx context.Context, query string, args ...any) ([]RankedRow, error) {
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankedRow
	for rows.Next() {
		var r RankedRow
		var id, rank int64
		if err := rows.Scan(&id, &r.Name, &r.Rating, &r.Values, &r.Extra, &rank); err != nil {
			return nil, err
		}
		r.UserID, r.Rank = uint64(id), uint64(rank)
		out = append(out, r)
	}
	return out, rows.Err()
}
