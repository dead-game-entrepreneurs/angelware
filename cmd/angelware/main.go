// Command angelware starts the configured game runtimes.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/princebyyourlogic/angelware/internal/app"
	"github.com/princebyyourlogic/angelware/internal/game"
	"github.com/princebyyourlogic/angelware/internal/games/codbro"
	"github.com/princebyyourlogic/angelware/internal/games/familyfeud"
	"github.com/princebyyourlogic/angelware/internal/games/outrun2006"
	"github.com/princebyyourlogic/angelware/internal/games/svr2007"
	"github.com/princebyyourlogic/angelware/internal/games/wsop2008"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(2)
	}
	var level slog.Level
	if cfg.LogLevel != "" {
		if err = level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
			slog.Error("log level", "err", err)
			os.Exit(2)
		}
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	definitions := []game.Definition{svr2007.Definition(), codbro.Definition(), outrun2006.Definition(), familyfeud.Definition(), wsop2008.Definition()}
	if err = app.Run(ctx, cfg, definitions, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}
