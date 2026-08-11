package historicalimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxSnapshotBytes = 64 << 10

type CurrentSnapshot struct {
	SchemaVersion int           `json:"schema_version"`
	ProjectID     string        `json:"project_id"`
	Tasks         []CurrentTask `json:"tasks"`
}

type CurrentTask struct {
	Key   string `json:"key,omitempty"`
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type Collision struct {
	TaskKey      string `json:"task_key"`
	MatchedBy    string `json:"matched_by"`
	CurrentID    string `json:"current_id"`
	CurrentTitle string `json:"current_title"`
	Resolution   string `json:"resolution"`
}

type PreviewCounts struct {
	Tasks                int `json:"tasks"`
	ProjectThreadAliases int `json:"project_thread_aliases"`
	TaskSessions         int `json:"task_sessions"`
	AccountedSessions    int `json:"accounted_sessions"`
	AttributionLinks     int `json:"attribution_links"`
	Events               int `json:"events"`
	Planned              int `json:"planned"`
	SkippedCurrent       int `json:"skipped_current"`
	Ambiguous            int `json:"ambiguous"`
}

type PreviewTask struct {
	Key              string  `json:"key"`
	Title            string  `json:"title"`
	Disposition      string  `json:"disposition"`
	IdempotencyKey   string  `json:"idempotency_key"`
	SourceOccurredAt string  `json:"source_occurred_at,omitempty"`
	RecordedAt       *string `json:"recorded_at"`
	Attributions     int     `json:"attributions"`
	Events           int     `json:"events"`
}

type PreviewReport struct {
	Mode              string            `json:"mode"`
	Status            string            `json:"status"`
	SchemaVersion     int               `json:"schema_version"`
	BatchID           string            `json:"batch_id"`
	ProjectID         string            `json:"project_id"`
	SourceDigest      string            `json:"source_digest"`
	ManifestDigest    string            `json:"manifest_digest"`
	CollisionPolicy   string            `json:"collision_policy"`
	ApplyEndpoint     string            `json:"apply_endpoint"`
	ApplyEligible     bool              `json:"apply_eligible"`
	NetworkCalls      int               `json:"network_calls"`
	Counts            PreviewCounts     `json:"counts"`
	Tasks             []PreviewTask     `json:"tasks"`
	Collisions        []Collision       `json:"collisions"`
	RedactionFindings []ValidationIssue `json:"redaction_findings"`
	Blockers          []ValidationIssue `json:"blockers"`
}

func DecodeSnapshot(r io.Reader) (CurrentSnapshot, error) {
	var snapshot CurrentSnapshot
	raw, err := io.ReadAll(io.LimitReader(r, maxSnapshotBytes+1))
	if err != nil {
		return snapshot, fmt.Errorf("read current snapshot: %w", err)
	}
	if len(raw) > maxSnapshotBytes {
		return snapshot, fmt.Errorf("current snapshot exceeds %d bytes", maxSnapshotBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("decode current snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return snapshot, errors.New("decode current snapshot: one JSON value required")
	}
	if snapshot.SchemaVersion != 1 {
		return snapshot, errors.New("current snapshot schema_version must be 1")
	}
	if !projectPattern.MatchString(snapshot.ProjectID) {
		return snapshot, errors.New("current snapshot project_id is invalid")
	}
	seenIDs := map[string]bool{}
	seenKeys := map[string]bool{}
	for i, task := range snapshot.Tasks {
		if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.Title) == "" {
			return snapshot, fmt.Errorf("current snapshot tasks[%d] requires id and title", i)
		}
		if seenIDs[task.ID] {
			return snapshot, fmt.Errorf("current snapshot repeats task id %q", task.ID)
		}
		seenIDs[task.ID] = true
		if task.Key != "" {
			if !keyPattern.MatchString(task.Key) {
				return snapshot, fmt.Errorf("current snapshot tasks[%d] key is invalid", i)
			}
			if seenKeys[task.Key] {
				return snapshot, fmt.Errorf("current snapshot repeats task key %q", task.Key)
			}
			seenKeys[task.Key] = true
		}
	}
	return snapshot, nil
}

func BuildPreview(manifest Manifest, snapshot CurrentSnapshot, externalIssues ...ValidationIssue) (PreviewReport, error) {
	sourceDigest, err := SourceDigest(manifest)
	if err != nil {
		return PreviewReport{}, err
	}
	manifestDigest, err := ManifestDigest(manifest)
	if err != nil {
		return PreviewReport{}, err
	}
	report := PreviewReport{
		Mode: "dry-run", Status: "ready_for_review", SchemaVersion: manifest.SchemaVersion,
		BatchID: manifest.BatchID, ProjectID: manifest.ProjectID, SourceDigest: sourceDigest,
		ManifestDigest: manifestDigest, CollisionPolicy: manifest.CollisionPolicy,
		ApplyEndpoint: "/v1/projects/{project}/historical-imports/apply",
		NetworkCalls:  0, Tasks: []PreviewTask{}, Collisions: []Collision{},
		RedactionFindings: []ValidationIssue{}, Blockers: []ValidationIssue{},
	}
	issues := append(Validate(manifest), externalIssues...)
	for _, issue := range issues {
		if issue.Blocker {
			report.Blockers = append(report.Blockers, issue)
		}
		if isRedactionFinding(issue.Code) {
			report.RedactionFindings = append(report.RedactionFindings, issue)
		}
	}
	if snapshot.ProjectID != "" && manifest.ProjectID != snapshot.ProjectID {
		report.Blockers = append(report.Blockers, ValidationIssue{
			Path: "current_snapshot.project_id", Code: "project_mismatch",
			Message: "current snapshot belongs to a different project", Blocker: true,
		})
	}
	currentByKey := map[string]CurrentTask{}
	currentByTitle := map[string][]CurrentTask{}
	for _, current := range snapshot.Tasks {
		if current.Key != "" {
			currentByKey[current.Key] = current
		}
		normalizedTitle := normalizeTitle(current.Title)
		currentByTitle[normalizedTitle] = append(currentByTitle[normalizedTitle], current)
	}

	distinctTaskSessions := map[string]bool{}
	attributionLinks := 0
	events := 0
	for _, task := range manifest.Tasks {
		item := PreviewTask{
			Key: task.Key, Title: task.Title, Disposition: "create",
			IdempotencyKey:   TaskIdempotencyKey(manifest.BatchID, task.Key, sourceDigest),
			SourceOccurredAt: sourceByRef(manifest.Sources, task.CompletionSourceRef).OccurredAt,
			RecordedAt:       nil, Attributions: len(task.Attributions), Events: len(task.Events),
		}
		attributionLinks += len(task.Attributions)
		events += len(task.Events)
		for _, attribution := range task.Attributions {
			distinctTaskSessions[attribution.SessionID] = true
		}
		current, matchedBy := currentByKey[task.Key], ""
		if current.ID != "" {
			matchedBy = "stable_key"
		} else {
			candidates := currentByTitle[normalizeTitle(task.Title)]
			if len(candidates) > 1 {
				item.Disposition = "blocked_ambiguous"
				report.Counts.Ambiguous++
				report.Blockers = append(report.Blockers, ValidationIssue{
					Path: fmt.Sprintf("tasks[%d].title", len(report.Tasks)), Code: "ambiguous_normalized_title",
					Message: fmt.Sprintf("task %q matches %d current tasks by normalized title without an exact stable key", task.Key, len(candidates)),
					Blocker: true,
				})
				report.Tasks = append(report.Tasks, item)
				continue
			}
			if len(candidates) == 1 {
				current = candidates[0]
				matchedBy = "normalized_title"
			}
		}
		if current.ID != "" {
			item.Disposition = "skip_current"
			report.Collisions = append(report.Collisions, Collision{
				TaskKey: task.Key, MatchedBy: matchedBy, CurrentID: current.ID,
				CurrentTitle: current.Title, Resolution: "current_wins",
			})
			report.Counts.SkippedCurrent++
		} else {
			report.Counts.Planned++
		}
		report.Tasks = append(report.Tasks, item)
	}
	report.Counts.Tasks = len(manifest.Tasks)
	report.Counts.ProjectThreadAliases = len(manifest.ProjectThreadAliases)
	report.Counts.TaskSessions = len(distinctTaskSessions)
	report.Counts.AccountedSessions = len(manifest.ProjectThreadAliases) + len(distinctTaskSessions)
	report.Counts.AttributionLinks = attributionLinks
	report.Counts.Events = events
	report.ApplyEligible = len(report.Blockers) == 0
	if !report.ApplyEligible {
		report.Status = "blocked_pending_evidence"
	}
	sort.Slice(report.Collisions, func(i, j int) bool { return report.Collisions[i].TaskKey < report.Collisions[j].TaskKey })
	return report, nil
}

func TaskIdempotencyKey(batchID, taskKey, sourceDigest string) string {
	suffix := strings.TrimPrefix(sourceDigest, "sha256:")
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	return "historical-import:" + batchID + ":task:" + taskKey + ":" + suffix
}

func BatchIdempotencyKey(batchID, sourceDigest string) string {
	suffix := strings.TrimPrefix(sourceDigest, "sha256:")
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	return "historical-import:" + batchID + ":" + suffix
}

func normalizeTitle(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func isRedactionFinding(code string) bool {
	switch code {
	case "possible_email", "private_path", "generated_image_path", "possible_secret":
		return true
	default:
		return false
	}
}
