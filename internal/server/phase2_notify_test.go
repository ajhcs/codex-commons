package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type phase2NotifierTicker struct {
	ch      chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newPhase2NotifierTicker() *phase2NotifierTicker {
	return &phase2NotifierTicker{ch: make(chan time.Time, 8), stopped: make(chan struct{})}
}

func (t *phase2NotifierTicker) Chan() <-chan time.Time { return t.ch }
func (t *phase2NotifierTicker) Stop() {
	t.once.Do(func() { close(t.stopped) })
}

type phase2NotifierRecorder struct {
	mu     sync.Mutex
	states []string
}

type phase2NotifierSnapshot struct {
	mu    sync.RWMutex
	value NotifierHealthSnapshot
}

func (s *phase2NotifierSnapshot) load() NotifierHealthSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *phase2NotifierSnapshot) store(value NotifierHealthSnapshot) {
	s.mu.Lock()
	s.value = value
	s.mu.Unlock()
}

func (r *phase2NotifierRecorder) send(state string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, state)
	return nil
}

func (r *phase2NotifierRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.states...)
}

func phase2NotifierTestNotifier(snapshot *phase2NotifierSnapshot, now *time.Time, ticker *phase2NotifierTicker, recorder *phase2NotifierRecorder, grace time.Duration) *serviceNotifier {
	return newServiceNotifierForTest(slog.Default(), serviceNotifierOptions{
		Sender:   recorder.send,
		Snapshot: snapshot.load,
		NewTicker: func(time.Duration) notifierTicker {
			return ticker
		},
		Now: func() time.Time {
			return *now
		},
		WatchdogInterval: time.Second,
		RecoveryGrace:    grace,
	})
}

func phase2NotifierRun(t *testing.T, notifier *serviceNotifier, ctx context.Context) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- notifier.Run(ctx) }()
	return done
}

func phase2NotifierWait(t *testing.T, done <-chan error, want error) {
	t.Helper()
	select {
	case got := <-done:
		if !errors.Is(got, want) {
			t.Fatalf("Run() error = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("notifier loop did not return")
	}
}

func phase2NotifierHas(states []string, want string) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}

func phase2NotifierCount(states []string, want string) int {
	count := 0
	for _, state := range states {
		if state == want {
			count++
		}
	}
	return count
}

func phase2NotifierWaitState(t *testing.T, recorder *phase2NotifierRecorder, want string) {
	t.Helper()
	deadline := time.After(time.Second)
	for !phase2NotifierHas(recorder.snapshot(), want) {
		select {
		case <-deadline:
			t.Fatalf("notifier state %q was not emitted: %v", want, recorder.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func phase2NotifierClose(t *testing.T, notifier *serviceNotifier) {
	t.Helper()
	notifier.close()
	select {
	case <-notifier.Fatal():
	default:
	}
}

func TestPhase2NotifierDelayedReadyAndSingleEmission(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)

	if states := recorder.snapshot(); phase2NotifierHas(states, "READY=1\nSTATUS=Ready") {
		t.Fatalf("startup emitted READY before shared snapshot: %v", states)
	}
	snapshot.store(NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true, CodexHealthy: true, WatchdogEligible: true})
	now = now.Add(time.Second)
	ticker.ch <- now
	deadline := time.After(time.Second)
	for phase2NotifierCount(recorder.snapshot(), "READY=1\nSTATUS=Ready") != 1 {
		select {
		case <-deadline:
			t.Fatalf("delayed READY was not emitted: %v", recorder.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	now = now.Add(time.Second)
	ticker.ch <- now
	time.Sleep(5 * time.Millisecond)
	if got := phase2NotifierCount(recorder.snapshot(), "READY=1\nSTATUS=Ready"); got != 1 {
		t.Fatalf("READY count = %d, want one; states=%v", got, recorder.snapshot())
	}
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierPreProbeUnknownDoesNotBecomeFatal(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	// Even an untrusted zero/projection with an exhaustion bit must remain
	// conservative until the first bounded health observation is published.
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{RequiredCodex: true, RecoveryExhausted: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)
	phase2NotifierWaitState(t, recorder, "STATUS=Starting")
	select {
	case fatal := <-notifier.Fatal():
		t.Fatalf("pre-probe snapshot emitted fatal %v", fatal)
	default:
	}
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierProbedStartupDatabaseOrCoreFailureIsFatal(t *testing.T) {
	cases := []struct {
		name string
		data NotifierHealthSnapshot
		want error
	}{
		{
			name: "database",
			data: NotifierHealthSnapshot{Probed: true, CoreHealthy: true},
			want: ErrNotifierDatabaseFailure,
		},
		{
			name: "core",
			data: NotifierHealthSnapshot{Probed: true, DatabaseHealthy: true},
			want: ErrNotifierCoreFailure,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			snapshot := &phase2NotifierSnapshot{value: test.data}
			ticker := newPhase2NotifierTicker()
			recorder := &phase2NotifierRecorder{}
			notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
			done := phase2NotifierRun(t, notifier, context.Background())
			phase2NotifierWait(t, done, test.want)
			states := recorder.snapshot()
			if phase2NotifierHas(states, "READY=1\nSTATUS=Ready") || phase2NotifierHas(states, "WATCHDOG=1") {
				t.Fatalf("startup failure emitted readiness/watchdog: %v", states)
			}
			if fatal := <-notifier.Fatal(); !errors.Is(fatal, test.want) {
				t.Fatalf("Fatal() = %v, want %v", fatal, test.want)
			}
			phase2NotifierClose(t, notifier)
		})
	}
}

func TestPhase2NotifierOptionalCodexDegradationRemainsWatchdogHealthy(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true, WatchdogEligible: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)

	phase2NotifierWaitState(t, recorder, "READY=1\nSTATUS=Degraded")
	ticker.ch <- now.Add(time.Second)
	deadline := time.After(time.Second)
	for phase2NotifierCount(recorder.snapshot(), "WATCHDOG=1") == 0 {
		select {
		case <-deadline:
			t.Fatalf("optional Codex degradation stopped watchdog: %v", recorder.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if phase2NotifierHas(recorder.snapshot(), "STATUS=Exhausted") {
		t.Fatalf("optional Codex degradation exhausted notifier: %v", recorder.snapshot())
	}
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierModelIneligibleDoesNotHeartbeat(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{
		Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true,
		CodexHealthy: true, WatchdogEligible: false,
	}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)
	phase2NotifierWaitState(t, recorder, "READY=1\nSTATUS=Ready")
	now = now.Add(time.Second)
	ticker.ch <- now
	time.Sleep(5 * time.Millisecond)
	if states := recorder.snapshot(); phase2NotifierHas(states, "WATCHDOG=1") {
		t.Fatalf("model-ineligible snapshot emitted watchdog: %v", states)
	}
	select {
	case fatal := <-notifier.Fatal():
		t.Fatalf("model-ineligible snapshot became fatal: %v", fatal)
	default:
	}
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierRequiredRecoveryGraceAndRecovery(t *testing.T) {
	start := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	now := start
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true, RequiredCodex: true, WatchdogEligible: false, RecoverySince: start, RequiredRecoveryActive: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)

	phase2NotifierWaitState(t, recorder, "READY=1\nSTATUS=Recovering")
	now = start.Add(5 * time.Second)
	ticker.ch <- now
	deadline := time.After(time.Second)
	for phase2NotifierCount(recorder.snapshot(), "WATCHDOG=1") == 0 {
		select {
		case <-deadline:
			t.Fatalf("required recovery grace stopped watchdog: %v", recorder.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	value := snapshot.load()
	value.CodexHealthy = true
	snapshot.store(value)
	now = start.Add(6 * time.Second)
	ticker.ch <- now
	deadline = time.After(time.Second)
	for !phase2NotifierHas(recorder.snapshot(), "STATUS=Ready") {
		select {
		case <-deadline:
			t.Fatalf("Codex recovery did not restore ready status: %v", recorder.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierRequiredRecoveryBeyondGraceExhausts(t *testing.T) {
	start := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	now := start
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true, RequiredCodex: true, WatchdogEligible: false, RecoverySince: start, RequiredRecoveryActive: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, 10*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)

	phase2NotifierWaitState(t, recorder, "READY=1\nSTATUS=Recovering")
	now = start.Add(11 * time.Second)
	ticker.ch <- now
	phase2NotifierWait(t, done, ErrNotifierExhausted)
	if !phase2NotifierHas(recorder.snapshot(), "STATUS=Exhausted") {
		t.Fatalf("exhausted status missing: %v", recorder.snapshot())
	}
	if got := <-notifier.Fatal(); !errors.Is(got, ErrNotifierExhausted) {
		t.Fatalf("Fatal() = %v, want exhausted", got)
	}
	cancel()
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierExplicitRequiredExhaustionIsFatalBeforeReady(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{
		Probed: true, Ready: false, DatabaseHealthy: true, CoreHealthy: true,
		RequiredCodex: true, CodexHealthy: false, RequiredRecoveryActive: true,
		RecoveryExhausted: true, WatchdogEligible: false, RecoverySince: now.Add(-time.Minute),
	}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, 10*time.Second)
	done := phase2NotifierRun(t, notifier, context.Background())
	phase2NotifierWait(t, done, ErrNotifierExhausted)
	if states := recorder.snapshot(); phase2NotifierHas(states, "WATCHDOG=1") || phase2NotifierHas(states, "READY=1\nSTATUS=Recovering") {
		t.Fatalf("explicit exhaustion emitted readiness/watchdog: %v", states)
	}
	if fatal := <-notifier.Fatal(); !errors.Is(fatal, ErrNotifierExhausted) {
		t.Fatalf("Fatal() = %v, want exhausted", fatal)
	}
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierDatabaseFailureStopsWatchdogAndSignalsFatal(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{Probed: true, Ready: false, CoreHealthy: true, CodexHealthy: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	done := phase2NotifierRun(t, notifier, context.Background())
	phase2NotifierWait(t, done, ErrNotifierDatabaseFailure)
	states := recorder.snapshot()
	if phase2NotifierHas(states, "READY=1\nSTATUS=Ready") || phase2NotifierHas(states, "WATCHDOG=1") {
		t.Fatalf("database failure emitted readiness/watchdog: %v", states)
	}
	if got := <-notifier.Fatal(); !errors.Is(got, ErrNotifierDatabaseFailure) {
		t.Fatalf("Fatal() = %v, want database failure", got)
	}
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierStatusNeverIncludesSnapshotPayload(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{value: NotifierHealthSnapshot{Probed: true, Ready: true, DatabaseHealthy: true, CoreHealthy: true, CodexHealthy: false, WatchdogEligible: true}}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)
	phase2NotifierWaitState(t, recorder, "READY=1\nSTATUS=Degraded")
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	for _, state := range recorder.snapshot() {
		if state == "WATCHDOG=1" {
			continue
		}
		if state != "STATUS=Starting" && state != "STATUS=Ready" && state != "STATUS=Recovering" && state != "STATUS=Degraded" && state != "STATUS=Exhausted" && state != "READY=1\nSTATUS=Degraded" {
			if state == "" || state == "STOPPING=1\nSTATUS=Stopping" {
				continue
			}
			t.Fatalf("unexpected/dynamic status %q", state)
		}
	}
	phase2NotifierClose(t, notifier)
}

func TestPhase2NotifierCancellationStopsInjectedTicker(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	snapshot := &phase2NotifierSnapshot{}
	ticker := newPhase2NotifierTicker()
	recorder := &phase2NotifierRecorder{}
	notifier := phase2NotifierTestNotifier(snapshot, &now, ticker, recorder, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	done := phase2NotifierRun(t, notifier, ctx)
	cancel()
	phase2NotifierWait(t, done, context.Canceled)
	select {
	case <-ticker.stopped:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop injected ticker")
	}
	phase2NotifierClose(t, notifier)
}
