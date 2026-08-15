package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"

	"codex-commons/internal/domain"
)

type archaeologyPageCursor struct {
	Version  int    `json:"v"`
	Scope    string `json:"scope"`
	Sort     string `json:"sort,omitempty"`
	Search   string `json:"search,omitempty"`
	Revision int64  `json:"revision"`
	Offset   int    `json:"offset"`
}

func decodeArchaeologyCursor(cursor, scope, sort, search string, revision int64) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) > 1024 {
		return 0, domain.ErrInvalid
	}
	var value archaeologyPageCursor
	if json.Unmarshal(raw, &value) != nil || value.Version != 1 || value.Scope != scope || value.Sort != sort || value.Search != search || value.Offset < 0 || value.Offset > 10000 {
		return 0, domain.ErrInvalid
	}
	if value.Revision != revision {
		return 0, domain.ErrConflict
	}
	return value.Offset, nil
}
func encodeArchaeologyCursor(scope, sort, search string, revision int64, offset int) string {
	body, _ := json.Marshal(archaeologyPageCursor{Version: 1, Scope: scope, Sort: sort, Search: search, Revision: revision, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(body)
}

func escapeCatalogSearch(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func (s *Store) ArchaeologyCatalog(ctx context.Context, principal string, query domain.ArchaeologyCatalogQuery) (domain.ArchaeologyCatalogPage, error) {
	if !boundedCoreText(principal, 200, true) || query.Limit < 1 || query.Limit > 100 || len(query.Search) > 200 || strings.TrimSpace(query.Search) != query.Search {
		return domain.ArchaeologyCatalogPage{}, domain.ErrInvalid
	}
	if query.Sort == "" {
		query.Sort = "recent"
	}
	order := "coalesce(last_activity_at,'') DESC,id ASC"
	switch query.Sort {
	case "recent":
	case "tasks":
		order = "codex_thread_count DESC,coalesce(last_activity_at,'') DESC,id ASC"
	case "name":
		order = "name COLLATE NOCASE ASC,id ASC"
	default:
		return domain.ArchaeologyCatalogPage{}, domain.ErrInvalid
	}
	var sessionID string
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT id,revision FROM archaeology_sessions WHERE principal=?`, principal).Scan(&sessionID, &revision); err != nil {
		return domain.ArchaeologyCatalogPage{}, mapErr(err)
	}
	offset, err := decodeArchaeologyCursor(query.Cursor, "catalog", query.Sort, query.Search, revision)
	if err != nil {
		return domain.ArchaeologyCatalogPage{}, err
	}
	where := "session_id=?"
	args := []any{sessionID}
	if query.Search != "" {
		where += ` AND (name LIKE ? ESCAPE '\' COLLATE NOCASE OR repository_label LIKE ? ESCAPE '\' COLLATE NOCASE)`
		needle := "%" + escapeCatalogSearch(query.Search) + "%"
		args = append(args, needle, needle)
	}
	var out domain.ArchaeologyCatalogPage
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM archaeology_candidates WHERE "+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	args = append(args, query.Limit+1, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id,canonical_project_id,name,path_label,repository_label,last_activity_at,has_git,has_docs,has_codex_history,duration_min_seconds,duration_max_seconds,relative_cost,privacy_note,selected,from_codex_metadata,from_configured_root,codex_thread_count FROM archaeology_candidates WHERE `+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.ArchaeologyCandidate
		var last sql.NullString
		var git, docs, history, selected, fromCodex, fromRoot int
		if err = rows.Scan(&c.ID, &c.CanonicalProjectID, &c.Name, &c.PathLabel, &c.RepositoryLabel, &last, &git, &docs, &history, &c.DurationMinSeconds, &c.DurationMaxSeconds, &c.RelativeCost, &c.PrivacyNote, &selected, &fromCodex, &fromRoot, &c.CodexThreadCount); err != nil {
			return out, err
		}
		c.HasGit, c.HasDocs, c.HasCodexHistory = git == 1, docs == 1, history == 1
		c.Selected, c.FromCodexMetadata, c.FromConfiguredRoot = selected == 1, fromCodex == 1, fromRoot == 1
		if last.Valid {
			c.LastActivityAt = parseStamp(last.String)
		}
		out.Candidates = append(out.Candidates, c)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Candidates) > query.Limit {
		out.Candidates = out.Candidates[:query.Limit]
		out.NextCursor = encodeArchaeologyCursor("catalog", query.Sort, query.Search, revision, offset+query.Limit)
	}
	return out, nil
}
