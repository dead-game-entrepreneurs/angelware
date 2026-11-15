package store

import "sync"

// Database owns shared credentials and game stores. Gameplay data is permanently
// scoped; credential operations intentionally address the unified account directory.
type Database interface {
	ForGame(key string) Store
	Close()
}

type MemoryDB struct {
	mu       sync.Mutex
	games    map[string]*Memory
	accounts *memoryAccounts
}

func NewMemoryDB() *MemoryDB {
	return &MemoryDB{games: map[string]*Memory{}, accounts: &memoryAccounts{values: map[uint64]Account{}}}
}
func (d *MemoryDB) ForGame(key string) Store {
	if key == "" {
		panic("storage: empty game scope")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.games[key] == nil {
		d.games[key] = NewMemory()
		d.games[key].accounts = d.accounts
		d.games[key].database = d
	}
	return d.games[key]
}
func (d *MemoryDB) Close() {}

func (d *MemoryDB) deleteAccount(id uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accounts.mu.Lock()
	delete(d.accounts.values, id)
	d.accounts.mu.Unlock()
	for _, s := range d.games {
		if err := s.deleteGameData(id); err != nil {
			return err
		}
	}
	return nil
}
