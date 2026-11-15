// Package server accepts TCP connections on the shared auth/gateway port.
//
// Both DemonWare hostnames (auth.mmp and lsg.mmp) point at one address and port, so
// each connection is classified by its first frame: the gateway opens with plain
// message 7, auth requests always use even message types.
package server

import (
	"bufio"
	"context"
	"errors"
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
	first, err := frame.Read(r)
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
