package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDB owns the connection pool; ForGame returns a permanently scoped view.
type PostgresDB struct{ pool *pgxpool.Pool }
type Postgres struct {
	pool *pgxpool.Pool
	game string
}

func OpenPostgres(ctx context.Context, url string) (*PostgresDB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresDB{pool: pool}, nil
}
func (p *PostgresDB) ForGame(key string) Store {
	if key == "" {
		panic("storage: empty game scope")
	}
	return &Postgres{pool: p.pool, game: key}
}
func (p *PostgresDB) Close() { p.pool.Close() }

func (p *Postgres) CreateAccount(ctx context.Context, a Account) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO accounts (user_id, name, password_tiger) VALUES ($1, $2, $3)`,
		int64(a.UserID), a.Name, a.PasswordTiger)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrExists
	}
	return err
}

func (p *Postgres) Account(ctx context.Context, id uint64) (Account, error) {
	a := Account{UserID: id}
	err := p.pool.QueryRow(ctx,
		`SELECT name, password_tiger FROM accounts WHERE user_id = $1`,
		int64(id)).Scan(&a.Name, &a.PasswordTiger)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (p *Postgres) TouchLogin(ctx context.Context, id uint64) error {
	_, err := p.pool.Exec(ctx, `UPDATE accounts SET last_login = now() WHERE user_id = $1`, int64(id))
	return err
}

func (p *Postgres) SetPassword(ctx context.Context, id uint64, pw []byte) error {
	_, err := p.pool.Exec(ctx, `UPDATE accounts SET password_tiger = $2 WHERE user_id = $1`, int64(id), pw)
	return err
}

// DeleteAccount removes the shared identity and its data in every game atomically.
func (p *Postgres) DeleteAccount(ctx context.Context, id uint64) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, q := range []string{
		`DELETE FROM accounts WHERE user_id = $1`,
		`DELETE FROM profiles WHERE user_id = $1`,
		`DELETE FROM stats WHERE user_id = $1`,
		`DELETE FROM storage_files WHERE owner = $1`,
		`DELETE FROM mailbox WHERE (to_id = $1 OR from_id = $1)`,
		`DELETE FROM friend_requests WHERE (from_id = $1 OR to_id = $1)`,
		`DELETE FROM friends WHERE (user_id = $1 OR friend_id = $1)`,
		`DELETE FROM blocks WHERE (user_id = $1 OR blocked_id = $1)`,
	} {
		if _, err := tx.Exec(ctx, q, int64(id)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *Postgres) Profile(ctx context.Context, id uint64, kind string) (Profile, error) {
	var r Profile
	err := p.pool.QueryRow(ctx, `SELECT version, payload FROM profiles WHERE game_id = $1 AND user_id = $2 AND kind = $3`, p.game, int64(id), kind).Scan(&r.Version, &r.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}
func (p *Postgres) SetProfile(ctx context.Context, id uint64, kind string, r Profile) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO profiles (game_id, user_id, kind, version, payload) VALUES ($1, $2, $3, $4, $5)
 ON CONFLICT (game_id, user_id, kind) DO UPDATE SET version = $4, payload = $5, updated_at = now()`, p.game, int64(id), kind, r.Version, r.Payload)
	return err
}
func (p *Postgres) DeleteProfile(ctx context.Context, id uint64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM profiles WHERE game_id = $1 AND user_id = $2`, p.game, int64(id))
	return err
}

func (p *Postgres) WriteStats(ctx context.Context, id uint64, r StatsRow) error {
	if r.Values == nil {
		r.Values = []int32{} // NOT NULL column; rating-only boards have no extra values
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO stats (game_id, user_id, board, flag, rating, vals, extra) VALUES ($6, $1, $2, $3, $4, $5, $7)
		ON CONFLICT (game_id, user_id, board) DO UPDATE
		SET flag = $3, rating = $4, vals = $5, extra = $7, updated_at = now()`,
		int64(id), r.Board, int16(r.Flag), r.Rating, r.Values, p.game, r.Extra)
	return err
}

func (p *Postgres) DeleteStats(ctx context.Context, id uint64, board int32) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM stats WHERE game_id = $3 AND user_id = $1 AND board = $2`, int64(id), board, p.game)
	return err
}
