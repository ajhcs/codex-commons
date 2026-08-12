package domain

import "time"

const ArchaeologySchemaVersion = 1

type ArchaeologySources struct{ Git, Docs, CodexHistory bool }

type ArchaeologyConfig struct {
	SelectedProjectIDs []string
	Depth              string
	Sources            ArchaeologySources
	MaxConcurrency     int
}

type ArchaeologyCandidate struct {
	ID, Name, PathLabel, RepositoryLabel, RelativeCost, PrivacyNote string
	LastActivityAt                                                  time.Time
	HasGit, HasDocs, HasCodexHistory                                bool
	FromCodexMetadata, FromConfiguredRoot                           bool
	DurationMinSeconds, DurationMaxSeconds, CodexThreadCount        int
	Selected                                                        bool
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
	MetadataOnly                                         bool
	Config                                               ArchaeologyConfig
	Candidates                                           []ArchaeologyCandidate
	Runs                                                 []ArchaeologyRun
	Outcomes                                             []ArchaeologyOutcome
	TaskLaunches                                         []ArchaeologyTaskLaunch
	Revision                                             int64
	DiscoveredAt, UpdatedAt                              time.Time
	Handoff                                              *ArchaeologyHandoff
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
}
