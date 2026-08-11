package store

import (
	"context"

	"codex-commons/internal/domain"
)

func (s *Store) BrowseTopics(ctx context.Context, limit int) ([]domain.Topic, bool, error) {
	if limit < 1 || limit > 100 {
		return nil, false, domain.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(project_id,''),name,created_at
FROM topics
ORDER BY CASE WHEN id='general' THEN 0 ELSE 1 END,name COLLATE NOCASE,id LIMIT ?`, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]domain.Topic, 0, limit)
	for rows.Next() {
		var item domain.Topic
		var created string
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.Name, &created); err != nil {
			return nil, false, err
		}
		item.CreatedAt = parseStamp(created)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(items) > limit
	if truncated {
		items = items[:limit]
	}
	return items, truncated, nil
}
