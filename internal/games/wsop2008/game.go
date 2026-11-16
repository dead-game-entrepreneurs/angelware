// Package wsop2008 implements the US PS2 SLUS-216.86 online protocol.
package wsop2008

import (
	"bytes"
	"context"

	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/auth"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/bitbuf"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/crypto"
	"github.com/princebyyourlogic/angelware/internal/protocol/dw/frame"
	"github.com/princebyyourlogic/angelware/internal/services"
)

const Key = "wsop2008"
const TitleID uint32 = 4963

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
	s := &services.Server{Store: o.Store, Matchmaking: mm, Log: o.Log, MessageFields: MessageFields}
	s.Register(services.MatchmakingService, services.MatchmakingHandler(mm))
	s.Register(services.ProfilesService, BootstrapQuery)
	s.Register(services.StatsService, services.Stats(StatsOptions()))
	s.Register(services.FriendsService, services.Friends())
	s.Register(services.MailboxService, services.Mailbox())
	a := &auth.Service{Game: Key, TitleID: TitleID, Protocol: frame.MMP3, Store: o.Store, Sessions: o.Sessions, Mode: o.AuthMode, Log: o.Log, NameID: AccountID}
	return game.Runtime{Auth: a, Lobby: s}, nil
}

// AccountID follows 0x33ecf0: lowercase ASCII before Tiger-192. Preserve the
// original display name; the identity is shared with other games using this ID.
func AccountID(name []byte) uint64 {
	canonical := bytes.Clone(name)
	for i, c := range canonical {
		if c >= 'A' && c <= 'Z' {
			canonical[i] = c + ('a' - 'A')
		}
	}
	return dwcrypto.UserID(canonical)
}

// 0x315240 submits the default profile query (0x349dc8) with no object fields
// and a null result pointer. This bootstrap does not create a persistent profile.
func BootstrapQuery(_ context.Context, _ *services.Conn, t services.Task) []byte {
	f, err := t.Args.Fields()
	if err != nil || t.ID != 1 || len(f) != 1 || f[0].Type != bitbuf.U8 || f[0].Int != 0 {
		return services.ErrorReply(t, 1)
	}
	return services.Reply(t, nil)
}
