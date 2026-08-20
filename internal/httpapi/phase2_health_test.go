package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/runtimehealth"
)

type phase2HealthBackend struct {
	*fakeBackend
	snapshot     RuntimeHealthSnapshot
	statusCalls  int
	providerRead int
}

type phase2SnapshotOnlyBackend struct {
	*fakeBackend
}

func (b *phase2HealthBackend) InstallationStatus(context.Context, RequestMeta) (InstallationStatusResult, error) {
	b.statusCalls++
	return InstallationStatusResult{Runtime: b.snapshot}, nil
}

func (b *phase2HealthBackend) RuntimeHealth() RuntimeHealthSnapshot {
	b.providerRead++
	return b.snapshot
}

func phase2ReadinessRequest(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8088/v1/internal/readiness", nil)
	req.RemoteAddr = "127.0.0.1:43123"
	req.Host = "127.0.0.1:8088"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func phase2HealthHandler(backend Backend) http.Handler {
	return NewHandler(backend, Config{InternalReadinessHost: "127.0.0.1:8088", Version: "phase2-test"})
}

func TestPhase2ReadinessUsesCachedSnapshotForRequiredAndOptionalDegradation(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		ready      bool
		wantStatus int
	}{
		{name: "optional Codex degradation keeps core ready", mode: "optional", ready: true, wantStatus: http.StatusOK},
		{name: "required Codex degradation fails readiness", mode: "required", ready: false, wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &phase2HealthBackend{fakeBackend: &fakeBackend{}, snapshot: RuntimeHealthSnapshot{
				Mode: test.mode, Required: test.mode == "required", State: "degraded", Ready: test.ready,
				Status: "codex_unavailable", Reason: "codex_optional_unavailable",
				Components: map[string]RuntimeComponentSnapshot{
					"codex": {State: "degraded", Ready: false, Required: test.mode == "required", Status: "unavailable"},
				},
			}}
			rec := phase2ReadinessRequest(t, phase2HealthHandler(backend))
			if rec.Code != test.wantStatus {
				t.Fatalf("readiness code=%d want=%d body=%s", rec.Code, test.wantStatus, rec.Body.String())
			}
			if backend.statusCalls != 0 || backend.providerRead != 1 {
				t.Fatalf("request-time status calls=%d provider reads=%d", backend.statusCalls, backend.providerRead)
			}
		})
	}
}

func TestPhase2ReadinessDBFailureUsesSnapshotWithoutProbingBackend(t *testing.T) {
	backend := &phase2HealthBackend{fakeBackend: &fakeBackend{}, snapshot: RuntimeHealthSnapshot{
		Mode: "optional", State: "degraded", Ready: false, Status: "database_failed", Reason: "database_failed",
		Components: map[string]RuntimeComponentSnapshot{
			"database": {State: "failed", Ready: false, Required: true, Status: "failed"},
		},
	}}
	rec := phase2ReadinessRequest(t, phase2HealthHandler(backend))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "runtime not ready") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if backend.statusCalls != 0 || backend.providerRead != 1 {
		t.Fatalf("request-time probes status=%d provider=%d", backend.statusCalls, backend.providerRead)
	}
}

func TestPhase2ReadinessAcceptsDirectProviderWithoutInstallationStatusBackend(t *testing.T) {
	providerReads := 0
	h := NewHandler(&phase2SnapshotOnlyBackend{fakeBackend: &fakeBackend{}}, Config{
		InternalReadinessHost: "127.0.0.1:8088", Version: "phase2-test",
		RuntimeHealth: RuntimeHealthProviderFunc(func() RuntimeHealthSnapshot {
			providerReads++
			return RuntimeHealthSnapshot{Mode: "optional", State: "ready", Ready: true, Live: true, Liveness: true, Status: "ok"}
		}),
	})
	rec := phase2ReadinessRequest(t, h)
	if rec.Code != http.StatusOK || providerReads != 1 {
		t.Fatalf("code=%d provider reads=%d body=%s", rec.Code, providerReads, rec.Body.String())
	}
}

func TestPhase2ReadinessRecoveringAndExhaustedAre503AndInstallationJSONKeepsMetadata(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		state string
	}{
		{name: "recovering", state: "recovering"},
		{name: "exhausted", state: "exhausted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			retryAt := now.Add(time.Minute)
			backend := &phase2HealthBackend{fakeBackend: &fakeBackend{}, snapshot: RuntimeHealthSnapshot{
				Mode: "required", Required: true, State: test.state, Ready: false, Status: test.state, Reason: "supervisor_" + test.state,
				Generation: 7, Supervisor: RuntimeSupervisorSnapshot{Generation: 7, State: test.state, RetryCount: 3, RetryAt: &retryAt, RecoveryActive: test.state == "recovering", RecoveryExhausted: test.state == "exhausted"},
			}}
			rec := phase2ReadinessRequest(t, phase2HealthHandler(backend))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("readiness code=%d body=%s", rec.Code, rec.Body.String())
			}

			status, err := backend.InstallationStatus(context.Background(), RequestMeta{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			for _, marker := range []string{`"service"`, `"database"`, `"codex"`, `"runtime"`, `"generation":7`, `"retry_count":3`} {
				if !strings.Contains(string(body), marker) {
					t.Fatalf("JSON missing %s: %s", marker, body)
				}
			}
		})
	}
}

func TestPhase2PublicHealthDoesNotUseReadinessSnapshotOrDependencyProbes(t *testing.T) {
	backend := &phase2HealthBackend{fakeBackend: &fakeBackend{}, snapshot: RuntimeHealthSnapshot{Ready: false, State: "degraded"}}
	h := phase2HealthHandler(backend)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8088/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if backend.statusCalls != 0 || backend.providerRead != 0 {
		t.Fatalf("health performed readiness work status=%d provider=%d", backend.statusCalls, backend.providerRead)
	}
}

func TestProjectRuntimeHealthPreservesSafeStateAndRequiredMode(t *testing.T) {
	at := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	snapshot := runtimehealth.Evaluate(runtimehealth.Input{
		ObservedAt:     at,
		Database:       runtimehealth.DatabaseObservation{Status: runtimehealth.DatabaseHealthy, LastSuccessAt: at},
		Codex:          runtimehealth.CodexObservation{Configured: true, Required: true, Available: false},
		Supervisor:     runtimehealth.SupervisorObservation{Status: runtimehealth.SupervisorRecovering, Generation: 11},
		Account:        runtimehealth.AccountObservation{Status: runtimehealth.AccountUnknown},
		Model:          runtimehealth.ModelObservation{Status: runtimehealth.ModelUnknown},
		Reconciliation: runtimehealth.HealthObservation{Status: runtimehealth.HealthHealthy},
		Persistence:    runtimehealth.HealthObservation{Status: runtimehealth.HealthHealthy},
	})
	projected := ProjectRuntimeHealth(snapshot, true)
	if projected.Mode != "required" || !projected.Required || projected.Ready || projected.Generation != 11 || projected.Supervisor.State != "recovering" {
		t.Fatalf("required projection=%+v", projected)
	}
	if projected.ObservedAt == nil || !projected.ObservedAt.Equal(at) || projected.Components["codex"].Reason == "" {
		t.Fatalf("projection lost bounded metadata=%+v", projected)
	}
	optional := ProjectRuntimeHealth(snapshot, false)
	if optional.Mode != "optional" || optional.Required {
		t.Fatalf("optional projection=%+v", optional)
	}
}
