// Package app composes enabled games and owns process resources and listeners.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/server"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/session"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

type App struct {
	TCP      map[string]*server.TCP
	UDP      []*stun.Server
	Services []game.PacketService
}

func Build(c Config, definitions []game.Definition, db store.Database, log *slog.Logger) (*App, error) {
	registry := map[string]game.Definition{}
	for _, d := range definitions {
		if d.Key == "" || d.New == nil {
			return nil, fmt.Errorf("invalid game definition")
		}
		if _, ok := registry[d.Key]; ok {
			return nil, fmt.Errorf("duplicate game definition %s", d.Key)
		}
		registry[d.Key] = d
	}
	if c.AuthMode == "" {
		c.AuthMode = auth.Verify
	}
	if c.AuthMode != auth.Verify && c.AuthMode != auth.Open {
		return nil, fmt.Errorf("invalid auth_mode %q", c.AuthMode)
	}
	if len(c.Games) == 0 {
		return nil, fmt.Errorf("no games enabled")
	}
	app := &App{TCP: map[string]*server.TCP{}}
	sessions := session.NewRegistry()
	enabled := map[string]bool{}
	type socket struct {
		bind netip.Addr
		port int
		game string
	}
	var udp []socket
	owners := map[string]*stun.Server{}
	type sharedDiscovery struct {
		game, owner  string
		bind, public netip.Addr
		version      uint16
		log          *slog.Logger
	}
	var shared []sharedDiscovery
	type configuredGame struct {
		spec         GameConfig
		definition   game.Definition
		bind, public netip.Addr
		connectivity *stun.Server
	}
	var configured []configuredGame
	for _, spec := range c.Games {
		d, ok := registry[spec.Game]
		if !ok {
			return nil, fmt.Errorf("unknown game %q", spec.Game)
		}
		if enabled[d.Key] {
			return nil, fmt.Errorf("duplicate enabled game %q", d.Key)
		}
		enabled[d.Key] = true
		if spec.Bind == "" {
			spec.Bind = "0.0.0.0"
		}
		bind, err := netip.ParseAddr(spec.Bind)
		if err != nil || !bind.Is4() {
			return nil, fmt.Errorf("%s: bind must be IPv4", d.Key)
		}
		public, err := netip.ParseAddr(spec.PublicIP)
		if err != nil || !public.Is4() || public.IsUnspecified() {
			return nil, fmt.Errorf("%s: public_ip must be reachable IPv4", d.Key)
		}
		glog := log.With("game", d.Key)
		var connectivity *stun.Server
		if spec.STUN.ShareTransportWith != "" {
			if d.DiscoveryOnly || d.DiscoveryVersion < 1 || d.DiscoveryVersion > 2 || d.STUNPort == 0 {
				return nil, fmt.Errorf("%s cannot share the SDK transport", d.Key)
			}
			sc := spec.STUN
			sc.ShareTransportWith = ""
			if sc != (Connectivity{}) {
				return nil, fmt.Errorf("%s: share_transport_with cannot override listener settings or share_with", d.Key)
			}
		} else if spec.STUN.ShareWith != "" {
			if !d.DiscoveryOnly || d.DiscoveryVersion == 0 || d.STUNPort == 0 {
				return nil, fmt.Errorf("%s cannot share discovery: native peer transport and an SDK wire version are required", d.Key)
			}
			sc := spec.STUN
			sc.ShareWith = ""
			if sc != (Connectivity{}) {
				return nil, fmt.Errorf("%s: share_with cannot override discovery listener settings", d.Key)
			}
			shared = append(shared, sharedDiscovery{d.Key, spec.STUN.ShareWith, bind, public, d.DiscoveryVersion, glog.With("svc", "stun")})
		} else if d.STUNPort != 0 {
			sc := spec.STUN
			if sc.Port == 0 {
				sc.Port = d.STUNPort
			}
			if sc.AltPort == 0 {
				sc.AltPort = d.AltPort
			}
			if sc.Mode == "" {
				sc.Mode = d.Mode
			}
			if sc.RelayPorts == "" {
				sc.RelayPorts = "20000-20199"
			}
			if sc.MaxClients == 0 {
				sc.MaxClients = 64
			}
			if sc.DirectWindow == 0 {
				sc.DirectWindow = 1
			}
			lo, hi, err := portRange(sc.RelayPorts)
			if err != nil {
				return nil, err
			}
			cfg := stun.Config{PublicIP: public, Port: sc.Port, AltPort: sc.AltPort, Mode: sc.Mode, RelayLo: lo, RelayHi: hi, RelayMaxClients: sc.MaxClients, DirectWindow: time.Duration(sc.DirectWindow * float64(time.Second))}
			if err = cfg.Validate(); err != nil {
				return nil, fmt.Errorf("%s: %w", d.Key, err)
			}
			for _, port := range []int{sc.Port, sc.AltPort} {
				for _, used := range udp {
					if port == used.port && (used.bind == bind || used.bind.IsUnspecified() || bind.IsUnspecified()) {
						return nil, fmt.Errorf("UDP %s:%d conflicts between %s and %s; use separate bind IPs for games with overlapping ports", spec.Bind, port, used.game, d.Key)
					}
				}
				udp = append(udp, socket{bind, port, d.Key})
			}
			connectivity = &stun.Server{Config: cfg, Bind: spec.Bind, Log: glog.With("svc", "stun"), DiscoveryVersion: d.DiscoveryVersion, Games: []string{d.Key}}
			owners[d.Key] = connectivity
		} else if spec.STUN.Port != 0 || spec.STUN.AltPort != 0 || spec.STUN.Mode != "" {
			return nil, fmt.Errorf("%s has no connectivity service", d.Key)
		}
		configured = append(configured, configuredGame{spec: spec, definition: d, bind: bind, public: public, connectivity: connectivity})
		if connectivity != nil {
			app.UDP = append(app.UDP, connectivity)
		}
	}
	// Resolve shared sockets before constructing runtimes. A common SDK server
	// owns generic peer state; game factories still own rooms, auth and hooks.
	for i := range configured {
		g := &configured[i]
		key := g.spec.STUN.ShareTransportWith
		if key == "" {
			continue
		}
		owner := owners[key]
		d := registry[key]
		if owner == nil || d.DiscoveryOnly || d.STUNPort != g.definition.STUNPort || d.AltPort != g.definition.AltPort {
			return nil, fmt.Errorf("%s: transport owner %q must have its own SDK listener with the same discovery ports", g.definition.Key, key)
		}
		if owner.Bind != g.bind.String() || owner.PublicIP != g.public {
			return nil, fmt.Errorf("%s: shared transport must use the owner's bind and public_ip", g.definition.Key)
		}
		if err := owner.AddTransport(g.definition.DiscoveryVersion); err != nil {
			return nil, fmt.Errorf("%s: %w", g.definition.Key, err)
		}
		g.connectivity = owner
		owner.Games = append(owner.Games, g.definition.Key)
		owner.Log = log.With("svc", "stun", "transport", key)
	}
	for _, share := range shared {
		owner := owners[share.owner]
		if owner == nil {
			return nil, fmt.Errorf("%s: discovery owner %q must have its own enabled listener", share.game, share.owner)
		}
		if owner.Bind != share.bind.String() || owner.PublicIP != share.public {
			return nil, fmt.Errorf("%s: shared discovery must use the owner's bind and public_ip", share.game)
		}
		if err := owner.AddDiscovery(share.version, share.log); err != nil {
			return nil, fmt.Errorf("%s: %w", share.game, err)
		}
	}
	for _, g := range configured {
		spec, d, bind, public, connectivity := g.spec, g.definition, g.bind, g.public, g.connectivity
		glog := log.With("game", d.Key)

		runtime, err := d.New(game.Options{Bind: bind, PublicIP: public, Store: db.ForGame(d.Key), Sessions: sessions, AuthMode: c.AuthMode, Log: glog, Connectivity: connectivity, Settings: spec.Options})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Key, err)
		}

		for _, svc := range runtime.Services {
			address := svc.UDPAddress()
			if !address.IsValid() || !address.Addr().Is4() || address.Port() == 0 {
				return nil, fmt.Errorf("%s: invalid UDP service address", d.Key)
			}
			for _, used := range udp {
				if int(address.Port()) == used.port && (address.Addr() == used.bind || address.Addr().IsUnspecified() || used.bind.IsUnspecified()) {
					return nil, fmt.Errorf("UDP %s conflicts between %s and %s", address, used.game, d.Key)
				}
			}
			udp = append(udp, socket{address.Addr(), int(address.Port()), d.Key})
			app.Services = append(app.Services, svc)
		}
		if len(d.TitleIDs) == 0 {
			if spec.TCPPort != 0 {
				return nil, fmt.Errorf("%s has no TCP service", d.Key)
			}
			continue
		}
		if runtime.Auth == nil || runtime.Lobby == nil {
			return nil, fmt.Errorf("%s: missing auth or lobby", d.Key)
		}
		port := spec.TCPPort
		if port == 0 {
			port = d.TCPPort
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("%s: invalid TCP port", d.Key)
		}
		address := net.JoinHostPort(spec.Bind, strconv.Itoa(port))
		for existing := range app.TCP {
			host, p, _ := net.SplitHostPort(existing)
			if p == strconv.Itoa(port) && existing != address && (host == "0.0.0.0" || bind.IsUnspecified()) {
				return nil, fmt.Errorf("TCP wildcard bind overlaps %s", existing)
			}
		}
		tcp := app.TCP[address]
		if tcp == nil {
			tcp = &server.TCP{Auth: map[uint32]*auth.Service{}, Lobby: &gateway.Server{Sessions: sessions, Applications: map[string]gateway.Application{}, Log: log.With("svc", "gateway")}, Log: log}
			app.TCP[address] = tcp
		}
		tcp.Lobby.Applications[d.Key] = runtime.Lobby
		for _, title := range d.TitleIDs {
			if _, ok := tcp.Auth[title]; ok {
				return nil, fmt.Errorf("duplicate title %d on %s", title, address)
			}
			a := *runtime.Auth
			a.TitleID = title
			tcp.Auth[title] = &a
		}
	}
	return app, nil
}

func Run(ctx context.Context, c Config, definitions []game.Definition, log *slog.Logger) error {
	var db store.Database = store.NewMemoryDB()
	if c.DatabaseURL != "" {
		pg, err := store.OpenPostgres(ctx, c.DatabaseURL)
		if err != nil {
			return err
		}
		db = pg
	} else {
		log.Warn("no database URL: data lives in memory")
	}
	defer db.Close()
	app, err := Build(c, definitions, db, log)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listeners := map[string]net.Listener{}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for address := range a.TCP {
		l, err := net.Listen("tcp4", address)
		if err != nil {
			return err
		}
		listeners[address] = l
	}
	errs := make(chan error, len(a.TCP)+len(a.UDP)+len(a.Services))
	var wg sync.WaitGroup
	launch := func(run func() error) { wg.Add(1); go func() { defer wg.Done(); errs <- run() }() }
	for address, s := range a.TCP {
		l := listeners[address]
		launch(func() error { return s.Serve(ctx, l) })
	}
	for _, s := range a.UDP {
		launch(func() error { return s.Run(ctx) })
	}
	for _, svc := range a.Services {
		launch(func() error { return svc.Run(ctx) })
	}
	var err error
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-errs:
	}
	cancel()
	wg.Wait()
	return err
}
