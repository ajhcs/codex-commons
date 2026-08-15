package storebackend

import (
	"context"
	"errors"
	"testing"

	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

type phase2Provider struct {
	snapshot httpapi.RuntimeHealthSnapshot
	reads    int
}

func (p *phase2Provider) Snapshot() httpapi.RuntimeHealthSnapshot {
	p.reads++
	return p.snapshot
}

func TestPhase2BackendHealthIsLivenessOnlyAndInstallationProjectsSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "phase2-test")
	if err != nil {
		t.Fatal(err)
	}
	provider := &phase2Provider{snapshot: httpapi.RuntimeHealthSnapshot{
		Mode: "required", Required: true, State: "recovering", Ready: false, Live: true,
		Status: "supervisor_recovering", Reason: "supervisor_recovering", Generation: 12,
		Supervisor: httpapi.RuntimeSupervisorSnapshot{Generation: 12, State: "recovering", RetryCount: 2, RecoveryActive: true},
	}}
	backend.ConfigureRuntimeHealth(provider)

	// A canceled context must not turn public liveness into a dependency probe.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	health, err := backend.Health(canceled, httpapi.RequestMeta{})
	if err != nil || health.Status != "ok" || health.Version != "phase2-test" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	if provider.reads != 1 {
		t.Fatalf("public liveness snapshot reads=%d want=1", provider.reads)
	}

	status, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Runtime.Mode != "required" || status.Runtime.Ready || status.Runtime.Generation != 12 || status.Runtime.Supervisor.RetryCount != 2 {
		t.Fatalf("runtime projection=%+v", status.Runtime)
	}
	if provider.reads != 2 {
		t.Fatalf("installation status provider reads=%d want=2", provider.reads)
	}
}

func TestPhase2BackendHealthReflectsCachedCoreLivenessButIgnoresOptionalCodex(t *testing.T) {
	store, err := commonsstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	provider := &phase2Provider{snapshot: httpapi.RuntimeHealthSnapshot{
		Mode: "optional", State: "degraded", Ready: true, Live: true, Liveness: true,
		Status: "codex_optional_unavailable", Reason: "codex_optional_unavailable",
	}}
	backend.ConfigureRuntimeHealth(provider)
	if got, err := backend.Health(context.Background(), httpapi.RequestMeta{}); err != nil || got.Status != "ok" {
		t.Fatalf("optional Codex liveness=%+v err=%v", got, err)
	}
	provider.snapshot = httpapi.RuntimeHealthSnapshot{
		Mode: "optional", State: "degraded", Ready: false, Live: false, Liveness: true,
		Status: "database_failed", Reason: "database_failed",
	}
	got, err := backend.Health(context.Background(), httpapi.RequestMeta{})
	if err == nil || got.Status != "degraded" {
		t.Fatalf("database failure liveness=%+v err=%v", got, err)
	}
	var apiErr *httpapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeUnavailable {
		t.Fatalf("health error=%v", err)
	}
}

func TestPhase2BackendWithoutProviderKeepsLegacyDBLivenessAndConservativeSnapshot(t *testing.T) {
	store, err := commonsstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := New(store, presence.New(nil), "legacy-test")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}

	if snapshot := backend.RuntimeHealth(); snapshot.Ready || snapshot.Live || snapshot.Liveness || snapshot.Status == "ok" {
		t.Fatalf("unconfigured backend exposed a green runtime snapshot: %+v", snapshot)
	}
	if got, err := backend.Health(context.Background(), httpapi.RequestMeta{}); err != nil || got.Status != "ok" || got.Version != "legacy-test" {
		t.Fatalf("legacy healthy DB liveness=%+v err=%v", got, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := backend.Health(context.Background(), httpapi.RequestMeta{}); err == nil || got.Status != "" {
		t.Fatalf("closed DB liveness=%+v err=%v", got, err)
	}
}

func TestPhase2BackendRuntimeHealthDefensivelyCopiesComponents(t *testing.T) {
	store, err := commonsstore.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	provider := &phase2Provider{snapshot: httpapi.RuntimeHealthSnapshot{Components: map[string]httpapi.RuntimeComponentSnapshot{
		"database": {State: "healthy", Ready: true, Required: true},
	}}}
	backend.ConfigureRuntimeHealth(provider)
	first := backend.RuntimeHealth()
	first.Components["database"] = httpapi.RuntimeComponentSnapshot{State: "failed"}
	second := backend.RuntimeHealth()
	if second.Components["database"].State != "healthy" {
		t.Fatalf("provider-owned component was mutated: %+v", second.Components)
	}
}
