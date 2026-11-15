package svr2007

import (
	"fmt"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	svrpeer "github.com/princebyyourlogic/angelware/internal/games/svr2007/peer"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/services"
)

const Key = "svr2007"
const TitleID uint32 = 4669

type Settings struct {
	RelayRewrite bool `json:"relay_rewrite"`
	Observer     bool `json:"observer"`
}

func Definition() game.Definition {
	return game.Definition{Key: Key, TitleIDs: []uint32{TitleID}, TCPPort: 3074, STUNPort: 3074, AltPort: 3075, Mode: stun.Direct, New: New}
}
func New(o game.Options) (game.Runtime, error) {
	var settings Settings
	if err := game.DecodeSettings(o.Settings, &settings); err != nil {
		return game.Runtime{}, err
	}
	if settings.Observer && (!settings.RelayRewrite || o.Connectivity == nil || o.Connectivity.Config.Mode != stun.Adaptive) {
		return game.Runtime{}, fmt.Errorf("WWE observer requires adaptive mode and relay_rewrite")
	}
	mm := services.NewMatchmaking(o.Log)
	mm.Search = Search
	mm.InspectConnectivity = InspectConnectivity
	mm.Completed = func(before, after int) bool { return before < 2 && after == 2 }
	s := &services.Server{Store: o.Store, Matchmaking: mm, Log: o.Log}
	s.Register(services.MatchmakingService, services.MatchmakingHandler(mm))
	s.Register(services.ProfilesService, Profiles)
	s.Register(services.StatsService, services.Stats(StatsOptions()))
	s.Register(services.FriendsService, services.Friends())
	s.Register(services.MailboxService, services.Mailbox())
	s.Register(services.StorageService, services.Files())
	s.Register(services.TitleService, services.TitleUtilities())
	a := &auth.Service{Game: Key, TitleID: TitleID, Store: o.Store, Sessions: o.Sessions, Mode: o.AuthMode, Log: o.Log}
	if settings.RelayRewrite && o.Connectivity != nil && o.Connectivity.Config.Mode != stun.Direct {
		peers := svrpeer.New(o.Log, mm.RoomKey)
		a.ObserveLogin = peers.ObserveLogin
		o.Connectivity.Config.RewritePeer = peers.Rewrite
	}
	if settings.Observer {
		mm.ObserveConnectivity = func(e services.ConnectivityEvent) {
			if e.Created {
				o.Connectivity.BeginObservation(e.Room, e.Host, e.Started)
			} else {
				o.Connectivity.ObserveConnectivity(e.Room, e.Host, e.GuestIP, e.Started, e.At, e.UniqueGuest)
			}
		}
	}
	return game.Runtime{Auth: a, Lobby: s}, nil
}
