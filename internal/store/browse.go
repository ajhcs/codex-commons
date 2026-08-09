package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"codex-commons/internal/domain"
)

const (
	maxBrowseLimit      = 100
	maxBrowseSearch     = 200
	maxPeopleSessionIDs = 500
	maxAttentionFacets  = 50
)

func validateAttentionBrowseQuery(query domain.AttentionBrowseQuery) bool {
	if query.Limit < 1 || query.Limit > maxBrowseLimit {
		return false
	}
	f := query.Filters
	if len(f.Search) > maxBrowseSearch || strings.TrimSpace(f.Search) != f.Search ||
		f.SourceKind != "" && !domain.AttentionSourceKinds[f.SourceKind] ||
		f.Severity != "" && !domain.AttentionSeverities[f.Severity] ||
		len(f.OwnerSessionID) > maxHomeIdentifier || len(f.ProjectID) > maxHomeIdentifier {
		return false
	}
	if f.UpdatedFrom != nil && f.UpdatedTo != nil && f.UpdatedFrom.After(*f.UpdatedTo) {
		return false
	}
	return query.After == nil || (!query.After.Time.IsZero() && boundedHomeValue(query.After.ID, maxHomeIdentifier))
}

func attentionWhere(filters domain.AttentionFilters, cursor *domain.BrowseCursor) (string, []any) {
	where := []string{"e.state='open'"}
	args := make([]any, 0, 8)
	if filters.Search != "" {
		pattern := "%" + strings.ToLower(escapeLike(filters.Search)) + "%"
		where = append(where, `(lower(e.attention_id) LIKE ? ESCAPE '\' OR lower(e.title) LIKE ? ESCAPE '\' OR lower(e.source_ref) LIKE ? ESCAPE '\' OR EXISTS (
 SELECT 1 FROM projects fp WHERE fp.id=e.project_id AND lower(fp.name) LIKE ? ESCAPE '\'
))`)
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filters.SourceKind != "" {
		where, args = append(where, "e.source_kind=?"), append(args, filters.SourceKind)
	}
	if filters.OwnerSessionID != "" {
		where, args = append(where, "e.accountable_session_id=?"), append(args, filters.OwnerSessionID)
	}
	if filters.Severity != "" {
		where, args = append(where, "e.severity=?"), append(args, filters.Severity)
	}
	if filters.ProjectID != "" {
		where, args = append(where, "e.project_id=?"), append(args, filters.ProjectID)
	}
	if filters.UpdatedFrom != nil {
		where, args = append(where, "julianday(e.recorded_at)>=julianday(?)"), append(args, stamp(filters.UpdatedFrom.UTC()))
	}
	if filters.UpdatedTo != nil {
		where, args = append(where, "julianday(e.recorded_at)<=julianday(?)"), append(args, stamp(filters.UpdatedTo.UTC()))
	}
	if cursor != nil {
		where, args = append(where, "(julianday(e.recorded_at)<julianday(?) OR (julianday(e.recorded_at)=julianday(?) AND e.attention_id>?))"),
			append(args, stamp(cursor.Time.UTC()), stamp(cursor.Time.UTC()), cursor.ID)
	}
	return strings.Join(where, " AND "), args
}

const latestAttentionCTE = `WITH latest AS (
 SELECT attention_id,max(sequence) AS sequence FROM attention_events GROUP BY attention_id
)`

// AttentionBrowseSnapshot reads the matching count, rows, and available
// filter facets from one SQLite snapshot. Facets describe the complete current
// open set so a client can change or clear filters without another schema call.
func (s *Store) AttentionBrowseSnapshot(ctx context.Context, query domain.AttentionBrowseQuery) (domain.AttentionBrowseSnapshot, error) {
	if !validateAttentionBrowseQuery(query) {
		return domain.AttentionBrowseSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.AttentionBrowseSnapshot{}, err
	}
	defer tx.Rollback()
	out := domain.AttentionBrowseSnapshot{Facets: domain.AttentionFacets{
		Sources: []domain.FacetCount{}, Owners: []domain.FacetCount{},
		Severities: []domain.FacetCount{}, Projects: []domain.FacetCount{},
	}}
	where, args := attentionWhere(query.Filters, nil)
	if err := tx.QueryRowContext(ctx, latestAttentionCTE+`
SELECT count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence WHERE `+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	pageWhere, pageArgs := attentionWhere(query.Filters, query.After)
	pageArgs = append(pageArgs, query.Limit+1)
	rows, err := tx.QueryContext(ctx, latestAttentionCTE+`
SELECT e.attention_id,e.severity,e.title,COALESCE(e.project_id,''),COALESCE(p.name,''),
e.source_ref,COALESCE(e.accountable_session_id,''),e.next_action,e.source_kind,e.untrusted,e.recorded_at,
CASE
 WHEN e.source_kind='task' AND EXISTS(SELECT 1 FROM tasks t WHERE t.id=e.source_ref) THEN 'task'
 WHEN e.source_kind='forum_question' AND EXISTS(SELECT 1 FROM posts x WHERE x.id=e.source_ref) THEN 'post'
 ELSE '' END
FROM attention_events e
JOIN latest l ON l.sequence=e.sequence
LEFT JOIN projects p ON p.id=e.project_id
WHERE `+pageWhere+`
ORDER BY julianday(e.recorded_at) DESC,e.attention_id
LIMIT ?`, pageArgs...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.AttentionBrowseItem
		var untrusted int
		var updated, destinationKind string
		if err := rows.Scan(&item.ID, &item.Severity, &item.Title, &item.ProjectID,
			&item.ProjectName, &item.SourceRef, &item.AccountableSessionID,
			&item.NextAction, &item.SourceKind, &untrusted, &updated, &destinationKind); err != nil {
			rows.Close()
			return out, err
		}
		item.Untrusted = untrusted == 1
		item.UpdatedAt = parseStamp(updated)
		if destinationKind != "" {
			item.Destination = &domain.BrowseDestination{Kind: destinationKind, Ref: item.SourceRef}
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	if err := readAttentionFacets(ctx, tx, &out.Facets); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AttentionBrowseSnapshot{}, err
	}
	return out, nil
}

func readAttentionFacets(ctx context.Context, tx *sql.Tx, facets *domain.AttentionFacets) error {
	type facetSpec struct {
		query     string
		dst       *[]domain.FacetCount
		truncated *bool
		limit     int
	}
	specs := []facetSpec{
		{query: `SELECT e.source_kind,'',count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence WHERE e.state='open' GROUP BY e.source_kind ORDER BY e.source_kind`, dst: &facets.Sources},
		{query: `SELECT COALESCE(e.accountable_session_id,''),'',count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence WHERE e.state='open' AND e.accountable_session_id IS NOT NULL GROUP BY e.accountable_session_id ORDER BY e.accountable_session_id LIMIT 51`, dst: &facets.Owners, truncated: &facets.OwnersTruncated, limit: maxAttentionFacets},
		{query: `SELECT e.severity,'',count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence WHERE e.state='open' GROUP BY e.severity ORDER BY CASE e.severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END`, dst: &facets.Severities},
		{query: `SELECT e.project_id,p.name,count(*) FROM attention_events e JOIN latest l ON l.sequence=e.sequence JOIN projects p ON p.id=e.project_id WHERE e.state='open' GROUP BY e.project_id,p.name ORDER BY lower(p.name),p.id LIMIT 51`, dst: &facets.Projects, truncated: &facets.ProjectsTruncated, limit: maxAttentionFacets},
	}
	for _, spec := range specs {
		rows, err := tx.QueryContext(ctx, latestAttentionCTE+"\n"+spec.query)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item domain.FacetCount
			if err := rows.Scan(&item.Value, &item.Label, &item.Count); err != nil {
				rows.Close()
				return err
			}
			if spec.limit == 0 || len(*spec.dst) < spec.limit {
				*spec.dst = append(*spec.dst, item)
			} else if spec.truncated != nil {
				*spec.truncated = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func validateProjectBrowseQuery(query domain.ProjectBrowseQuery) bool {
	if query.Limit < 1 || query.Limit > maxBrowseLimit || len(query.Search) > maxBrowseSearch || strings.TrimSpace(query.Search) != query.Search {
		return false
	}
	return validPeopleSessionIDs(query.SessionIDs) &&
		(query.After == nil || (query.After.Text != "" && boundedHomeValue(query.After.ID, maxHomeIdentifier)))
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

// ProjectBrowseSnapshot derives current work and last action-changing activity
// from canonical task and activity records in one read transaction.
func (s *Store) ProjectBrowseSnapshot(ctx context.Context, query domain.ProjectBrowseQuery) (domain.ProjectBrowseSnapshot, error) {
	if !validateProjectBrowseQuery(query) {
		return domain.ProjectBrowseSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ProjectBrowseSnapshot{}, err
	}
	defer tx.Rollback()
	where, args := []string{"1=1"}, []any{}
	if query.Search != "" {
		pattern := "%" + strings.ToLower(escapeLike(query.Search)) + "%"
		where, args = append(where, `(lower(p.name) LIKE ? ESCAPE '\' OR lower(p.purpose) LIKE ? ESCAPE '\')`), append(args, pattern, pattern)
	}
	whereText := strings.Join(where, " AND ")
	out := domain.ProjectBrowseSnapshot{Sessions: make(map[string]domain.PeopleSessionFact, len(query.SessionIDs))}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects p WHERE `+whereText, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	if query.After != nil {
		where = append(where, `(p.name>? OR (p.name=? AND p.id>?))`)
		args = append(args, query.After.Text, query.After.Text, query.After.ID)
	}
	args = append(args, query.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.name,p.status,p.purpose,
COALESCE((SELECT count(*) FROM tasks ot WHERE ot.project_id=p.id AND ot.state IN ('ready','in_progress','blocked')),0),
COALESCE((SELECT t.id FROM tasks t WHERE t.project_id=p.id AND t.state='in_progress' ORDER BY t.priority,t.id LIMIT 1),''),
COALESCE((SELECT t.title FROM tasks t WHERE t.project_id=p.id AND t.state='in_progress' ORDER BY t.priority,t.id LIMIT 1),''),
COALESCE((SELECT t.state FROM tasks t WHERE t.project_id=p.id AND t.state='in_progress' ORDER BY t.priority,t.id LIMIT 1),''),
COALESCE((SELECT t.priority FROM tasks t WHERE t.project_id=p.id AND t.state='in_progress' ORDER BY t.priority,t.id LIMIT 1),0),
COALESCE((SELECT a.occurred_at FROM activity_events a WHERE a.project_id=p.id ORDER BY julianday(a.occurred_at) DESC,a.id DESC LIMIT 1),'')
FROM projects p WHERE `+strings.Join(where, " AND ")+`
ORDER BY p.name,p.id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.ProjectBrowseItem
		var workID, workTitle, workState, latest string
		var workPriority int
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Purpose, &item.OpenTasks,
			&workID, &workTitle, &workState, &workPriority, &latest); err != nil {
			rows.Close()
			return out, err
		}
		if workID != "" {
			item.CurrentWork = &domain.ProjectCurrentWork{ID: workID, Title: workTitle, State: workState, Priority: workPriority}
		}
		if latest != "" {
			value := parseStamp(latest)
			item.LatestActivity = &value
		}
		out.Items = append(out.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	out.Sessions, err = readPeopleSessionFacts(ctx, tx, query.SessionIDs)
	if err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProjectBrowseSnapshot{}, err
	}
	return out, nil
}

// PeopleFactsSnapshot joins a captured live-registry identity set to durable
// session/project facts in one query and one SQLite read snapshot.
func (s *Store) PeopleFactsSnapshot(ctx context.Context, query domain.PeopleFactsQuery) (domain.PeopleFactsSnapshot, error) {
	if !validPeopleSessionIDs(query.SessionIDs) {
		return domain.PeopleFactsSnapshot{}, domain.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.PeopleFactsSnapshot{}, err
	}
	defer tx.Rollback()
	sessions, err := readPeopleSessionFacts(ctx, tx, query.SessionIDs)
	if err != nil {
		return domain.PeopleFactsSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.PeopleFactsSnapshot{}, err
	}
	return domain.PeopleFactsSnapshot{Sessions: sessions}, nil
}

func validPeopleSessionIDs(sessionIDs []string) bool {
	if len(sessionIDs) > maxPeopleSessionIDs {
		return false
	}
	seen := make(map[string]bool, len(sessionIDs))
	for _, id := range sessionIDs {
		if !boundedHomeValue(id, maxHomeIdentifier) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func readPeopleSessionFacts(ctx context.Context, tx *sql.Tx, sessionIDs []string) (map[string]domain.PeopleSessionFact, error) {
	out := make(map[string]domain.PeopleSessionFact, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(sessionIDs)), ",")
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		args[i] = id
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT s.id,s.host,COALESCE(s.project_id,''),COALESCE(p.name,''),s.purpose
FROM sessions s LEFT JOIN projects p ON p.id=s.project_id WHERE s.id IN (%s)`, marks), args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item domain.PeopleSessionFact
		if err := rows.Scan(&item.ID, &item.Host, &item.ProjectID, &item.ProjectName, &item.Purpose); err != nil {
			rows.Close()
			return out, err
		}
		out[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	if err := rows.Close(); err != nil {
		return out, err
	}
	return out, nil
}
