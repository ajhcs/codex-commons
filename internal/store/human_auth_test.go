package store

import (
	"context"
	"crypto/subtle"
	"errors"
	"testing"

	"codex-commons/internal/domain"
)

func testHumanDigest(value byte) []byte {
	digest := make([]byte, 32)
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func TestHumanAccountBindingIsOneInstallationAndIdempotent(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	digest := testHumanDigest(0x11)

	first, err := store.BindHumanAccount(ctx, domain.BindHumanAccountRequest{
		ProviderSubjectDigest: digest,
		DisplayName:           "Local Admin",
		Handle:                "local-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Principal != domain.HumanLocalPrincipal || first.Provider != "chatgpt" || first.Revision != 1 {
		t.Fatalf("unexpected first binding: %+v", first)
	}
	if subtle.ConstantTimeCompare(first.ProviderSubjectDigest, digest) != 1 {
		t.Fatalf("digest changed: got %x want %x", first.ProviderSubjectDigest, digest)
	}

	// Repeating the same provider identity is safe and does not overwrite the
	// public profile or append another account_bound outcome.
	repeated, err := store.BindHumanAccount(ctx, domain.BindHumanAccountRequest{
		ProviderSubjectDigest: append([]byte(nil), digest...),
		DisplayName:           "A Different Display Name",
		Handle:                "different-handle",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Revision != first.Revision || repeated.DisplayName != first.DisplayName || repeated.Handle != first.Handle {
		t.Fatalf("idempotent bind changed the binding: first=%+v repeated=%+v", first, repeated)
	}

	if _, err := store.BindHumanAccount(ctx, domain.BindHumanAccountRequest{
		ProviderSubjectDigest: testHumanDigest(0x22),
		DisplayName:           "Another Admin",
		Handle:                "another-admin",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("mismatched second account error=%v, want %v", err, domain.ErrConflict)
	}

	var bindings, accountBound int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_account_bindings`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_auth_events WHERE event_type='account_bound'`).Scan(&accountBound); err != nil {
		t.Fatal(err)
	}
	if bindings != 1 || accountBound != 1 {
		t.Fatalf("binding=%d account_bound_events=%d, want 1/1", bindings, accountBound)
	}
}

func TestHumanProfileCASAndIdempotency(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	if _, err := store.BindHumanAccount(ctx, domain.BindHumanAccountRequest{
		ProviderSubjectDigest: testHumanDigest(0x31),
		DisplayName:           "Initial Admin",
		Handle:                "initial-admin",
	}); err != nil {
		t.Fatal(err)
	}

	request := domain.UpdateHumanProfileRequest{
		Principal:      domain.HumanLocalPrincipal,
		DisplayName:    "Renamed Admin",
		Handle:         "renamed-admin",
		BaseRevision:   1,
		IdempotencyKey: "profile-rename-1",
	}
	updated, err := store.UpdateHumanProfile(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || updated.DisplayName != request.DisplayName || updated.Handle != request.Handle {
		t.Fatalf("updated binding=%+v", updated)
	}

	// The idempotency key is bound to the full semantic request payload.
	retry := request
	retry.DisplayName = "Retry Payload Must Not Win"
	retry.Handle = "retry-payload"
	if _, err := store.UpdateHumanProfile(ctx, retry); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("changed profile replay error=%v, want %v", err, domain.ErrConflict)
	}

	repeated, err := store.UpdateHumanProfile(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Revision != 2 || repeated.DisplayName != updated.DisplayName || repeated.Handle != updated.Handle {
		t.Fatalf("idempotent retry changed binding: %+v", repeated)
	}

	if _, err := store.UpdateHumanProfile(ctx, domain.UpdateHumanProfileRequest{
		Principal:      domain.HumanLocalPrincipal,
		DisplayName:    "Stale Admin",
		Handle:         "stale-admin",
		BaseRevision:   1,
		IdempotencyKey: "profile-stale-1",
	}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale profile update error=%v, want %v", err, domain.ErrConflict)
	}

	second, err := store.UpdateHumanProfile(ctx, domain.UpdateHumanProfileRequest{
		Principal:      domain.HumanLocalPrincipal,
		DisplayName:    "Final Admin",
		Handle:         "final-admin",
		BaseRevision:   2,
		IdempotencyKey: "profile-rename-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 3 || second.DisplayName != "Final Admin" || second.Handle != "final-admin" {
		t.Fatalf("second update=%+v", second)
	}

	rows, err := store.DB().QueryContext(ctx, `SELECT event_type,binding_revision,request_key FROM human_auth_events ORDER BY binding_revision`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type event struct {
		typ      string
		revision int64
		key      string
	}
	var events []event
	for rows.Next() {
		var current event
		if err := rows.Scan(&current.typ, &current.revision, &current.key); err != nil {
			t.Fatal(err)
		}
		events = append(events, current)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []event{
		{typ: "account_bound", revision: 1},
		{typ: "profile_updated", revision: 2, key: "profile-rename-1"},
		{typ: "profile_updated", revision: 3, key: "profile-rename-2"},
	}
	if len(events) != len(want) {
		t.Fatalf("events=%+v, want %+v", events, want)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("event[%d]=%+v, want %+v", index, events[index], want[index])
		}
	}
}

func TestHumanAuthEventsAreAppendOnlyAndRecoveryIdempotent(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	if _, err := store.BindHumanAccount(ctx, domain.BindHumanAccountRequest{
		ProviderSubjectDigest: testHumanDigest(0x41),
		DisplayName:           "Audit Admin",
		Handle:                "audit-admin",
	}); err != nil {
		t.Fatal(err)
	}

	recovery := domain.HumanAuthEventRequest{
		Principal:       domain.HumanLocalPrincipal,
		EventType:       "recovery_login",
		BindingRevision: 1,
		IdempotencyKey:  "recovery-request-1",
	}
	if err := store.RecordHumanAuthEvent(ctx, recovery); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordHumanAuthEvent(ctx, recovery); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordHumanAuthEvent(ctx, domain.HumanAuthEventRequest{
		Principal:       domain.HumanLocalPrincipal,
		EventType:       "recovery_login",
		BindingRevision: 1,
		IdempotencyKey:  "recovery-request-2",
	}); err != nil {
		t.Fatal(err)
	}

	var recoveryEvents int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_auth_events WHERE event_type='recovery_login'`).Scan(&recoveryEvents); err != nil {
		t.Fatal(err)
	}
	if recoveryEvents != 2 {
		t.Fatalf("recovery events=%d, want 2", recoveryEvents)
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE human_auth_events SET request_key='mutated' WHERE event_type='recovery_login'`); err == nil {
		t.Fatal("append-only auth event update unexpectedly succeeded")
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM human_auth_events WHERE event_type='recovery_login'`); err == nil {
		t.Fatal("append-only auth event delete unexpectedly succeeded")
	}

	for _, invalid := range []domain.HumanAuthEventRequest{
		{Principal: "agent:wrong", EventType: "recovery_login", BindingRevision: 1, IdempotencyKey: "bad-principal"},
		{Principal: domain.HumanLocalPrincipal, EventType: "account_bound", BindingRevision: 1, IdempotencyKey: "bad-event"},
		{Principal: domain.HumanLocalPrincipal, EventType: "recovery_login", BindingRevision: -1, IdempotencyKey: "bad-revision"},
	} {
		if err := store.RecordHumanAuthEvent(ctx, invalid); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("invalid event %+v error=%v, want %v", invalid, err, domain.ErrInvalid)
		}
	}
}

func TestHumanAccountBindingValidationDoesNotPersistPartialState(t *testing.T) {
	store, _ := openTest(t)
	ctx := context.Background()
	invalid := []domain.BindHumanAccountRequest{
		{ProviderSubjectDigest: make([]byte, 31), DisplayName: "Admin", Handle: "admin-user"},
		{ProviderSubjectDigest: testHumanDigest(0x51), DisplayName: "Admin", Handle: "Admin-User"},
		{ProviderSubjectDigest: testHumanDigest(0x52), DisplayName: "Admin", Handle: "-admin-user"},
	}
	for _, request := range invalid {
		if _, err := store.BindHumanAccount(ctx, request); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("invalid bind %+v error=%v, want %v", request, err, domain.ErrInvalid)
		}
	}
	var bindings, events int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_account_bindings`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM human_auth_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 || events != 0 {
		t.Fatalf("invalid bind persisted bindings=%d events=%d", bindings, events)
	}
}

func TestHumanBindingNotFoundMapsToDomainError(t *testing.T) {
	store, _ := openTest(t)
	_, err := store.GetHumanAccountBinding(context.Background())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing binding error=%v, want %v", err, domain.ErrNotFound)
	}
}
