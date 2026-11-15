// Package peer carries OutRun's native packets through a single outbound UDP
// connection. It deliberately does not use WWE's DemonWare peer transport.
package peer

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"
)

const HeaderSize = 32
const MaxPayload = 2048
const (
	Register uint32 = 1
	Data     uint32 = 2
	Ack      uint32 = 3
	Deliver  uint32 = 4
)

type Token [16]byte

type client struct {
	ip       netip.Addr
	virtual  netip.Addr
	endpoint netip.AddrPort
	seen     time.Time
	owners   map[any]bool
	window   time.Time
	packets  int
}

// Relay is bounded by authenticated lobby connections, not UDP registrations.
// A token is admitted only after a task encrypted with its lobby key was decoded.
type Relay struct {
	Bind      netip.AddrPort
	Log       *slog.Logger
	mu        sync.Mutex
	clients   map[Token]*client
	addresses map[netip.Addr]Token
	rooms     map[any]map[Token]bool
	next      uint32
}

func New(bind netip.AddrPort, log *slog.Logger) *Relay {
	return &Relay{Bind: bind, Log: log, clients: map[Token]*client{}, addresses: map[netip.Addr]Token{}, rooms: map[any]map[Token]bool{}}
}
func (r *Relay) UDPAddress() netip.AddrPort { return r.Bind }
func (r *Relay) Authorize(token Token, ip netip.Addr, owner any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if token == (Token{}) || !ip.Is4() {
		return
	}
	c := r.clients[token]
	if c == nil {
		if len(r.clients) >= 1024 {
			return
		}
		for {
			r.next = r.next%65534 + 1
			v := netip.AddrFrom4([4]byte{10, 254, byte(r.next >> 8), byte(r.next)})
			if _, exists := r.addresses[v]; !exists {
				c = &client{ip: ip, virtual: v, owners: map[any]bool{}}
				r.clients[token] = c
				r.addresses[v] = token
				break
			}
		}
	}
	// A second connection cannot move the same ticket to another public IP.
	if c.ip == ip {
		c.owners[owner] = true
	}
}
func (r *Relay) Revoke(token Token, owner any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.clients[token]
	if c == nil {
		return
	}
	delete(c.owners, owner)
	if len(c.owners) > 0 {
		return
	}
	delete(r.addresses, c.virtual)
	delete(r.clients, token)
	for room, members := range r.rooms {
		delete(members, token)
		if len(members) < 2 {
			delete(r.rooms, room)
		}
	}
}

// Offer permits traffic among clients given the same room by matchmaking.
func (r *Relay) Offer(room any, host, viewer Token) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients[host] == nil || r.clients[viewer] == nil {
		return
	}
	members := r.rooms[room]
	if members == nil {
		members = map[Token]bool{host: true}
		r.rooms[room] = members
	}
	if len(members) < 64 {
		members[viewer] = true
	}
}
func (r *Relay) DropRoom(room any) { r.mu.Lock(); delete(r.rooms, room); r.mu.Unlock() }
func Frame(kind uint32, token Token, ip netip.Addr, payload []byte) []byte {
	b := make([]byte, HeaderSize+len(payload))
	copy(b, "AOR1")
	binary.LittleEndian.PutUint32(b[4:], kind)
	copy(b[8:], token[:])
	if ip.Is4() {
		v := ip.As4()
		copy(b[24:], v[:])
	}
	binary.LittleEndian.PutUint32(b[28:], uint32(len(payload)))
	copy(b[32:], payload)
	return b
}

// Handle returns at most one packet and its destination. Native payloads remain
// opaque; neither a destination IP nor a port supplied by UDP is dialled directly.
func (r *Relay) Handle(src netip.AddrPort, b []byte, now time.Time) (netip.AddrPort, []byte) {
	if len(b) < HeaderSize || len(b) > HeaderSize+MaxPayload || string(b[:4]) != "AOR1" || int(binary.LittleEndian.Uint32(b[28:])) != len(b)-HeaderSize {
		return netip.AddrPort{}, nil
	}
	token := Token(b[8:24])
	kind := binary.LittleEndian.Uint32(b[4:])
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.clients[token]
	if c == nil || c.ip != src.Addr().Unmap() {
		return netip.AddrPort{}, nil
	}
	if now.Sub(c.window) >= time.Second {
		c.window = now
		c.packets = 0
	}
	c.packets++
	if c.packets > 600 {
		return netip.AddrPort{}, nil
	}
	if kind == Register && len(b) == HeaderSize {
		first := !c.endpoint.IsValid()
		c.endpoint = src
		c.seen = now
		if first && r.Log != nil {
			r.Log.Info("native peer registered", "virtual", c.virtual, "endpoint", src)
		}
		return src, Frame(Ack, token, c.virtual, nil)
	}
	if kind != Data || len(b) < HeaderSize+3 || c.endpoint != src || now.Sub(c.seen) > 30*time.Second {
		return netip.AddrPort{}, nil
	}
	dst := netip.AddrFrom4([4]byte(b[24:28]))
	otherToken, ok := r.addresses[dst]
	if !ok || otherToken == token {
		return netip.AddrPort{}, nil
	}
	other := r.clients[otherToken]
	if !other.endpoint.IsValid() || now.Sub(other.seen) > 30*time.Second {
		return netip.AddrPort{}, nil
	}
	allowed := false
	for _, members := range r.rooms {
		if members[token] && members[otherToken] {
			allowed = true
			break
		}
	}
	if !allowed {
		return netip.AddrPort{}, nil
	}
	return other.endpoint, Frame(Deliver, otherToken, c.virtual, b[HeaderSize:])
}
func (r *Relay) Run(ctx context.Context) error {
	conn, err := net.ListenUDP("udp4", net.UDPAddrFromAddrPort(r.Bind))
	if err != nil {
		return err
	}
	defer conn.Close()
	r.Log.Info("native relay listening", "address", conn.LocalAddr())
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stopped:
		}
	}()
	buf := make([]byte, HeaderSize+MaxPayload+1)
	for {
		n, src, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("native relay: %w", err)
		}
		dst, out := r.Handle(src, buf[:n], time.Now())
		if out != nil {
			if _, err = conn.WriteToUDPAddrPort(out, dst); err != nil && ctx.Err() == nil {
				r.Log.Debug("native relay send failed", "err", err)
			}
		}
	}
}
