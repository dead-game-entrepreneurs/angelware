package services

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
)

// Matchmaking is lobby service 5 (senders at LBYM 0x64a88..0x64da8):
//
//	1 create(u8, info)  2 update(u8, info)  3 delete(u8, blob id)
//	4 find by id(u8, blob id)               5 find(u8, query)
//
// A session info serializes symmetrically (base 0x652c8/0x65480, game fields
// 0x27c90/0x281b0): blob host address[25], blob id[8], blob key[16], 4 x i32 slot
// counts, then the game's fields. The host's info is stored with the id and key we
// assign and returned to searchers unchanged. Sessions only live as long as the
// host's gateway connection.
//
// Replies (0x64ea0): u32 error, u8 task id (1/4/5/6 carry results), u32 count, results.
// A create result is blob id[8], blob key[16].
type Matchmaking struct {
	// Disclosed and Removed let a game admit its own peer transport. Callbacks
	// run under the matchmaking lock and must not call back into Matchmaking.
	Disclosed           func(room [8]byte, host, viewer any)
	Removed             func(room [8]byte)
	MaxResults          int
	Log                 *slog.Logger
	ObserveConnectivity func(ConnectivityEvent)
	Search              SearchPolicy
	Validate            func([]bitbuf.Field) error
	InspectConnectivity func([]bitbuf.Field) (netip.AddrPort, int, bool)
	Completed           func(before, after int) bool
	mu                  sync.RWMutex
	sessions            map[[8]byte]*mmSession
	created             uint64
}

type mmSession struct {
	key         []byte
	owner       any
	info        []bitbuf.Field
	order       uint64 // creation order, for stable search results
	createdAt   time.Time
	disclosed   any
	disclosedIP netip.Addr
	ambiguous   bool
}

const (
	mmCreate   = 1
	mmUpdate   = 2
	mmDelete   = 3
	mmFindByID = 4
	mmFind     = 5

	mmMaxResults = 20
	infoID       = 1 // field positions in a session info
	infoKey      = 2
)

func NewMatchmaking(log *slog.Logger) *Matchmaking {
	return &Matchmaking{Log: log, sessions: map[[8]byte]*mmSession{}}
}

func mmReply(t Task, count uint32, fields []bitbuf.Field) []byte {
	return ListReply(t, count, fields)
}

// Handle answers one matchmaking task for owner (the host's connection).
func (m *Matchmaking) Handle(owner any, t Task) []byte {
	fields, err := t.Args.Fields()
	if err != nil || len(fields) < 1 {
		m.Log.Warn("matchmaking task did not parse", "task", t.ID, "err", err)
		return mmReply(t, 0, nil)
	}
	args := fields[1:] // skip the leading u8 (always 0 so far)

	switch t.ID {
	case mmCreate, mmUpdate:
		if !validBase(args) {
			return ErrorReply(t, 1)
		}
		if m.Validate != nil {
			if err := m.Validate(args); err != nil {
				m.Log.Warn("invalid session", "err", err)
				return ErrorReply(t, 1)
			}
		}

		if t.ID == mmCreate {
			return m.create(owner, t, args)
		}
		m.update(owner, args)
		return mmReply(t, 0, nil)
	case mmDelete:
		if len(args) > 0 && len(args[0].Bytes) == 8 {
			m.mu.Lock()
			id := [8]byte(args[0].Bytes)
			if entry := m.sessions[id]; entry != nil && entry.owner == owner {
				delete(m.sessions, id)
				if m.Removed != nil {
					m.Removed(id)
				}
			}
			m.mu.Unlock()
			m.Log.Info("session deleted", "session", fmt.Sprintf("%x", args[0].Bytes))
		}
		return mmReply(t, 0, nil)
	case mmFindByID:
		if len(args) > 0 && len(args[0].Bytes) == 8 {
			m.mu.Lock()
			entry := m.sessions[[8]byte(args[0].Bytes)]
			var info []bitbuf.Field
			if entry != nil {
				info = append([]bitbuf.Field(nil), entry.info...)
				if m.Disclosed != nil {
					m.Disclosed([8]byte(args[0].Bytes), entry.owner, owner)
				}
				if m.ObserveConnectivity != nil {
					entry.noteDisclosure(owner)
				}
			}
			m.mu.Unlock()
			if entry != nil {
				return mmReply(t, 1, info)
			}
		}
		return mmReply(t, 0, nil)
	case mmFind:
		results := m.search(owner, args)
		var out []bitbuf.Field
		for _, info := range results {
			out = append(out, info...)
		}
		m.mu.RLock()
		total := len(m.sessions)
		m.mu.RUnlock()
		m.Log.Info("find sessions", "returned", len(results), "open", total, "query_fields", len(args))
		return mmReply(t, uint32(len(results)), out)
	}
	m.Log.Info("matchmaking task not implemented", "task", t.ID)
	return mmReply(t, 0, nil)
}

func (m *Matchmaking) create(owner any, t Task, info []bitbuf.Field) []byte {
	var id [8]byte
	key := make([]byte, 16)
	rand.Read(id[:])
	rand.Read(key)
	info[infoID] = bitbuf.Bytes(bitbuf.Blob, id[:])
	info[infoKey] = bitbuf.Bytes(bitbuf.Blob, key)
	m.mu.Lock()
	m.created++
	entry := &mmSession{key: key, owner: owner, info: info, order: m.created, createdAt: time.Now()}
	m.sessions[id] = entry
	event, eligible := entry.connectivityEvent(id, info, entry.createdAt, m.InspectConnectivity)
	n := len(m.sessions)
	m.mu.Unlock()
	m.Log.Info("session created", "session", fmt.Sprintf("%x", id), "open", n)
	if m.ObserveConnectivity != nil && eligible {
		event.Created = true
		m.ObserveConnectivity(event)
	}
	return mmReply(t, 1, []bitbuf.Field{bitbuf.Bytes(bitbuf.Blob, id[:]), bitbuf.Bytes(bitbuf.Blob, key)})
}

func (m *Matchmaking) update(owner any, info []bitbuf.Field) {
	id := [8]byte(info[infoID].Bytes)
	m.mu.Lock()
	var event ConnectivityEvent
	notify := false
	if entry := m.sessions[id]; entry != nil && entry.owner == owner {
		before, validBefore := m.inspect(entry.info)
		after, validAfter := m.inspect(info)
		if m.ObserveConnectivity != nil && m.Completed != nil && validBefore && validAfter && m.Completed(before, after) {
			event, notify = entry.connectivityEvent(id, info, time.Now(), m.InspectConnectivity)
		}
		info[infoKey] = bitbuf.Bytes(bitbuf.Blob, entry.key)
		entry.info = info
	}
	m.mu.Unlock()
	if notify {
		m.ObserveConnectivity(event)
	}
}

// DropOwner removes the sessions hosted on a closed connection.
func (m *Matchmaking) DropOwner(owner any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.owner == owner {
			delete(m.sessions, id)
			if m.Removed != nil {
				m.Removed(id)
			}
			m.Log.Info("session closed with its host", "session", fmt.Sprintf("%x", id))
		}
	}
}

// Open returns the number of live sessions.
func (m *Matchmaking) Open() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// RoomKey supplies the optional peer rewriter with the key from the same room
// that the captured DTLS handshake names. The caller receives its own copy.
func (m *Matchmaking) RoomKey(id [8]byte) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s := m.sessions[id]; s != nil {
		return append([]byte(nil), s.key...), true
	}
	return nil, false
}

// Candidate is a snapshot of one room; policies cannot mutate room ownership or keys.
type Candidate struct {
	ID    [8]byte
	Info  []bitbuf.Field
	Order uint64
}
type SearchPolicy func(query []bitbuf.Field, rooms []Candidate) []Candidate

func (m *Matchmaking) inspect(info []bitbuf.Field) (int, bool) {
	if m.InspectConnectivity == nil {
		return 0, false
	}
	_, n, ok := m.InspectConnectivity(info)
	return n, ok
}
func (m *Matchmaking) search(owner any, query []bitbuf.Field) [][]bitbuf.Field {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Search == nil {
		return nil
	}
	var rooms []Candidate
	for id, s := range m.sessions {
		if s.owner != owner {
			rooms = append(rooms, Candidate{ID: id, Info: append([]bitbuf.Field(nil), s.info...), Order: s.order})
		}
	}
	selected := m.Search(query, rooms)
	var out [][]bitbuf.Field
	for _, room := range selected[:min(len(selected), m.limit())] {
		s := m.sessions[room.ID]
		if s == nil || s.owner == owner || s.order != room.Order {
			continue
		}
		if m.ObserveConnectivity != nil {
			s.noteDisclosure(owner)
		}
		if m.Disclosed != nil {
			m.Disclosed(room.ID, s.owner, owner)
		}
		out = append(out, append([]bitbuf.Field(nil), s.info...))
	}
	return out
}

func validBase(info []bitbuf.Field) bool {
	if len(info) < 7 {
		return false
	}
	for i, n := range []int{25, 8, 16} {
		if info[i].Type != bitbuf.Blob || len(info[i].Bytes) != n {
			return false
		}
	}
	for _, v := range info[3:7] {
		if v.Type != bitbuf.I32 || int32(v.Int) < 0 {
			return false
		}
	}
	return info[4].Int <= info[3].Int && info[6].Int <= info[5].Int
}

func (m *Matchmaking) limit() int {
	if m.MaxResults > 0 {
		return m.MaxResults
	}
	return mmMaxResults
}
