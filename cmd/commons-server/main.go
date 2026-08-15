package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"codex-commons/internal/demodata"
	"codex-commons/internal/server"
)

// releaseID is set only by ops/build-release.sh. A production release refuses
// to stage when this embedded identity differs from its immutable directory.
var releaseID = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--build-id" {
		fmt.Println(releaseID)
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	config, err := server.ParseConfig(os.Args[1:], os.Getenv, os.Stderr)
	if err != nil {
		logger.Error("invalid commons server configuration", "error", err)
		os.Exit(2)
	}
	if config.ReleaseIdentityFile != "" && config.Version != releaseID {
		logger.Error("release identity does not match embedded build identity")
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
