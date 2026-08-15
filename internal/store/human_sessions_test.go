package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestHumanBrowserSessionsPersistOnlyDigestsRevisionExpiryAndRevocation(t *testing.T) {
	s, path := openTest(t)
	ctx := context.Background()
	now := testNow.UTC()
	token, csrf := []byte("plaintext-browser-cookie"), []byte("plaintext-csrf-secret")
	td, cd := sha256.Sum256(token), sha256.Sum256(csrf)
	err := s.SaveHumanBrowserSession(ctx, domain.HumanBrowserSession{TokenDigest: td[:], CSRFDigest: cd[:], Principal: domain.HumanLocalPrincipal, AuthMethod: "codex", BindingRevision: 3, CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
	must(t, err)
	var raw []byte
	must(t, s.DB().QueryRowContext(ctx, `SELECT CAST(token_digest AS BLOB)||CAST(csrf_digest AS BLOB) FROM human_browser_sessions`).Scan(&raw))
	if bytes.Contains(raw, token) || bytes.Contains(raw, csrf) {
		t.Fatal("plaintext browser credential persisted")
	}
	must(t, s.UpdateHumanBrowserSessionRevisions(ctx, domain.HumanLocalPrincipal, 4))
	must(t, s.Close())
	reopened, err := Open(ctx, path)
	must(t, err)
	defer reopened.Close()
	got, err := reopened.HumanBrowserSession(ctx, td[:], now)
	must(t, err)
	if got.BindingRevision != 4 || got.AuthMethod != "codex" {
		t.Fatalf("session=%+v", got)
	}
	must(t, reopened.RevokeHumanBrowserSessionsByMethod(ctx, "codex", now))
	if _, err = reopened.HumanBrowserSession(ctx, td[:], now); err != domain.ErrNotFound {
		t.Fatalf("revoked err=%v", err)
	}
	expiredTD := sha256.Sum256([]byte("expired"))
	expiredCD := sha256.Sum256([]byte("expired-csrf"))
	must(t, reopened.SaveHumanBrowserSession(ctx, domain.HumanBrowserSession{TokenDigest: expiredTD[:], CSRFDigest: expiredCD[:], Principal: domain.HumanLocalPrincipal, AuthMethod: "recovery", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}))
	if _, err = reopened.HumanBrowserSession(ctx, expiredTD[:], now); err != domain.ErrNotFound {
		t.Fatalf("expired err=%v", err)
	}
}
