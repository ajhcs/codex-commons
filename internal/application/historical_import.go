package application

import (
	"context"
	"time"

	"codex-commons/internal/domain"
)

type HistoricalImportRepository interface {
	PreviewHistoricalImport(context.Context, domain.HistoricalImportCommand) (domain.HistoricalImportReceipt, error)
	ApplyHistoricalImport(context.Context, domain.HistoricalImportCommand) (domain.HistoricalImportReceipt, error)
	SupersedeHistoricalImport(context.Context, domain.SupersedeHistoricalImportCommand) (domain.HistoricalImportReceipt, error)
}

type HistoricalSourceRequest struct {
	Kind       string    `json:"kind"`
	StableID   string    `json:"stable_id"`
	Digest     string    `json:"digest"`
	OccurredAt time.Time `json:"occurred_at"`
}

type HistoricalProjectThreadAliasRequest struct {
	Alias   string                  `json:"alias"`
	Session string                  `json:"session"`
	Source  HistoricalSourceRequest `json:"source"`
}

type HistoricalAttributionRequest struct {
	Session    string                  `json:"session"`
	Role       string                  `json:"role"`
	Confidence string                  `json:"confidence"`
	Source     HistoricalSourceRequest `json:"source"`
}

type HistoricalTaskEventRequest struct {
	Key        string                  `json:"key"`
	Kind       string                  `json:"kind"`
	Summary    string                  `json:"summary"`
	Session    string                  `json:"session,omitempty"`
	Confidence string                  `json:"confidence"`
	Source     HistoricalSourceRequest `json:"source"`
}

type HistoricalTaskRequest struct {
	Key          string                         `json:"key"`
	Title        string                         `json:"title"`
	Description  string                         `json:"description,omitempty"`
	Acceptance   string                         `json:"acceptance,omitempty"`
	State        string                         `json:"state"`
	Source       HistoricalSourceRequest        `json:"source"`
	Attributions []HistoricalAttributionRequest `json:"attributions"`
	Events       []HistoricalTaskEventRequest   `json:"events,omitempty"`
}

type HistoricalImportRequest struct {
	SchemaVersion         int                                   `json:"schema_version"`
	BatchID               string                                `json:"batch_id"`
	SourceDigest          string                                `json:"source_digest"`
	ConfirmSourceDigest   string                                `json:"confirm_source_digest,omitempty"`
	ConfirmManifestDigest string                                `json:"confirm_manifest_digest,omitempty"`
	CollisionPolicy       string                                `json:"collision_policy"`
	ProjectThreadAliases  []HistoricalProjectThreadAliasRequest `json:"project_thread_aliases,omitempty"`
	Tasks                 []HistoricalTaskRequest               `json:"tasks"`
}

type SupersedeHistoricalImportRequest struct {
	Reason string `json:"reason"`
}

type HistoricalImportTaskReceipt struct {
	Key         string `json:"key"`
	ID          string `json:"id"`
	Disposition string `json:"disposition"`
}

type HistoricalImportCounts struct {
	ProjectThreadAliases int `json:"project_thread_aliases"`
	Tasks                int `json:"tasks"`
	Attributions         int `json:"attributions"`
	Events               int `json:"events"`
	Created              int `json:"created"`
	SkippedCurrent       int `json:"skipped_current"`
	Replayed             int `json:"replayed"`
}

type HistoricalImportResult struct {
	BatchID         string                        `json:"batch_id"`
	SourceDigest    string                        `json:"source_digest"`
	ManifestDigest  string                        `json:"manifest_digest"`
	CollisionPolicy string                        `json:"collision_policy"`
	State           string                        `json:"state"`
	Applied         bool                          `json:"applied"`
	RecordedAt      *time.Time                    `json:"recorded_at,omitempty"`
	Tasks           []HistoricalImportTaskReceipt `json:"tasks"`
	Counts          HistoricalImportCounts        `json:"counts"`
	ProjectRevision int64                         `json:"project_revision"`
}

func historicalSource(input HistoricalSourceRequest) domain.HistoricalSource {
	return domain.HistoricalSource{
		Kind: input.Kind, StableID: input.StableID, Digest: input.Digest, OccurredAt: input.OccurredAt,
	}
}

func historicalImportCommand(project string, input HistoricalImportRequest, actor ProjectCoreActor) domain.HistoricalImportCommand {
	command := domain.HistoricalImportCommand{
		ProjectID: project, SchemaVersion: input.SchemaVersion, BatchID: input.BatchID,
		SourceDigest: input.SourceDigest, ConfirmSourceDigest: input.ConfirmSourceDigest,
		ConfirmManifestDigest: input.ConfirmManifestDigest,
		CollisionPolicy:       input.CollisionPolicy, Meta: coreMeta(actor),
		ProjectThreadAliases: make([]domain.HistoricalProjectThreadAliasInput, 0, len(input.ProjectThreadAliases)),
		Tasks:                make([]domain.HistoricalTaskInput, 0, len(input.Tasks)),
	}
	for _, alias := range input.ProjectThreadAliases {
		command.ProjectThreadAliases = append(command.ProjectThreadAliases, domain.HistoricalProjectThreadAliasInput{
			Alias: alias.Alias, SessionID: alias.Session, Source: historicalSource(alias.Source),
		})
	}
	for _, task := range input.Tasks {
		item := domain.HistoricalTaskInput{
			Key: task.Key, Title: task.Title, Description: task.Description, Acceptance: task.Acceptance,
			State: task.State, Source: historicalSource(task.Source),
			Attributions: make([]domain.HistoricalAttributionInput, 0, len(task.Attributions)),
			Events:       make([]domain.HistoricalEventInput, 0, len(task.Events)),
		}
		for _, attribution := range task.Attributions {
			item.Attributions = append(item.Attributions, domain.HistoricalAttributionInput{
				SessionID: attribution.Session, Role: attribution.Role, Confidence: attribution.Confidence,
				Source: historicalSource(attribution.Source),
			})
		}
		for _, event := range task.Events {
			item.Events = append(item.Events, domain.HistoricalEventInput{
				Key: event.Key, Kind: event.Kind, Summary: event.Summary, SessionID: event.Session,
				Confidence: event.Confidence, Source: historicalSource(event.Source),
			})
		}
		command.Tasks = append(command.Tasks, item)
	}
	return command
}

func historicalImportResult(receipt domain.HistoricalImportReceipt) HistoricalImportResult {
	out := HistoricalImportResult{
		BatchID: receipt.BatchID, SourceDigest: receipt.SourceDigest, ManifestDigest: receipt.ManifestDigest,
		CollisionPolicy: receipt.CollisionPolicy, State: receipt.State, Applied: receipt.Applied,
		RecordedAt: optionalTime(receipt.RecordedAt), Tasks: make([]HistoricalImportTaskReceipt, 0, len(receipt.Tasks)),
		Counts: HistoricalImportCounts{
			ProjectThreadAliases: receipt.Counts.ProjectThreadAliases, Tasks: receipt.Counts.Tasks,
			Attributions: receipt.Counts.Attributions, Events: receipt.Counts.Events,
			Created: receipt.Counts.Created, SkippedCurrent: receipt.Counts.SkippedCurrent,
			Replayed: receipt.Counts.Replayed,
		},
		ProjectRevision: receipt.ProjectRevision,
	}
	for _, task := range receipt.Tasks {
		out.Tasks = append(out.Tasks, HistoricalImportTaskReceipt{Key: task.Key, ID: task.TaskID, Disposition: task.Disposition})
	}
	return out
}

func (s *Service) PreviewHistoricalTaskImport(ctx context.Context, project string, input HistoricalImportRequest, actor ProjectCoreActor) (HistoricalImportResult, error) {
	if s == nil {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	repository, ok := s.repository.(HistoricalImportRepository)
	if !ok {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	receipt, err := repository.PreviewHistoricalImport(ctx, historicalImportCommand(project, input, actor))
	return historicalImportResult(receipt), err
}

func (s *Service) ApplyHistoricalTaskImport(ctx context.Context, project string, input HistoricalImportRequest, actor ProjectCoreActor) (HistoricalImportResult, error) {
	if s == nil {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	repository, ok := s.repository.(HistoricalImportRepository)
	if !ok {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	receipt, err := repository.ApplyHistoricalImport(ctx, historicalImportCommand(project, input, actor))
	return historicalImportResult(receipt), err
}

func (s *Service) SupersedeHistoricalTaskImport(ctx context.Context, project, batch string, input SupersedeHistoricalImportRequest, actor ProjectCoreActor) (HistoricalImportResult, error) {
	if s == nil {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	repository, ok := s.repository.(HistoricalImportRepository)
	if !ok {
		return HistoricalImportResult{}, domain.ErrUnavailable
	}
	receipt, err := repository.SupersedeHistoricalImport(ctx, domain.SupersedeHistoricalImportCommand{
		ProjectID: project, BatchID: batch, Reason: input.Reason, Meta: coreMeta(actor),
	})
	return historicalImportResult(receipt), err
}
