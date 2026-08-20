package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
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
type ArchaeologyCatalogRepository interface {
	ArchaeologyCatalog(context.Context, string, domain.ArchaeologyCatalogQuery) (domain.ArchaeologyCatalogPage, error)
}
type ArchaeologyHistoryRepository interface {
	ArchaeologyBatchHistory(context.Context, string, domain.ArchaeologyBatchHistoryQuery) (domain.ArchaeologyBatchHistoryPage, error)
	ArchaeologyBatch(context.Context, string, string) (domain.ArchaeologyBatchDetail, error)
	ArchaeologyBatchOutcomes(context.Context, string, string, domain.ArchaeologyOutcomePageQuery) (domain.ArchaeologyOutcomePage, error)
}
type ArchaeologySelectedOutcomesRepository interface {
	ArchaeologySelectedOutcomes(context.Context, string, string, []string) ([]domain.ArchaeologyOutcome, error)
}
type ArchaeologySelectedApplyRepository interface {
	ReplayArchaeologySelectedImports(context.Context, domain.ArchaeologySelectedApplyReplayQuery) (domain.ArchaeologySelectedApplyReceipt, bool, error)
	ApplyArchaeologySelectedImports(context.Context, domain.ArchaeologySelectedApplyCommand) (domain.ArchaeologySelectedApplyReceipt, error)
}
type ArchaeologySelectedPreviewRepository interface {
	PreviewArchaeologySelectedImports(context.Context, domain.ArchaeologySelectedPreviewCommand) (domain.ArchaeologySelectedPreviewReceipt, error)
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
	TasksExamined      int                    `json:"tasks_examined"`
	ProjectsGrouped    int                    `json:"projects_grouped"`
	Truncated          bool                   `json:"truncated"`
	CompletedAt        *time.Time             `json:"completed_at,omitempty"`
	AppServerIdentity  string                 `json:"app_server_identity,omitempty"`
}
type ArchaeologyCatalogRequest struct {
	Cursor, Sort, Search string
	Limit                int
}
type ArchaeologyCatalogPage struct {
	Items      []ArchaeologyCandidate `json:"items"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	Total      int                    `json:"total"`
}
type ArchaeologyBatchSummary struct {
	BatchID        string             `json:"batch_id"`
	State          string             `json:"state"`
	Mode           string             `json:"mode"`
	Depth          string             `json:"depth"`
	Sources        ArchaeologySources `json:"sources"`
	Concurrency    int                `json:"concurrency"`
	SelectedTotal  int                `json:"selected_total"`
	QueuedCount    int                `json:"queued_count"`
	ActiveCount    int                `json:"active_count"`
	CompletedCount int                `json:"completed_count"`
	AttentionCount int                `json:"attention_count"`
	HasReport      bool               `json:"has_report"`
	CreatedAt      *time.Time         `json:"created_at,omitempty"`
	UpdatedAt      *time.Time         `json:"updated_at,omitempty"`
}
type ArchaeologyBatchHistoryPage struct {
	Items      []ArchaeologyBatchSummary `json:"items"`
	NextCursor string                    `json:"next_cursor,omitempty"`
}
type ArchaeologyBatchDetail struct {
	ArchaeologyBatchSummary
	Tasks              []ArchaeologyTaskLaunch `json:"tasks"`
	Review             *ArchaeologyReview      `json:"review,omitempty"`
	OutcomesNextCursor string                  `json:"outcomes_next_cursor,omitempty"`
}
type ArchaeologyOutcomePage struct {
	Items      []ArchaeologyOutcome `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
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
	SourceDigest   string                     `json:"source_digest,omitempty"`
	SourceCount    int                        `json:"source_count"`
	Provenance     []ArchaeologyProvenance    `json:"provenance"`
	MemberSessions []ArchaeologyMemberSession `json:"member_sessions"`
}
type ArchaeologyReview struct {
	BatchID                  string                     `json:"batch_id,omitempty"`
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
	BatchID        string                    `json:"batch_id,omitempty"`
	ID             string                    `json:"id"`
	State          string                    `json:"state"`
	ClaimedBy      string                    `json:"claimed_by,omitempty"`
	Failure        string                    `json:"failure,omitempty"`
	CreatedAt      *time.Time                `json:"created_at,omitempty"`
	UpdatedAt      *time.Time                `json:"updated_at,omitempty"`
	ClaimedAt      *time.Time                `json:"claimed_at,omitempty"`
	Depth          string                    `json:"depth"`
	Sources        ArchaeologySources        `json:"sources"`
	Concurrency    int                       `json:"concurrency"`
	PolicyAttested bool                      `json:"policy_attested"`
	CandidateIDs   []string                  `json:"candidate_ids"`
	Tasks          []ArchaeologyTaskLaunch   `json:"tasks"`
	Progress       ArchaeologyLaunchProgress `json:"progress"`
	AllowedActions []string                  `json:"allowed_actions"`
}
type ArchaeologyTaskLaunch struct {
	JobID            string     `json:"job_id,omitempty"`
	BatchID          string     `json:"batch_id,omitempty"`
	Mode             string     `json:"mode,omitempty"`
	PhaseLabel       string     `json:"phase_label,omitempty"`
	SourcesExamined  int        `json:"sources_examined"`
	DurationMS       *int64     `json:"duration_ms,omitempty"`
	LaunchID         string     `json:"launch_id"`
	CandidateID      string     `json:"candidate_id,omitempty"`
	ProjectID        string     `json:"project_id"`
	ProjectName      string     `json:"project_name,omitempty"`
	State            string     `json:"state"`
	ThreadID         string     `json:"thread_id,omitempty"`
	TurnID           string     `json:"turn_id,omitempty"`
	CreatedAt        *time.Time `json:"created_at,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	Error            string     `json:"error,omitempty"`
	AvailableActions []string   `json:"available_actions"`
}
type ArchaeologyLaunchProgress struct {
	QueuedCount      int        `json:"queued_count"`
	ActiveCount      int        `json:"active_count"`
	AttentionCount   int        `json:"attention_count"`
	SelectedTotal    int        `json:"selected_total"`
	PreparingCount   int        `json:"preparing_count"`
	StartingCount    int        `json:"starting_count"`
	TaskCreatedCount int        `json:"task_created_count"`
	ClaimedCount     int        `json:"claimed_count"`
	RunningCount     int        `json:"running_count"`
	ReportReadyCount int        `json:"report_ready_count"`
	CompletedCount   int        `json:"completed_count"`
	FailedCount      int        `json:"failed_count"`
	UncertainCount   int        `json:"uncertain_count"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
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
	BaseRevision          int64 `json:"base_revision"`
	AcknowledgeLargeBatch bool  `json:"acknowledge_large_batch"`
}
type ArchaeologyResolutionRequest struct {
	BaseRevision int64  `json:"base_revision"`
	JobID        string `json:"job_id"`
	ThreadID     string `json:"thread_id"`
	TurnID       string `json:"turn_id"`
	Resolution   string `json:"resolution"`
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
type ArchaeologySelectedPreviewRequest struct {
	OutcomeIDs         []string `json:"outcome_ids"`
	ReviewSessionToken string   `json:"review_session_token,omitempty"`
	ReviewRequestID    string   `json:"-"`
}
type ArchaeologySelectedApplyRequest struct {
	OutcomeIDs            []string `json:"outcome_ids"`
	SelectionDigest       string   `json:"selection_digest"`
	ManifestDigest        string   `json:"manifest_digest"`
	ReviewAcknowledged    bool     `json:"review_acknowledged"`
	ReviewCompletionToken string   `json:"review_completion_token"`
}
type ArchaeologySelectedProjectPreview struct {
	OutcomeID string                  `json:"outcome_id"`
	ProjectID string                  `json:"project_id"`
	Request   HistoricalImportRequest `json:"request"`
	Preview   HistoricalImportResult  `json:"preview"`
}
type ArchaeologySelectedPreview struct {
	BatchID               string                              `json:"batch_id"`
	OutcomeIDs            []string                            `json:"outcome_ids"`
	SelectionDigest       string                              `json:"selection_digest"`
	ManifestDigest        string                              `json:"manifest_digest"`
	Projects              []ArchaeologySelectedProjectPreview `json:"projects"`
	NextCursor            string                              `json:"next_cursor,omitempty"`
	ReviewSessionToken    string                              `json:"review_session_token"`
	ReviewCompletionToken string                              `json:"review_completion_token,omitempty"`
	ReviewExpiresAt       time.Time                           `json:"review_expires_at"`
}

func selectedPreviewPage(value ArchaeologySelectedPreview, cursor string) (ArchaeologySelectedPreview, error) {
	offset := 0
	if cursor != "" {
		var err error
		offset, err = strconv.Atoi(cursor)
		if err != nil || strconv.Itoa(offset) != cursor || offset < 0 || offset%5 != 0 || offset >= len(value.Projects) {
			return value, domain.ErrInvalid
		}
	}
	end := offset + 5
	if end > len(value.Projects) {
		end = len(value.Projects)
	}
	all := value.Projects
	value.Projects = append([]ArchaeologySelectedProjectPreview(nil), all[offset:end]...)
	if end < len(all) {
		value.NextCursor = fmt.Sprintf("%d", end)
	}
	return value, nil
}
func (s *Service) PreviewSelectedArchaeologyImportsPage(ctx context.Context, principal, batchID, cursor string, input ArchaeologySelectedPreviewRequest, actor ProjectCoreActor) (ArchaeologySelectedPreview, error) {
	out, err := s.PreviewSelectedArchaeologyImports(ctx, principal, batchID, input, actor)
	if err != nil {
		return out, err
	}
	page, err := selectedPreviewPage(out, cursor)
	if err != nil {
		return page, err
	}
	pageIndex := 0
	if cursor != "" {
		pageIndex, _ = strconv.Atoi(cursor)
		pageIndex /= 5
	}
	pageCount := (len(out.OutcomeIDs) + 4) / 5
	reviewRepository, ok := s.repository.(interface {
		AdvanceArchaeologySelectedReview(context.Context, domain.ArchaeologySelectedReviewCommand) (domain.ArchaeologySelectedReviewReceipt, error)
	})
	if !ok {
		return page, domain.ErrUnavailable
	}
	receipt, err := reviewRepository.AdvanceArchaeologySelectedReview(ctx, domain.ArchaeologySelectedReviewCommand{Principal: principal, BatchID: batchID, SelectionDigest: out.SelectionDigest, ManifestDigest: out.ManifestDigest, SessionToken: input.ReviewSessionToken, RequestID: input.ReviewRequestID, OutcomeIDs: out.OutcomeIDs, Page: pageIndex, PageCount: pageCount})
	if err != nil {
		return page, err
	}
	page.ReviewSessionToken, page.ReviewCompletionToken, page.ReviewExpiresAt = receipt.SessionToken, receipt.CompletionToken, receipt.ExpiresAt
	return page, nil
}

type ArchaeologySelectedApplyResult struct {
	BatchID         string   `json:"batch_id"`
	OutcomeIDs      []string `json:"outcome_ids"`
	SelectionDigest string   `json:"selection_digest"`
	ManifestDigest  string   `json:"manifest_digest"`
	Applied         bool     `json:"applied"`
	AuditID         string   `json:"audit_id"`
}

func (s *Service) ConfigureProjectArchaeology(discoverer ArchaeologyDiscoverer, launcher ArchaeologyHistorianLauncher) {
	s.archaeologyDiscoverer = discoverer
	s.archaeologyLauncher = launcher
}
func (s *Service) ConfigureNativeArchaeologyApply(enabled bool) {
	if s != nil {
		s.nativeApplyEnabled = enabled
	}
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

func latestArchaeologyTime(values ...*time.Time) *time.Time {
	var latest time.Time
	for _, value := range values {
		if value != nil && value.After(latest) {
			latest = *value
		}
	}
	return optionalArchaeologyTime(latest)
}

func cohereArchaeologyViewTimes(value *ArchaeologySession) {
	if value == nil || value.Handoff == nil {
		return
	}
	latest := latestArchaeologyTime(value.Handoff.UpdatedAt, value.Handoff.Progress.UpdatedAt)
	for _, task := range value.Handoff.Tasks {
		latest = latestArchaeologyTime(latest, task.UpdatedAt)
	}
	value.Handoff.UpdatedAt = latest
	value.UpdatedAt = latestArchaeologyTime(value.UpdatedAt, latest)
}

func archaeologyNativeMappingReady(value domain.ArchaeologySession) bool {
	if len(value.Config.SelectedProjectIDs) == 0 {
		return false
	}
	wanted := make(map[string]bool, len(value.Config.SelectedProjectIDs))
	for _, id := range value.Config.SelectedProjectIDs {
		wanted[id] = true
	}
	for _, candidate := range value.Candidates {
		if wanted[candidate.ID] {
			if candidate.CanonicalProjectID == "" {
				return false
			}
			delete(wanted, candidate.ID)
		}
	}
	return len(wanted) == 0
}

func archaeologyView(value domain.ArchaeologySession) ArchaeologySession {
	selectedProjectIDs := make([]string, len(value.Config.SelectedProjectIDs))
	copy(selectedProjectIDs, value.Config.SelectedProjectIDs)
	out := ArchaeologySession{ID: value.ID, State: value.State, Discovery: ArchaeologyDiscovery{State: value.DiscoveryState, MetadataOnly: true, SourceRootsScanned: value.SourceRootsScanned, TasksExamined: value.TasksExamined, ProjectsGrouped: value.ProjectsGrouped, Truncated: value.CatalogTruncated, AppServerIdentity: value.AppServerIdentity, DiscoveredAt: optionalArchaeologyTime(value.DiscoveredAt), CompletedAt: optionalArchaeologyTime(value.DiscoveredAt), Error: value.DiscoveryError, Candidates: []ArchaeologyCandidate{}}, Config: ArchaeologyConfig{SelectedProjectIDs: selectedProjectIDs, Depth: value.Config.Depth, Sources: ArchaeologySources{Git: value.Config.Sources.Git, Docs: value.Config.Sources.Docs, CodexHistory: value.Config.Sources.CodexHistory}, MaxConcurrency: value.Config.MaxConcurrency}, Runs: []ArchaeologyRun{}, Revision: value.Revision, UpdatedAt: optionalArchaeologyTime(value.UpdatedAt)}
	for _, candidate := range value.Candidates {
		sources := []string{}
		if candidate.FromCodexMetadata {
			sources = append(sources, "codex_metadata")
		}
		if candidate.FromConfiguredRoot {
			sources = append(sources, "configured_root")
		}
		out.Discovery.Candidates = append(out.Discovery.Candidates, archaeologyCandidateView(candidate, sources))
	}
	for _, run := range value.Runs {
		out.Runs = append(out.Runs, ArchaeologyRun{ID: run.ID, ProjectID: run.ProjectID, State: run.State, PhaseLabel: run.PhaseLabel, CompletedUnits: run.CompletedUnits, TotalUnits: run.TotalUnits, OutcomesFound: run.OutcomesFound, SourcesExamined: run.SourcesExamined, Error: run.Error, UpdatedAt: optionalArchaeologyTime(run.UpdatedAt)})
	}
	out.Controls = ArchaeologyControls{CanStart: value.State == "draft" && archaeologyNativeMappingReady(value), CanPause: value.State == "running", CanResume: value.State == "paused" || value.State == "pause_requested", CanCancel: value.State == "running" || value.State == "paused" || value.State == "pause_requested"}
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
			launchTasks = append(launchTasks, ArchaeologyTaskLaunch{LaunchID: launch.ID, ProjectID: launch.ProjectID, State: launch.State, ThreadID: launch.ThreadID, TurnID: launch.TurnID, CreatedAt: optionalArchaeologyTime(launch.CreatedAt), UpdatedAt: optionalArchaeologyTime(launch.UpdatedAt), Error: publicLaunchError(launch.State), AvailableActions: actions})
		}
		progress := archaeologyLaunchProgress(launchTasks)
		if len(launchTasks) > 0 && out.Handoff == nil {
			ids := make([]string, 0, len(launchTasks))
			for _, task := range launchTasks {
				ids = append(ids, task.ProjectID)
			}
			out.Handoff = &ArchaeologyHandoff{State: "launching", Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: launchTasks, Progress: progress, AllowedActions: []string{}}
		}
		ids := append([]string(nil), out.Config.SelectedProjectIDs...)
		out.Handoff = &ArchaeologyHandoff{ID: value.Handoff.ID, State: value.Handoff.State, ClaimedBy: value.Handoff.ClaimedBy, Failure: value.Handoff.Failure, CreatedAt: optionalArchaeologyTime(value.Handoff.CreatedAt), UpdatedAt: optionalArchaeologyTime(value.Handoff.UpdatedAt), ClaimedAt: optionalArchaeologyTime(value.Handoff.ClaimedAt), Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: launchTasks, Progress: progress, AllowedActions: actions}
		out.Controls.CanStart = false
		out.Controls.CanPause, out.Controls.CanResume, out.Controls.CanCancel = false, false, false
	}
	if len(value.NativeBatches) > 0 {
		batch := value.NativeBatches[0]
		tasks := make([]ArchaeologyTaskLaunch, 0, len(batch.Jobs))
		ids := make([]string, 0, len(batch.Jobs))
		for _, job := range batch.Jobs {
			ids = append(ids, job.CandidateID)
			errorMessage := ""
			if job.State == "uncertain" && job.ThreadID != "" && job.TurnID != "" {
				errorMessage = "Codex may have accepted this task, but Commons cannot safely retry it. Confirm that the exact Codex task has stopped before resolving it."
			}
			if job.State == "uncertain" && (job.ThreadID == "" || job.TurnID == "") {
				errorMessage = "Codex may have accepted this task, but Commons could not recover one exact task identity. Starting remains blocked."
			}
			if job.State == "attention" {
				errorMessage = "This task needs attention before project history can continue."
			}
			if job.State == "failed" || job.State == "interrupted" {
				errorMessage = "This Codex task stopped without a review-ready report."
			}
			actions := []string{}
			if job.State == "uncertain" && job.ThreadID != "" && job.TurnID != "" {
				actions = append(actions, "resolve")
			}
			tasks = append(tasks, ArchaeologyTaskLaunch{JobID: job.ID, BatchID: job.BatchID, CandidateID: job.CandidateID, Mode: job.Mode, PhaseLabel: job.PhaseLabel, SourcesExamined: job.SourcesExamined, DurationMS: job.DurationMS, LaunchID: job.ID, ProjectID: job.ProjectID, ProjectName: job.ProjectName, State: job.State, ThreadID: job.ThreadID, TurnID: job.TurnID, CreatedAt: optionalArchaeologyTime(job.CreatedAt), UpdatedAt: optionalArchaeologyTime(job.UpdatedAt), Error: errorMessage, AvailableActions: actions})
		}
		progress := archaeologyLaunchProgress(tasks)
		batchActions := []string{}
		for _, task := range tasks {
			if task.State == "uncertain" && task.ThreadID != "" && task.TurnID != "" {
				batchActions = append(batchActions, "resolve")
				break
			}
		}
		out.Handoff = &ArchaeologyHandoff{BatchID: batch.ID, State: batch.State, CreatedAt: optionalArchaeologyTime(batch.CreatedAt), UpdatedAt: optionalArchaeologyTime(batch.UpdatedAt), Depth: batch.Policy.Depth, Sources: ArchaeologySources{Git: batch.Policy.Sources.Git, Docs: batch.Policy.Sources.Docs, CodexHistory: batch.Policy.Sources.CodexHistory}, Concurrency: batch.MaxConcurrency, PolicyAttested: batch.PolicyAttested, CandidateIDs: ids, Tasks: tasks, Progress: progress, AllowedActions: batchActions}
		restartableTerminal := batch.State == "completed" || batch.State == "canceled" ||
			(batch.State == "attention" && progress.QueuedCount == 0 && progress.ActiveCount == 0 && progress.UncertainCount == 0)
		out.Controls.CanStart = value.State == "draft" && restartableTerminal && archaeologyNativeMappingReady(value)
		out.Controls.CanPause, out.Controls.CanResume = false, false
		out.Controls.CanCancel = batch.State == "queued" || batch.State == "running"
	}
	if len(value.Outcomes) > 0 || value.State == "completed" {
		review := ArchaeologyReview{BatchID: value.NativeReviewBatchID, ProposedOutcomes: []ArchaeologyOutcome{}, MemberSessions: []ArchaeologyMemberSession{}, RequiresExplicitApproval: true}
		members := map[string]*ArchaeologyMemberSession{}
		for _, item := range value.Outcomes {
			var proposalIdentity struct {
				SourceDigest string `json:"source_digest"`
			}
			_ = json.Unmarshal([]byte(item.ProposalJSON), &proposalIdentity)
			outcome := ArchaeologyOutcome{ID: item.ID, Title: item.Title, Summary: item.Summary, ProjectID: item.ProjectID, SourceDigest: proposalIdentity.SourceDigest, SourceCount: item.SourceCount, Provenance: []ArchaeologyProvenance{}, MemberSessions: []ArchaeologyMemberSession{}}
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
		// Native historian outcomes remain review-only until Commons presents an
		// exact task/evidence diff and confirms a server-derived manifest digest.
		// A model-supplied source digest is not sufficient human authorization.
		if len(value.NativeBatches) > 0 {
			review.CanApply = archaeologyNativeBatchEligible(value)
		} else {
			for _, item := range value.Outcomes {
				var request HistoricalImportRequest
				if json.Unmarshal([]byte(item.ProposalJSON), &request) == nil && request.SchemaVersion == domain.HistoricalImportSchemaVersion && request.CollisionPolicy == domain.HistoricalCollisionCurrentWins && len(request.Tasks) > 0 {
					review.CanApply = true
					break
				}
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
		CanonicalApply:   ArchaeologyCapability{Configured: true, Available: out.Review != nil && out.Review.CanApply, Mode: "preview_manifest_confirm", Reason: "A signed-in human must review the exact task and evidence diff, then confirm the server-derived manifest digest and source digest."},
	}
	cohereArchaeologyViewTimes(&out)
	return out
}

func archaeologyNativeBatchEligible(value domain.ArchaeologySession) bool {
	if value.NativeReviewBatchID == "" {
		return false
	}
	for _, batch := range value.NativeBatches {
		if batch.ID == value.NativeReviewBatchID {
			return batch.Eligibility.Eligible
		}
	}
	return false
}

func archaeologyCandidateView(candidate domain.ArchaeologyCandidate, sources []string) ArchaeologyCandidate {
	return ArchaeologyCandidate{ID: candidate.ID, Name: candidate.Name, PathLabel: candidate.PathLabel, RepositoryLabel: candidate.RepositoryLabel, LastActivityAt: optionalArchaeologyTime(candidate.LastActivityAt), Signals: ArchaeologySignals{Git: candidate.HasGit, Docs: candidate.HasDocs, CodexHistory: candidate.HasCodexHistory}, Estimate: ArchaeologyEstimate{DurationSecondsMin: candidate.DurationMinSeconds, DurationSecondsMax: candidate.DurationMaxSeconds, RelativeCost: candidate.RelativeCost}, PrivacyNote: candidate.PrivacyNote, SelectedByDefault: candidate.Selected, Sources: sources, CodexThreadCount: candidate.CodexThreadCount}
}
func archaeologyLaunchProgress(tasks []ArchaeologyTaskLaunch) ArchaeologyLaunchProgress {
	out := ArchaeologyLaunchProgress{SelectedTotal: len(tasks)}
	var latest time.Time
	for _, task := range tasks {
		switch task.State {
		case "queued":
			out.QueuedCount++
		case "starting":
			out.StartingCount++
			out.ActiveCount++
		case "active":
			out.RunningCount++
			out.ActiveCount++
		case "cancel_requested":
			out.ActiveCount++
		case "canceled":
			out.FailedCount++
		case "attention":
			out.AttentionCount++
		case "interrupted":
			out.FailedCount++
		case "preparing":
			out.PreparingCount++
		case "starting_codex":
			out.StartingCount++
		case "task_created":
			out.TaskCreatedCount++
		case "claimed":
			out.ClaimedCount++
		case "running":
			out.RunningCount++
		case "report_ready":
			out.ReportReadyCount++
			out.ActiveCount++
		case "completed":
			out.CompletedCount++
		case "failed":
			out.FailedCount++
		case "uncertain":
			out.UncertainCount++
		}
		if task.UpdatedAt != nil && task.UpdatedAt.After(latest) {
			latest = *task.UpdatedAt
		}
	}
	out.UpdatedAt = optionalArchaeologyTime(latest)
	return out
}

func (s *Service) archaeologySessionView(value domain.ArchaeologySession) ArchaeologySession {
	out := archaeologyView(value)
	if s != nil {
		schedulerNative := s.archaeologyScheduler != nil
		_, directTaskNative := s.archaeologyLauncher.(ArchaeologyTaskLauncher)
		if (schedulerNative || directTaskNative) && len(value.NativeBatches) == 0 && value.Handoff != nil &&
			value.Handoff.State == "ready_to_claim" && value.Handoff.ClaimedBy == "" && value.Handoff.ClaimedAt.IsZero() {
			// An untouched pre-native handoff remains durable audit history, but is
			// not a second control plane once direct App Server launch is configured.
			legacyTasks := []ArchaeologyTaskLaunch{}
			if out.Handoff != nil {
				legacyTasks = out.Handoff.Tasks
			}
			out.Handoff = nil
			out.Controls.CanStart = (schedulerNative || len(legacyTasks) == 0) && value.State == "draft" && archaeologyNativeMappingReady(value)
			out.Controls.CanPause, out.Controls.CanResume, out.Controls.CanCancel = false, false, false
			if directTaskNative && !schedulerNative && len(legacyTasks) > 0 {
				ids := append([]string(nil), out.Config.SelectedProjectIDs...)
				out.Handoff = &ArchaeologyHandoff{State: "launching", Depth: out.Config.Depth, Sources: out.Config.Sources, Concurrency: out.Config.MaxConcurrency, CandidateIDs: ids, Tasks: legacyTasks, AllowedActions: []string{}}
			}
		}
		if len(value.NativeBatches) > 0 && !schedulerNative {
			out.Controls.CanStart, out.Controls.CanPause, out.Controls.CanResume, out.Controls.CanCancel = false, false, false, false
		}
	}
	if s != nil && s.archaeologyDiscoverer != nil {
		out.Capabilities.Discovery = ArchaeologyCapability{Configured: true, Available: true, Mode: "codex_known_metadata", Reason: "Catalog uses workspace metadata only. Codex 0.147 protocol preview bytes may arrive, but Commons does not represent, retain, persist, project, or log them."}
		out.Capabilities.ProjectCatalog = ArchaeologyCapability{Configured: true, Available: true, Mode: "codex_metadata", Reason: "Projects are grouped from Codex-known workspaces and additive configured roots."}
	}
	if s != nil && s.archaeologyLauncher != nil {
		capabilityCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.archaeologyLauncher.Available(capabilityCtx)
		cancel()
		// TaskLaunch describes installation/runtime capability, not whether the
		// currently persisted selection is eligible for an immediate transition.
		// Controls.CanStart retains the latter, mapping-sensitive meaning. Keeping
		// these facts separate lets a client persist its local selection before it
		// asks the server to start that newly eligible configuration.
		if err == nil {
			out.Capabilities.TaskLaunch = ArchaeologyCapability{Configured: true, Available: true, Mode: "app_server_stdio", Reason: "Commons can submit one ordinary Codex historian task for every manually confirmed project; Codex governs execution capacity."}
		} else {
			out.Capabilities.TaskLaunch = ArchaeologyCapability{Configured: true, Available: false, Mode: "app_server_stdio", Reason: "Historian task launch is paused until Commons can reach the paired Codex App Server and reconcile terminal state."}
		}
	}
	if s != nil && len(value.NativeBatches) > 0 {
		available := s.nativeApplyEnabled && out.Review != nil && out.Review.CanApply
		out.Capabilities.CanonicalApply = ArchaeologyCapability{Configured: s.nativeApplyEnabled, Available: available, Mode: "selected_preview_manifest_confirm", Reason: "Native Apply is enabled only after acceptance; selected outcomes require exact preview, review acknowledgement, and both selection and manifest digests."}
		if out.Review != nil {
			out.Review.CanApply = available
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
func (s *Service) ProjectArchaeologyCatalog(ctx context.Context, principal string, input ArchaeologyCatalogRequest) (ArchaeologyCatalogPage, error) {
	repository, ok := s.repository.(ArchaeologyCatalogRepository)
	if !ok {
		return ArchaeologyCatalogPage{}, domain.ErrUnavailable
	}
	page, err := repository.ArchaeologyCatalog(ctx, principal, domain.ArchaeologyCatalogQuery{Cursor: input.Cursor, Limit: input.Limit, Sort: input.Sort, Search: input.Search})
	if err != nil {
		return ArchaeologyCatalogPage{}, err
	}
	out := ArchaeologyCatalogPage{Items: make([]ArchaeologyCandidate, 0, len(page.Candidates)), NextCursor: page.NextCursor, Total: page.Total}
	for _, candidate := range page.Candidates {
		sources := []string{}
		if candidate.FromCodexMetadata {
			sources = append(sources, "codex_metadata")
		}
		if candidate.FromConfiguredRoot {
			sources = append(sources, "configured_root")
		}
		out.Items = append(out.Items, archaeologyCandidateView(candidate, sources))
	}
	return out, nil
}
func archaeologyBatchSummaryView(item domain.ArchaeologyBatchSummary) ArchaeologyBatchSummary {
	return ArchaeologyBatchSummary{BatchID: item.ID, State: item.State, Mode: item.Mode, Depth: item.Policy.Depth, Sources: ArchaeologySources{Git: item.Policy.Sources.Git, Docs: item.Policy.Sources.Docs, CodexHistory: item.Policy.Sources.CodexHistory}, Concurrency: item.MaxConcurrency, SelectedTotal: item.SelectedTotal, QueuedCount: item.QueuedCount, ActiveCount: item.ActiveCount, CompletedCount: item.CompletedCount, AttentionCount: item.AttentionCount, HasReport: item.HasReport, CreatedAt: optionalArchaeologyTime(item.CreatedAt), UpdatedAt: optionalArchaeologyTime(item.UpdatedAt)}
}
func (s *Service) ProjectArchaeologyBatchHistory(ctx context.Context, principal, cursor string, limit int) (ArchaeologyBatchHistoryPage, error) {
	repository, ok := s.repository.(ArchaeologyHistoryRepository)
	if !ok {
		return ArchaeologyBatchHistoryPage{}, domain.ErrUnavailable
	}
	page, err := repository.ArchaeologyBatchHistory(ctx, principal, domain.ArchaeologyBatchHistoryQuery{Cursor: cursor, Limit: limit})
	if err != nil {
		return ArchaeologyBatchHistoryPage{}, err
	}
	out := ArchaeologyBatchHistoryPage{Items: make([]ArchaeologyBatchSummary, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		out.Items = append(out.Items, archaeologyBatchSummaryView(item))
	}
	return out, nil
}
func (s *Service) ProjectArchaeologyBatch(ctx context.Context, principal, batchID string) (ArchaeologyBatchDetail, error) {
	repository, ok := s.repository.(ArchaeologyHistoryRepository)
	if !ok {
		return ArchaeologyBatchDetail{}, domain.ErrUnavailable
	}
	detail, err := repository.ArchaeologyBatch(ctx, principal, batchID)
	if err != nil {
		return ArchaeologyBatchDetail{}, err
	}
	summary := domain.ArchaeologyBatchSummary{ID: detail.Batch.ID, State: detail.Batch.State, Mode: detail.Batch.Mode, Policy: detail.Batch.Policy, MaxConcurrency: detail.Batch.MaxConcurrency, SelectedTotal: len(detail.Batch.Jobs), CreatedAt: detail.Batch.CreatedAt, UpdatedAt: detail.Batch.UpdatedAt, HasReport: len(detail.Outcomes) > 0}
	pageOutcomes := detail.Outcomes
	if len(pageOutcomes) > 5 {
		pageOutcomes = pageOutcomes[:5]
	}
	temp := domain.ArchaeologySession{NativeBatches: []domain.ArchaeologyNativeBatch{detail.Batch}, NativeReviewBatchID: detail.Batch.ID, Outcomes: pageOutcomes}
	view := archaeologyView(temp)
	out := ArchaeologyBatchDetail{ArchaeologyBatchSummary: archaeologyBatchSummaryView(summary), Tasks: []ArchaeologyTaskLaunch{}}
	if view.Handoff != nil {
		out.Tasks = view.Handoff.Tasks
	}
	out.Review = view.Review
	out.OutcomesNextCursor = detail.OutcomesNextCursor
	if out.Review != nil {
		out.Review.CanApply = s.nativeApplyEnabled && detail.Batch.Eligibility.Eligible
	}
	return out, nil
}
func (s *Service) ProjectArchaeologyBatchOutcomes(ctx context.Context, principal, batchID, cursor string) (ArchaeologyOutcomePage, error) {
	repository, ok := s.repository.(ArchaeologyHistoryRepository)
	if !ok {
		return ArchaeologyOutcomePage{}, domain.ErrUnavailable
	}
	page, err := repository.ArchaeologyBatchOutcomes(ctx, principal, batchID, domain.ArchaeologyOutcomePageQuery{Cursor: cursor, Limit: 5})
	if err != nil {
		return ArchaeologyOutcomePage{}, err
	}
	temp := domain.ArchaeologySession{NativeReviewBatchID: batchID, Outcomes: page.Items}
	view := archaeologyView(temp)
	out := ArchaeologyOutcomePage{Items: []ArchaeologyOutcome{}, NextCursor: page.NextCursor}
	if view.Review != nil {
		out.Items = view.Review.ProposedOutcomes
	}
	return out, nil
}

func selectedSHA(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (s *Service) PreviewSelectedArchaeologyImports(ctx context.Context, principal, batchID string, input ArchaeologySelectedPreviewRequest, actor ProjectCoreActor) (ArchaeologySelectedPreview, error) {
	ids := append([]string(nil), input.OutcomeIDs...)
	sort.Strings(ids)
	if len(ids) < 1 || len(ids) > domain.ArchaeologyNativeMaxProjects*2 {
		return ArchaeologySelectedPreview{}, domain.ErrInvalid
	}
	selectedRepository, ok := s.repository.(ArchaeologySelectedOutcomesRepository)
	if !ok {
		return ArchaeologySelectedPreview{}, domain.ErrUnavailable
	}
	selected, err := selectedRepository.ArchaeologySelectedOutcomes(ctx, principal, batchID, ids)
	if err != nil {
		return ArchaeologySelectedPreview{}, err
	}
	byID := map[string]domain.ArchaeologyOutcome{}
	for _, o := range selected {
		byID[o.ID] = o
	}
	out := ArchaeologySelectedPreview{BatchID: batchID, OutcomeIDs: ids, Projects: []ArchaeologySelectedProjectPreview{}}
	imports := []domain.HistoricalImportCommand{}
	seenSources := map[string]struct{}{}
	for index, id := range ids {
		if index > 0 && ids[index-1] == id {
			return out, domain.ErrInvalid
		}
		o, exists := byID[id]
		if !exists {
			return out, domain.ErrNotFound
		}
		var request HistoricalImportRequest
		if json.Unmarshal([]byte(o.ProposalJSON), &request) != nil {
			return out, domain.ErrInvalid
		}
		sourceKey := o.ProjectID + "\x00" + request.SourceDigest
		if _, duplicate := seenSources[sourceKey]; duplicate {
			return out, domain.ErrConflict
		}
		seenSources[sourceKey] = struct{}{}
		imports = append(imports, historicalImportCommand(o.ProjectID, request, actor))
		out.Projects = append(out.Projects, ArchaeologySelectedProjectPreview{OutcomeID: id, ProjectID: o.ProjectID, Request: request})
	}
	previewRepository, ok := s.repository.(ArchaeologySelectedPreviewRepository)
	if !ok {
		return out, domain.ErrUnavailable
	}
	requestID := selectedSHA(struct {
		Principal, BatchID string
		IDs                []string
	}{principal, batchID, ids})
	receipt, err := previewRepository.PreviewArchaeologySelectedImports(ctx, domain.ArchaeologySelectedPreviewCommand{BatchID: batchID, Principal: principal, RequestID: requestID, OutcomeIDs: ids, Imports: imports})
	if err != nil {
		return out, err
	}
	if len(receipt.Imports) != len(out.Projects) {
		return out, domain.ErrConflict
	}
	out.SelectionDigest, out.ManifestDigest = receipt.SelectionDigest, receipt.ManifestDigest
	for index := range out.Projects {
		out.Projects[index].Preview = historicalImportResult(receipt.Imports[index])
	}
	return out, nil
}
func selectedApplyResult(receipt domain.ArchaeologySelectedApplyReceipt) (ArchaeologySelectedApplyResult, error) {
	if receipt.AuditID == "" || len(receipt.OutcomeIDs) < 1 || len(receipt.Imports) != len(receipt.OutcomeIDs) {
		return ArchaeologySelectedApplyResult{}, domain.ErrConflict
	}
	return ArchaeologySelectedApplyResult{BatchID: receipt.BatchID, OutcomeIDs: append([]string(nil), receipt.OutcomeIDs...), SelectionDigest: receipt.SelectionDigest, ManifestDigest: receipt.ManifestDigest, Applied: true, AuditID: receipt.AuditID}, nil
}
func (s *Service) ApplySelectedArchaeologyImports(ctx context.Context, principal, requestID, batchID string, input ArchaeologySelectedApplyRequest, actor ProjectCoreActor) (ArchaeologySelectedApplyResult, error) {
	if !s.nativeApplyEnabled {
		return ArchaeologySelectedApplyResult{}, domain.ErrUnavailable
	}
	if !input.ReviewAcknowledged {
		return ArchaeologySelectedApplyResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(ArchaeologySelectedApplyRepository)
	if !ok {
		return ArchaeologySelectedApplyResult{}, domain.ErrUnavailable
	}
	ids := append([]string(nil), input.OutcomeIDs...)
	sort.Strings(ids)
	if len(ids) < 1 || len(ids) > domain.ArchaeologyNativeMaxProjects*2 {
		return ArchaeologySelectedApplyResult{}, domain.ErrInvalid
	}
	for index, id := range ids {
		if id == "" || index > 0 && ids[index-1] == id {
			return ArchaeologySelectedApplyResult{}, domain.ErrInvalid
		}
	}
	prior, found, err := repository.ReplayArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyReplayQuery{BatchID: batchID, Principal: principal, RequestID: requestID, SelectionDigest: input.SelectionDigest, ManifestDigest: input.ManifestDigest, OutcomeIDs: ids})
	if err != nil {
		return ArchaeologySelectedApplyResult{}, err
	}
	if found {
		return selectedApplyResult(prior)
	}
	preview, err := s.PreviewSelectedArchaeologyImports(ctx, principal, batchID, ArchaeologySelectedPreviewRequest{OutcomeIDs: input.OutcomeIDs}, actor)
	if err != nil {
		return ArchaeologySelectedApplyResult{}, err
	}
	if preview.SelectionDigest != input.SelectionDigest || preview.ManifestDigest != input.ManifestDigest {
		return ArchaeologySelectedApplyResult{}, domain.ErrConflict
	}
	imports := make([]domain.HistoricalImportCommand, 0, len(preview.Projects))
	for _, project := range preview.Projects {
		request := project.Request
		request.ConfirmSourceDigest = request.SourceDigest
		request.ConfirmManifestDigest = project.Preview.ManifestDigest
		imports = append(imports, historicalImportCommand(project.ProjectID, request, actor))
	}
	receipt, err := repository.ApplyArchaeologySelectedImports(ctx, domain.ArchaeologySelectedApplyCommand{BatchID: batchID, Principal: principal, RequestID: requestID, SelectionDigest: preview.SelectionDigest, ManifestDigest: preview.ManifestDigest, ReviewCompletionToken: input.ReviewCompletionToken, OutcomeIDs: preview.OutcomeIDs, Imports: imports})
	if err != nil {
		return ArchaeologySelectedApplyResult{}, err
	}
	return selectedApplyResult(receipt)
}
func (s *Service) DiscoverProjectArchaeology(ctx context.Context, principal, requestID string) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	if s.archaeologyDiscoverer == nil {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	current, currentErr := repository.ArchaeologySession(ctx, principal)
	if currentErr == nil {
		for _, batch := range current.NativeBatches {
			for _, job := range batch.Jobs {
				switch job.State {
				case "queued", "starting", "active", "report_ready", "cancel_requested":
					return ArchaeologySession{}, domain.ErrConflict
				}
			}
		}
	} else if !errors.Is(currentErr, domain.ErrNotFound) {
		return ArchaeologySession{}, currentErr
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
	// MaxConcurrency remains fixed for schema, wire, and audit compatibility; it
	// is not an execution-capacity promise. Codex governs capacity after submit.
	mutation.Config = domain.ArchaeologyConfig{SelectedProjectIDs: input.SelectedProjectIDs, Depth: input.Depth, Sources: domain.ArchaeologySources{Git: input.Sources.Git, Docs: input.Sources.Docs, CodexHistory: input.Sources.CodexHistory}, MaxConcurrency: 2}
	value, err := repository.ConfigureArchaeology(ctx, mutation)
	return s.archaeologySessionView(value), err
}
func (s *Service) StartProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	if s.archaeologyScheduler != nil {
		value, queueErr := s.queueNativeProjectArchaeology(ctx, principal, requestID, input.BaseRevision, input.AcknowledgeLargeBatch)
		if queueErr != nil {
			return ArchaeologySession{}, queueErr
		}
		return s.archaeologySessionView(value), nil
	}
	launcher, supported := s.archaeologyLauncher.(ArchaeologyTaskLauncher)
	if supported && s.archaeologyLauncher.Available(ctx) != nil {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.StartArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	if err != nil {
		return ArchaeologySession{}, err
	}
	if !supported {
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
	workers := len(candidates)
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
	if s.archaeologyScheduler != nil {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.PauseArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	return s.archaeologySessionView(value), err
}
func (s *Service) ResumeProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	if s.archaeologyScheduler != nil {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	repository, err := s.archaeologyRepository()
	if err != nil {
		return ArchaeologySession{}, err
	}
	value, err := repository.ResumeArchaeology(ctx, archaeologyMutation(principal, requestID, input.BaseRevision))
	return s.archaeologySessionView(value), err
}
func (s *Service) CancelProjectArchaeology(ctx context.Context, principal, requestID string, input ArchaeologyTransitionRequest) (ArchaeologySession, error) {
	if s.archaeologyScheduler != nil {
		value, err := s.archaeologyScheduler.Cancel(ctx, principal, requestID, input.BaseRevision)
		return s.archaeologySessionView(value), err
	}
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
