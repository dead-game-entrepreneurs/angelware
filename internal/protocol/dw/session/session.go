// Package session issues the lobby tickets handed out at login and redeemed when
// the client opens the lobby service gateway.
package session

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

const (
	TicketSize = 0x80 // the client forwards these 1024 bits verbatim
	KeySize    = 24   // 3DES session key for lobby traffic
	ttl        = 15 * time.Minute
)

var ticketMagic = [4]byte{'A', 'N', 'G', 'L'}

type Session struct {
	Token   [16]byte
	Game    string
	TitleID uint32
	UserID  uint64
	Name    string
	Key     []byte
	Guest   bool // temporary identity, never a password account
	expires time.Time
}

type Registry struct {
	mu       sync.Mutex
	sessions map[[16]byte]Session
}

func NewRegistry() *Registry { return &Registry{sessions: map[[16]byte]Session{}} }

// Issue creates a session and returns its opaque lobby ticket.
func (r *Registry) Issue(game string, title uint32, userID uint64, name string) (Session, []byte) {
	return r.issue(game, title, userID, name, false)
}

// IssueGuest creates a fresh identity for an anonymous title login. Neither the
// request seed nor the client's IP is a persistent account identifier.
func (r *Registry) IssueGuest(game string, title uint32) (Session, []byte) {
	var id [8]byte
	for binary.LittleEndian.Uint64(id[:]) == 0 {
		rand.Read(id[:])
	}
	user := binary.LittleEndian.Uint64(id[:])
	return r.issue(game, title, user, fmt.Sprintf("Guest%08x", uint32(user)), true)
}

func (r *Registry) issue(game string, title uint32, userID uint64, name string, guest bool) (Session, []byte) {
	var token [16]byte
	rand.Read(token[:])
	s := Session{Token: token, Game: game, TitleID: title, UserID: userID, Name: name, Key: make([]byte, KeySize), Guest: guest, expires: time.Now().Add(ttl)}
	rand.Read(s.Key)

	ticket := make([]byte, TicketSize)
	copy(ticket, ticketMagic[:])
	copy(ticket[4:], token[:])
	binary.LittleEndian.PutUint64(ticket[20:], userID)

	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for k, v := range r.sessions {
		if now.After(v.expires) {
			delete(r.sessions, k)
		}
	}
	r.sessions[token] = s
	return s, ticket
}

// Redeem looks up the session for a ticket. Tickets stay valid until they expire so
// a client can reconnect to the gateway after a dropped connection.
func (r *Registry) Redeem(ticket []byte) (Session, bool) {
	if len(ticket) < 20 || [4]byte(ticket[:4]) != ticketMagic {
		return Session{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[[16]byte(ticket[4:20])]
	if !ok || time.Now().After(s.expires) {
		return Session{}, false
	}
	return s, true
}
