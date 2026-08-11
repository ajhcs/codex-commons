package store

import (
	"context"
	"errors"
	"testing"

	"codex-commons/internal/domain"
)

func TestBrowseTopicsIsCanonicalSortedAndBounded(t *testing.T) {
	store, _ := openTest(t)
	seed(t, store)
	ctx := context.Background()
	must(t, store.CreateTopic(ctx, domain.Topic{ID: "topic-z", ProjectID: "commons-lab", Name: "zeta"}))
	must(t, store.CreateTopic(ctx, domain.Topic{ID: "topic-a", ProjectID: "commons-lab", Name: "Alpha"}))
	items, truncated, err := store.BrowseTopics(ctx, 2)
	must(t, err)
	if !truncated || len(items) != 2 || items[0].ID != domain.TopicGeneral || items[1].Name != "Alpha" || items[1].ProjectID != "commons-lab" {
		t.Fatalf("topics=%+v truncated=%t", items, truncated)
	}
	if _, _, err := store.BrowseTopics(ctx, 101); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid limit err=%v", err)
	}
}
