// Package game defines composition contracts. Shared protocol and services never
// import a concrete game. The executable explicitly registers the games it ships.
package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

type Definition struct {
	Key                        string
	TitleIDs                   []uint32
	TCPPort, STUNPort, AltPort int
	Mode                       stun.Mode
	// DiscoveryOnly titles use their own native peer transport. They can share
	// discovery sockets using a separate wire-version router or, when the
	// format overlaps an SDK member, its common real-address discovery policy.
	DiscoveryVersion uint16
	DiscoveryOnly    bool
	New              func(Options) (Runtime, error)
}
type Options struct {
	Bind         netip.Addr
	PublicIP     netip.Addr
	Store        store.Store
	Sessions     *session.Registry
	AuthMode     auth.Mode
	Log          *slog.Logger
	Connectivity *stun.Server
	Settings     json.RawMessage
}

// PacketService is an optional game-specific UDP transport.
type PacketService interface {
	UDPAddress() netip.AddrPort
	Run(context.Context) error
}
type Runtime struct {
	Services []PacketService
	Auth     *auth.Service
	Lobby    gateway.Application
}

func DecodeSettings(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one game options object")
	}
	return nil
}
