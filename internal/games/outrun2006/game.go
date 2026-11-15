// Package outrun2006 implements the US PS2 build SLUS-212.74.
package outrun2006

import (
	"fmt"
	"net/netip"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006/peer"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/gateway"
	"github.com/princebyyourlogic/angelware/internal/services"
)

const Key = "outrun2006"
const TitleID uint32 = 182

// Board direction is configurable until the retail server's board catalogue is
// recovered. NativeRelay opts into the matching experimental PCSX2 PNACH transport.
type Settings struct {
	NativeRelay     bool    `json:"native_relay"`
	AscendingBoards []int32 `json:"ascending_boards"`
}

func Definition() game.Definition {
	return game.Definition{Key: Key, TitleIDs: []uint32{TitleID}, TCPPort: 3074, STUNPort: 3074, AltPort: 3075, Mode: stun.Direct, New: New}
}
func New(o game.Options) (game.Runtime, error) {
	var settings Settings
	if err := game.DecodeSettings(o.Settings, &settings); err != nil {
		return game.Runtime{}, err
	}
	if o.Connectivity != nil && o.Connectivity.Mode != stun.Direct {
		return game.Runtime{}, fmt.Errorf("OutRun currently supports direct connectivity only")
	}
	mm := services.NewMatchmaking(o.Log)
	mm.Search = Search
	mm.Validate = ValidateSession
	mm.MaxResults = 50
	s := &services.Server{Store: o.Store, Matchmaking: mm, Log: o.Log}
	s.Register(services.MatchmakingService, services.MatchmakingHandler(mm))
	s.Register(8, BootstrapQuery)
	s.Register(services.StatsService, services.Stats(StatsOptions(settings.AscendingBoards)))
	s.Register(services.FriendsService, services.Friends())
	s.Register(services.MailboxService, services.Mailbox())
	s.Register(services.StorageService, services.Files())
	var lobby gateway.Application = s
	var extra []game.PacketService
	if settings.NativeRelay {
		if !o.Bind.Is4() {
			return game.Runtime{}, fmt.Errorf("native relay requires IPv4 bind")
		}
		relay := peer.New(netip.AddrPortFrom(o.Bind, 3658), o.Log.With("svc", "native-relay"))
		mm.Disclosed = func(room [8]byte, host, viewer any) {
			relay.Offer(room, peer.Token(host.(*services.Conn).Session().Token), peer.Token(viewer.(*services.Conn).Session().Token))
		}
		mm.Removed = func(room [8]byte) { relay.DropRoom(room) }
		lobby = relayLobby{Application: s, relay: relay}
		extra = append(extra, relay)
	}
	return game.Runtime{Services: extra, Auth: &auth.Service{Game: Key, TitleID: TitleID, Store: o.Store, Sessions: o.Sessions, Mode: o.AuthMode, Log: o.Log}, Lobby: lobby}, nil
}
