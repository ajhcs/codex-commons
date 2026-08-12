package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"codex-commons/internal/domain"
)

type ArchaeologyRepository interface {
	ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error)
	ReplaceArchaeologyDiscovery(context.Context, domain.ArchaeologyMutation, domain.ArchaeologyDiscovery) (domain.ArchaeologySession, error)
	ConfigureArchaeology(context.Context, domain.ArchaeologyMutation) (domain.ArchaeologySession, error)
	StartArchaeology(context.Context, domain.ArchaeologyMutation) (domain.ArchaeologySession, error)
	PauseArchaeology(context.Context, domain.ArchaeologyMutation) (domain.ArchaeologySession, error)
	ResumeArchaeology(context.Context, domain.ArchaeologyMutation) (domain.ArchaeologySession, error)
	CancelArchaeology(context.Context, domain.ArchaeologyMutation) (domain.ArchaeologySession, error)
	PrepareArchaeologyTaskLaunch(context.Context, string, string, string, string, [32]byte, time.Time) (domain.ArchaeologyTaskLaunch, bool, error)
	CompleteArchaeologyTaskLaunch(context.Context, domain.ArchaeologyLaunchResult) (domain.ArchaeologyTaskLaunch, error)
	ClaimArchaeologyTaskLaunch(context.Context, domain.ArchaeologyTaskClaim) (domain.ArchaeologyTaskClaimResult, error)
	ReportArchaeologyTaskLaunch(context.Context, domain.ArchaeologyTaskReport) (domain.ArchaeologySession, error)
	ClaimArchaeologyHandoff(context.Context, domain.ArchaeologyHandoffClaim) (domain.ArchaeologySession, error)
	ReportArchaeologyHandoff(context.Context, domain.ArchaeologyHandoffReport) (domain.ArchaeologySession, error)
}

type ArchaeologyDiscoverer interface {
	DiscoverMetadata(context.Context) (domain.ArchaeologyDiscovery, error)
}
type ArchaeologyHistorianLauncher interface {
	Available(context.Context) error
	Launch(context.Context, domain.ArchaeologySession) error
}
type ArchaeologyTaskLauncher interface {
	LaunchProject(context.Context, domain.ArchaeologySession, domain.ArchaeologyCandidate, string, string) (domain.ArchaeologyLaunchResult, error)
}

type ArchaeologySources struct {
	Git          bool `json:"git"`
	Docs         bool `json:"docs"`
	CodexHistory bool `json:"codex_history"`
}
type ArchaeologyConfig struct {
	SelectedProjectIDs []string           `json:"selected_project_ids"`
	Depth              string             `json:"depth"`
	Sources            ArchaeologySources `json:"sources"`
	MaxConcurrency     int                `json:"max_concurrency"`
}
type ArchaeologyEstimate struct {
	DurationSecondsMin int    `json:"duration_seconds_min"`
	DurationSecondsMax int    `json:"duration_seconds_max"`
	RelativeCost       string `json:"relative_cost"`
}
type ArchaeologySignals struct {
	Git          bool `json:"git"`
	Docs         bool `json:"docs"`
	CodexHistory bool `json:"codex_history"`
}
type ArchaeologyCandidate struct {
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	PathLabel         string              `json:"path_label"`
	RepositoryLabel   string              `json:"repository_label,omitempty"`
	LastActivityAt    *time.Time          `json:"last_activity_at,omitempty"`
	Signals           ArchaeologySignals  `json:"signals"`
	Estimate          ArchaeologyEstimate `json:"estimate"`
	PrivacyNote       string              `json:"privacy_note"`
	SelectedByDefault bool                `json:"selected_by_default"`
	Sources           []string            `json:"sources"`
	CodexThreadCount  int                 `json:"codex_thread_count"`
}
type ArchaeologyDiscovery struct {
	State              string                 `json:"state"`
	Candidates         []ArchaeologyCandidate `json:"candidates"`
	DiscoveredAt       *time.Time             `json:"discovered_at,omitempty"`
	SourceRootsScanned int                    `json:"source_roots_scanned"`
	MetadataOnly       bool                   `json:"metadata_only"`
	Error              string                 `json:"error,omitempty"`
}
type ArchaeologyRun struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	State           string     `json:"state"`
	PhaseLabel      string     `json:"phase_label"`
	CompletedUnits  int        `json:"completed_units"`
	TotalUnits      *int       `json:"total_units"`
	OutcomesFound   int        `json:"outcomes_found"`
	SourcesExamined int        `json:"sources_examined"`
	Error           string     `json:"error,omitempty"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}
type ArchaeologyProvenance struct {
	SourceKind  string     `json:"source_kind"`
	SourceLabel string     `json:"source_label"`
	Digest      string     `json:"digest"`
	RecordedAt  *time.Time `json:"recorded_at,omitempty"`
}
type ArchaeologyMemberSession struct {
	SessionID             string   `json:"session_id"`
	DisplayName           string   `json:"display_name,omitempty"`
	Reachability          string   `json:"reachability"`
	Execution             string   `json:"execution"`
	Authority             string   `json:"authority"`
	ContributionCount     int      `json:"contribution_count"`
	SourceCount           int      `json:"source_count"`
	CollaborationCount    int      `json:"collaboration_count"`
	DemonstratedStrengths []string `json:"demonstrated_strengths"`
	Uncertainties         []string `json:"uncertainties"`
}
type ArchaeologyOutcome struct {
	ID             string                     `json:"id"`
	Title          string                     `json:"title"`
	Summary        string                     `json:"summary"`
	ProjectID      string                     `json:"project_id"`
	SourceCount    int                        `json:"source_count"`
	Provenance     []ArchaeologyProvenance    `json:"provenance"`
	MemberSessions []ArchaeologyMemberSession `json:"member_sessions"`
}
type ArchaeologyReview struct {
	ProposedOutcomes         []ArchaeologyOutcome       `json:"proposed_outcomes"`
	MemberSessions           []ArchaeologyMemberSession `json:"member_sessions"`
	ProvenanceSummary        string                     `json:"provenance_summary"`
	CanApply                 bool                       `json:"can_apply"`
	RequiresExplicitApproval bool                       `json:"requires_explicit_approval"`
}
type ArchaeologyControls struct {
	CanStart  bool `json:"can_start"`
	CanPause  bool `json:"can_pause"`
	CanResume bool `json:"can_resume"`
	CanCancel bool `json:"can_cancel"`
}
type ArchaeologyCapability struct {
	Configured bool   `json:"configured"`
	Available  bool   `json:"available"`
	Mode       string `json:"mode"`
	Reason     string `json:"reason,omitempty"`
}
type ArchaeologyCapabilities struct {
	ProjectCatalog   ArchaeologyCapability `json:"project_catalog"`
	TaskLaunch       ArchaeologyCapability `json:"task_launch"`
	Discovery        ArchaeologyCapability `json:"discovery"`
	HistorianHandoff ArchaeologyCapability `json:"historian_handoff"`
	Review           ArchaeologyCapability `json:"review"`
	CanonicalApply   ArchaeologyCapability `json:"canonical_apply"`
}
type ArchaeologyHandoff struct {
	ID             string                  `json:"id"`
	State          string                  `json:"state"`
	ClaimedBy      string                  `json:"claimed_by,omitempty"`
	Failure        string                  `json:"failure,omitempty"`
	CreatedAt      *time.Time              `json:"created_at,omitempty"`
	UpdatedAt      *time.Time              `json:"updated_at,omitempty"`
	ClaimedAt      *time.Time              `json:"claimed_at,omitempty"`
	Depth          string                  `json:"depth"`
	Sources        ArchaeologySources      `json:"sources"`
	Concurrency    int                     `json:"concurrency"`
	CandidateIDs   []string                `json:"candidate_ids"`
	Tasks          []ArchaeologyTaskLaunch `json:"tasks"`
	AllowedActions []string                `json:"allowed_actions"`
}
type ArchaeologyTaskLaunch struct {
	ProjectID        string     `json:"project_id"`
	State            string     `json:"state"`
	ThreadID         string     `json:"thread_id,omitempty"`
	TurnID           string     `json:"turn_id,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	Error            string     `json:"error,omitempty"`
	AvailableActions []string   `json:"available_actions"`
}
type ArchaeologySession struct {
	ID           string                  `json:"id"`
	State        string                  `json:"state"`
	Discovery    ArchaeologyDiscovery    `json:"discovery"`
	Config       ArchaeologyConfig       `json:"config"`
	Runs         []ArchaeologyRun        `json:"runs"`
	Review       *ArchaeologyReview      `json:"review,omitempty"`
	Controls     ArchaeologyControls     `json:"controls"`
	Revision     int64                   `json:"revision"`
	UpdatedAt    *time.Time              `json:"updated_at,omitempty"`
	Capabilities ArchaeologyCapabilities `json:"capabilities"`
	Handoff      *ArchaeologyHandoff     `json:"handoff,omitempty"`
}
type ArchaeologyConfigRequest struct {
	SelectedProjectIDs []string           `json:"selected_project_ids"`
	Depth              string             `json:"depth"`
	Sources            ArchaeologySources `json:"sources"`
	MaxConcurrency     int                `json:"max_concurrency"`
	BaseRevision       int64              `json:"base_revision"`
}
type ArchaeologyTransitionRequest struct {
	BaseRevision int64 `json:"base_revision"`
}
type ArchaeologyHandoffReportRequest struct {
	Outcomes []ArchaeologyOutcomeReportRequest `json:"outcomes"`
}
type ArchaeologyOutcomeReportRequest struct {
	ID               string                                `json:"id,omitempty"`
	Title            string                                `json:"title"`
	Summary          string                                `json:"summary"`
	ProjectID        string                                `json:"project_id"`
	SourceCount      int                                   `json:"source_count"`
	Provenance       []ArchaeologyProvenanceReportRequest  `json:"provenance"`
	Contributors     []ArchaeologyContributorReportRequest `json:"contributors"`
	HistoricalImport HistoricalImportRequest               `json:"historical_import"`
}
type ArchaeologyProvenanceReportRequest struct {
	SourceKind  string    `json:"source_kind"`
	SourceLabel string    `json:"source_label"`
	Digest      string    `json:"digest"`
	RecordedAt  time.Time `json:"recorded_at"`
}
type ArchaeologyContributorReportRequest struct {
	SessionID            string `json:"session_id"`
	Contribution         string `json:"contribution"`
	DemonstratedStrength string `json:"demonstrated_strength,omitempty"`
	Uncertainty          string `json:"uncertainty,omitempty"`
	Confidence           string `json:"confidence"`
}
type ArchaeologyImportPreviewRequest struct {
	OutcomeID string `json:"outcome_id"`
}
type ArchaeologyHandoffClaimRequest struct {
	HandoffID string `json:"handoff_id"`
}
type ArchaeologyHandoffReportEnvelope struct {
	HandoffID string                            `json:"handoff_id"`
	Outcomes  []ArchaeologyOutcomeReportRequest `json:"outcomes"`
}
type ArchaeologyTaskClaimRequest struct {
	LaunchID       string `json:"launch_id"`
	ProjectID      string `json:"project_id"`
	ThreadID       string `json:"thread_id"`
	CodexSessionID string `json:"session_id"`
	Grant          string `json:"grant"`
}
type ArchaeologyTaskClaimResponse struct {
	LaunchID        string    `json:"launch_id"`
	ProjectID       string    `json:"project_id"`
	ThreadID        string    `json:"thread_id"`
	CodexSessionID  string    `json:"session_id"`
	ReportToken     string    `json:"report_token"`
	ReportExpiresAt time.Time `json:"report_expires_at"`
}
type ArchaeologyTaskReportEnvelope struct {
	LaunchID       string                            `json:"launch_id"`
	ProjectID      string                            `json:"project_id"`
	ThreadID       string                            `json:"thread_id"`
	CodexSessionID string                            `json:"session_id"`
	ReportToken    string                            `json:"report_token"`
	Outcomes       []ArchaeologyOutcomeReportRequest `json:"outcomes"`
}
type ArchaeologyImportPreview struct {
	ProjectID string                  `json:"project_id"`
	Request   HistoricalImportRequest `json:"request"`
	Preview   HistoricalImportResult  `json:"preview"`
}

func (s *Service) ConfigureProjectArchaeology(discoverer ArchaeologyDiscoverer, launcher ArchaeologyHistorianLauncher) {
	s.archaeologyDiscoverer = discoverer
	s.archaeologyLauncher = launcher
}
func (s *Service) archaeologyRepository() (ArchaeologyRepository, error) {
	if s == nil {
		return nil, domain.ErrUnavailable
	}
	repository, ok := s.repository.(ArchaeologyRepository)
	if !ok {
		return nil, domain.ErrUnavailable
	}
	return repository, nil
}
func publicLaunchError(state string) string {
	if state == "failed" {
		return "Codex could not start this task. Try again when the connection is available."
	}
	if state == "uncertain" {
		return "Codex may have created this task, but Commons could not confirm it. Review the task ID before taking action."
	}
	return ""
}

func optionalArchaeologyTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func archaeologyView(value domain.ArchaeologySession) ArchaeologySession {
	selectedProjectIDs := make([]string, len(value.Config.SelectedProjectIDs))
	copy(selectedProjectIDs, value.Config.SelectedProjectIDs)
	out := ArchaeologySession{ID: value.ID, State: value.State, Discovery: ArchaeologyDiscovery{State: value.DiscoveryState, MetadataOnly: true, SourceRootsScanned: value.SourceRootsScanned, DiscoveredAt: optionalArchaeologyTime(value.DiscoveredAt), Error: value.DiscoveryError, Candidates: []ArchaeologyCandidate{}}, Config: ArchaeologyConfig{SelectedProjectIDs: selectedProjectIDs, Depth: value.Config.Depth, Sources: ArchaeologySources{Git: value.Config.Sources.Git, Docs: value.Config.Sources.Docs, CodexHistory: value.Config.Sources.CodexHistory}, MaxConcurrency: value.Config.MaxConcurrency}, Runs: []ArchaeologyRun{}, Revision: value.Revision, UpdatedAt: optionalArchaeologyTime(value.UpdatedAt)}
	for _, candidate := range value.Candidates {
		sources := []string{}
		if candidate.FromCodexMetadata {
			sources = append(sources, "codex_metadata")
		}
		if candidate.FromConfiguredRoot {
			sources = append(sources, "configured_root")
		}
		out.Discovery.Candidates = append(out.Discovery.Candidates, ArchaeologyCandidate{ID: candidate.ID, Name: candidate.Name, PathLabel: candidate.PathLabel, RepositoryLabel: candidate.RepositoryLabel, LastActivityAt: optionalArchaeologyTime(candidate.LastActivityAt), Signals: ArchaeologySignals{Git: candidate.HasGit, Docs: candidate.HasDocs, CodexHistory: candidate.HasCodexHistory}, Estimate: ArchaeologyEstimate{DurationSecondsMin: candidate.DurationMinSeconds, DurationSecondsMax: candidate.DurationMaxSeconds, RelativeCost: candidate.RelativeCost}, PrivacyNote: candidate.PrivacyNote, SelectedByDefault: false, Sources: sources, CodexThreadCount: candidate.CodexThreadCount})
	}
	for _, run := range value.Runs {
		out.Runs = append(out.Runs, ArchaeologyRun{ID: run.ID, ProjectID: run.ProjectID, State: run.State, PhaseLabel: run.PhaseLabel, CompletedUnits: run.CompletedUnits, TotalUnits: run.TotalUnits, OutcomesFound: run.OutcomesFound, SourcesExamined: run.SourcesExamined, Error: run.Error, UpdatedAt: optionalArchaeologyTime(run.UpdatedAt)})
	}
	out.Controls = ArchaeologyControls{CanStart: value.State == "draft" && len(value.Config.SelectedProjectIDs) > 0, CanPause: value.State == "running", CanResume: value.State == "paused" || value.State == "pause_requested", CanCancel: value.State == "running" || value.State == "paused" || value.State == "pause_requested"}
	if value.Handoff != nil {
		actions := []string{}
		if value.Handoff.State == "ready_to_claim" {
			actions = append(actions, "claim")
		}
		if value.Handoff.State == "claimed" {
			actions = append(actions, "report")
		}
		launchTasks := make([]ArchaeologyTaskLaunch, 0, len(value.TaskLaunches))
		for _, launch := range value.TaskLaunches {
			actions := []string{}
			if launch.State == "failed" {
				actions = append(actions, "retry")
			}
			launchTasks = append(launchTasks, ArchaeologyTaskLaunch{ProjectID: launch.ProjectID, State: launch.State, ThreadID: launch.ThreadID, TurnID: launch.TurnID, CreatedAt: optionalArchaeologyTime(launch.CreatedAt), UpdatedAt: optionalArchaeologyTime(launch.UpdatedAt), Error: publicLaunchError(launch.State), AvailableActions: actions})
		}
		if len(launchTasks) > 0 && out.Handoff == nil {
			ids := make([]string, 0, len(launchTasks))
			for _, task := range launchTasks {
				ids = append(ids, task.ProjectID)
			}
			out.Handoff = &ArchaeologyHandoff{State: "launching", Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: launchTasks, AllowedActions: []string{}}
		}
		ids := append([]string(nil), out.Config.SelectedProjectIDs...)
		out.Handoff = &ArchaeologyHandoff{ID: value.Handoff.ID, State: value.Handoff.State, ClaimedBy: value.Handoff.ClaimedBy, Failure: value.Handoff.Failure, CreatedAt: optionalArchaeologyTime(value.Handoff.CreatedAt), UpdatedAt: optionalArchaeologyTime(value.Handoff.UpdatedAt), ClaimedAt: optionalArchaeologyTime(value.Handoff.ClaimedAt), Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: launchTasks, AllowedActions: actions}
		out.Controls.CanStart = false
		out.Controls.CanPause, out.Controls.CanResume, out.Controls.CanCancel = false, false, false
	}
	if len(value.Outcomes) > 0 || value.State == "completed" {
		review := ArchaeologyReview{ProposedOutcomes: []ArchaeologyOutcome{}, MemberSessions: []ArchaeologyMemberSession{}, RequiresExplicitApproval: true}
		members := map[string]*ArchaeologyMemberSession{}
		for _, item := range value.Outcomes {
			outcome := ArchaeologyOutcome{ID: item.ID, Title: item.Title, Summary: item.Summary, ProjectID: item.ProjectID, SourceCount: item.SourceCount, Provenance: []ArchaeologyProvenance{}, MemberSessions: []ArchaeologyMemberSession{}}
			for _, source := range item.Provenance {
				outcome.Provenance = append(outcome.Provenance, ArchaeologyProvenance{SourceKind: source.Kind, SourceLabel: source.StableID, Digest: source.Digest, RecordedAt: optionalArchaeologyTime(source.OccurredAt)})
			}
			for _, member := range item.Contributors {
				summary := ArchaeologyMemberSession{SessionID: member.SessionID, Reachability: "historical_or_unknown", Execution: "not_attested", Authority: "provenance_only", ContributionCount: 1, SourceCount: item.SourceCount, DemonstratedStrengths: []string{}, Uncertainties: []string{}}
				if member.DemonstratedStrength != "" {
					summary.DemonstratedStrengths = append(summary.DemonstratedStrengths, member.DemonstratedStrength)
				}
				if member.Uncertainty != "" {
					summary.Uncertainties = append(summary.Uncertainties, member.Uncertainty)
				}
				outcome.MemberSessions = append(outcome.MemberSessions, summary)
				aggregate := members[member.SessionID]
				if aggregate == nil {
					copy := summary
					members[member.SessionID] = &copy
				} else {
					aggregate.ContributionCount++
					aggregate.SourceCount += item.SourceCount
					aggregate.DemonstratedStrengths = appendUnique(aggregate.DemonstratedStrengths, member.DemonstratedStrength)
					aggregate.Uncertainties = appendUnique(aggregate.Uncertainties, member.Uncertainty)
				}
			}
			review.ProposedOutcomes = append(review.ProposedOutcomes, outcome)
		}
		for _, member := range members {
			review.MemberSessions = append(review.MemberSessions, *member)
		}
		sort.Slice(review.MemberSessions, func(i, j int) bool { return review.MemberSessions[i].SessionID < review.MemberSessions[j].SessionID })
		review.ProvenanceSummary = "Exact digests retained; proposed outcomes require canonical preview and explicit human approval."
		review.CanApply = false
		for _, item := range value.Outcomes {
			var request HistoricalImportRequest
			if json.Unmarshal([]byte(item.ProposalJSON), &request) == nil && request.SchemaVersion == domain.HistoricalImportSchemaVersion && request.CollisionPolicy == domain.HistoricalCollisionCurrentWins && len(request.Tasks) > 0 {
				review.CanApply = true
				break
			}
		}
		out.Review = &review
	}
	out.Capabilities = ArchaeologyCapabilities{
		ProjectCatalog:   ArchaeologyCapability{Configured: false, Available: false, Mode: "codex_metadata", Reason: "Connect a Codex App Server to list known projects."},
		TaskLaunch:       ArchaeologyCapability{Configured: false, Available: false, Mode: "app_server_stdio", Reason: "Connect a compatible Codex App Server to start historian tasks."},
		Discovery:        ArchaeologyCapability{Configured: false, Available: false, Mode: "allowlisted_metadata", Reason: "Configure an explicit project-root allowlist to enable metadata discovery."},
		HistorianHandoff: ArchaeologyCapability{Configured: true, Available: true, Mode: "exact_task_claim_report", Reason: "Historian reports remain bound to exact launched task identity and human review."},
		Review:           ArchaeologyCapability{Configured: true, Available: out.Review != nil, Mode: "durable_manifest"},
		CanonicalApply:   ArchaeologyCapability{Configured: true, Available: out.Review != nil && out.Review.CanApply, Mode: "preview_digest_confirm", Reason: "A signed-in human must preview and explicitly confirm the exact source digest."},
	}
	return out
}
func (s *Service) archaeologySessionView(value domain.ArchaeologySession) ArchaeologySession {
	out := archaeologyView(value)
	if s != nil {
		_, native := s.archaeologyLauncher.(ArchaeologyTaskLauncher)
		if native && value.Handoff != nil &&
			value.Handoff.State == "ready_to_claim" && value.Handoff.ClaimedBy == "" && value.Handoff.ClaimedAt.IsZero() {
			// An untouched pre-native handoff remains durable audit history, but is
			// not a second control plane once direct App Server launch is configured.
			nativeTasks := []ArchaeologyTaskLaunch{}
			if out.Handoff != nil {
				nativeTasks = out.Handoff.Tasks
			}
			out.Handoff = nil
			out.Controls.CanStart = len(nativeTasks) == 0 && value.State == "draft" && len(value.Config.SelectedProjectIDs) > 0
			out.Controls.CanPause, out.Controls.CanResume, out.Controls.CanCancel = false, false, false
			if len(nativeTasks) > 0 {
				// Present only the exact direct-task ledger. The compatibility row's ID,
				// ready-to-claim state, and broad claim action stay server-side.
				ids := append([]string(nil), out.Config.SelectedProjectIDs...)
				out.Handoff = &ArchaeologyHandoff{State: "launching", Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: nativeTasks, AllowedActions: []string{}}
			}
		}
	}
	if s != nil && s.archaeologyDiscoverer != nil {
		out.Capabilities.Discovery = ArchaeologyCapability{Configured: true, Available: true, Mode: "codex_known_metadata", Reason: "Catalog uses bounded Codex thread metadata plus configured roots; message bodies are not read."}
		out.Capabilities.ProjectCatalog = ArchaeologyCapability{Configured: true, Available: true, Mode: "codex_metadata", Reason: "Projects are grouped from Codex-known workspaces and additive configured roots."}
	}
	if s != nil && s.archaeologyLauncher != nil {
		capabilityCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.archaeologyLauncher.Available(capabilityCtx)
		cancel()
		if err == nil {
			out.Capabilities.TaskLaunch = ArchaeologyCapability{Configured: true, Available: true, Mode: "app_server_stdio", Reason: "Commons can start one ordinary Codex historian task for each selected project."}
		} else {
			out.Capabilities.TaskLaunch = ArchaeologyCapability{Configured: true, Available: false, Mode: "app_server_stdio", Reason: "The required gpt-5.6-luna max task profile is not currently available."}
		}
	}
	return out
}
func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
func archaeologyMutation(principal, requestID string, base int64) domain.ArchaeologyMutation {
	return domain.ArchaeologyMutation{Principal: principal, RequestID: requestID, BaseRevision: base}
}
func (s *Service) ProjectArchaeology(ctx context.Context, principal string) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ArchaeologySession(ctx, principal)
	if errors.Is(err, domain.ErrNotFound) {
		return s.archaeologySessionView(domain.ArchaeologySession{
			State: "draft", DiscoveryState: "idle", MetadataOnly: true, Revision: 0,
			Config: domain.ArchaeologyConfig{Depth: "standard", Sources: domain.ArchaeologySources{Git: true, Docs: true}, MaxConcurrency: 2},
		}), nil
	}
	return s.archaeologySessionView(value), err
}
func (s *Service) DiscoverProjectArchaeology(ctx context.Context, principal, requestID string) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	if s.archaeologyDiscoverer == nil {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	discovery, err := s.archaeologyDiscoverer.DiscoverMetadata(ctx)
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, archaeologyMutation(principal, requestID, 0), discovery)
	return s.archaeologySessionView(value), err
}
func (s *Service) ConfigureArchaeologySession(ctx context.Context, principal, requestID string, input ArchaeologyConfigRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	mutation := archaeologyMutation(principal, requestID, input.BaseRevision)
	mutation.Config = domain.ArchaeologyConfig{SelectedProjectIDs: input.SelectedProjectIDs, Depth: input.Depth, Sources: domain.ArchaeologySources{Git: input.Sources.Git, Docs: input.Sources.Docs, CodexHistory: input.Sources.CodexHistory}, MaxConcurrency: input.MaxConcurrency}
	value, err := repository.ConfigureArchaeology(ctx, mutation)
	return s.archaeologySessionView(value), err
}
func (s *Service) StartProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.StartArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	if err != nil {
		return ArchaeologySession{}, err
	}
	launcher, supported := s.archaeologyLauncher.(ArchaeologyTaskLauncher)
	if !supported || s.archaeologyLauncher.Available(ctx) != nil {
		return s.archaeologySessionView(value), nil
	}
	selected := map[string]bool{}
	for _, id := range value.Config.SelectedProjectIDs {
		selected[id] = true
	}
	var candidates []domain.ArchaeologyCandidate
	for _, candidate := range value.Candidates {
		if selected[candidate.ID] {
			candidates = append(candidates, candidate)
		}
	}
	workers := value.Config.MaxConcurrency
	if workers < 1 {
		workers = 1
	}
	if workers > 2 {
		workers = 2
	}
	jobs := make(chan domain.ArchaeologyCandidate)
	errs := make(chan error, len(candidates))
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for candidate := range jobs {
				if err := s.launchArchaeologyCandidate(ctx, repository, launcher, principal, requestID, value, candidate); err != nil {
					errs <- err
				}
			}
		}()
	}
	for _, candidate := range candidates {
		jobs <- candidate
	}
	close(jobs)
	group.Wait()
	close(errs)
	for launchErr := range errs {
		if launchErr != nil {
			return ArchaeologySession{}, launchErr
		}
	}
	value, err = repository.ArchaeologySession(ctx, principal)
	return s.archaeologySessionView(value), err
}

func (s *Service) launchArchaeologyCandidate(ctx context.Context, repository ArchaeologyRepository, launcher ArchaeologyTaskLauncher, principal, requestID string, session domain.ArchaeologySession, candidate domain.ArchaeologyCandidate) error {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return domain.ErrUnavailable
	}
	grant := base64.RawURLEncoding.EncodeToString(raw[:])
	clientMessageID := "commons-archaeology-" + session.ID + "-" + candidate.ID
	prepared, reserved, err := repository.PrepareArchaeologyTaskLaunch(ctx, principal, candidate.ID, requestID, clientMessageID, sha256.Sum256([]byte(grant)), s.clock.Now().UTC().Add(30*time.Minute))
	if err != nil {
		return err
	}
	// Only the caller that inserted the durable reservation may cross the
	// non-idempotent App Server boundary. Replays return the existing task.
	if !reserved {
		return nil
	}
	result, launchErr := launcher.LaunchProject(ctx, session, candidate, grant, prepared.ID)
	result.LaunchID, result.ProjectID = prepared.ID, candidate.ID
	if launchErr != nil {
		if result.ThreadID != "" {
			result.State = "uncertain"
		} else {
			result.State = "failed"
		}
		result.Error = "Codex task launch did not complete; review this task before retrying."
	} else {
		result.State = "task_created"
	}
	_, err = repository.CompleteArchaeologyTaskLaunch(ctx, result)
	return err
}
func (s *Service) PauseProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.PauseArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	return s.archaeologySessionView(value), err
}
func (s *Service) ResumeProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ResumeArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	return s.archaeologySessionView(value), err
}
func (s *Service) CancelProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.CancelArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	return s.archaeologySessionView(value), err
}

func (s *Service) ClaimProjectArchaeology(ctx context.Context, handoffID, requestID, sessionID string) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ClaimArchaeologyHandoff(ctx, domain.ArchaeologyHandoffClaim{HandoffID: handoffID, RequestID: requestID, SessionID: sessionID})
	return s.archaeologySessionView(value), err
}

func (s *Service) ReportProjectArchaeology(ctx context.Context, handoffID, requestID, sessionID string, input ArchaeologyHandoffReportRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	outcomes := make([]domain.ArchaeologyOutcome, 0, len(input.Outcomes))
	for _, item := range input.Outcomes {
		if item.HistoricalImport.ConfirmSourceDigest != "" {
			return ArchaeologySession{}, domain.ErrInvalid
		}
		if _, previewErr := s.PreviewHistoricalTaskImport(ctx, item.ProjectID, item.HistoricalImport, ProjectCoreActor{}); previewErr != nil {
			return ArchaeologySession{}, previewErr
		}
		proposal, marshalErr := json.Marshal(item.HistoricalImport)
		if marshalErr != nil {
			return ArchaeologySession{}, domain.ErrInvalid
		}
		outcome := domain.ArchaeologyOutcome{ID: item.ID, Title: item.Title, Summary: item.Summary, ProjectID: item.ProjectID, SourceCount: item.SourceCount, ProposalJSON: string(proposal)}
		for _, source := range item.Provenance {
			outcome.Provenance = append(outcome.Provenance, domain.ArchaeologyProvenance{Kind: source.SourceKind, StableID: source.SourceLabel, Digest: source.Digest, OccurredAt: source.RecordedAt})
		}
		for _, member := range item.Contributors {
			outcome.Contributors = append(outcome.Contributors, domain.ArchaeologyContributor{SessionID: member.SessionID, Contribution: member.Contribution, DemonstratedStrength: member.DemonstratedStrength, Uncertainty: member.Uncertainty, Confidence: member.Confidence})
		}
		outcomes = append(outcomes, outcome)
	}
	value, err := repository.ReportArchaeologyHandoff(ctx, domain.ArchaeologyHandoffReport{HandoffID: handoffID, RequestID: requestID, SessionID: sessionID, Outcomes: outcomes})
	return s.archaeologySessionView(value), err
}

func (s *Service) ClaimProjectArchaeologyTask(ctx context.Context, input ArchaeologyTaskClaimRequest) (ArchaeologyTaskClaimResponse, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologyTaskClaimResponse{}, err
	}
	if len(input.Grant) < 32 || len(input.Grant) > 100 || strings.TrimSpace(input.Grant) != input.Grant {
		return ArchaeologyTaskClaimResponse{}, domain.ErrInvalid
	}
	var raw [32]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return ArchaeologyTaskClaimResponse{}, domain.ErrUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	expires := s.clock.Now().UTC().Add(30 * time.Minute)
	claim, err := repository.ClaimArchaeologyTaskLaunch(ctx, domain.ArchaeologyTaskClaim{
		LaunchID: input.LaunchID, ProjectID: input.ProjectID, ThreadID: input.ThreadID, CodexSessionID: input.CodexSessionID,
		GrantDigest: sha256.Sum256([]byte(input.Grant)), ReportDigest: sha256.Sum256([]byte(token)), ReportExpiresAt: expires,
	})
	if err != nil {
		return ArchaeologyTaskClaimResponse{}, err
	}
	return ArchaeologyTaskClaimResponse{LaunchID: claim.LaunchID, ProjectID: claim.ProjectID, ThreadID: claim.ThreadID, CodexSessionID: claim.CodexSessionID, ReportToken: token, ReportExpiresAt: claim.ReportExpiresAt}, nil
}

func (s *Service) ReportProjectArchaeologyTask(ctx context.Context, requestID string, input ArchaeologyTaskReportEnvelope) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	if len(input.ReportToken) < 32 || len(input.ReportToken) > 100 || strings.TrimSpace(input.ReportToken) != input.ReportToken {
		return ArchaeologySession{}, domain.ErrInvalid
	}
	outcomes, err := s.archaeologyReportOutcomes(ctx, input.Outcomes)
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ReportArchaeologyTaskLaunch(ctx, domain.ArchaeologyTaskReport{
		LaunchID: input.LaunchID, ProjectID: input.ProjectID, ThreadID: input.ThreadID, CodexSessionID: input.CodexSessionID,
		RequestID: requestID, ReportDigest: sha256.Sum256([]byte(input.ReportToken)), Outcomes: outcomes,
	})
	return s.archaeologySessionView(value), err
}

func (s *Service) archaeologyReportOutcomes(ctx context.Context, input []ArchaeologyOutcomeReportRequest) ([]domain.ArchaeologyOutcome, error) {
	outcomes := make([]domain.ArchaeologyOutcome, 0, len(input))
	for _, item := range input {
		if item.HistoricalImport.ConfirmSourceDigest != "" {
			return nil, domain.ErrInvalid
		}
		if _, err := s.PreviewHistoricalTaskImport(ctx, item.ProjectID, item.HistoricalImport, ProjectCoreActor{}); err != nil {
			return nil, err
		}
		proposal, err := json.Marshal(item.HistoricalImport)
		if err != nil {
			return nil, domain.ErrInvalid
		}
		outcome := domain.ArchaeologyOutcome{ID: item.ID, Title: item.Title, Summary: item.Summary, ProjectID: item.ProjectID, SourceCount: item.SourceCount, ProposalJSON: string(proposal)}
		for _, source := range item.Provenance {
			outcome.Provenance = append(outcome.Provenance, domain.ArchaeologyProvenance{Kind: source.SourceKind, StableID: source.SourceLabel, Digest: source.Digest, OccurredAt: source.RecordedAt})
		}
		for _, member := range item.Contributors {
			outcome.Contributors = append(outcome.Contributors, domain.ArchaeologyContributor{SessionID: member.SessionID, Contribution: member.Contribution, DemonstratedStrength: member.DemonstratedStrength, Uncertainty: member.Uncertainty, Confidence: member.Confidence})
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func (s *Service) PreviewArchaeologyImport(ctx context.Context, principal string, input ArchaeologyImportPreviewRequest, actor ProjectCoreActor) (ArchaeologyImportPreview, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologyImportPreview{}, err
	}
	session, err := repository.ArchaeologySession(ctx, principal)
	if err != nil {
		return ArchaeologyImportPreview{}, err
	}
	for _, outcome := range session.Outcomes {
		if outcome.ID != input.OutcomeID {
			continue
		}
		var request HistoricalImportRequest
		if json.Unmarshal([]byte(outcome.ProposalJSON), &request) != nil {
			return ArchaeologyImportPreview{}, domain.ErrInvalid
		}
		request.ConfirmSourceDigest = ""
		preview, previewErr := s.PreviewHistoricalTaskImport(ctx, outcome.ProjectID, request, actor)
		return ArchaeologyImportPreview{ProjectID: outcome.ProjectID, Request: request, Preview: preview}, previewErr
	}
	return ArchaeologyImportPreview{}, domain.ErrNotFound
}
