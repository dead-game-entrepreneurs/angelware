// Package stacked implements the US PS2 SLUS-212.59 online protocol.
package stacked

import (
	"context"
	"errors"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/services"
	"github.com/princebyyourlogic/angelware/internal/storage"
)

const Key = "stacked"
const TitleID uint32 = 302

func Definition() game.Definition {
	return game.Definition{Key: Key, TitleIDs: []uint32{TitleID}, TCPPort: 3074, STUNPort: 3074, AltPort: 3075, Mode: stun.Adaptive, DiscoveryVersion: 1, New: New}
}

func New(o game.Options) (game.Runtime, error) {
	if err := game.DecodeSettings(o.Settings, &struct{}{}); err != nil {
		return game.Runtime{}, err
	}
	mm := services.NewMatchmaking(o.Log)
	mm.AddressSize = 33
	mm.MaxResults = 35
	mm.Validate = ValidateSession
	mm.Search = Search
	mm.EncodeReply = matchmakingReply
	s := &services.Server{Store: o.Store, Matchmaking: mm, Log: o.Log, SignedTaskStatus: true, MessageFields: MessageFields, PollNotifications: true}
	s.Register(services.MatchmakingService, services.MatchmakingHandler(mm))
	s.Register(services.ProfilesService, BootstrapQuery)
	s.Register(services.StatsService, Stats())
	s.Register(services.MailboxService, MailboxPoll)
	s.Register(services.FriendsService, Friends())
	s.Register(33, MTT)
	a := &auth.Service{Game: Key, TitleID: TitleID, Store: o.Store, Sessions: o.Sessions, Mode: o.AuthMode, Log: o.Log}
	return game.Runtime{Auth: a, Lobby: s}, nil
}

// 0x1f3e68 submits the empty query 0x277d80 with a null result pointer.
func BootstrapQuery(ctx context.Context, c *services.Conn, t services.Task) []byte {
	f, err := t.Args.Fields()
	if err != nil || len(f) < 1 || f[0].Type != bitbuf.U8 || f[0].Int != 0 {
		return services.ErrorReply(t, 1)
	}
	switch t.ID {
	case 1:
		if len(f) != 1 {
			return services.ErrorReply(t, 1)
		}
	case 2:
		// 0x1eabc8 checks account existence before sending a proposal. Its
		// 0x277fb0 result pointer is null, so success needs no profile payload.
		if len(f) != 2 || f[1].Type != bitbuf.U64 {
			return services.ErrorReply(t, 1)
		}
		if _, err := c.Store().Account(ctx, f[1].Int); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return services.ErrorReply(t, 101)
			}
			return services.ErrorReply(t, 1)
		}
	default:
		return services.ErrorReply(t, 1)
	}
	return services.Reply(t, nil)
}
