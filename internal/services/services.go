// Package services contains reusable DemonWare application services.
package services

import (
	"context"
	"log/slog"
	"net"
	"sync"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

type Task = gateway.Task

var Reply = gateway.Reply
var ErrorReply = gateway.ErrorReply
var ListReply = gateway.ListReply

const msgPush = 2
const (
	StatsService       = 4
	MatchmakingService = 5
	MailboxService     = 6
	ProfilesService    = 8
	FriendsService     = 9
	StorageService     = 10
	TitleService       = 12
)

type Handler func(context.Context, *Conn, Task) []byte

// Server is a game's service set and transient state. Register only at startup.
type Server struct {
	Store       store.Store
	Matchmaking *Matchmaking
	Log         *slog.Logger
	// MessageFields selects a game's notification codec for both push and fetch.
	// Nil uses the legacy shared codec.
	MessageFields func(store.Message) []bitbuf.Field
	handlers      map[uint8]Handler
	mu            sync.Mutex
	online        map[uint64]*Conn
	invites       map[[2]uint64]invite
}
type Conn struct {
	srv    *Server
	client gateway.Client
	log    *slog.Logger
	sess   session.Session
}

func (s *Server) Register(id uint8, h Handler) {
	if s.handlers == nil {
		s.handlers = map[uint8]Handler{}
	}
	if h == nil {
		panic("services: nil handler")
	}
	if _, ok := s.handlers[id]; ok {
		panic("services: duplicate handler")
	}
	s.handlers[id] = h
}
func (s *Server) Open(ctx context.Context, client gateway.Client) gateway.Handler {
	c := &Conn{srv: s, client: client, log: client.Log(), sess: client.Session()}
	s.setOnline(ctx, c)
	return c
}
func (c *Conn) Handle(ctx context.Context, t Task) []byte {
	if h := c.srv.handlers[t.Service]; h != nil {
		return h(ctx, c, t)
	}
	c.log.Warn("unsupported lobby service", "service", t.Service, "task", t.ID)
	return ErrorReply(t, 1)
}
func (c *Conn) Close(ctx context.Context) {
	if c.srv.Matchmaking != nil {
		c.srv.Matchmaking.DropOwner(c)
	}
	c.srv.setOffline(ctx, c)
}
func (c *Conn) Store() store.Store       { return c.srv.Store }
func (c *Conn) Session() session.Session { return c.sess }
func (c *Conn) UserID() uint64           { return c.sess.UserID }
func (c *Conn) Log() *slog.Logger        { return c.log }
func (c *Conn) RemoteAddr() net.Addr     { return c.client.RemoteAddr() }
func MatchmakingHandler(m *Matchmaking) Handler {
	return func(_ context.Context, c *Conn, t Task) []byte { return m.Handle(c, t) }
}
func Friends() Handler {
	return func(ctx context.Context, c *Conn, t Task) []byte { return c.friends(ctx, t) }
}
func Mailbox() Handler {
	return func(ctx context.Context, c *Conn, t Task) []byte { return c.mailbox(ctx, t) }
}
func Files() Handler {
	return func(ctx context.Context, c *Conn, t Task) []byte { return c.storage(ctx, t) }
}
func TitleUtilities() Handler {
	return func(_ context.Context, c *Conn, t Task) []byte { return c.title(t) }
}

// Fields is a convenience for game codecs; the envelope remains in gateway.
func Fields(t Task) ([]bitbuf.Field, error) { return t.Args.Fields() }

// push sends one mailbox message to this client right away, without blocking.
func (c *Conn) push(m store.Message) {
	payload := bitbuf.NewWriter(true).Fields(c.srv.messageFields(m)).Bytes()
	if !c.client.TrySend(msgPush, payload) {
		c.log.Warn("push dropped: client queue full", "type", m.Type)
	}
}

func (s *Server) setOnline(ctx context.Context, c *Conn) {
	s.mu.Lock()
	if s.online == nil {
		s.online = map[uint64]*Conn{}
	}
	prev := s.online[c.sess.UserID]
	s.online[c.sess.UserID] = c
	s.mu.Unlock()
	if prev == nil {
		s.announce(ctx, c.sess, msgFriendOnline)
	}
}

func (s *Server) setOffline(ctx context.Context, c *Conn) {
	s.mu.Lock()
	gone := s.online[c.sess.UserID] == c
	var invitees []uint64
	if gone {
		delete(s.online, c.sess.UserID)
		for key := range s.invites {
			if key[0] == c.sess.UserID {
				delete(s.invites, key)
				invitees = append(invitees, key[1])
			}
		}
	}
	s.mu.Unlock()
	if gone {
		for _, to := range invitees {
			if err := s.Store.ClearMessages(context.WithoutCancel(ctx), to, c.sess.UserID, msgInvite); err != nil {
				s.Log.Error("clear disconnected host invitation", "err", err)
			}
		}
		s.announce(context.WithoutCancel(ctx), c.sess, msgFriendOffline)
	}
}

// Online returns the gateway connection of a user, or nil.
func (s *Server) Online(user uint64) *Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.online[user]
}
