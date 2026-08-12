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
	"strings"
	"time"
	"unicode/utf8"

	"codex-commons/internal/appbackend"
	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
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
	codex    codexauth.Client
}

func New(ctx context.Context, config Config, seed SeedFunc) (*App, error) {
	if config.CodexAuth && config.HumanAuth == nil {
		config.HumanAuth = &httpapi.HumanAuthConfig{DisplayName: "Local admin", Handle: "local-admin", Principal: domain.HumanLocalPrincipal, Actor: "local-admin", Session: domain.HumanLegacySession, Host: "browser", SessionTTL: 12 * time.Hour, CodexEnabled: true}
	}
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
	if err := store.ReconcileArchaeology(ctx); err != nil {
		return nil, fmt.Errorf("reconcile project archaeology: %w", err)
	}
	failed := true
	var codexClient codexauth.Client = config.CodexClient
	if config.CodexAuth && codexClient == nil {
		codexClient, err = codexauth.NewManagedProcess(ctx, codexauth.ProcessConfig{Executable: config.CodexBin, Env: codexauth.ApprovedEnvironment(os.Environ())})
		if err != nil {
			// Codex is an optional capability. Keep Commons available and let
			// /v1/auth/codex/status report the unavailable state.
			codexClient = codexauth.NewUnavailable()
		}
	}
	defer func() {
		if failed {
			if codexClient != nil {
				_ = codexClient.Close()
			}
			_ = store.Close()
		}
	}()
	live := presence.New(nil)
	for _, credential := range config.Credentials {
		if credential.Project != "" {
			exists, err := store.ProjectExists(ctx, credential.Project)
			if err != nil {
				return nil, fmt.Errorf("verify configured agent project %q: %w", credential.Project, err)
			}
			if !exists {
				return nil, fmt.Errorf("configured agent session %q references missing project %q", credential.Session, credential.Project)
			}
		}
		purpose := credential.Purpose
		if purpose == "" {
			purpose = configuredCredentialPurpose(credential.Actor)
		}
		if err := store.UpsertSession(ctx, domain.Session{
			ID: credential.Session, Host: credential.Host, ProjectID: credential.Project, Purpose: purpose,
		}); err != nil {
			return nil, fmt.Errorf("register configured agent session %q: %w", credential.Session, err)
		}
	}
	humanDisplayName, humanHandle := "Local admin", "local-admin"
	if config.HumanAuth != nil {
		humanDisplayName, humanHandle = config.HumanAuth.DisplayName, config.HumanAuth.Handle
		if binding, bindingErr := store.GetHumanAccountBinding(ctx); bindingErr == nil {
			humanDisplayName, humanHandle = binding.DisplayName, binding.Handle
		} else if !errors.Is(bindingErr, domain.ErrNotFound) {
			return nil, fmt.Errorf("load human account binding: %w", bindingErr)
		}
		if err := store.UpsertSession(ctx, domain.Session{
			ID: config.HumanAuth.Session, Host: config.HumanAuth.Host, Purpose: humanDisplayName,
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
	if archaeologyClient, ok := codexClient.(codexauth.ArchaeologyClient); ok && codexClient.Available() {
		bridge := &codexArchaeologyBridge{client: archaeologyClient, roots: config.ArchaeologyRoots, baseURL: "http://" + config.Listen, catalogKey: config.CodexBindingKey}
		service.ConfigureProjectArchaeology(bridge, bridge)
	} else if len(config.ArchaeologyRoots) > 0 {
		service.ConfigureProjectArchaeology(allowlistedArchaeologyDiscoverer{roots: config.ArchaeologyRoots}, nil)
	}
	if config.HumanAuth != nil {
		config.HumanAuth.DisplayName = humanDisplayName
		config.HumanAuth.Handle = humanHandle
		service.ConfigureHumanIdentity(humanDisplayName, humanHandle)
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
	apiConfig := httpapi.Config{Credentials: credentials, ExpectedHost: config.Listen, HumanAuth: config.HumanAuth, HumanBindingStore: store, HumanAuthEvents: store, OnHumanIdentityUpdated: func(displayName, handle string, _ int64) {
		service.ConfigureHumanIdentity(displayName, handle)
	}, Version: config.Version}
	if config.CodexAuth && codexClient != nil {
		apiConfig.CodexAuth = &httpapi.CodexAuthConfig{Client: codexClient, BindingKey: config.CodexBindingKey, AllowFirstBindLAN: config.AllowFirstCodexBindLAN}
	}
	api := httpapi.NewHandler(backend, apiConfig)
	web := os.DirFS(config.WebDir)
	if _, err := fs.Stat(web, "index.html"); err != nil {
		return nil, fmt.Errorf("web index: %w", err)
	}
	handler, err := newMux(api, web, anonymousToken)
	if err != nil {
		return nil, fmt.Errorf("build web handler: %w", err)
	}
	failed = false
	return &App{config: config, handler: handler, store: store, presence: live, codex: codexClient}, nil
}

func (a *App) Handler() http.Handler { return a.handler }

func (a *App) Close() error {
	if a == nil || a.store == nil {
		return nil
	}
	if a.codex != nil {
		_ = a.codex.Close()
	}
	return a.store.Close()
}

func configuredCredentialPurpose(actor string) string {
	purpose := "Configured agent credential for " + strings.TrimSpace(actor)
	const maximumBytes = 200
	if len(purpose) <= maximumBytes {
		return purpose
	}
	purpose = purpose[:maximumBytes]
	for !utf8.ValidString(purpose) {
		purpose = purpose[:len(purpose)-1]
	}
	return strings.TrimSpace(purpose)
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
