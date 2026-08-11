package application

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

type postsRepositoryStub struct {
	feed   domain.PostBrowseSnapshot
	thread domain.PostThread
	topics []domain.Topic
}

func (r *postsRepositoryStub) HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error) {
	return domain.HomeDurableSnapshot{}, nil
}
func (r *postsRepositoryStub) PostBrowseSnapshot(context.Context, domain.PostBrowseQuery) (domain.PostBrowseSnapshot, error) {
	return r.feed, nil
}
func (r *postsRepositoryStub) PostThread(context.Context, domain.PostThreadQuery) (domain.PostThread, error) {
	return r.thread, nil
}
func (r *postsRepositoryStub) SetPostState(context.Context, domain.PostStateRequest) (domain.WriteResult, error) {
	return domain.WriteResult{ID: "PS-1", Revision: 4}, nil
}
func (r *postsRepositoryStub) BrowseTopics(context.Context, int) ([]domain.Topic, bool, error) {
	return r.topics, false, nil
}

func boundedFeedStub() *postsRepositoryStub {
	at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	items := make([]domain.PostBrowseItem, 21)
	for i := range items {
		items[i] = domain.PostBrowseItem{
			ID: "P-" + string(rune('A'+i)), Kind: "finding", Title: strings.Repeat("t", 200),
			Preview: strings.Repeat("p", 320), Topic: domain.PostTopic{ID: "general", Name: "General"},
			Author: domain.PostAuthor{SessionID: "S-1"}, CreatedAt: at.Add(-time.Duration(i) * time.Minute),
			State: "open", Attachments: []domain.PostAttachment{{Kind: "link", URL: "https://example.com/item"}},
		}
	}
	comments := make([]domain.PostComment, 11)
	for i := range comments {
		comments[i] = domain.PostComment{ID: "R-" + string(rune('A'+i)), Body: "bounded reply", Intent: "clarify",
			Author: domain.PostAuthor{SessionID: "S-2"}, CreatedAt: at.Add(time.Duration(i) * time.Minute)}
	}
	return &postsRepositoryStub{
		feed: domain.PostBrowseSnapshot{Total: 21, Items: items},
		thread: domain.PostThread{
			Post: domain.Object{Ref: "P-A", Kind: "finding", Title: "Canonical", Body: "full body",
				Basis: "evidence", SessionID: "S-1", CreatedAt: at},
			Topic: domain.PostTopic{ID: "general", Name: "General"}, Author: domain.PostAuthor{SessionID: "S-1"},
			State: "open", CommentCount: 11, Attachments: []domain.PostAttachment{}, Comments: comments,
		},
		topics: []domain.Topic{{ID: "general", Name: "General"}, {ID: "alpha", ProjectID: "alpha-project", Name: "Alpha"}},
	}
}

func TestPostsFeedIsBoundedMetadataAndOpenIsExplicit(t *testing.T) {
	service := New(boundedFeedStub(), nil, nil)
	feed, err := service.BrowsePosts(context.Background(), PostFeedRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if feed.Total != 21 || feed.Limit != 20 || len(feed.Items) != 20 || feed.NextCursor == "" ||
		feed.Items[0].Author.Provenance == nil || feed.Items[0].Author.Provenance.Kind != "attested" ||
		feed.Items[0].Author.Provenance.Session != "S-1" {
		t.Fatalf("feed bounds=%+v", feed)
	}
	payload, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > 32<<10 {
		t.Fatalf("feed bytes=%d exceeds 32KiB", len(payload))
	}
	if strings.Contains(string(payload), "full body") || strings.Contains(string(payload), `"body"`) ||
		strings.Contains(string(payload), `"basis"`) || strings.Contains(string(payload), `"comments"`) {
		t.Fatalf("feed leaked canonical thread fields: %s", payload)
	}

	opened, err := service.OpenPost(context.Background(), PostOpenRequest{Ref: "P-A"})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Post.Body != "full body" || opened.Post.Basis != "evidence" ||
		opened.Post.CommentCount != 11 || opened.Post.Destination.Ref != "P-A" ||
		opened.Comments.Limit != 10 || len(opened.Comments.Items) != 10 || opened.Comments.NextCursor == "" ||
		opened.Comments.Items[0].Intent != "clarify" || opened.Post.Author.Provenance == nil ||
		opened.Post.Author.Provenance.Session != "S-1" || opened.Comments.Items[0].Author.Provenance == nil ||
		opened.Comments.Items[0].Author.Provenance.Session != "S-2" {
		t.Fatalf("explicit open=%+v", opened)
	}
}

func BenchmarkBoundedPostsFeed(b *testing.B) {
	service := New(boundedFeedStub(), nil, nil)
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := service.BrowsePosts(ctx, PostFeedRequest{})
		if err != nil {
			b.Fatal(err)
		}
		payload, err := json.Marshal(result)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(payload)), "bytes/op")
		b.ReportMetric(float64((len(payload)+2)/3), "est_tokens/op")
	}
}
