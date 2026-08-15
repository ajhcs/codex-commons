package store

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"

	"codex-commons/internal/domain"
)

var (
	nativeGitObjectID = regexp.MustCompile(`^(commit|tree|blob|tag):[0-9a-f]{40}([0-9a-f]{24})?$`)
	nativeCodexID     = regexp.MustCompile(`^(task|thread):[A-Za-z0-9][A-Za-z0-9_-]{7,119}$`)
)

func opaqueNativeStableID(value string) bool {
	if !boundedCoreText(value, 300, true) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"/home/", `\\`, "prompt", "transcript", "secret", "private", "credential", "password", "token", ".env", "id_rsa", "id_ed25519"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func validNativeGitRef(value string) bool {
	if !strings.HasPrefix(value, "ref:refs/") {
		return false
	}
	ref := strings.TrimPrefix(value, "ref:")
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, "//") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.HasSuffix(ref, ".lock") {
		return false
	}
	for _, segment := range strings.Split(ref, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") || strings.HasSuffix(segment, ".lock") {
			return false
		}
		for _, r := range segment {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.+", r)) {
				return false
			}
		}
	}
	return true
}

func validNativeDocumentPath(value string) bool {
	if !opaqueNativeStableID(value) || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~") || strings.Contains(value, ":") || path.Clean(value) != value || value == "." {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return false
		}
	}
	return true
}

func validNativeStableID(kind, value string) bool {
	if !opaqueNativeStableID(value) {
		return false
	}
	switch kind {
	case "git":
		return nativeGitObjectID.MatchString(value) || validNativeGitRef(value)
	case "docs":
		return validNativeDocumentPath(value)
	case "codex_history":
		return nativeCodexID.MatchString(value)
	default:
		return false
	}
}

type nativeProposalSource struct {
	Kind       string    `json:"kind"`
	StableID   string    `json:"stable_id"`
	Digest     string    `json:"digest"`
	OccurredAt time.Time `json:"occurred_at"`
}
type nativeProposalAlias struct {
	Alias   string               `json:"alias"`
	Session string               `json:"session"`
	Source  nativeProposalSource `json:"source"`
}
type nativeProposalAttribution struct {
	Session    string               `json:"session"`
	Role       string               `json:"role"`
	Confidence string               `json:"confidence"`
	Source     nativeProposalSource `json:"source"`
}
type nativeProposalEvent struct {
	Key        string               `json:"key"`
	Kind       string               `json:"kind"`
	Session    string               `json:"session"`
	Confidence string               `json:"confidence"`
	Source     nativeProposalSource `json:"source"`
}
type nativeProposalTask struct {
	Key          string                      `json:"key"`
	Source       nativeProposalSource        `json:"source"`
	Attributions []nativeProposalAttribution `json:"attributions"`
	Events       []nativeProposalEvent       `json:"events"`
}
type nativeProposal struct {
	SchemaVersion        int                   `json:"schema_version"`
	BatchID              string                `json:"batch_id"`
	SourceDigest         string                `json:"source_digest"`
	CollisionPolicy      string                `json:"collision_policy"`
	ProjectThreadAliases []nativeProposalAlias `json:"project_thread_aliases"`
	Tasks                []nativeProposalTask  `json:"tasks"`
}

func validNativeSource(source nativeProposalSource, policy domain.ArchaeologyExecutionPolicy, now string) bool {
	return policy.Allows(source.Kind) && validNativeStableID(source.Kind, source.StableID) &&
		validHistoricalSource(domain.HistoricalSource{Kind: source.Kind, StableID: source.StableID, Digest: source.Digest, OccurredAt: source.OccurredAt}, parseStamp(now))
}

type nativeEvidenceKey struct{ kind, stableID, digest, occurredAt string }

func nativeProposalEvidenceKey(source nativeProposalSource) nativeEvidenceKey {
	return nativeEvidenceKey{source.Kind, source.StableID, source.Digest, source.OccurredAt.UTC().Format(time.RFC3339Nano)}
}

func validNativeProposal(value string, policy domain.ArchaeologyExecutionPolicy, limits domain.ArchaeologyExecutionLimits, now string) (map[string]bool, map[nativeEvidenceKey]bool, bool) {
	var proposal nativeProposal
	if json.Unmarshal([]byte(value), &proposal) != nil || proposal.SchemaVersion != domain.HistoricalImportSchemaVersion || !historicalKeyPattern.MatchString(proposal.BatchID) || !historicalDigestPattern.MatchString(proposal.SourceDigest) || proposal.CollisionPolicy != domain.HistoricalCollisionCurrentWins || len(proposal.Tasks) < 1 || len(proposal.Tasks) > limits.MaxHistoricalTasks || len(proposal.ProjectThreadAliases) > limits.MaxHistoricalAliases {
		return nil, nil, false
	}
	sessions := make(map[string]bool)
	evidence := make(map[nativeEvidenceKey]bool)
	aliasSessions, aliases, taskKeys := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	totalAttributions, totalEvents := 0, 0
	for _, alias := range proposal.ProjectThreadAliases {
		if !historicalKeyPattern.MatchString(alias.Alias) || aliases[alias.Alias] || !boundedCoreText(alias.Session, 200, true) || aliasSessions[alias.Session] || !validNativeSource(alias.Source, policy, now) {
			return nil, nil, false
		}
		evidence[nativeProposalEvidenceKey(alias.Source)] = true
		aliases[alias.Alias], aliasSessions[alias.Session] = true, true
		sessions[alias.Session] = true
	}
	for _, task := range proposal.Tasks {
		if !historicalKeyPattern.MatchString(task.Key) || taskKeys[task.Key] || len(task.Attributions) < 1 || len(task.Attributions) > 2 || len(task.Events) > 1 || !validNativeSource(task.Source, policy, now) {
			return nil, nil, false
		}
		evidence[nativeProposalEvidenceKey(task.Source)] = true
		taskKeys[task.Key] = true
		totalAttributions += len(task.Attributions)
		totalEvents += len(task.Events)
		if totalAttributions > maxHistoricalAttributions || totalEvents > maxHistoricalEvents {
			return nil, nil, false
		}
		attributed := make(map[string]bool)
		for _, attribution := range task.Attributions {
			if !boundedCoreText(attribution.Session, 200, true) || aliasSessions[attribution.Session] || !domain.HistoricalRoles[attribution.Role] || !domain.HistoricalConfidences[attribution.Confidence] || !validNativeSource(attribution.Source, policy, now) {
				return nil, nil, false
			}
			evidence[nativeProposalEvidenceKey(attribution.Source)] = true
			attributed[attribution.Session] = true
			sessions[attribution.Session] = true
		}
		eventKeys := make(map[string]bool)
		for _, event := range task.Events {
			if !historicalKeyPattern.MatchString(event.Key) || eventKeys[event.Key] || !domain.HistoricalEventKinds[event.Kind] || !boundedCoreText(event.Session, 200, false) || aliasSessions[event.Session] || (event.Session != "" && !attributed[event.Session]) || !domain.HistoricalConfidences[event.Confidence] || !validNativeSource(event.Source, policy, now) {
				return nil, nil, false
			}
			evidence[nativeProposalEvidenceKey(event.Source)] = true
			eventKeys[event.Key] = true
			if event.Session != "" {
				sessions[event.Session] = true
			}
		}
	}
	return sessions, evidence, true
}

func validNativeOutcome(outcome domain.ArchaeologyOutcome, policy domain.ArchaeologyExecutionPolicy, limits domain.ArchaeologyExecutionLimits, now string) bool {
	if !validArchaeologyOutcome(outcome, now) || outcome.SourceCount > limits.MaxSourcesExamined || len(outcome.Provenance) > limits.MaxProvenancePerOutcome || len(outcome.Contributors) > limits.MaxContributorsPerOutcome {
		return false
	}
	sessions, proposalEvidence, validProposal := validNativeProposal(outcome.ProposalJSON, policy, limits, now)
	if !validProposal {
		return false
	}
	for _, source := range outcome.Provenance {
		if !policy.Allows(source.Kind) || !validNativeStableID(source.Kind, source.StableID) {
			return false
		}
		delete(proposalEvidence, nativeEvidenceKey{source.Kind, source.StableID, source.Digest, source.OccurredAt.UTC().Format(time.RFC3339Nano)})
	}
	if len(proposalEvidence) != 0 {
		return false
	}
	for _, contributor := range outcome.Contributors {
		if !sessions[contributor.SessionID] {
			return false
		}
	}
	return true
}
