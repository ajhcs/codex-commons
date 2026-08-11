package store

import (
	"context"
	"errors"
	"testing"

	"codex-commons/internal/domain"
)

func TestPostDiscoveryUsesScopeAndExactRecipients(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir()+"/discovery.sqlite3")
	must(t, err)
	defer s.Close()
	for _, project := range []domain.Project{{ID: "alpha", Name: "Alpha", Purpose: "A"}, {ID: "beta", Name: "Beta", Purpose: "B"}} {
		must(t, s.CreateProject(ctx, project))
		must(t, s.CreateTopic(ctx, domain.Topic{ID: project.ID + "-posts", ProjectID: project.ID, Name: "Posts"}))
	}
	for _, session := range []domain.Session{{ID: "agent-a", Host: "a", ProjectID: "alpha", Purpose: "A"}, {ID: "agent-a2", Host: "a", ProjectID: "alpha", Purpose: "A2"}, {ID: "agent-b", Host: "b", ProjectID: "beta", Purpose: "B"}} {
		must(t, s.UpsertSession(ctx, session))
	}
	makePost := func(topic, title, request string, mentions ...string) domain.Post {
		post, err := s.Post(ctx, domain.PostRequest{TopicID: topic, Kind: "finding", Title: title, Body: "Body", Basis: "Basis", ActorID: "author", ActorKind: "agent", ActorPrincipal: "agent-a", SessionID: "agent-a", RequestID: request, MentionPrincipals: mentions})
		must(t, err)
		return post
	}
	closed := makePost("alpha-posts", "closed", "closed")
	mentioned := makePost("alpha-posts", "mentioned", "mentioned", "agent-b")
	project := makePost("alpha-posts", "project", "project")
	_, err = s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: project.ID, Scope: "project", BaseRevision: 0, ActorID: "human", SessionID: "agent-a", RequestID: "scope-project"})
	must(t, err)
	commons := makePost("alpha-posts", "commons", "commons")
	_, err = s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: commons.ID, Scope: "commons", BaseRevision: 0, ActorID: "human", SessionID: "agent-a", RequestID: "scope-commons"})
	must(t, err)
	generalClosed := makePost(domain.TopicGeneral, "general closed", "general-closed")
	generalCommons := makePost(domain.TopicGeneral, "general commons", "general-commons")
	_, err = s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: generalCommons.ID, Scope: "commons", BaseRevision: 0, ActorID: "human", SessionID: "agent-a", RequestID: "scope-general"})
	must(t, err)
	if _, err := s.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: generalClosed.ID, Scope: "project", BaseRevision: 0, ActorID: "human", SessionID: "agent-a", RequestID: "bad-general-project"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("general project scope err=%v", err)
	}

	alpha, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{Limit: 20, ViewerKind: "agent", ViewerSession: "agent-a2"})
	must(t, err)
	if got := postTitles(alpha.Items); !equalStrings(got, []string{"commons", "project", "general commons"}) {
		t.Fatalf("alpha discovery=%v", got)
	}
	beta, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{Limit: 20, ViewerKind: "agent", ViewerSession: "agent-b"})
	must(t, err)
	if got := postTitles(beta.Items); !equalStrings(got, []string{"commons", "mentioned", "general commons"}) {
		t.Fatalf("beta discovery=%v", got)
	}
	if _, err := s.PostThread(ctx, domain.PostThreadQuery{PostID: closed.ID, Limit: 10, ViewerKind: "agent", ViewerSession: "agent-b"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("closed cross-project open err=%v", err)
	}
	if _, err := s.PostThread(ctx, domain.PostThreadQuery{PostID: mentioned.ID, Limit: 10, ViewerKind: "agent", ViewerSession: "agent-b"}); err != nil {
		t.Fatalf("mentioned exact open err=%v", err)
	}
	if _, err := s.Comment(ctx, domain.CommentRequest{PostID: closed.ID, Body: "hidden", Intent: "clarify", ActorID: "actor-b", ActorKind: "agent", ActorPrincipal: "agent-b", SessionID: "agent-b", RequestID: "hidden-comment"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("closed cross-project comment err=%v", err)
	}
	if _, err := s.Comment(ctx, domain.CommentRequest{PostID: mentioned.ID, Body: "visible", Intent: "clarify", ActorID: "actor-b", ActorKind: "agent", ActorPrincipal: "agent-b", SessionID: "agent-b", RequestID: "mentioned-comment"}); err != nil {
		t.Fatalf("mentioned exact comment err=%v", err)
	}
	human, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{Limit: 20, ViewerKind: "human", ViewerSession: "human-local-admin"})
	must(t, err)
	if len(human.Items) != 6 {
		t.Fatalf("human items=%d", len(human.Items))
	}
}

func postTitles(items []domain.PostBrowseItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Title)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
