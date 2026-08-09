package application

import (
	"context"
	"time"

	"codex-commons/internal/domain"
)

const (
	projectOverviewDays         = 14
	defaultOverviewPreviewLimit = 5
	maxOverviewPreviewLimit     = 20
)

type ProjectOverviewRepository interface {
	ProjectOverviewSnapshot(context.Context, domain.ProjectOverviewReadQuery) (domain.ProjectOverviewDurableSnapshot, error)
}

type ProjectOverviewQuery struct {
	Project        string
	AttentionLimit int
	WorkLimit      int
}

type ProjectSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Purpose   string `json:"purpose"`
	Milestone string `json:"milestone,omitempty"`
	Now       string `json:"now,omitempty"`
	Revision  int64  `json:"revision"`
}

type ActivityDay struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type ActivityWindow struct {
	Timezone     string        `json:"timezone"`
	Start        time.Time     `json:"start"`
	EndExclusive time.Time     `json:"end_exclusive"`
	Days         []ActivityDay `json:"days"`
}

type CountMetric struct {
	Available bool `json:"available"`
	Count     *int `json:"count,omitempty"`
}

type ProjectMetrics struct {
	AttentionTotal     int         `json:"attention_total"`
	AttentionHigh      int         `json:"attention_high"`
	OpenWork           int         `json:"open_work"`
	MergedPullRequests CountMetric `json:"merged_pull_requests"`
	ActiveSessions     int         `json:"active_sessions"`
}

type AttentionPreview struct {
	Total int             `json:"total"`
	Limit int             `json:"limit"`
	Items []AttentionItem `json:"items"`
}

type NavigationRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

type CurrentWorkItem struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	State     string        `json:"state"`
	Priority  int           `json:"priority"`
	Owner     string        `json:"owner,omitempty"`
	UpdatedAt *time.Time    `json:"updated_at,omitempty"`
	Target    NavigationRef `json:"target"`
}

type CurrentWorkPreview struct {
	Total int               `json:"total"`
	Limit int               `json:"limit"`
	Items []CurrentWorkItem `json:"items"`
}

type ProjectOverview struct {
	Project                    ProjectSummary     `json:"project"`
	Activity                   ActivityWindow     `json:"activity"`
	Metrics                    ProjectMetrics     `json:"metrics"`
	NeedsAttention             AttentionPreview   `json:"needs_attention"`
	CurrentWork                CurrentWorkPreview `json:"current_work"`
	LastActionChangingActivity *time.Time         `json:"last_action_changing_activity,omitempty"`
	SnapshotAt                 time.Time          `json:"snapshot_at"`
}

func normalizeOverviewLimit(value int) (int, bool) {
	if value == 0 {
		return defaultOverviewPreviewLimit, true
	}
	return value, value >= 1 && value <= maxOverviewPreviewLimit
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

// ProjectOverview composes one durable SQLite snapshot with one captured view
// of process-local live presence. Activity always contains fourteen UTC days,
// including explicit zeroes.
func (s *Service) ProjectOverview(ctx context.Context, query ProjectOverviewQuery) (ProjectOverview, error) {
	if s == nil || s.presence == nil || query.Project == "" {
		return ProjectOverview{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(ProjectOverviewRepository)
	if !ok {
		return ProjectOverview{}, domain.ErrUnavailable
	}
	attentionLimit, ok := normalizeOverviewLimit(query.AttentionLimit)
	if !ok {
		return ProjectOverview{}, domain.ErrInvalid
	}
	workLimit, ok := normalizeOverviewLimit(query.WorkLimit)
	if !ok {
		return ProjectOverview{}, domain.ErrInvalid
	}

	now := s.clock.Now().UTC()
	start := utcDay(now).AddDate(0, 0, -(projectOverviewDays - 1))
	end := utcDay(now).AddDate(0, 0, 1)
	live := s.presence.List("")
	if len(live) > maxPeopleScan {
		return ProjectOverview{}, domain.ErrUnavailable
	}
	sessionIDs := make([]string, len(live))
	for i := range live {
		sessionIDs[i] = live[i].Session
	}

	durable, err := repository.ProjectOverviewSnapshot(ctx, domain.ProjectOverviewReadQuery{
		ProjectID: query.Project, ActivityStart: start, ActivityEnd: end,
		AttentionLimit: attentionLimit, WorkLimit: workLimit, SessionIDs: sessionIDs,
	})
	if err != nil {
		return ProjectOverview{}, err
	}
	activeSessions := 0
	for _, item := range live {
		project := item.Project
		if project == "" {
			project = durable.Sessions[item.Session].ProjectID
		}
		if item.HostConnected && project == query.Project {
			activeSessions++
		}
	}

	days := make([]ActivityDay, projectOverviewDays)
	dayCounts := make(map[string]int, len(durable.Activity))
	for _, item := range durable.Activity {
		dayCounts[item.Day.UTC().Format(time.DateOnly)] = item.Count
	}
	for i := range days {
		day := start.AddDate(0, 0, i).Format(time.DateOnly)
		days[i] = ActivityDay{Day: day, Count: dayCounts[day]}
	}
	attention := make([]AttentionItem, 0, len(durable.Attention))
	for _, item := range durable.Attention {
		attention = append(attention, AttentionItem{
			ID: item.ID, Severity: item.Severity, Title: item.Title,
			Project: item.ProjectID, ProjectName: item.ProjectName,
			SourceRef: item.SourceRef, Owner: item.AccountableSessionID,
			NextAction: item.NextAction, SourceKind: item.SourceKind,
			UpdatedAt: item.UpdatedAt, Untrusted: item.Untrusted,
		})
		if item.Destination != nil {
			attention[len(attention)-1].Destination = &BrowseDestination{Kind: item.Destination.Kind, Ref: item.Destination.Ref}
		}
	}
	work := make([]CurrentWorkItem, 0, len(durable.CurrentWork))
	for _, item := range durable.CurrentWork {
		work = append(work, CurrentWorkItem{
			ID: item.ID, Title: item.Title, State: item.State,
			Priority: item.Priority, Owner: item.OwnerSessionID,
			UpdatedAt: item.UpdatedAt,
			Target:    NavigationRef{Kind: "task", Ref: item.ID},
		})
	}
	merged := CountMetric{Available: durable.MergedPullRequests != nil, Count: durable.MergedPullRequests}
	return ProjectOverview{
		Project: ProjectSummary{
			ID: durable.Project.ID, Name: durable.Project.Name,
			Status: durable.Project.Status, Purpose: durable.Project.Purpose,
			Milestone: durable.Project.Milestone, Now: durable.Project.Now,
			Revision: durable.Project.Revision,
		},
		Activity: ActivityWindow{Timezone: "UTC", Start: start, EndExclusive: end, Days: days},
		Metrics: ProjectMetrics{
			AttentionTotal: durable.AttentionTotal, AttentionHigh: durable.AttentionHigh,
			OpenWork: durable.OpenWorkTotal, MergedPullRequests: merged,
			ActiveSessions: activeSessions,
		},
		NeedsAttention:             AttentionPreview{Total: durable.AttentionTotal, Limit: attentionLimit, Items: attention},
		CurrentWork:                CurrentWorkPreview{Total: durable.OpenWorkTotal, Limit: workLimit, Items: work},
		LastActionChangingActivity: durable.LastActionChangingActivity,
		SnapshotAt:                 now,
	}, nil
}
