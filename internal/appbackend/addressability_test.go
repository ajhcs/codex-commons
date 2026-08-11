package appbackend

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
	"codex-commons/internal/presence"
	commonsstore "codex-commons/internal/store"
)

func TestAddressabilityApplicationAuthorityAndTruthfulLookup(t *testing.T) {
	ctx := context.Background()
	store, err := commonsstore.Open(ctx, filepath.Join(t.TempDir(), "addressability.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Status: "active", Purpose: "test"}); err != nil {
		t.Fatal(err)
	}
	if err = store.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Posts"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []domain.Session{{ID: "S-agent", Host: "plumbob", ProjectID: "alpha", Purpose: "Build"}, {ID: "S-other", Host: "studio", ProjectID: "alpha", Purpose: "Review"}, {ID: "human-local-admin", Host: "browser", Purpose: "Local admin"}} {
		if err = store.UpsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	post, err := store.Post(ctx, domain.PostRequest{TopicID: "alpha-posts", Kind: "finding", Title: "Finding", Body: "Body", Basis: "Basis", ActorID: "agent", SessionID: "S-agent", RequestID: "post"})
	if err != nil {
		t.Fatal(err)
	}
	live := presence.New(nil)
	humanPost, err := store.Post(ctx, domain.PostRequest{TopicID: "alpha-posts", Kind: "finding", Title: "Human finding", Body: "Body", Basis: "Basis", ActorID: "local-admin", ActorKind: "human", ActorPrincipal: domain.HumanLocalPrincipal, SessionID: "human-local-admin", RequestID: "human-post"})
	if err != nil {
		t.Fatal(err)
	}
	live.Connect(presence.Session{ID: "S-agent", Actor: "agent", Host: "plumbob", Project: "alpha"})
	backend, err := New(legacyStub{}, application.New(store, live, nil))
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := backend.LookupContributors(ctx, httpapi.ContributorLookupQuery{Project: "alpha", Limit: 10}, httpapi.RequestMeta{Actor: "agent", Session: "S-agent", Host: "plumbob"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lookup.Items) != 2 {
		t.Fatalf("lookup=%+v", lookup)
	}
	bySession := map[string]application.ContributorItem{}
	for _, item := range lookup.Items {
		bySession[item.Session] = item
	}
	if agent := bySession["S-agent"]; !agent.Addressable || !agent.Reachable || !agent.HostConnected || agent.Project == nil || agent.Project.ID != "alpha" {
		t.Fatalf("live agent=%+v lookup=%+v", agent, lookup)
	}
	if other := bySession["S-other"]; !other.Addressable || other.Reachable || other.HostConnected || other.Project == nil || other.Project.ID != "alpha" {
		t.Fatalf("offline agent=%+v lookup=%+v", other, lookup)
	}
	written, err := backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "commons", BaseRevision: 0}, httpapi.RequestMeta{PrincipalKind: "agent", Actor: "agent", Session: "S-agent", Host: "plumbob", IdempotencyKey: "agent-scope"})
	if err != nil || !written.Persisted || written.Revision != 1 {
		t.Fatalf("agent scope=%+v err=%v", written, err)
	}
	replay, err := backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "commons", BaseRevision: 0}, httpapi.RequestMeta{PrincipalKind: "agent", Actor: "agent", Session: "S-agent", Host: "plumbob", IdempotencyKey: "agent-scope"})
	if err != nil || replay != written {
		t.Fatalf("agent replay=%+v err=%v", replay, err)
	}
	_, err = backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "project", BaseRevision: 1}, httpapi.RequestMeta{PrincipalKind: "agent", Actor: "other", Session: "S-other", Host: "studio", IdempotencyKey: "other-scope"})
	var apiErr *httpapi.Error
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeForbidden {
		t.Fatalf("other-agent scope=%#v", err)
	}
	_, err = backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: humanPost.ID, Scope: "commons", BaseRevision: 0}, httpapi.RequestMeta{PrincipalKind: "agent", Actor: "agent", Session: "S-agent", Host: "plumbob", IdempotencyKey: "human-post-scope"})
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeForbidden {
		t.Fatalf("human post agent scope=%#v", err)
	}
	_, err = backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "project", BaseRevision: 1}, httpapi.RequestMeta{IdempotencyKey: "anonymous-scope"})
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeForbidden {
		t.Fatalf("anonymous scope=%#v", err)
	}
	written, err = backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "project", BaseRevision: 1}, httpapi.RequestMeta{PrincipalKind: "human", Actor: "local-admin", Session: "human-local-admin", Host: "browser", IdempotencyKey: "human-scope"})
	if err != nil {
		t.Fatal(err)
	}
	if !written.Persisted || written.Revision != 2 {
		t.Fatalf("scope write=%+v", written)
	}
	_, err = backend.SetPerspectiveScope(ctx, httpapi.PerspectiveScopeWriteRequest{Ref: post.ID, Scope: "commons", BaseRevision: 1}, httpapi.RequestMeta{PrincipalKind: "human", Actor: "local-admin", Session: "human-local-admin", Host: "browser", IdempotencyKey: "stale-scope"})
	if !errors.As(err, &apiErr) || apiErr.Code != httpapi.CodeConflict {
		t.Fatalf("stale scope=%#v", err)
	}
}
