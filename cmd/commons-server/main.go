package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"codex-commons/internal/demodata"
	"codex-commons/internal/server"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	config, err := server.ParseConfig(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		logger.Error("invalid commons server configuration", "error", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := server.New(ctx, config, demodata.Seed)
	if err != nil {
		logger.Error("initialize commons server", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := app.Close(); err != nil {
			logger.Error("close commons database", "error", err)
		}
	}()
	if err := app.Serve(ctx, logger); err != nil {
		logger.Error("serve commons", "error", err)
		os.Exit(1)
	}
}
