package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/larryjoelane/healbot/internal/app"
	"github.com/larryjoelane/healbot/internal/config"
)

func main() {
	path := flag.String("config", "config.demo.yaml", "path to the YAML config; HEALBOT_CONFIG overrides it")
	flag.Parse()
	cfg, err := config.Load(config.ConfigFile(*path))
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	application, err := app.Build(cfg)
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := application.Run(ctx); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
