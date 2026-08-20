package storebackend

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

type betaCodexClient struct {
	available bool
	state     codexauth.AccountState
	err       error
}

func (c betaCodexClient) Available() bool { return c.available }
func (c betaCodexClient) StartDeviceCode(context.Context) (codexauth.DeviceCode, error) {
	return codexauth.DeviceCode{}, codexauth.ErrUnavailable
}
func (c betaCodexClient) PollLogin(context.Context, string) (codexauth.LoginResult, error) {
	return codexauth.LoginResult{}, codexauth.ErrUnavailable
}
func (c betaCodexClient) CancelLogin(context.Context, string) error { return codexauth.ErrUnavailable }
func (c betaCodexClient) AccountState(context.Context) (codexauth.AccountState, error) {
	return c.state, c.err
}
func (c betaCodexClient) SetEventHandler(func(codexauth.Event)) {}
func (c betaCodexClient) Close() error                          { return nil }

func TestMatchesPresenceState(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	live := presence.Snapshot{Execution: "executing", LastActivity: now.Add(-2 * time.Hour)}
	idle := presence.Snapshot{Execution: "not_running", LastActivity: now.Add(-30 * time.Minute)}
	inactive := presence.Snapshot{Execution: "not_running", LastActivity: now.Add(-2 * time.Hour)}

	tests := []struct {
		name  string
		state string
		item  presence.Snapshot
		want  bool
	}{
		{"all includes inactive", "all", inactive, true},
		{"active includes live", "active", live, true},
		{"active includes idle", "active", idle, true},
		{"active excludes inactive", "active", inactive, false},
		{"live includes executing", "live", live, true},
		{"live excludes idle", "live", idle, false},
		{"idle includes recent not-running", "idle", idle, true},
		{"idle excludes executing", "idle", live, false},
		{"inactive includes old not-running", "inactive", inactive, true},
		{"inactive excludes recent not-running", "inactive", idle, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesPresenceState(test.state, test.item, now); got != test.want {
				t.Fatalf("matchesPresenceState(%q)=%v want %v", test.state, got, test.want)
			}
		})
	}
}

func TestInstallationStatusEmptyRestoreEvidenceDoesNotSatisfyBeta(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	id, err := backend.InstallationIdentityHex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 32 || id != strings.ToLower(id) || id == strings.Repeat("0", 32) {
		t.Fatalf("installation identity hex=%q", id)
	}
	status, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Database.SchemaVersion != 17 {
		t.Fatalf("schema version=%d", status.Database.SchemaVersion)
	}
	if status.Database.InstallationID != id {
		t.Fatalf("status identity=%q helper=%q", status.Database.InstallationID, id)
	}
	if status.Evidence.RestoreDrill.Status != "unknown" || status.Evidence.BetaPrerequisitesMet {
		t.Fatalf("empty restore evidence was treated as Beta-ready: %+v", status.Evidence)
	}
	var evidence int
	if err = store.DB().QueryRowContext(ctx, `SELECT count(*) FROM installation_restore_evidence`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 {
		t.Fatalf("restore evidence count=%d", evidence)
	}
}

func TestInstallationStatusRejectsAllZeroIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().ExecContext(ctx, `DROP TRIGGER installation_status_identity_no_update`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().ExecContext(ctx, `UPDATE installation_status SET installation_id=x'00000000000000000000000000000000' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.InstallationIdentityHex(ctx); err == nil {
		t.Fatal("helper accepted all-zero identity")
	}
	if _, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{}); err == nil {
		t.Fatal("status accepted all-zero identity")
	}
}

func TestInstallationStatusRecordedRestoreReceiptDoesNotSatisfyBeta(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.InstallationIdentityHex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	backup := strings.Repeat("b", 64)
	input := []byte(`{"schema_version":17,"release_id":"continuous-dogfood-test","drill_id":"drill-1","recorded_at":"2026-08-20T00:00:00Z","installation_id":"` + id + `","restored_backup_digest":"` + backup + `"}`)
	if _, err = store.RecordRestoreEvidence(ctx, input); err != nil {
		t.Fatal(err)
	}
	status, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Evidence.RestoreDrill.Status != "unknown" || status.Evidence.BetaPrerequisitesMet {
		t.Fatalf("parsed restore receipt was treated as Beta-ready: %+v", status.Evidence)
	}
}

func TestInstallationStatusRejectsUnreceiptedEvidenceAndPendingRevocation(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend, err := New(store, presence.New(nil), "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().ExecContext(ctx, `UPDATE installation_status SET
		backup_status='verified',restore_status='verified',compatibility_status='compatible',reconciliation_status='healthy',
		report_recovery_status='verified',report_recovery_checked_at='2026-08-13T12:00:00Z',report_recovery_receipt_digest=lower(hex(randomblob(32))),
		duplicate_launch_status='verified',duplicate_launch_checked_at='2026-08-13T12:00:00Z',duplicate_launch_receipt_digest=lower(hex(randomblob(32))),
		repository_immutability_status='verified',repository_immutability_checked_at='2026-08-13T12:00:00Z',repository_immutability_receipt_digest=lower(hex(randomblob(32))),
		canonical_immutability_status='verified',canonical_immutability_checked_at='2026-08-13T12:00:00Z',canonical_immutability_receipt_digest=lower(hex(randomblob(32))),
		codex_session_revocation_pending=1 WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	status, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Evidence.ReportRecovery.Status != "unknown" || status.Evidence.DuplicateLaunchCheck.Status != "unknown" || status.Evidence.RepositoryImmutability.Status != "unknown" || status.Evidence.CanonicalImmutability.Status != "unknown" {
		t.Fatalf("unreceipted evidence was trusted: %+v", status.Evidence)
	}
	if !status.Codex.SessionRevocationPending || status.Evidence.BetaPrerequisitesMet {
		t.Fatalf("pending revocation status=%+v evidence=%+v", status.Codex, status.Evidence)
	}
	status.Codex.SessionRevocationPending = false
	if status.Evidence.BetaPrerequisitesMet {
		t.Fatal("unconfigured/signed-out Codex account was Beta-ready")
	}
}

func TestInstallationStatusBetaRequiresLiveSignedInCodexAndReceiptedEvidence(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured bool
		available  bool
		state      codexauth.AccountState
		err        error
		want       bool
	}{
		{name: "signed in", configured: true, available: true, state: codexauth.AccountSignedIn, want: true},
		{name: "signed out", configured: true, available: true, state: codexauth.AccountSignedOut},
		{name: "unknown", configured: true, available: true, state: codexauth.AccountUnknown},
		{name: "account error", configured: true, available: true, state: codexauth.AccountSignedIn, err: errors.New("read failed")},
		{name: "unavailable", configured: true, state: codexauth.AccountSignedIn},
		{name: "unconfigured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := commonsstore.Open(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			backend, err := New(store, presence.New(nil), "release-test")
			if err != nil {
				t.Fatal(err)
			}
			if test.configured {
				backend.ConfigureCodex(betaCodexClient{available: test.available, state: test.state, err: test.err}, "0.147.0")
			}
			checked := "2026-08-13T12:00:00Z"
			for index, kind := range []string{"report_recovery", "duplicate_launch", "repository_immutability", "canonical_immutability"} {
				digest := strings.Repeat(string(rune('a'+index)), 64)
				if _, err = store.DB().ExecContext(ctx, `INSERT INTO installation_evidence_receipts(kind,status,violations,checked_at,scope_digest,receipt_digest,recorded_at) VALUES(?,'verified',0,?,lower(hex(randomblob(32))),?,?)`, kind, checked, digest, checked); err != nil {
					t.Fatal(err)
				}
				if _, err = store.DB().ExecContext(ctx, `UPDATE installation_status SET `+kind+`_status='verified',`+kind+`_violations=0,`+kind+`_checked_at=?,`+kind+`_receipt_digest=? WHERE id=1`, checked, digest); err != nil {
					t.Fatal(err)
				}
			}
			_, err = store.DB().ExecContext(ctx, `UPDATE installation_status SET backup_status='verified',restore_status='verified',compatibility_status='compatible',reconciliation_status='healthy',codex_session_revocation_pending=0 WHERE id=1`)
			if err != nil {
				t.Fatal(err)
			}
			status, err := backend.InstallationStatus(ctx, httpapi.RequestMeta{})
			if err != nil {
				t.Fatal(err)
			}
			if status.Evidence.BetaPrerequisitesMet != test.want {
				t.Fatalf("Beta ready=%v want=%v Codex=%+v", status.Evidence.BetaPrerequisitesMet, test.want, status.Codex)
			}
		})
	}
}
