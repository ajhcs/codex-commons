package domain

import "time"

const (
	ArchaeologySchemaVersion          = 1
	ArchaeologyNativeReportMaxBytes   = 60 << 10
	ArchaeologyNativeProposalMaxBytes = 32 << 10
	ArchaeologyNativeMaxProjects      = 30
	// Legacy prototype rows remain durable, but canonical polling exposes one
	// deterministic recent review page bounded below the browser's 1 MiB cap.
	ArchaeologyLegacyOutcomePage            = 30
	ArchaeologyLegacyProvenancePerOutcome   = 8
	ArchaeologyLegacyContributorsPerOutcome = 4
)

type ArchaeologySources struct{ Git, Docs, CodexHistory bool }

type ArchaeologyExecutionPolicy struct {
	Depth   string
	Sources ArchaeologySources
}

type ArchaeologyExecutionLimits struct {
	MaxOutcomes, MaxProvenancePerOutcome, MaxContributorsPerOutcome int
	MaxHistoricalAliases, MaxHistoricalTasks, MaxSourcesExamined    int
}

func (p ArchaeologyExecutionPolicy) Limits() (ArchaeologyExecutionLimits, bool) {
	if !p.Sources.Git && !p.Sources.Docs && !p.Sources.CodexHistory {
		return ArchaeologyExecutionLimits{}, false
	}
	switch p.Depth {
	case "quick":
		return ArchaeologyExecutionLimits{MaxOutcomes: 2, MaxProvenancePerOutcome: 4, MaxContributorsPerOutcome: 1, MaxHistoricalAliases: 2, MaxHistoricalTasks: 2, MaxSourcesExamined: 50}, true
	case "standard":
		return ArchaeologyExecutionLimits{MaxOutcomes: 2, MaxProvenancePerOutcome: 4, MaxContributorsPerOutcome: 1, MaxHistoricalAliases: 3, MaxHistoricalTasks: 3, MaxSourcesExamined: 250}, true
	case "deep":
		return ArchaeologyExecutionLimits{MaxOutcomes: 2, MaxProvenancePerOutcome: 4, MaxContributorsPerOutcome: 1, MaxHistoricalAliases: 3, MaxHistoricalTasks: 4, MaxSourcesExamined: 1000}, true
	default:
		return ArchaeologyExecutionLimits{}, false
	}
}

func (p ArchaeologyExecutionPolicy) Allows(kind string) bool {
	switch kind {
	case "git":
		return p.Sources.Git
	case "docs":
		return p.Sources.Docs
	case "codex_history":
		return p.Sources.CodexHistory
	default:
		return false
	}
}

type ArchaeologyConfig struct {
	SelectedProjectIDs []string
	Depth              string
	Sources            ArchaeologySources
	MaxConcurrency     int
}

type ArchaeologyCandidate struct {
	ID, CanonicalProjectID, Name, PathLabel, RepositoryLabel, RelativeCost, PrivacyNote string
	LastActivityAt                                                                      time.Time
	HasGit, HasDocs, HasCodexHistory                                                    bool
	FromCodexMetadata, FromConfiguredRoot                                               bool
	DurationMinSeconds, DurationMaxSeconds, CodexThreadCount                            int
	Selected                                                                            bool
}

type ArchaeologyRun struct {
	ID, ProjectID, State, PhaseLabel, Error, RunnerKey string
	CompletedUnits                                     int
	TotalUnits                                         *int
	OutcomesFound, SourcesExamined                     int
	UpdatedAt                                          time.Time
}

type ArchaeologyProvenance struct {
	Kind, StableID, Digest string
	OccurredAt             time.Time
}
type ArchaeologyContributor struct{ SessionID, Contribution, DemonstratedStrength, Uncertainty, Confidence string }
type ArchaeologyOutcome struct {
	ID, Title, Summary, ProjectID string
	SourceCount                   int
	Provenance                    []ArchaeologyProvenance
	Contributors                  []ArchaeologyContributor
	ProposalJSON                  string
}

type ArchaeologySession struct {
	ID, Principal, State, DiscoveryState, DiscoveryError string
	SourceRootsScanned                                   int
	TasksExamined, ProjectsGrouped                       int
	CatalogTruncated                                     bool
	AppServerIdentity                                    string
	MetadataOnly                                         bool
	Config                                               ArchaeologyConfig
	Candidates                                           []ArchaeologyCandidate
	Runs                                                 []ArchaeologyRun
	Outcomes                                             []ArchaeologyOutcome
	NativeReviewBatchID                                  string
	TaskLaunches                                         []ArchaeologyTaskLaunch
	NativeBatches                                        []ArchaeologyNativeBatch
	Revision                                             int64
	DiscoveredAt, UpdatedAt                              time.Time
	Handoff                                              *ArchaeologyHandoff
}

type ArchaeologyNativeBatch struct {
	ID, State, Mode          string
	MaxConcurrency           int
	Policy                   ArchaeologyExecutionPolicy
	PolicyAttested           bool
	Jobs                     []ArchaeologyNativeJob
	CreatedAt, UpdatedAt     time.Time
	LargeBatchAcknowledgedAt time.Time
	LargeBatchAcknowledgedBy string
}

type ArchaeologyNativeJob struct {
	ID, BatchID, CandidateID, ProjectID, ProjectName, Mode, State string
	ThreadID, CodexSessionID, TurnID                              string
	PhaseLabel, ErrorCode                                         string
	SourcesExamined                                               int
	DurationMS                                                    *int64
	Policy                                                        ArchaeologyExecutionPolicy
	CreatedAt, StartedAt, ReportedAt, TerminalAt, UpdatedAt       time.Time
}

type ArchaeologyNativeBatchRequest struct {
	Principal, RequestID  string
	BaseRevision          int64
	AcknowledgeLargeBatch bool
}
type ArchaeologyNativeResolution struct {
	Principal, RequestID, JobID, ThreadID, TurnID, Resolution string
	BaseRevision                                              int64
}
type ArchaeologyNativeProgress struct {
	JobID, ThreadID, TurnID, PhaseLabel string
	SourcesExamined                     int
}
type ArchaeologyNativeReport struct {
	JobID, ThreadID, TurnID string
	Digest                  [32]byte
	Outcomes                []ArchaeologyOutcome
}
type ArchaeologyNativeTerminal struct {
	JobID, ThreadID, TurnID, Status string
	DurationMS                      *int64
}

type ArchaeologyHandoff struct {
	ID, State, ClaimedBy, Failure, PackJSON string
	CreatedAt, UpdatedAt, ClaimedAt         time.Time
}

type ArchaeologyTaskLaunch struct {
	ID, ProjectID, State, ThreadID, CodexSessionID, TurnID string
	ClientMessageID, Error                                 string
	RequestDigest, GrantDigest                             [32]byte
	ReportDigest, ReportRequestDigest                      [32]byte
	GrantExpiresAt, GrantConsumedAt                        time.Time
	ReportExpiresAt, ReportConsumedAt                      time.Time
	CreatedAt, UpdatedAt                                   time.Time
}

type ArchaeologyLaunchResult struct {
	LaunchID, ProjectID, State, ThreadID, CodexSessionID, TurnID, Error string
}

type ArchaeologyTaskClaim struct {
	LaunchID, ProjectID, ThreadID, CodexSessionID string
	GrantDigest, ReportDigest                     [32]byte
	ReportExpiresAt                               time.Time
}

type ArchaeologyTaskClaimResult struct {
	LaunchID, ProjectID, ThreadID, CodexSessionID string
	ReportExpiresAt                               time.Time
}

type ArchaeologyTaskReport struct {
	LaunchID, ProjectID, ThreadID, CodexSessionID, RequestID string
	ReportDigest                                             [32]byte
	Outcomes                                                 []ArchaeologyOutcome
}

type ArchaeologyMutation struct {
	Principal, RequestID, Operation string
	BaseRevision                    int64
	Config                          ArchaeologyConfig
}

type ArchaeologyRunUpdate struct {
	Principal, RunID, RunnerKey, State, PhaseLabel, Error string
	CompletedUnits                                        int
	TotalUnits                                            *int
	SourcesExamined                                       int
	Outcomes                                              []ArchaeologyOutcome
}

type ArchaeologyHandoffClaim struct {
	HandoffID, RequestID, SessionID string
}

type ArchaeologyHandoffReport struct {
	HandoffID, RequestID, SessionID string
	Outcomes                        []ArchaeologyOutcome
}

type ArchaeologyDiscovery struct {
	Candidates         []ArchaeologyCandidate
	SourceRootsScanned int
	TasksExamined      int
	ProjectsGrouped    int
	Truncated          bool
	AppServerIdentity  string
}

type ArchaeologyCatalogQuery struct {
	Cursor, Search, Sort string
	Limit                int
}
type ArchaeologyCatalogPage struct {
	Candidates []ArchaeologyCandidate
	NextCursor string
	Total      int
}

type ArchaeologyBatchHistoryQuery struct {
	Cursor string
	Limit  int
}
type ArchaeologyBatchSummary struct {
	ID, State, Mode                                                                         string
	Policy                                                                                  ArchaeologyExecutionPolicy
	MaxConcurrency, SelectedTotal, QueuedCount, ActiveCount, CompletedCount, AttentionCount int
	HasReport                                                                               bool
	CreatedAt, UpdatedAt                                                                    time.Time
}
type ArchaeologyBatchHistoryPage struct {
	Items      []ArchaeologyBatchSummary
	NextCursor string
}
type ArchaeologyBatchDetail struct {
	Batch              ArchaeologyNativeBatch
	Outcomes           []ArchaeologyOutcome
	OutcomesNextCursor string
}
type ArchaeologyOutcomePageQuery struct {
	Cursor string
	Limit  int
}
type ArchaeologyOutcomePage struct {
	Items      []ArchaeologyOutcome
	NextCursor string
}
type ArchaeologySelectedApplyCommand struct {
	BatchID, Principal, RequestID, SelectionDigest, ManifestDigest, ReviewCompletionToken string
	OutcomeIDs                                                                            []string
	Imports                                                                               []HistoricalImportCommand
}
type ArchaeologySelectedApplyReplayQuery struct {
	BatchID, Principal, RequestID, SelectionDigest, ManifestDigest string
	OutcomeIDs                                                     []string
}
type ArchaeologySelectedReviewCommand struct {
	Principal, BatchID, SelectionDigest, ManifestDigest, SessionToken, RequestID string
	OutcomeIDs                                                                   []string
	Page, PageCount                                                              int
}
type ArchaeologySelectedReviewReceipt struct {
	SessionToken, CompletionToken string
	NextPage                      int
	ExpiresAt                     time.Time
}
type ArchaeologySelectedPreviewCommand struct {
	BatchID, Principal, RequestID string
	OutcomeIDs                    []string
	Imports                       []HistoricalImportCommand
}
type ArchaeologySelectedPreviewReceipt struct {
	BatchID, SelectionDigest, ManifestDigest string
	OutcomeIDs                               []string
	Imports                                  []HistoricalImportReceipt
	PreparedImports                          []HistoricalImportCommand
}
type ArchaeologySelectedApplyReceipt struct {
	AuditID, BatchID, SelectionDigest, ManifestDigest string
	OutcomeIDs                                        []string
	Imports                                           []HistoricalImportReceipt
}
