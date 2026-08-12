// Package application owns composed product read models. Transport adapters
// encode these types; persistence and live presence remain separate authorities.
package application

import (
	"context"
	"sort"
	"sync"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/presence"
)

const (
	defaultHomeLimit = 5
	maxHomeLimit     = 20
	maxHomePage      = 500
)

type HomeRepository interface {
	HomeSnapshot(context.Context, domain.HomeReadQuery) (domain.HomeDurableSnapshot, error)
}

type PresenceRegistry interface {
	Get(session string) (presence.Snapshot, bool)
	List(project string) []presence.Snapshot
}

type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Service struct {
	repository            HomeRepository
	presence              PresenceRegistry
	clock                 Clock
	humanDisplayName      string
	humanHandle           string
	identityMu            sync.RWMutex
	archaeologyDiscoverer ArchaeologyDiscoverer
	archaeologyLauncher   ArchaeologyHistorianLauncher
}

func New(repository HomeRepository, live PresenceRegistry, clock Clock) *Service {
	if clock == nil {
		clock = realClock{}
	}
	return &Service{repository: repository, presence: live, clock: clock, humanDisplayName: "Local admin", humanHandle: "local-admin"}
}

func (s *Service) ConfigureHumanIdentity(displayName, handle string) {
	if s != nil && displayName != "" && handle != "" {
		s.identityMu.Lock()
		s.humanDisplayName = displayName
		s.humanHandle = handle
		s.identityMu.Unlock()
	}
}

func (s *Service) humanIdentity() (displayName, handle string) {
	if s == nil {
		return "Local admin", "local-admin"
	}
	s.identityMu.RLock()
	defer s.identityMu.RUnlock()
	return s.humanDisplayName, s.humanHandle
}

type HomeQuery struct {
	PresenceLimit  int
	AttentionLimit int
	AttentionPage  int
	ActivityLimit  int
	ActivityPage   int
}

type Navigation struct {
	Projects int `json:"projects"`
	People   int `json:"people"`
}

type PresenceItem struct {
	Session        string     `json:"session"`
	Actor          string     `json:"actor,omitempty"`
	Host           string     `json:"host"`
	Project        string     `json:"project,omitempty"`
	ProjectName    string     `json:"project_name,omitempty"`
	Purpose        string     `json:"purpose,omitempty"`
	HostConnected  bool       `json:"host_connected"`
	Execution      string     `json:"execution"`
	LeaseExpires   *time.Time `json:"lease_expires,omitempty"`
	LastActivity   time.Time  `json:"last_activity"`
	RecencySeconds int64      `json:"recency_seconds"`
	Loaded         *string    `json:"loaded,omitempty"`
}

type PresencePage struct {
	Total int            `json:"total"`
	Items []PresenceItem `json:"items"`
}

type AttentionItem struct {
	ID          string             `json:"id"`
	Severity    string             `json:"severity"`
	Title       string             `json:"title"`
	Project     string             `json:"project,omitempty"`
	ProjectName string             `json:"project_name,omitempty"`
	SourceRef   string             `json:"source_ref"`
	Owner       string             `json:"owner,omitempty"`
	NextAction  string             `json:"next_action"`
	SourceKind  string             `json:"source_kind"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Untrusted   bool               `json:"untrusted"`
	Destination *BrowseDestination `json:"destination,omitempty"`
}

type AttentionPage struct {
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	Limit   int             `json:"limit"`
	Items   []AttentionItem `json:"items"`
	HasMore bool            `json:"has_more"`
}

type ActivityItem struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Project     string    `json:"project,omitempty"`
	ProjectName string    `json:"project_name,omitempty"`
	Actor       string    `json:"actor"`
	ObjectRef   string    `json:"object_ref"`
	ObjectTitle string    `json:"object_title"`
	Outcome     string    `json:"outcome,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Untrusted   bool      `json:"untrusted"`
}

type ActivityPage struct {
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	Limit   int            `json:"limit"`
	Items   []ActivityItem `json:"items"`
	HasMore bool           `json:"has_more"`
}

type GeneralHome struct {
	Navigation     Navigation    `json:"navigation"`
	Presence       PresencePage  `json:"presence"`
	NeedsAttention AttentionPage `json:"needs_attention"`
	RecentActivity ActivityPage  `json:"recent_activity"`
}

func normalizeLimit(value int) (int, bool) {
	if value == 0 {
		return defaultHomeLimit, true
	}
	return value, value >= 1 && value <= maxHomeLimit
}

func (s *Service) GeneralHome(ctx context.Context, query HomeQuery) (GeneralHome, error) {
	if s == nil || s.repository == nil || s.presence == nil ||
		query.AttentionPage < 0 || query.AttentionPage > maxHomePage || query.ActivityPage < 0 || query.ActivityPage > maxHomePage {
		return GeneralHome{}, domain.ErrInvalid
	}
	presenceLimit, ok := normalizeLimit(query.PresenceLimit)
	if !ok {
		return GeneralHome{}, domain.ErrInvalid
	}
	attentionLimit, ok := normalizeLimit(query.AttentionLimit)
	if !ok {
		return GeneralHome{}, domain.ErrInvalid
	}
	activityLimit, ok := normalizeLimit(query.ActivityLimit)
	if !ok {
		return GeneralHome{}, domain.ErrInvalid
	}

	live := s.presence.List("")
	sort.Slice(live, func(i, j int) bool {
		if live[i].LastActivity.Equal(live[j].LastActivity) {
			return live[i].Session < live[j].Session
		}
		return live[i].LastActivity.After(live[j].LastActivity)
	})
	sessionCount := len(live)
	if len(live) > presenceLimit {
		live = live[:presenceLimit]
	}
	sessionIDs := make([]string, len(live))
	for i := range live {
		sessionIDs[i] = live[i].Session
	}

	durable, err := s.repository.HomeSnapshot(ctx, domain.HomeReadQuery{
		Attention:  domain.HomePageRequest{Offset: query.AttentionPage * attentionLimit, Limit: attentionLimit},
		Activity:   domain.HomePageRequest{Offset: query.ActivityPage * activityLimit, Limit: activityLimit},
		SessionIDs: sessionIDs,
	})
	if err != nil {
		return GeneralHome{}, err
	}
	now := s.clock.Now().UTC()
	out := GeneralHome{
		Navigation: Navigation{Projects: durable.ProjectsTotal, People: sessionCount},
		Presence:   PresencePage{Total: sessionCount, Items: make([]PresenceItem, 0, len(live))},
		NeedsAttention: AttentionPage{
			Total: durable.AttentionTotal, Page: query.AttentionPage, Limit: attentionLimit,
			Items:   make([]AttentionItem, 0, len(durable.Attention)),
			HasMore: (query.AttentionPage+1)*attentionLimit < durable.AttentionTotal,
		},
		RecentActivity: ActivityPage{
			Total: durable.ActivityTotal, Page: query.ActivityPage, Limit: activityLimit,
			Items:   make([]ActivityItem, 0, len(durable.Activity)),
			HasMore: (query.ActivityPage+1)*activityLimit < durable.ActivityTotal,
		},
	}
	for _, liveItem := range live {
		fact := durable.Sessions[liveItem.Session]
		project := liveItem.Project
		if project == "" {
			project = fact.ProjectID
		}
		host := liveItem.Host
		if host == "" {
			host = fact.Host
		}
		recency := now.Sub(liveItem.LastActivity).Seconds()
		if recency < 0 {
			recency = 0
		}
		out.Presence.Items = append(out.Presence.Items, PresenceItem{
			Session: liveItem.Session, Actor: liveItem.Actor, Host: host,
			Project: project, ProjectName: fact.ProjectName, Purpose: fact.Purpose,
			HostConnected: liveItem.HostConnected, Execution: liveItem.Execution,
			LeaseExpires: liveItem.LeaseExpires, LastActivity: liveItem.LastActivity,
			RecencySeconds: int64(recency), Loaded: liveItem.LoadedFact,
		})
	}
	for _, item := range durable.Attention {
		out.NeedsAttention.Items = append(out.NeedsAttention.Items, AttentionItem{
			ID: item.ID, Severity: item.Severity, Title: item.Title,
			Project: item.ProjectID, ProjectName: item.ProjectName,
			SourceRef: item.SourceRef, Owner: item.AccountableSessionID,
			NextAction: item.NextAction, SourceKind: item.SourceKind,
			UpdatedAt: item.UpdatedAt, Untrusted: item.Untrusted,
		})
	}
	for _, item := range durable.Activity {
		out.RecentActivity.Items = append(out.RecentActivity.Items, ActivityItem{
			ID: item.ID, Kind: item.Kind, Project: item.ProjectID,
			ProjectName: item.ProjectName, Actor: item.ActorID,
			ObjectRef: item.ObjectRef, ObjectTitle: item.ObjectTitle,
			Outcome: item.Outcome, Timestamp: item.OccurredAt, Untrusted: item.Untrusted,
		})
	}
	return out, nil
}
