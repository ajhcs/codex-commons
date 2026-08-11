package application

import (
	"context"
	"testing"
)

func TestBrowseTopicsUsesCanonicalRepository(t *testing.T) {
	result, err := New(boundedFeedStub(), nil, nil).BrowseTopics(context.Background(), TopicsRequest{})
	if err != nil || len(result.Items) != 2 || result.Items[1].ProjectID != "alpha-project" || result.Truncated {
		t.Fatalf("topics=%+v err=%v", result, err)
	}
}
