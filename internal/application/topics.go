package application

import (
	"context"

	"codex-commons/internal/domain"
)

type TopicRepository interface {
	BrowseTopics(context.Context, int) ([]domain.Topic, bool, error)
}

type TopicsRequest struct {
	Limit int
}

type TopicItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ProjectID string `json:"project_id,omitempty"`
}

type TopicsResult struct {
	Items     []TopicItem `json:"items"`
	Truncated bool        `json:"truncated"`
}

func (s *Service) BrowseTopics(ctx context.Context, request TopicsRequest) (TopicsResult, error) {
	if s == nil {
		return TopicsResult{}, domain.ErrInvalid
	}
	limit := request.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 100 {
		return TopicsResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(TopicRepository)
	if !ok {
		return TopicsResult{}, domain.ErrInvalid
	}
	items, truncated, err := repository.BrowseTopics(ctx, limit)
	if err != nil {
		return TopicsResult{}, err
	}
	out := TopicsResult{Items: make([]TopicItem, 0, len(items)), Truncated: truncated}
	for _, item := range items {
		out.Items = append(out.Items, TopicItem{ID: item.ID, Name: item.Name, ProjectID: item.ProjectID})
	}
	return out, nil
}
