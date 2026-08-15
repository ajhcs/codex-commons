package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/runtimehealth"
)

func TestPhase2ServeRequiredExhaustionReturnsFatalAndStops(t *testing.T) {
	config := DefaultConfig()
	config.Listen = "127.0.0.1:0"
	config.DatabasePath = filepath.Join(t.TempDir(), "serve-exhaustion.sqlite")
	config.WebDir = phase2AcceptanceWeb(t)
	config.CodexAuth = true
	config.RequireCodexReady = true
	config.CodexBin = "/usr/bin/codex"
	config.CodexClient = codexauth.NewUnavailable()
	config.CodexBindingKeySet = true
	config.CodexBindingKey[0] = 1

	app, err := New(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Stop the normal monitor after startup, then install the already-published
	// required exhaustion snapshot that a supervisor may expose at Serve time.
	app.runtime.Close()
	now := time.Now().UTC()
	snapshot := runtimehealth.Evaluate(runtimehealth.Input{
		ObservedAt:     now,
		Database:       runtimehealth.DatabaseObservation{Status: runtimehealth.DatabaseHealthy},
		Persistence:    runtimehealth.HealthObservation{Status: runtimehealth.HealthHealthy},
		Reconciliation: runtimehealth.HealthObservation{Status: runtimehealth.HealthHealthy},
		Codex:          runtimehealth.CodexObservation{Configured: true, Required: true},
		Supervisor: runtimehealth.SupervisorObservation{
			Status: runtimehealth.SupervisorExhausted, Generation: 7, LastFailureAt: now,
		},
	})
	app.runtime.publication.Store(&runtimePublication{
		snapshot: snapshot,
		meta: runtimeHealthMeta{
			readyLatched: true, recoveryActive: true, recoveryExhausted: true, recoverySince: now,
		},
		supervisor: runtimeSupervisorResult{
			Configured: true, State: codexauth.ProcessStateExhausted, Generation: 7,
			RecoveryActive: true, RecoveryExhausted: true, RecoverySince: now,
		},
	})

	notifyPath := filepath.Join(t.TempDir(), "notify.sock")
	notifySocket, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer notifySocket.Close()
	t.Setenv("NOTIFY_SOCKET", notifyPath)

	serveErr := app.Serve(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(serveErr, ErrNotifierExhausted) {
		t.Fatalf("Serve error=%v, want %v", serveErr, ErrNotifierExhausted)
	}

	if err := notifySocket.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var states []string
	buffer := make([]byte, 256)
	for {
		n, _, readErr := notifySocket.ReadFromUnix(buffer)
		if readErr != nil {
			t.Fatalf("read notifier states=%v: %v", states, readErr)
		}
		states = append(states, string(buffer[:n]))
		if strings.Contains(states[len(states)-1], "STOPPING=1") {
			break
		}
	}
	if !containsPhase2ServeState(states, "STATUS=Exhausted") {
		t.Fatalf("notifier states=%v missing exhaustion", states)
	}
	if !containsPhase2ServeState(states, "STOPPING=1\nSTATUS=Stopping") {
		t.Fatalf("notifier states=%v missing deterministic STOPPING cleanup", states)
	}
	for _, state := range states {
		if strings.Contains(state, "READY=1") {
			t.Fatalf("notifier states=%v unexpectedly reported READY", states)
		}
		if strings.Contains(state, "WATCHDOG=1") {
			t.Fatalf("notifier states=%v unexpectedly reported WATCHDOG", states)
		}
	}
}

func containsPhase2ServeState(states []string, want string) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}
