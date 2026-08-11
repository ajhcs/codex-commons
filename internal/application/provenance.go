package application

import (
	"time"

	"codex-commons/internal/domain"
)

type ProvenanceSource struct {
	Kind       string     `json:"kind"`
	StableID   string     `json:"stable_id"`
	Digest     string     `json:"digest"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

type ProvenanceRecorder struct {
	Actor   string `json:"actor"`
	Session string `json:"session"`
}

type Provenance struct {
	Kind       string              `json:"kind"`
	Actor      string              `json:"actor,omitempty"`
	Session    string              `json:"session,omitempty"`
	Purpose    string              `json:"purpose,omitempty"`
	Role       string              `json:"role,omitempty"`
	Confidence string              `json:"confidence,omitempty"`
	RecordedAt *time.Time          `json:"recorded_at,omitempty"`
	Source     *ProvenanceSource   `json:"source,omitempty"`
	RecordedBy *ProvenanceRecorder `json:"recorded_by,omitempty"`
}

func provenanceView(item domain.Provenance) *Provenance {
	if item.Kind == "" || item.SessionID == "" && item.Source == nil {
		return nil
	}
	out := &Provenance{
		Kind: item.Kind, Actor: item.ActorID, Session: item.SessionID, Purpose: item.Purpose,
		Role: item.Role, Confidence: item.Confidence, RecordedAt: optionalTime(item.RecordedAt),
	}
	if item.Source != nil {
		out.Source = &ProvenanceSource{
			Kind: item.Source.Kind, StableID: item.Source.StableID, Digest: item.Source.Digest,
			OccurredAt: optionalTime(item.Source.OccurredAt),
		}
	}
	if item.RecordedBy != nil && (item.RecordedBy.ActorID != "" || item.RecordedBy.SessionID != "") {
		out.RecordedBy = &ProvenanceRecorder{Actor: item.RecordedBy.ActorID, Session: item.RecordedBy.SessionID}
	}
	return out
}

func attestedProvenance(actor, session, purpose string) *Provenance {
	return provenanceView(domain.Provenance{
		Kind: domain.ProvenanceAttested, ActorID: actor, SessionID: session, Purpose: purpose,
	})
}
