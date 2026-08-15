package domain

import "time"

const (
	HistoricalImportSchemaVersion  = 1
	HistoricalCollisionCurrentWins = "current_wins"
)

var HistoricalRoles = map[string]bool{
	"originator": true, "implementer": true, "reviewer": true, "evaluator": true,
}

var HistoricalConfidences = map[string]bool{
	"verified": true, "supported": true, "uncertain": true,
}

var HistoricalEventKinds = map[string]bool{
	"completed": true, "reviewed": true, "failed": true, "retried": true,
	"remediated": true, "evaluated": true,
}

type HistoricalProjectThreadAliasInput struct {
	Alias, SessionID string
	Source           HistoricalSource
}

type HistoricalAttributionInput struct {
	SessionID, Role, Confidence string
	Source                      HistoricalSource
}

type HistoricalEventInput struct {
	Key, Kind, Summary, SessionID, Confidence string
	Source                                    HistoricalSource
}

type HistoricalTaskInput struct {
	Key, Title, Description, Acceptance, State string
	Priority                                   int
	Source                                     HistoricalSource
	Attributions                               []HistoricalAttributionInput
	Events                                     []HistoricalEventInput
}

type HistoricalImportCommand struct {
	ProjectID, BatchID, SourceDigest, CollisionPolicy, ConfirmSourceDigest, ConfirmManifestDigest string
	SchemaVersion                                                                                 int
	ProjectThreadAliases                                                                          []HistoricalProjectThreadAliasInput
	Tasks                                                                                         []HistoricalTaskInput
	Meta                                                                                          CoreWriteMeta
}

type HistoricalImportTaskReceipt struct {
	Key, TaskID, Disposition string
}

type HistoricalImportCounts struct {
	ProjectThreadAliases, Tasks, Attributions, Events, Created, SkippedCurrent, Replayed int
}

type HistoricalImportReceipt struct {
	ProjectID, BatchID, SourceDigest, ManifestDigest, CollisionPolicy, State string
	Applied                                                                  bool
	RecordedAt                                                               time.Time
	Tasks                                                                    []HistoricalImportTaskReceipt
	Counts                                                                   HistoricalImportCounts
	ProjectRevision                                                          int64
}

type SupersedeHistoricalImportCommand struct {
	ProjectID, BatchID, Reason string
	Meta                       CoreWriteMeta
}
