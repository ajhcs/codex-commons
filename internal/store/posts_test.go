package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"codex-commons/internal/domain"
)

func TestPostFeedThreadAttachmentsAndAppendOnlyState(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	ctx := context.Background()
	attachment := domain.PostAttachment{Kind: "github", URL: "https://github.com/openai/codex/pull/9", Title: "Codex PR"}
	first, err := s.Post(ctx, domain.PostRequest{
		TopicID: "commons-lab", Kind: "finding", Title: "Bounded feed record",
		Body: strings.Repeat("meaningful needle evidence ", 40), Basis: "verified test",
		SessionID: "S-1", RequestID: "feed-1", Attachments: []domain.PostAttachment{attachment},
	})
	must(t, err)
	_, err = s.Comment(ctx, domain.CommentRequest{PostID: first.ID, Body: "First reply", Intent: "answer", SessionID: "S-2", RequestID: "reply-1"})
	must(t, err)
	_, err = s.Comment(ctx, domain.CommentRequest{PostID: first.ID, Body: "Second reply", Intent: "clarify", SessionID: "S-1", RequestID: "reply-2"})
	must(t, err)
	replacement, err := s.Post(ctx, domain.PostRequest{
		TopicID: domain.TopicGeneral, Kind: "decision", Title: "Replacement record",
		Body: "Canonical replacement", Basis: "new evidence", SessionID: "S-2", RequestID: "feed-2",
	})
	must(t, err)
	_, err = s.Post(ctx, domain.PostRequest{
		TopicID: "commons-lab", Kind: "notice", Title: "Another project record",
		Body: "Chronological tie breaker", Basis: "test", SessionID: "S-1", RequestID: "feed-3",
	})
	must(t, err)

	state, err := s.SetPostState(ctx, domain.PostStateRequest{
		PostID: first.ID, State: "superseded", SupersededBy: replacement.ID,
		ActorID: "agent", SessionID: "S-1", RequestID: "state-1",
	})
	must(t, err)
	if state.Revision == 0 {
		t.Fatalf("project state revision=%+v", state)
	}
	replay, err := s.SetPostState(ctx, domain.PostStateRequest{
		PostID: first.ID, State: "superseded", SupersededBy: replacement.ID,
		ActorID: "agent", SessionID: "S-1", RequestID: "state-1",
	})
	must(t, err)
	if replay != state {
		t.Fatalf("state replay=%+v want %+v", replay, state)
	}

	snapshot, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{
		Filters: domain.PostFilters{Search: "meaningful needle", TopicID: "commons-lab", Kind: "finding"},
		Limit:   2,
	})
	must(t, err)
	if snapshot.Total != 1 || len(snapshot.Items) != 1 {
		t.Fatalf("filtered feed=%+v", snapshot)
	}
	item := snapshot.Items[0]
	if item.ID != first.ID || len(item.Preview) > 320 || item.CommentCount != 2 ||
		item.State != "superseded" || item.SupersededBy != replacement.ID ||
		len(item.Attachments) != 1 || item.Attachments[0] != attachment {
		t.Fatalf("feed item=%+v", item)
	}
	if strings.Contains(item.Preview, "verified test") {
		t.Fatalf("basis leaked into preview=%q", item.Preview)
	}

	thread, err := s.PostThread(ctx, domain.PostThreadQuery{PostID: first.ID, Limit: 1})
	must(t, err)
	commentBodies := map[string]bool{}
	for _, comment := range thread.Comments {
		commentBodies[comment.Body] = true
	}
	if thread.Post.Body == "" || thread.Post.Basis != "verified test" || thread.CommentCount != 2 ||
		len(thread.Comments) != 2 || !commentBodies["First reply"] || !commentBodies["Second reply"] ||
		thread.State != "superseded" || len(thread.Attachments) != 1 {
		t.Fatalf("thread=%+v", thread)
	}

	all, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{Limit: 1})
	must(t, err)
	if all.Total != 3 || len(all.Items) != 2 {
		t.Fatalf("keyset sentinel=%+v", all)
	}
	cursor := &domain.BrowseCursor{Time: all.Items[0].CreatedAt, ID: all.Items[0].ID}
	next, err := s.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{After: cursor, Limit: 1})
	must(t, err)
	if len(next.Items) == 0 || next.Items[0].ID == all.Items[0].ID {
		t.Fatalf("keyset page=%+v first=%+v", next, all.Items[0])
	}

	for _, request := range []domain.PostRequest{
		{TopicID: "commons-lab", Kind: "finding", Title: "bad", Body: "bad", Basis: "bad", SessionID: "S-1", Attachments: []domain.PostAttachment{{Kind: "link", URL: "http://example.com"}}},
		{TopicID: "commons-lab", Kind: "finding", Title: "bad", Body: "bad", Basis: "bad", SessionID: "S-1", Attachments: []domain.PostAttachment{{Kind: "github", URL: "https://example.com/not-github"}}},
	} {
		if _, err := s.Post(ctx, request); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("invalid attachment request=%+v err=%v", request, err)
		}
	}
	for _, query := range []string{
		"UPDATE post_attachments SET title='rewrite' WHERE post_id='" + first.ID + "'",
		"DELETE FROM post_state_events WHERE post_id='" + first.ID + "'",
	} {
		if _, err := s.DB().ExecContext(ctx, query); err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("append-only query=%q err=%v", query, err)
		}
	}
}

func TestGeneralPostStateUsesZeroRevision(t *testing.T) {
	s, _ := openTest(t)
	seed(t, s)
	post, err := s.Post(context.Background(), domain.PostRequest{
		TopicID: domain.TopicGeneral, Kind: "question", Title: "Shared question",
		Body: "Does this remain global?", Basis: "contract", SessionID: "S-1", RequestID: "general-state-post",
	})
	must(t, err)
	state, err := s.SetPostState(context.Background(), domain.PostStateRequest{
		PostID: post.ID, State: "resolved", SessionID: "S-1", RequestID: "general-resolved",
	})
	must(t, err)
	if state.Revision != 0 {
		t.Fatalf("general state revision=%d", state.Revision)
	}
	thread, err := s.PostThread(context.Background(), domain.PostThreadQuery{PostID: post.ID, Limit: 10})
	must(t, err)
	if thread.State != "resolved" {
		t.Fatalf("thread state=%q", thread.State)
	}
}
