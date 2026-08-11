package domain

import "time"

const (
	ProvenanceAttested   = "attested"
	ProvenanceHistorical = "historical"
)

// Provenance keeps server-attested authorship separate from historical source
// attribution. Historical sessions are evidence links only: they never imply
// ownership, presence, reachability, or authority.
type Provenance struct {
	Kind, ActorID, SessionID, Purpose, Role, Confidence string
	RecordedAt                                          time.Time
	Source                                              *HistoricalSource
	RecordedBy                                          *RecordedPrincipal
}

type RecordedPrincipal struct {
	ActorID, SessionID string
}

type HistoricalSource struct {
	Kind, StableID, Digest string
	OccurredAt             time.Time
}

type HistoricalTaskImport struct {
	BatchID, SourceKey, State string
	SourceCompletedAt         time.Time
	RecordedAt                time.Time
	Source                    *HistoricalSource
}
