// Package store holds persistent data: accounts, profiles and stats.
package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrExists   = errors.New("store: already exists")
)

// Account is a DemonWare account. UserID is Tiger(name)[:8]; PasswordTiger is the
// Tiger hash the client sends at signup and uses as its login key. It is
// password-equivalent for this protocol, so treat the column as a secret.
type Account struct {
	UserID        uint64
	Name          string
	PasswordTiger []byte
}

// Profile is a versioned game-owned record. The game codec interprets Payload.
type Profile struct {
	Version int
	Payload []byte
}

// StatsRow is one leaderboard write (lobby service 4, task 1).
type StatsRow struct {
	Board  int32
	Flag   uint8
	Rating int64
	Values []int32
	Extra  []byte
}

// Accounts addresses the unified credential directory shared by every game.
type Accounts interface {
	CreateAccount(ctx context.Context, a Account) error
	Account(ctx context.Context, userID uint64) (Account, error)
	TouchLogin(ctx context.Context, userID uint64) error
	SetPassword(ctx context.Context, userID uint64, passwordTiger []byte) error
	// DeleteAccount removes the shared identity and its data across every game.
	DeleteAccount(ctx context.Context, userID uint64) error
}

// Store combines shared identity with permanently scoped gameplay data.
type Store interface {
	Accounts
	Profile(ctx context.Context, userID uint64, kind string) (Profile, error)
	SetProfile(ctx context.Context, userID uint64, kind string, record Profile) error
	DeleteProfile(ctx context.Context, userID uint64) error
	WriteStats(ctx context.Context, userID uint64, row StatsRow) error
	DeleteStats(ctx context.Context, userID uint64, board int32) error
	Social
	Leaderboards
	Files
}

// Memory is an in-process Store for tests and quick local runs.
type Memory struct {
	mu       sync.Mutex
	accounts *memoryAccounts
	database *MemoryDB
	profiles map[profileKey]Profile
	stats    map[[2]int64]StatsRow
	s        memSocial
	f        memFiles
}

func NewMemory() *Memory {
	return &Memory{accounts: &memoryAccounts{values: map[uint64]Account{}}, profiles: map[profileKey]Profile{}, stats: map[[2]int64]StatsRow{}, s: newMemSocial()}
}

func (m *Memory) CreateAccount(_ context.Context, a Account) error {
	m.accounts.mu.Lock()
	defer m.accounts.mu.Unlock()
	for _, existing := range m.accounts.values {
		if existing.UserID == a.UserID || existing.Name == a.Name {
			return ErrExists
		}
	}
	a.PasswordTiger = append([]byte{}, a.PasswordTiger...)
	m.accounts.values[a.UserID] = a
	return nil
}

func (m *Memory) Account(_ context.Context, id uint64) (Account, error) {
	m.accounts.mu.Lock()
	defer m.accounts.mu.Unlock()
	a, ok := m.accounts.values[id]
	if !ok {
		return a, ErrNotFound
	}
	a.PasswordTiger = append([]byte{}, a.PasswordTiger...)
	return a, nil
}

func (m *Memory) TouchLogin(context.Context, uint64) error { return nil }

func (m *Memory) SetPassword(_ context.Context, id uint64, pw []byte) error {
	m.accounts.mu.Lock()
	defer m.accounts.mu.Unlock()
	a, ok := m.accounts.values[id]
	if !ok {
		return ErrNotFound
	}
	a.PasswordTiger = append([]byte{}, pw...)
	m.accounts.values[id] = a
	return nil
}

type memoryAccounts struct {
	mu     sync.Mutex
	values map[uint64]Account
}

func (m *Memory) DeleteAccount(_ context.Context, id uint64) error {
	if m.database != nil {
		return m.database.deleteAccount(id)
	}
	m.accounts.mu.Lock()
	delete(m.accounts.values, id)
	m.accounts.mu.Unlock()
	return m.deleteGameData(id)
}
func (m *Memory) deleteGameData(id uint64) error {
	m.mu.Lock()
	for k := range m.profiles {
		if k.user == id {
			delete(m.profiles, k)
		}
	}
	for k := range m.stats {
		if uint64(k[0]) == id {
			delete(m.stats, k)
		}
	}
	for k, f := range m.f.files {
		if f.Owner == id {
			delete(m.f.files, k)
		}
	}
	m.mu.Unlock()
	m.s.mu.Lock()
	defer m.s.mu.Unlock()
	for k, msg := range m.s.mail {
		if msg.To == id || msg.From == id {
			delete(m.s.mail, k)
		}
	}
	for k := range m.s.requests {
		if k[0] == id || k[1] == id {
			delete(m.s.requests, k)
		}
	}
	for k := range m.s.friends {
		if k[0] == id || k[1] == id {
			delete(m.s.friends, k)
		}
	}
	for k := range m.s.blocks {
		if k[0] == id || k[1] == id {
			delete(m.s.blocks, k)
		}
	}
	return nil
}

type profileKey struct {
	user uint64
	kind string
}

func (m *Memory) Profile(_ context.Context, id uint64, kind string) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[profileKey{id, kind}]
	if !ok {
		return p, ErrNotFound
	}
	p.Payload = append([]byte(nil), p.Payload...)
	return p, nil
}
func (m *Memory) SetProfile(_ context.Context, id uint64, kind string, p Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p.Payload = append([]byte(nil), p.Payload...)
	m.profiles[profileKey{id, kind}] = p
	return nil
}
func (m *Memory) DeleteProfile(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.profiles {
		if k.user == id {
			delete(m.profiles, k)
		}
	}
	return nil
}

func (m *Memory) WriteStats(_ context.Context, id uint64, row StatsRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats[[2]int64{int64(id), int64(row.Board)}] = row
	return nil
}

func (m *Memory) DeleteStats(_ context.Context, id uint64, board int32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.stats, [2]int64{int64(id), int64(board)})
	return nil
}

// Stats returns a stored row (tests).
func (m *Memory) Stats(id uint64, board int32) (StatsRow, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.stats[[2]int64{int64(id), int64(board)}]
	return r, ok
}

func (m *Memory) Close() {}

// Timeout bounds every store call made while handling a client request.
const Timeout = 5 * time.Second
