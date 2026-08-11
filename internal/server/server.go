// Package server assembles the persistent Commons prototype without owning
// product logic or host deployment configuration.
package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"codex-commons/internal/appbackend"
	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
	"codex-commons/internal/storebackend"
)

var zeroTime time.Time

type SeedFunc func(context.Context, *commonsstore.Store, *presence.Registry, time.Time) error

type App struct {
	config   Config
	handler  http.Handler
	store    *commonsstore.Store
	presence *presence.Registry
}

func New(ctx context.Context, config Config, seed SeedFunc) (*App, error) {
	if config.HumanAuth != nil {
		if config.HumanAuth.Principal == "" {
			config.HumanAuth.Principal = domain.HumanLocalPrincipal
		}
		if config.HumanAuth.Handle == "" {
			config.HumanAuth.Handle = "local-admin"
		}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.DemoSeed && seed == nil {
		return nil, errors.New("demo seed requested but no seed implementation is configured")
	}
	store, err := commonsstore.Open(ctx, config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	failed := true
	defer func() {
		if failed {
			_ = store.Close()
		}
	}()
	live := presence.New(nil)
	if config.HumanAuth != nil {
		if err := store.UpsertSession(ctx, domain.Session{
			ID: config.HumanAuth.Session, Host: config.HumanAuth.Host, Purpose: config.HumanAuth.DisplayName,
		}); err != nil {
			return nil, fmt.Errorf("register human admin session: %w", err)
		}
	}
	if config.DemoSeed {
		if err := seed(ctx, store, live, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("seed demo data: %w", err)
		}
	}
	legacy, err := storebackend.New(store, live, config.Version)
	if err != nil {
		return nil, err
	}
	service := application.New(store, live, nil)
	if config.HumanAuth != nil {
		service.ConfigureHumanIdentity(config.HumanAuth.DisplayName, config.HumanAuth.Handle)
	}
	backend, err := appbackend.New(legacy, service)
	if err != nil {
		return nil, err
	}
	credentials := append([]httpapi.Credential(nil), config.Credentials...)
	anonymousToken := ""
	if config.AnonymousRead {
		anonymousToken, err = randomToken()
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, httpapi.Credential{
			BearerToken: anonymousToken, Actor: "human-browser", Session: "browser-lan", Host: "plumbob",
		})
	}
	api := httpapi.NewHandler(backend, httpapi.Config{Credentials: credentials, HumanAuth: config.HumanAuth, Version: config.Version})
	web := os.DirFS(config.WebDir)
	if _, err := fs.Stat(web, "index.html"); err != nil {
		return nil, fmt.Errorf("web index: %w", err)
	}
	handler, err := newMux(api, web, anonymousToken)
	if err != nil {
		return nil, fmt.Errorf("build web handler: %w", err)
	}
	failed = false
	return &App{config: config, handler: handler, store: store, presence: live}, nil
}

func (a *App) Handler() http.Handler { return a.handler }

func (a *App) Close() error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.Close()
}

func (a *App) Serve(ctx context.Context, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	listener, err := net.Listen("tcp", a.config.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", a.config.Listen, err)
	}
	server := &http.Server{
		Addr: a.config.Listen, Handler: a.handler,
		ReadTimeout: a.config.ReadTimeout, ReadHeaderTimeout: a.config.ReadHeaderTimeout,
		WriteTimeout: a.config.WriteTimeout, IdleTimeout: a.config.IdleTimeout,
		MaxHeaderBytes: 16 << 10,
	}
	serveDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), a.config.ShutdownTimeout)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				logger.Error("commons server shutdown", "error", err)
			}
		case <-serveDone:
		}
	}()
	logger.Info("commons server listening", "address", listener.Addr().String(), "anonymous_read", a.config.AnonymousRead)
	err = server.Serve(listener)
	close(serveDone)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate anonymous read credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
