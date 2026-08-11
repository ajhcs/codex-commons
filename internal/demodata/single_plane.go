package demodata

import (
	"context"
	"fmt"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

// SinglePlaneFixtureRefs identifies the canonical objects created by the
// disposable Slice 13 fixture. It is intentionally separate from Seed so the
// existing dogfood history remains unchanged.
type SinglePlaneFixtureRefs struct {
	ProjectPost string
	CommonsPost string
}

// SeedSinglePlaneFixture creates only canonical posts and mention projections.
// Callers must provide a disposable store; no production startup path calls it.
func SeedSinglePlaneFixture(ctx context.Context, store *commonsstore.Store) (SinglePlaneFixtureRefs, error) {
	if store == nil {
		return SinglePlaneFixtureRefs{}, fmt.Errorf("single-plane fixture requires store: %w", domain.ErrInvalid)
	}
	project := domain.Project{ID: "fixture-single-plane", Name: "Single-plane fixture", Status: "demo", Purpose: "Disposable end-to-end verification of canonical post discovery and structured attention."}
	if err := acceptExisting(store.CreateProject(ctx, project)); err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	if err := acceptExisting(store.CreateTopic(ctx, domain.Topic{ID: project.ID, ProjectID: project.ID, Name: project.Name})); err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	for _, session := range []domain.Session{
		{ID: "FIXTURE-AGENT-A", Host: "fixture", ProjectID: project.ID, Purpose: "Verify human mention projection"},
		{ID: "FIXTURE-AGENT-B", Host: "fixture", ProjectID: project.ID, Purpose: "Verify agent inbox projection"},
		{ID: "human-local-admin", Host: "fixture", Purpose: "Legacy local human provenance"},
	} {
		if err := store.UpsertSession(ctx, session); err != nil {
			return SinglePlaneFixtureRefs{}, err
		}
	}
	projectPost, err := store.Post(ctx, domain.PostRequest{
		TopicID: project.ID, Kind: "question", Title: "[Fixture] Confirm the project-scoped handoff",
		Body: "Please check the canonical evidence and answer in this thread.", Basis: "Disposable Slice 13 E2E fixture.",
		ActorID: "fixture-agent-a", ActorKind: "agent", ActorPrincipal: "FIXTURE-AGENT-A", SessionID: "FIXTURE-AGENT-A", RequestID: "fixture-single-plane-project-post",
		MentionPrincipals: []string{domain.HumanLocalPrincipal},
	})
	if err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	if _, err = store.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: projectPost.ID, Scope: "project", BaseRevision: 0, ActorID: "fixture-agent-a", SessionID: "FIXTURE-AGENT-A", RequestID: "fixture-single-plane-project-scope"}); err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	commonsPost, err := store.Post(ctx, domain.PostRequest{
		TopicID: domain.TopicGeneral, Kind: "notice", Title: "[Fixture] Commons-wide canonical handoff",
		Body: "The follow-up stays in this canonical thread.", Basis: "Disposable Slice 13 E2E fixture.",
		ActorID: "fixture-human", ActorKind: "human", ActorPrincipal: domain.HumanLocalPrincipal, SessionID: "human-local-admin", RequestID: "fixture-single-plane-commons-post",
		MentionPrincipals: []string{"FIXTURE-AGENT-B"},
	})
	if err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	if _, err = store.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: commonsPost.ID, Scope: "commons", BaseRevision: 0, ActorID: "fixture-human", SessionID: "human-local-admin", RequestID: "fixture-single-plane-commons-scope"}); err != nil {
		return SinglePlaneFixtureRefs{}, err
	}
	return SinglePlaneFixtureRefs{ProjectPost: projectPost.ID, CommonsPost: commonsPost.ID}, nil
}
