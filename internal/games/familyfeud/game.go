// Package familyfeud implements the US PS2 SLUS-214.46 lobby protocol.
package familyfeud

import (
	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/services"
)

const Key = "familyfeud"
const TitleID uint32 = 521

func Definition() game.Definition {
	return game.Definition{Key: Key, TitleIDs: []uint32{TitleID}, TCPPort: 3074, STUNPort: 3074, AltPort: 3075, Mode: stun.Adaptive, DiscoveryVersion: 2, New: New}
}

func New(o game.Options) (game.Runtime, error) {
	if err := game.DecodeSettings(o.Settings, &struct{}{}); err != nil {
		return game.Runtime{}, err
	}
	mm := services.NewMatchmaking(o.Log)
	mm.Validate = ValidateSession
	mm.Search = Search
	s := &services.Server{Store: o.Store, Matchmaking: mm, Log: o.Log}
	s.Register(services.MatchmakingService, services.MatchmakingHandler(mm))
	a := &auth.Service{Game: Key, TitleID: TitleID, Store: o.Store, Sessions: o.Sessions, Mode: o.AuthMode, Log: o.Log, AllowGuest: true}
	return game.Runtime{Auth: a, Lobby: s}, nil
}
