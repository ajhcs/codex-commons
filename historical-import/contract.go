package historicalimport

import (
	"errors"
	"fmt"
)

type EvidenceSource struct {
	Kind       string `json:"kind"`
	StableID   string `json:"stable_id"`
	Digest     string `json:"digest"`
	OccurredAt string `json:"occurred_at"`
}

type ApplyProjectThreadAlias struct {
	Alias   string         `json:"alias"`
	Session string         `json:"session"`
	Source  EvidenceSource `json:"source"`
}

type ApplyAttribution struct {
	Session    string         `json:"session"`
	Role       string         `json:"role"`
	Confidence string         `json:"confidence"`
	Source     EvidenceSource `json:"source"`
}

type ApplyEvent struct {
	Key        string         `json:"key"`
	Kind       string         `json:"kind"`
	Summary    string         `json:"summary"`
	Session    string         `json:"session,omitempty"`
	Confidence string         `json:"confidence"`
	Source     EvidenceSource `json:"source"`
}

type ApplyTask struct {
	Key          string             `json:"key"`
	Title        string             `json:"title"`
	Description  string             `json:"description"`
	Acceptance   string             `json:"acceptance"`
	State        string             `json:"state"`
	Source       EvidenceSource     `json:"source"`
	Attributions []ApplyAttribution `json:"attributions"`
	Events       []ApplyEvent       `json:"events,omitempty"`
}

type ApplyRequest struct {
	SchemaVersion         int                       `json:"schema_version"`
	BatchID               string                    `json:"batch_id"`
	SourceDigest          string                    `json:"source_digest"`
	ConfirmSourceDigest   string                    `json:"confirm_source_digest"`
	ConfirmManifestDigest string                    `json:"confirm_manifest_digest"`
	CollisionPolicy       string                    `json:"collision_policy"`
	ProjectThreadAliases  []ApplyProjectThreadAlias `json:"project_thread_aliases"`
	Tasks                 []ApplyTask               `json:"tasks"`
}

type TaskReceipt struct {
	Key         string `json:"key"`
	ID          string `json:"id,omitempty"`
	Disposition string `json:"disposition"`
}

type ReceiptCounts struct {
	Created        int `json:"created"`
	SkippedCurrent int `json:"skipped_current"`
	Replayed       int `json:"replayed"`
}

type ApplyReceipt struct {
	BatchID      string        `json:"batch_id"`
	SourceDigest string        `json:"source_digest"`
	Applied      bool          `json:"applied"`
	RecordedAt   string        `json:"recorded_at,omitempty"`
	Tasks        []TaskReceipt `json:"tasks"`
	Counts       ReceiptCounts `json:"counts"`
}

func BuildApplyRequest(manifest Manifest, report PreviewReport, confirmSourceDigest, confirmManifestDigest string) (ApplyRequest, error) {
	if !report.ApplyEligible {
		return ApplyRequest{}, errors.New("preview is not apply-eligible")
	}
	sourceDigest, err := SourceDigest(manifest)
	if err != nil {
		return ApplyRequest{}, err
	}
	if report.SourceDigest != sourceDigest {
		return ApplyRequest{}, errors.New("preview source digest does not match manifest")
	}
	manifestDigest, err := ManifestDigest(manifest)
	if err != nil {
		return ApplyRequest{}, err
	}
	if report.ManifestDigest != manifestDigest {
		return ApplyRequest{}, errors.New("preview manifest digest does not match manifest")
	}
	if report.SchemaVersion != manifest.SchemaVersion || report.BatchID != manifest.BatchID ||
		report.ProjectID != manifest.ProjectID || report.CollisionPolicy != manifest.CollisionPolicy {
		return ApplyRequest{}, errors.New("preview identity does not match manifest")
	}
	if confirmSourceDigest == "" || confirmSourceDigest != sourceDigest {
		return ApplyRequest{}, errors.New("confirm_source_digest must exactly match the reviewed source digest")
	}
	if confirmManifestDigest == "" || confirmManifestDigest != manifestDigest {
		return ApplyRequest{}, errors.New("confirm_manifest_digest must exactly match the reviewed manifest digest")
	}
	request := ApplyRequest{
		SchemaVersion: manifest.SchemaVersion, BatchID: manifest.BatchID,
		SourceDigest: sourceDigest, ConfirmSourceDigest: confirmSourceDigest,
		ConfirmManifestDigest: confirmManifestDigest,
		CollisionPolicy:       manifest.CollisionPolicy,
		ProjectThreadAliases:  []ApplyProjectThreadAlias{}, Tasks: []ApplyTask{},
	}
	for _, alias := range manifest.ProjectThreadAliases {
		source, err := embeddedSource(manifest.Sources, alias.SourceRef, "")
		if err != nil {
			return ApplyRequest{}, fmt.Errorf("project thread alias %q: %w", alias.Alias, err)
		}
		request.ProjectThreadAliases = append(request.ProjectThreadAliases, ApplyProjectThreadAlias{
			Alias: alias.Alias, Session: alias.SessionID, Source: source,
		})
	}
	for _, task := range manifest.Tasks {
		source, err := embeddedSource(manifest.Sources, task.CompletionSourceRef, "")
		if err != nil {
			return ApplyRequest{}, fmt.Errorf("task %q: %w", task.Key, err)
		}
		item := ApplyTask{
			Key: task.Key, Title: task.Title, Description: task.Description,
			Acceptance: task.Acceptance, State: task.State, Source: source,
			Attributions: []ApplyAttribution{}, Events: []ApplyEvent{},
		}
		for _, attribution := range task.Attributions {
			source, err := embeddedSource(manifest.Sources, attribution.SourceRef, "")
			if err != nil {
				return ApplyRequest{}, fmt.Errorf("task %q attribution %q: %w", task.Key, attribution.SessionID, err)
			}
			item.Attributions = append(item.Attributions, ApplyAttribution{
				Session: attribution.SessionID, Role: attribution.Role,
				Confidence: attribution.Confidence, Source: source,
			})
		}
		for _, event := range task.Events {
			source, err := embeddedSource(manifest.Sources, event.SourceRef, event.OccurredAt)
			if err != nil {
				return ApplyRequest{}, fmt.Errorf("task %q event %q: %w", task.Key, event.Key, err)
			}
			item.Events = append(item.Events, ApplyEvent{
				Key: event.Key, Kind: event.Kind, Summary: event.Summary,
				Session: event.SessionID, Confidence: event.Confidence, Source: source,
			})
		}
		request.Tasks = append(request.Tasks, item)
	}
	return request, nil
}

func embeddedSource(sources []Source, ref, occurredAtOverride string) (EvidenceSource, error) {
	source := sourceByRef(sources, ref)
	if source.Ref == "" {
		return EvidenceSource{}, errors.New("source is not declared")
	}
	occurredAt := source.OccurredAt
	if occurredAtOverride != "" {
		occurredAt = occurredAtOverride
	}
	if occurredAt == "" {
		return EvidenceSource{}, errors.New("source occurred_at is unresolved")
	}
	return EvidenceSource{
		Kind: source.Kind, StableID: source.StableID,
		Digest: source.Digest, OccurredAt: occurredAt,
	}, nil
}

func ReplayDisposition(existingBatchID, existingSourceDigest, batchID, sourceDigest string) string {
	if existingBatchID == "" {
		return "new"
	}
	if existingBatchID == batchID && existingSourceDigest == sourceDigest {
		return "replay"
	}
	if existingBatchID == batchID {
		return "conflict"
	}
	return "new"
}

func ValidateAtomicReceipt(receipt ApplyReceipt) error {
	if receipt.BatchID == "" || !digestPattern.MatchString(receipt.SourceDigest) {
		return errors.New("receipt requires batch_id and source_digest")
	}
	if !receipt.Applied {
		if receipt.RecordedAt != "" || receipt.Counts.Created != 0 {
			return errors.New("non-applied receipt cannot claim recorded time or created tasks")
		}
		for _, task := range receipt.Tasks {
			if task.ID != "" || task.Disposition == "created" {
				return errors.New("non-applied receipt contains a partial canonical write")
			}
		}
		return nil
	}
	if !validTime(receipt.RecordedAt) {
		return errors.New("applied receipt recorded_at must be RFC3339")
	}
	created, skipped, replayed := 0, 0, 0
	for _, task := range receipt.Tasks {
		switch task.Disposition {
		case "created":
			if task.ID == "" {
				return errors.New("created receipt task requires canonical id")
			}
			created++
		case "skipped_current":
			skipped++
		case "replayed":
			if task.ID == "" {
				return errors.New("replayed receipt task requires canonical id")
			}
			replayed++
		default:
			return errors.New("receipt task has invalid disposition")
		}
	}
	if receipt.Counts != (ReceiptCounts{Created: created, SkippedCurrent: skipped, Replayed: replayed}) {
		return errors.New("receipt counts do not match task dispositions")
	}
	return nil
}
