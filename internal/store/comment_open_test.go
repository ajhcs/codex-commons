package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"codex-commons/internal/domain"
)

func TestDirectCommentOpenIsBoundedExactAndScopeChecked(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, t.TempDir()+"/comment-open.sqlite3")
	must(t, err)
	defer s.Close()
	must(t, s.CreateProject(ctx, domain.Project{ID: "alpha", Name: "Alpha", Purpose: "A"}))
	must(t, s.CreateTopic(ctx, domain.Topic{ID: "alpha-posts", ProjectID: "alpha", Name: "Posts"}))
	for _, session := range []domain.Session{{ID: "author", Host: "a", ProjectID: "alpha", Purpose: "Author"}, {ID: "recipient", Host: "b", Purpose: "Recipient"}, {ID: "outsider", Host: "c", Purpose: "Outsider"}} {
		must(t, s.UpsertSession(ctx, session))
	}
	post, err := s.Post(ctx, domain.PostRequest{TopicID: "alpha-posts", Kind: "question", Title: "Question", Body: "Body", Basis: "Basis", ActorID: "author", ActorKind: "agent", ActorPrincipal: "author", SessionID: "author", RequestID: "post"})
	must(t, err)
	var target string
	for i := 0; i < 25; i++ {
		mentions := []string(nil)
		if i == 24 {
			mentions = []string{"recipient"}
		}
		result, err := s.Comment(ctx, domain.CommentRequest{PostID: post.ID, Body: fmt.Sprintf("Comment %02d", i), Intent: "clarify", ActorID: "author", ActorKind: "agent", ActorPrincipal: "author", SessionID: "author", RequestID: fmt.Sprintf("comment-%02d", i), MentionPrincipals: mentions})
		must(t, err)
		target = result.ID
	}
	postID, comment, err := s.PostCommentByID(ctx, target, "agent", "recipient")
	must(t, err)
	if postID != post.ID || comment.ID != target || comment.Body != "Comment 24" || len(comment.Mentions) != 1 || comment.Mentions[0].Principal != "recipient" {
		t.Fatalf("direct comment post=%s comment=%+v", postID, comment)
	}
	if _, _, err := s.PostCommentByID(ctx, target, "agent", "outsider"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("outsider comment open err=%v", err)
	}
}
