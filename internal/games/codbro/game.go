// Package codbro composes DemonWare connectivity only; its lobby is external.
package codbro

import (
	"github.com/princebyyourlogic/angelware/internal/connectivity/stun"
	"github.com/princebyyourlogic/angelware/internal/game"
)

func Definition() game.Definition {
	return game.Definition{Key: "codbro", STUNPort: 2273, AltPort: 2274, Mode: stun.Relay, New: New}
}
func New(o game.Options) (game.Runtime, error) {
	if err := game.DecodeSettings(o.Settings, &struct{}{}); err != nil {
		return game.Runtime{}, err
	}
	return game.Runtime{}, nil
}
