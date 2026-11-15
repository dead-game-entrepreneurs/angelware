// Package gateway implements the lobby service gateway (bdLobbyService).
//
// The client opens with plain message 7 {u32 ticket.a, u32 ticket.seed, 1024-bit
// lobby ticket} and then sends tasks encrypted with the session key from its login
// ticket. A task's message type is its service; the typed payload starts with
// u8 slot, u8 seq, u8 task id. Server messages: 1 = task reply, 2 = push (one
// mailbox message, see mailbox.go).
//
// Task replies (docs/games/svr2007/services.md): u8 slot, u8 seq, u32 error, u8 task id
// (echoed; each service's parser only accepts its own result-bearing ids), then
// u32 count and results for tasks that return lists.
package gateway

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/crypto"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
)

const (
	MsgHello     = 7
	msgTaskReply = 1
	msgPush      = 2
)

// Client is the transport surface available to application handlers.
type Client interface {
	Session() session.Session
	RemoteAddr() net.Addr
	Log() *slog.Logger
	Send(uint8, []byte) error
	TrySend(uint8, []byte) bool
}
type Handler interface {
	Handle(context.Context, Task) []byte
	Close(context.Context)
}
type Application interface {
	Open(context.Context, Client) Handler
}
type Server struct {
	Sessions     *session.Registry
	Applications map[string]Application
	Log          *slog.Logger
}

// Conn is one client's gateway connection.
type Conn struct {
	srv     *Server
	conn    net.Conn
	log     *slog.Logger
	sess    session.Session
	handler Handler
	out     chan outbound // frames for the writer goroutine
	done    chan struct{}
}

type outbound struct {
	frame  []byte
	queued time.Time
	task   *Task // correlation only; Args is not retained
}

// outQueue bounds a client's unsent frames; a client that stops reading loses
// pushes (they stay in the mailbox) instead of blocking whoever notifies it.
const outQueue = 64

// Task is a decoded client task.
type Task struct {
	Service, Slot, Seq, ID uint8
	Args                   *bitbuf.Reader
}

// Serve runs a gateway connection that opened with hello.
func (s *Server) Serve(ctx context.Context, nc net.Conn, r *bufio.Reader, hello frame.Frame) {
	c := &Conn{srv: s, conn: nc, log: s.Log.With("peer", nc.RemoteAddr().String()),
		out: make(chan outbound, outQueue), done: make(chan struct{})}
	go c.writer()
	defer close(c.done)
	if err := c.hello(hello); err != nil {
		c.log.Warn("gateway hello rejected", "err", err)
		return
	}
	app, ok := s.Applications[c.sess.Game]
	if !ok {
		c.log.Warn("game not enabled", "game", c.sess.Game)
		return
	}
	c.handler = app.Open(ctx, c)
	defer c.handler.Close(context.WithoutCancel(ctx))
	for {
		f, err := frame.Read(r)
		if err != nil {
			c.log.Info("gateway closed", "err", err)
			return
		}
		if err := c.handle(ctx, f); err != nil {
			c.log.Warn("gateway frame dropped", "err", err)
		}
	}
}

func (c *Conn) hello(f frame.Frame) error {
	r, err := bitbuf.NewReader(f.Payload)
	if err != nil {
		return err
	}
	r.U32()
	r.U32()
	ticket, err := r.Raw(session.TicketSize)
	if err != nil {
		return err
	}
	sess, ok := c.srv.Sessions.Redeem(ticket)
	if !ok {
		return fmt.Errorf("unknown or expired ticket")
	}
	c.sess = sess
	c.log = c.log.With("user", fmt.Sprintf("%016x", sess.UserID), "name", sess.Name, "game", sess.Game)
	c.log.Info("gateway connected")
	return nil
}

func (c *Conn) handle(ctx context.Context, f frame.Frame) error {
	started := time.Now()
	if !f.Encrypted {
		c.log.Debug("plain gateway message ignored", "type", f.Type)
		return nil
	}
	plain, err := dwcrypto.Decrypt(c.sess.Key, dwcrypto.SeedIV(f.Seed), f.Payload)
	if err != nil {
		return err
	}
	service, payload, err := frame.Open(plain)
	if err != nil {
		return err
	}
	r, err := bitbuf.NewReader(payload)
	if err != nil {
		return err
	}
	t := Task{Service: service, Args: r}
	if t.Slot, err = r.U8(); err == nil {
		if t.Seq, err = r.U8(); err == nil {
			t.ID, err = r.U8()
		}
	}
	if err != nil {
		return fmt.Errorf("task header: %w", err)
	}
	c.log.Debug("task", "service", t.Service, "task", t.ID, "slot", t.Slot, "seq", t.Seq,
		"payload", fmt.Sprintf("%x", payload))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	reply := c.handler.Handle(ctx, t)
	correlation := Task{Service: t.Service, Slot: t.Slot, Seq: t.Seq, ID: t.ID}
	err = c.enqueue(outbound{frame: frame.Plain(msgTaskReply, reply), task: &correlation})
	c.log.Debug("task replied", "service", t.Service, "task", t.ID, "slot", t.Slot, "seq", t.Seq,
		"elapsed_ms", float64(time.Since(started).Microseconds())/1000,
		"reply_bytes", len(reply), "queue_depth", len(c.out), "err", err)
	return err
}

// send queues a frame, waiting for room (task replies must not be lost).
func (c *Conn) Send(msgType uint8, payload []byte) error {
	return c.enqueue(outbound{frame: frame.Plain(msgType, payload)})
}

func (c *Conn) enqueue(b outbound) error {
	b.queued = time.Now()
	select {
	case c.out <- b:
		return nil
	case <-c.done:
		return net.ErrClosed
	}
}

// trySend queues a frame only if there is room right away.
func (c *Conn) TrySend(msgType uint8, payload []byte) bool {
	select {
	case c.out <- outbound{frame: frame.Plain(msgType, payload), queued: time.Now()}:
		return true
	default:
		return false
	}
}

func (c *Conn) writer() {
	for {
		select {
		case b := <-c.out:
			started := time.Now()
			n, err := c.conn.Write(b.frame)
			attrs := []any{"type", b.frame[3], "bytes", n,
				"queue_ms", float64(started.Sub(b.queued).Microseconds()) / 1000,
				"write_ms", float64(time.Since(started).Microseconds()) / 1000, "err", err}
			if b.task != nil {
				attrs = append(attrs, "service", b.task.Service, "task", b.task.ID, "slot", b.task.Slot, "seq", b.task.Seq)
			}
			c.log.Debug("gateway frame sent", attrs...)
			if err != nil {
				c.conn.Close()
				return
			}
		case <-c.done:
			return
		}
	}
}

// Reply is a task result without a list: u32 error 0, u8 task id, then fields
// (a single result object, e.g. a profile).
func Reply(t Task, fields []bitbuf.Field) []byte {
	return bitbuf.NewWriter(true).U8(t.Slot).U8(t.Seq).U32(0).U8(t.ID).Fields(fields).Bytes()
}

// ErrorReply fails a task: the client reads the non-zero error and nothing after it.
func ErrorReply(t Task, code uint32) []byte {
	return bitbuf.NewWriter(true).U8(t.Slot).U8(t.Seq).U32(code).Bytes()
}

// ListReply is a task result carrying count items (fields holds all of them).
func ListReply(t Task, count uint32, fields []bitbuf.Field) []byte {
	return bitbuf.NewWriter(true).U8(t.Slot).U8(t.Seq).U32(0).U8(t.ID).U32(count).Fields(fields).Bytes()
}

// UserID is the connection's user.
func (c *Conn) UserID() uint64 { return c.sess.UserID }

func (c *Conn) Session() session.Session { return c.sess }
func (c *Conn) RemoteAddr() net.Addr     { return c.conn.RemoteAddr() }
func (c *Conn) Log() *slog.Logger        { return c.log }
