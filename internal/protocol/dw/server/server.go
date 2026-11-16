// Package server accepts TCP connections on the shared auth/gateway port.
//
// Both DemonWare hostnames (auth.mmp and lsg.mmp) point at one address and port, so
// each connection is classified by its first frame: the gateway opens with plain
// message 7, auth requests always use even message types.
package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
)

// firstFrameTimeout bounds how long an idle connection may sit before speaking.
const firstFrameTimeout = 30 * time.Second

type TCP struct {
	Auth  map[uint32]*auth.Service
	Lobby *gateway.Server
	Log   *slog.Logger
}

// Serve accepts connections until ctx ends.
func (s *TCP) Serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	var active sync.WaitGroup
	defer active.Wait()
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			ln.Close()
		case <-stopped:
		}
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			s.Log.Warn("accept", "err", err)
			continue
		}
		active.Add(1)
		go func() { defer active.Done(); s.handle(ctx, c) }()
	}
}

func (s *TCP) handle(ctx context.Context, c net.Conn) {
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			c.Close()
		case <-finished:
		}
	}()
	r := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(firstFrameTimeout))
	first, err := s.readFirst(r)
	if err != nil {
		s.Log.Debug("connection closed before first frame", "peer", c.RemoteAddr().String(), "err", err)
		return
	}
	c.SetReadDeadline(time.Time{})
	if !first.Encrypted && first.Type == gateway.MsgHello {
		s.Lobby.Serve(ctx, c, r, first)
		return
	}
	if first.Encrypted {
		return
	}
	payload, err := bitbuf.NewReader(first.Payload)
	if err != nil {
		return
	}
	if _, err = payload.U32(); err != nil {
		return
	}
	title, err := payload.U32()
	if err != nil {
		return
	}
	service, ok := s.Auth[title]
	if !ok {
		s.Log.Warn("unsupported auth title", "title", title)
		return
	}
	service.Serve(ctx, c, r, first)
}

// readFirst chooses framing from a registered title or an issued lobby ticket.
// Looking only at the two zero high length bytes is ambiguous with legacy
// CreateAccount frames. The bounded peek never waits beyond a legacy record.
func (s *TCP) readFirst(r *bufio.Reader) (frame.Frame, error) {
	hdr, err := r.Peek(2)
	if err != nil {
		return frame.Frame{}, err
	}
	n := int(binary.LittleEndian.Uint16(hdr))
	if n < 2 {
		return frame.Frame{}, fmt.Errorf("invalid first frame length %d", n)
	}
	prefix, err := r.Peek(min(n+2, 48))
	if err != nil {
		return frame.Frame{}, err
	}
	type candidate struct {
		protocol frame.Protocol
		skip     int
	}
	candidates := []candidate{{protocol: frame.Legacy}, {protocol: frame.MMP3}}
	// bdLobbyService 1.80 sends {u32 180, u32 receive capacity} as raw bytes
	// before its framed hello (SLUS-216.86 0x33af60 -> 0x339eb8).
	if len(prefix) >= 8 && binary.LittleEndian.Uint32(prefix) == 180 {
		candidates = append(candidates, candidate{protocol: frame.MMP3, skip: 8})
	}
	var found []candidate
	for _, c := range candidates {
		p := c.protocol
		h := p.HeaderSize()
		record := prefix[c.skip:]
		if len(record) < h+3 {
			continue
		}
		length := uint32(n)
		if p == frame.MMP3 {
			length = binary.LittleEndian.Uint32(record)
		}
		if length < 2 || length > uint32(p.MaxLength()) || record[h] != 0 {
			continue
		}
		kind := record[h+1]
		if c.skip != 0 && kind != gateway.MsgHello {
			continue
		}
		args, e := bitbuf.NewReader(record[h+2 : min(len(record), h+int(length))])
		if e != nil {
			continue
		}
		if _, e = args.U32(); e != nil {
			continue
		}
		title, e := args.U32()
		if e != nil {
			continue
		}
		if kind == gateway.MsgHello {
			ticket, e := args.Raw(20)
			if e == nil && s.Lobby != nil && s.Lobby.Sessions != nil {
				sess, ok := s.Lobby.Sessions.Redeem(ticket)
				if ok && sess.Protocol == p && s.Lobby.Applications[sess.Game] != nil {
					found = append(found, c)
				}
			}
		} else if kind%2 == 0 {
			if a := s.Auth[title]; a != nil && a.Protocol == p {
				found = append(found, c)
			}
		}
	}
	if len(found) != 1 {
		return frame.Frame{}, errors.New("unrecognized or ambiguous first frame")
	}
	if _, err := r.Discard(found[0].skip); err != nil {
		return frame.Frame{}, err
	}
	return found[0].protocol.Read(r)
}
