// Package historicalimport validates a bounded, review-first description of
// historical Codex work. It deliberately has no database or network access.
package historicalimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SchemaVersion    = 1
	MaxManifestBytes = 1 << 20
	MaxTasks         = 25
	MaxSessions      = 50
	MaxSources       = 100
)

var (
	keyPattern         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	projectPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
	sessionPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	digestPattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	emailPattern       = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:[\\/][^\s]+`)
	secretPattern      = regexp.MustCompile(`(?i)\b(sk-[a-z0-9_-]{12,}|gh[ps]_[a-z0-9]{20,}|api[_-]?key\s*[:=]|private[_ -]?key\s*[:=])`)
)

type Manifest struct {
	SchemaVersion        int                  `json:"schema_version"`
	BatchID              string               `json:"batch_id"`
	ProjectID            string               `json:"project_id"`
	CollisionPolicy      string               `json:"collision_policy"`
	Expected             ExpectedCounts       `json:"expected"`
	Sources              []Source             `json:"sources"`
	Sessions             []Session            `json:"sessions"`
	ProjectThreadAliases []ProjectThreadAlias `json:"project_thread_aliases"`
	Tasks                []Task               `json:"tasks"`
}

type ExpectedCounts struct {
	Tasks                int `json:"tasks"`
	Sessions             int `json:"sessions"`
	ProjectThreadAliases int `json:"project_thread_aliases"`
	TaskSessions         int `json:"task_sessions"`
}

type ProjectThreadAlias struct {
	Alias     string `json:"alias"`
	SessionID string `json:"session_id"`
	SourceRef string `json:"source_ref"`
}

type Source struct {
	Ref        string `json:"ref"`
	Kind       string `json:"kind"`
	StableID   string `json:"stable_id"`
	Locator    string `json:"locator"`
	Digest     string `json:"digest"`
	OccurredAt string `json:"occurred_at,omitempty"`
	ObservedAt string `json:"observed_at,omitempty"`
	Note       string `json:"note,omitempty"`
}

type Session struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Purpose    string   `json:"purpose"`
	SourceRefs []string `json:"source_refs"`
}

type Task struct {
	Key                 string        `json:"key"`
	Title               string        `json:"title"`
	Description         string        `json:"description"`
	Acceptance          string        `json:"acceptance"`
	State               string        `json:"state"`
	CompletionSourceRef string        `json:"completion_source_ref"`
	SourceRefs          []string      `json:"source_refs"`
	Attributions        []Attribution `json:"attributions"`
	Events              []Event       `json:"events,omitempty"`
}

type Attribution struct {
	SessionID  string `json:"session_id"`
	Role       string `json:"role"`
	Confidence string `json:"confidence"`
	SourceRef  string `json:"source_ref"`
}

type Event struct {
	Key        string `json:"key"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
	OccurredAt string `json:"occurred_at,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Confidence string `json:"confidence"`
	SourceRef  string `json:"source_ref"`
}

type ValidationIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Blocker bool   `json:"blocker"`
}

func Decode(r io.Reader) (Manifest, []byte, error) {
	var manifest Manifest
	raw, err := io.ReadAll(io.LimitReader(r, MaxManifestBytes+1))
	if err != nil {
		return manifest, nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) > MaxManifestBytes {
		return manifest, nil, fmt.Errorf("manifest exceeds %d bytes", MaxManifestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, nil, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return manifest, nil, errors.New("decode manifest: one JSON value required")
	}
	return manifest, raw, nil
}

func Validate(manifest Manifest) []ValidationIssue {
	var issues []ValidationIssue
	add := func(path, code, message string, blocker bool) {
		issues = append(issues, ValidationIssue{Path: path, Code: code, Message: message, Blocker: blocker})
	}
	if manifest.SchemaVersion != SchemaVersion {
		add("schema_version", "invalid_schema", "must be 1", true)
	}
	if !keyPattern.MatchString(manifest.BatchID) {
		add("batch_id", "invalid_key", "must be a lowercase stable key of at most 64 characters", true)
	}
	if !projectPattern.MatchString(manifest.ProjectID) {
		add("project_id", "invalid_project", "must be a lowercase project id", true)
	}
	if manifest.CollisionPolicy != "current_wins" {
		add("collision_policy", "invalid_policy", "must be current_wins", true)
	}
	if len(manifest.Sources) > MaxSources {
		add("sources", "limit", fmt.Sprintf("exceeds %d", MaxSources), true)
	}
	if len(manifest.Sessions) > MaxSessions {
		add("sessions", "limit", fmt.Sprintf("exceeds %d", MaxSessions), true)
	}
	if len(manifest.Tasks) > MaxTasks {
		add("tasks", "limit", fmt.Sprintf("exceeds %d", MaxTasks), true)
	}

	sourceRefs := make(map[string]bool, len(manifest.Sources))
	for i, source := range manifest.Sources {
		path := fmt.Sprintf("sources[%d]", i)
		if !keyPattern.MatchString(source.Ref) {
			add(path+".ref", "invalid_key", "is invalid", true)
		} else if sourceRefs[source.Ref] {
			add(path+".ref", "duplicate", "repeats a source ref", true)
		}
		sourceRefs[source.Ref] = true
		bounded(&issues, path+".kind", source.Kind, 40, true)
		bounded(&issues, path+".stable_id", source.StableID, 300, true)
		bounded(&issues, path+".locator", source.Locator, 300, true)
		bounded(&issues, path+".note", source.Note, 1000, false)
		if !digestPattern.MatchString(source.Digest) {
			add(path+".digest", "invalid_digest", "must be sha256 followed by 64 lowercase hex characters", true)
		}
		if source.OccurredAt != "" && !validTime(source.OccurredAt) {
			add(path+".occurred_at", "invalid_time", "must be RFC3339", true)
		}
		if source.ObservedAt != "" && !validTime(source.ObservedAt) {
			add(path+".observed_at", "invalid_time", "must be RFC3339", true)
		}
		scanText(&issues, path+".kind", source.Kind)
		scanText(&issues, path+".stable_id", source.StableID)
		scanText(&issues, path+".locator", source.Locator)
		scanText(&issues, path+".note", source.Note)
	}

	sessions := make(map[string]bool, len(manifest.Sessions))
	sessionKinds := make(map[string]string, len(manifest.Sessions))
	rootSessionCount := 0
	taskSessionCount := 0
	for i, session := range manifest.Sessions {
		path := fmt.Sprintf("sessions[%d]", i)
		if !sessionPattern.MatchString(session.ID) {
			add(path+".id", "invalid_session", "must be an exact UUID session id", true)
		} else if sessions[session.ID] {
			add(path+".id", "duplicate", "repeats a session id", true)
		}
		sessions[session.ID] = true
		sessionKinds[session.ID] = session.Kind
		if session.Kind != "root_alias" && session.Kind != "subagent" {
			add(path+".kind", "invalid_kind", "must be root_alias or subagent", true)
		} else if session.Kind == "root_alias" {
			rootSessionCount++
		} else {
			taskSessionCount++
		}
		bounded(&issues, path+".purpose", session.Purpose, 300, true)
		checkSourceRefs(&issues, path+".source_refs", session.SourceRefs, sourceRefs)
		scanText(&issues, path+".purpose", session.Purpose)
	}
	if len(manifest.ProjectThreadAliases) > 20 {
		add("project_thread_aliases", "limit", "exceeds 20", true)
	}
	aliasNames := map[string]bool{}
	aliasSessions := map[string]bool{}
	for i, alias := range manifest.ProjectThreadAliases {
		path := fmt.Sprintf("project_thread_aliases[%d]", i)
		if !keyPattern.MatchString(alias.Alias) {
			add(path+".alias", "invalid_key", "must be a stable lowercase key", true)
		} else if aliasNames[alias.Alias] {
			add(path+".alias", "duplicate", "repeats an alias", true)
		}
		aliasNames[alias.Alias] = true
		if aliasSessions[alias.SessionID] {
			add(path+".session_id", "duplicate", "repeats an alias session", true)
		}
		aliasSessions[alias.SessionID] = true
		if sessionKinds[alias.SessionID] != "root_alias" {
			add(path+".session_id", "not_root_alias", "must reference a declared root_alias session", true)
		}
		if !sourceRefs[alias.SourceRef] {
			add(path+".source_ref", "unknown_source", "must reference exactly one declared source", true)
		}
	}
	for sessionID, kind := range sessionKinds {
		if kind == "root_alias" && !aliasSessions[sessionID] {
			add("project_thread_aliases", "missing_root_alias", "does not account for "+sessionID, true)
		}
	}

	taskKeys := make(map[string]bool, len(manifest.Tasks))
	linkedSessions := make(map[string]bool)
	for i, task := range manifest.Tasks {
		path := fmt.Sprintf("tasks[%d]", i)
		if !keyPattern.MatchString(task.Key) {
			add(path+".key", "invalid_key", "is invalid", true)
		} else if taskKeys[task.Key] {
			add(path+".key", "duplicate", "repeats a task key", true)
		}
		taskKeys[task.Key] = true
		bounded(&issues, path+".title", task.Title, 300, true)
		bounded(&issues, path+".description", task.Description, 12000, true)
		bounded(&issues, path+".acceptance", task.Acceptance, 4000, true)
		if task.State != "done" {
			add(path+".state", "invalid_state", "historical imports must be done", true)
		}
		if !sourceRefs[task.CompletionSourceRef] {
			add(path+".completion_source_ref", "unknown_source", "must reference the immutable completion source", true)
		} else if sourceByRef(manifest.Sources, task.CompletionSourceRef).OccurredAt == "" {
			add(path+".completion_source_ref", "unresolved_completion_time", "source requires an immutable occurred_at before apply", true)
		}
		checkSourceRefs(&issues, path+".source_refs", task.SourceRefs, sourceRefs)
		taskSourceRefs := map[string]bool{}
		for _, ref := range task.SourceRefs {
			taskSourceRefs[ref] = true
		}
		if !taskSourceRefs[task.CompletionSourceRef] {
			add(path+".completion_source_ref", "source_not_attached", "must also appear in task source_refs", true)
		}
		if len(task.Attributions) == 0 || len(task.Attributions) > 20 {
			add(path+".attributions", "limit", "must contain 1..20 entries", true)
		}
		seenAttributions := map[string]bool{}
		attributedSessions := map[string]bool{}
		for j, attribution := range task.Attributions {
			itemPath := fmt.Sprintf("%s.attributions[%d]", path, j)
			if !sessions[attribution.SessionID] {
				add(itemPath+".session_id", "unknown_session", "must reference a declared session", true)
			}
			if sessionKinds[attribution.SessionID] == "root_alias" {
				add(itemPath+".session_id", "root_alias_task_link", "project thread aliases cannot be task contributors", true)
			}
			linkKey := attribution.SessionID + "\x00" + attribution.Role
			if seenAttributions[linkKey] {
				add(itemPath, "duplicate", "repeats the session and role", true)
			}
			seenAttributions[linkKey] = true
			attributedSessions[attribution.SessionID] = true
			linkedSessions[attribution.SessionID] = true
			if !oneOf(attribution.Role, "originator", "implementer", "reviewer", "evaluator") {
				add(itemPath+".role", "invalid_role", "is invalid", true)
			}
			if !oneOf(attribution.Confidence, "verified", "supported", "uncertain") {
				add(itemPath+".confidence", "invalid_confidence", "is invalid", true)
			}
			if !sourceRefs[attribution.SourceRef] {
				add(itemPath+".source_ref", "unknown_source", "must reference exactly one declared source", true)
			}
			if !taskSourceRefs[attribution.SourceRef] {
				add(itemPath+".source_ref", "source_not_attached", "must also appear in task source_refs", true)
			}
		}
		if len(task.Events) > 25 {
			add(path+".events", "limit", "exceeds 25", true)
		}
		eventKeys := map[string]bool{}
		for j, event := range task.Events {
			itemPath := fmt.Sprintf("%s.events[%d]", path, j)
			if !keyPattern.MatchString(event.Key) || eventKeys[event.Key] {
				add(itemPath+".key", "invalid_or_duplicate_key", "must be a unique stable key", true)
			}
			eventKeys[event.Key] = true
			if !oneOf(event.Kind, "completed", "reviewed", "failed", "retried", "remediated", "evaluated") {
				add(itemPath+".kind", "invalid_kind", "is invalid", true)
			}
			bounded(&issues, itemPath+".summary", event.Summary, 1000, true)
			if event.OccurredAt != "" && !validTime(event.OccurredAt) {
				add(itemPath+".occurred_at", "invalid_time", "must be RFC3339", true)
			}
			if !sourceRefs[event.SourceRef] {
				add(itemPath+".source_ref", "unknown_source", "must reference exactly one declared source", true)
			} else if event.OccurredAt == "" && sourceByRef(manifest.Sources, event.SourceRef).OccurredAt == "" {
				add(itemPath+".occurred_at", "unresolved_event_time", "source requires an immutable occurred_at before apply", true)
			}
			if event.SessionID != "" && !sessions[event.SessionID] {
				add(itemPath+".session_id", "unknown_session", "must reference a declared session", true)
			}
			if event.SessionID != "" && sessionKinds[event.SessionID] == "root_alias" {
				add(itemPath+".session_id", "root_alias_task_link", "project thread aliases cannot author task events", true)
			}
			if event.SessionID != "" && !attributedSessions[event.SessionID] {
				add(itemPath+".session_id", "event_session_not_attributed", "must also appear in this task's attributions", true)
			}
			if !oneOf(event.Confidence, "verified", "supported", "uncertain") {
				add(itemPath+".confidence", "invalid_confidence", "is invalid", true)
			}
			scanText(&issues, itemPath+".summary", event.Summary)
		}
		scanText(&issues, path+".title", task.Title)
		scanText(&issues, path+".description", task.Description)
		scanText(&issues, path+".acceptance", task.Acceptance)
	}
	if manifest.Expected.Tasks != len(manifest.Tasks) {
		add("expected.tasks", "count_mismatch", fmt.Sprintf("expected %d but found %d", manifest.Expected.Tasks, len(manifest.Tasks)), true)
	}
	if manifest.Expected.Sessions != len(manifest.Sessions) {
		add("expected.sessions", "count_mismatch", fmt.Sprintf("expected %d but found %d", manifest.Expected.Sessions, len(manifest.Sessions)), true)
	}
	if manifest.Expected.ProjectThreadAliases != len(manifest.ProjectThreadAliases) {
		add("expected.project_thread_aliases", "count_mismatch", fmt.Sprintf("expected %d but found %d", manifest.Expected.ProjectThreadAliases, len(manifest.ProjectThreadAliases)), true)
	}
	if manifest.Expected.TaskSessions != taskSessionCount {
		add("expected.task_sessions", "count_mismatch", fmt.Sprintf("expected %d but found %d", manifest.Expected.TaskSessions, taskSessionCount), true)
	}
	if rootSessionCount != len(aliasSessions) {
		add("project_thread_aliases", "count_mismatch", fmt.Sprintf("found %d root sessions but %d aliases", rootSessionCount, len(aliasSessions)), true)
	}
	for id, kind := range sessionKinds {
		if kind == "subagent" && !linkedSessions[id] {
			add("sessions", "unlinked_session", "session "+id+" is not attributed to any task", true)
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func SourceDigest(manifest Manifest) (string, error) {
	type digestInput struct {
		SchemaVersion        int                  `json:"schema_version"`
		ProjectID            string               `json:"project_id"`
		Sources              []Source             `json:"sources"`
		Sessions             []Session            `json:"sessions"`
		ProjectThreadAliases []ProjectThreadAlias `json:"project_thread_aliases"`
		Tasks                []Task               `json:"tasks"`
	}
	input := digestInput{
		SchemaVersion: manifest.SchemaVersion, ProjectID: manifest.ProjectID,
		Sources:              append([]Source(nil), manifest.Sources...),
		Sessions:             append([]Session(nil), manifest.Sessions...),
		ProjectThreadAliases: append([]ProjectThreadAlias(nil), manifest.ProjectThreadAliases...),
		Tasks:                make([]Task, len(manifest.Tasks)),
	}
	sort.Slice(input.Sources, func(i, j int) bool { return input.Sources[i].Ref < input.Sources[j].Ref })
	for i := range input.Sessions {
		input.Sessions[i].SourceRefs = append([]string(nil), input.Sessions[i].SourceRefs...)
		sort.Strings(input.Sessions[i].SourceRefs)
	}
	sort.Slice(input.Sessions, func(i, j int) bool { return input.Sessions[i].ID < input.Sessions[j].ID })
	sort.Slice(input.ProjectThreadAliases, func(i, j int) bool { return input.ProjectThreadAliases[i].Alias < input.ProjectThreadAliases[j].Alias })
	for i, task := range manifest.Tasks {
		task.SourceRefs = append([]string(nil), task.SourceRefs...)
		sort.Strings(task.SourceRefs)
		task.Attributions = append([]Attribution(nil), task.Attributions...)
		sort.Slice(task.Attributions, func(i, j int) bool {
			return task.Attributions[i].SessionID+"\x00"+task.Attributions[i].Role < task.Attributions[j].SessionID+"\x00"+task.Attributions[j].Role
		})
		task.Events = append([]Event(nil), task.Events...)
		sort.Slice(task.Events, func(i, j int) bool { return task.Events[i].Key < task.Events[j].Key })
		input.Tasks[i] = task
	}
	sort.Slice(input.Tasks, func(i, j int) bool { return input.Tasks[i].Key < input.Tasks[j].Key })
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ManifestDigest(manifest Manifest) (string, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func bounded(issues *[]ValidationIssue, path, value string, limit int, required bool) {
	if required && strings.TrimSpace(value) == "" {
		*issues = append(*issues, ValidationIssue{Path: path, Code: "required", Message: "is required", Blocker: true})
		return
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || utf8.RuneCountInString(value) > limit {
		*issues = append(*issues, ValidationIssue{Path: path, Code: "invalid_text", Message: fmt.Sprintf("must be valid text of at most %d characters", limit), Blocker: true})
	}
}

func checkSourceRefs(issues *[]ValidationIssue, path string, refs []string, sources map[string]bool) {
	if len(refs) == 0 || len(refs) > 20 {
		*issues = append(*issues, ValidationIssue{Path: path, Code: "limit", Message: "must contain 1..20 source refs", Blocker: true})
		return
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[ref] {
			*issues = append(*issues, ValidationIssue{Path: path, Code: "duplicate_source", Message: "repeats " + ref, Blocker: true})
		}
		seen[ref] = true
		if !sources[ref] {
			*issues = append(*issues, ValidationIssue{Path: path, Code: "unknown_source", Message: "references " + ref, Blocker: true})
		}
	}
}

func validTime(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func sourceByRef(sources []Source, ref string) Source {
	for _, source := range sources {
		if source.Ref == ref {
			return source
		}
	}
	return Source{}
}

func scanText(issues *[]ValidationIssue, path, value string) {
	if value == "" {
		return
	}
	lower := strings.ToLower(value)
	switch {
	case emailPattern.MatchString(value):
		*issues = append(*issues, ValidationIssue{
			Path: path, Code: "possible_email", Message: "contains an email address; redact or replace it with a safe actor label", Blocker: true,
		})
	case strings.Contains(lower, ".codex/generated_images/"):
		*issues = append(*issues, ValidationIssue{
			Path: path, Code: "generated_image_path", Message: "contains a generated-image absolute path", Blocker: true,
		})
	case strings.Contains(lower, "/home/") || strings.Contains(lower, "/users/") ||
		strings.Contains(lower, "/root/") || strings.Contains(lower, "/mnt/") ||
		windowsPathPattern.MatchString(value):
		*issues = append(*issues, ValidationIssue{
			Path: path, Code: "private_path", Message: "contains a private absolute path; use a repository-relative or typed source locator", Blocker: true,
		})
	case secretPattern.MatchString(value):
		*issues = append(*issues, ValidationIssue{
			Path: path, Code: "possible_secret", Message: "contains text resembling a credential or private key", Blocker: true,
		})
	}
}
