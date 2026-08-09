// Package jobs defines bounded, explicitly-triggered background work. It does
// not start goroutines, poll, write GitHub, edit the canonical wiki, or expose
// any facility for creating jobs or waking agents.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type JobName string
type Capability string
type OutputType string

const (
	GitHubWatcher JobName = "github_watcher"
	WikiCurator   JobName = "wiki_curator"

	CapabilityGitHubRead       Capability = "github.read"
	CapabilityInterpret        Capability = "model.interpret"
	CapabilityWikiPropose      Capability = "wiki.propose"
	CapabilityCreateJobs       Capability = "jobs.create"
	CapabilityWakeAgents       Capability = "agents.wake"
	OutputSyncReceipt          OutputType = "sync_receipt"
	OutputWikiRevisionProposal OutputType = "wiki_revision_proposal"

	watcherInputScope = "one project and one upstream cursor"
	curatorInputScope = "one resolved task and its bounded resolution evidence"
)

var (
	ErrInvalidDefinition   = errors.New("invalid job definition")
	ErrInvalidInput        = errors.New("input outside job scope")
	ErrDisabled            = errors.New("job disabled")
	ErrCapability          = errors.New("capability denied")
	ErrBudget              = errors.New("budget exceeded")
	ErrAlreadyRunning      = errors.New("job already running")
	ErrTooSoon             = errors.New("minimum interval has not elapsed")
	ErrIdempotencyConflict = errors.New("run id input conflict")
)

// Definition is the complete authority envelope for a job. Implementations
// may narrow it but cannot add capabilities or output types at run time.
type Definition struct {
	Name            JobName
	InputScope      string
	Capabilities    []Capability
	MaxWallRuntime  time.Duration
	TokenBudget     int
	MinimumInterval time.Duration
	AllowedOutput   OutputType
}

func (d Definition) Validate() error {
	if d.Name == "" || d.InputScope == "" || d.MaxWallRuntime <= 0 || d.TokenBudget < 0 || d.MinimumInterval < 0 || d.AllowedOutput == "" {
		return ErrInvalidDefinition
	}
	seen := make(map[Capability]bool, len(d.Capabilities))
	for _, capability := range d.Capabilities {
		if capability == "" || seen[capability] || capability == CapabilityCreateJobs || capability == CapabilityWakeAgents {
			return fmt.Errorf("%w: %q", ErrInvalidDefinition, capability)
		}
		seen[capability] = true
	}
	return nil
}

func WatcherDefinition(minimumInterval, maxRuntime time.Duration) Definition {
	return Definition{Name: GitHubWatcher, InputScope: watcherInputScope, Capabilities: []Capability{CapabilityGitHubRead}, MaxWallRuntime: maxRuntime, TokenBudget: 0, MinimumInterval: minimumInterval, AllowedOutput: OutputSyncReceipt}
}

func CuratorDefinition(minimumInterval, maxRuntime time.Duration, tokenBudget int) Definition {
	return Definition{Name: WikiCurator, InputScope: curatorInputScope, Capabilities: []Capability{CapabilityInterpret, CapabilityWikiPropose}, MaxWallRuntime: maxRuntime, TokenBudget: tokenBudget, MinimumInterval: minimumInterval, AllowedOutput: OutputWikiRevisionProposal}
}

type WatchInput struct {
	Project string `json:"project"`
	Cursor  string `json:"cursor,omitempty"`
}

type ResolvedTask struct {
	Project            string   `json:"project"`
	TaskID             string   `json:"task_id"`
	State              string   `json:"state"`
	ResolutionRevision int64    `json:"resolution_revision"`
	Title              string   `json:"title"`
	Resolution         string   `json:"resolution"`
	Evidence           []string `json:"evidence,omitempty"`
}

type RunRequest struct {
	RunID    string        `json:"run_id"`
	Job      JobName       `json:"job"`
	Watch    *WatchInput   `json:"watch,omitempty"`
	Resolved *ResolvedTask `json:"resolved,omitempty"`
}

type WikiProposal struct {
	Project        string `json:"project"`
	TaskID         string `json:"task_id"`
	Page           string `json:"page"`
	BaseRevision   int64  `json:"base_revision"`
	ProposedTitle  string `json:"proposed_title"`
	ProposedBody   string `json:"proposed_body"`
	Basis          string `json:"basis"`
	RequiresReview bool   `json:"requires_review"`
}

// Receipt is a durable, JSON-serializable record. Proposal is review material,
// not an instruction to mutate the canonical wiki.
type Receipt struct {
	RunID      string        `json:"run_id"`
	Job        JobName       `json:"job"`
	Status     string        `json:"status"`
	OutputType OutputType    `json:"output_type"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Cursor     string        `json:"cursor,omitempty"`
	Changed    bool          `json:"changed,omitempty"`
	TokensUsed int           `json:"tokens_used,omitempty"`
	Error      string        `json:"error,omitempty"`
	Proposal   *WikiProposal `json:"proposal,omitempty"`
}

func (r Receipt) Marshal() ([]byte, error) { return json.Marshal(r) }

type Clock interface{ Now() time.Time }

type GitHubReader interface {
	Check(context.Context, WatchInput) (SyncResult, error)
}

type SyncResult struct {
	Cursor  string
	Changed bool
}

type Interpreter interface {
	ProposeWikiRevision(context.Context, ResolvedTask, int) (WikiProposal, int, error)
}

type KillSwitch interface {
	Disabled(JobName) bool
}

type CapabilitySet map[Capability]bool

// StateStore is deliberately persistence-shaped: acquisition must atomically
// enforce lease, interval, and completed-run idempotency.
type StateStore interface {
	Acquire(context.Context, Definition, string, string, time.Time, time.Time) (AcquireResult, error)
	Finish(context.Context, Receipt) error
}

type AcquireResult struct {
	Acquired bool
	Existing *Receipt
}
