package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	maxIdentifier = 200
	maxText       = 16 << 10
	maxEvidence   = 32
)

type Runner struct {
	clock       Clock
	store       StateStore
	github      GitHubReader
	interpreter Interpreter
	kill        KillSwitch
	granted     CapabilitySet
	definitions map[JobName]Definition
}

func NewRunner(clock Clock, store StateStore, github GitHubReader, interpreter Interpreter, kill KillSwitch, granted CapabilitySet, definitions ...Definition) (*Runner, error) {
	if clock == nil || store == nil || kill == nil {
		return nil, ErrInvalidDefinition
	}
	r := &Runner{clock: clock, store: store, github: github, interpreter: interpreter, kill: kill, granted: granted, definitions: make(map[JobName]Definition)}
	for _, definition := range definitions {
		if err := definition.Validate(); err != nil {
			return nil, err
		}
		if err := validateBuiltinDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := r.definitions[definition.Name]; exists {
			return nil, ErrInvalidDefinition
		}
		r.definitions[definition.Name] = definition
	}
	if len(r.definitions) != 2 || r.definitions[GitHubWatcher].Name == "" || r.definitions[WikiCurator].Name == "" {
		return nil, fmt.Errorf("%w: exactly watcher and curator are required", ErrInvalidDefinition)
	}
	return r, nil
}

func (r *Runner) Run(ctx context.Context, request RunRequest) (Receipt, error) {
	definition, ok := r.definitions[request.Job]
	if !ok || request.RunID == "" || len(request.RunID) > maxIdentifier || strings.TrimSpace(request.RunID) != request.RunID {
		return Receipt{}, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if r.kill.Disabled(request.Job) {
		return Receipt{}, ErrDisabled
	}
	for _, capability := range definition.Capabilities {
		if !r.granted[capability] {
			return Receipt{}, fmt.Errorf("%w: %s", ErrCapability, capability)
		}
	}
	if err := validateInput(request); err != nil {
		return Receipt{}, err
	}

	started := r.clock.Now().UTC()
	digest, err := semanticInputDigest(request)
	if err != nil {
		return Receipt{}, err
	}
	acquired, err := r.store.Acquire(ctx, definition, request.RunID, digest, started, started.Add(definition.MaxWallRuntime))
	if err != nil {
		return Receipt{}, err
	}
	if acquired.Existing != nil {
		return *acquired.Existing, nil
	}
	if !acquired.Acquired {
		return Receipt{}, ErrAlreadyRunning
	}

	runCtx, cancel := context.WithTimeout(ctx, definition.MaxWallRuntime)
	defer cancel()
	receipt := Receipt{RunID: request.RunID, Job: request.Job, Status: "failed", OutputType: definition.AllowedOutput, StartedAt: started}
	executionErr := r.execute(runCtx, definition, request, &receipt)
	if executionErr == nil {
		executionErr = runCtx.Err()
	}
	finished := r.clock.Now().UTC()
	if finished.Sub(started) > definition.MaxWallRuntime {
		executionErr = context.DeadlineExceeded
	}
	if executionErr == nil {
		receipt.Status = "succeeded"
	} else {
		receipt.Error = stableError(executionErr)
	}
	receipt.FinishedAt = finished
	if err := r.store.Finish(context.WithoutCancel(ctx), receipt); err != nil {
		return Receipt{}, err
	}
	return receipt, executionErr
}

// semanticInputDigest uses versioned JSON structs containing no maps. Go's
// encoding/json emits struct fields deterministically, and SHA-256 keeps the
// persistence key bounded without expanding receipts.
func semanticInputDigest(request RunRequest) (string, error) {
	type watcherInputV1 struct {
		Version int        `json:"version"`
		Input   WatchInput `json:"input"`
	}
	type curatorInputV1 struct {
		Version int          `json:"version"`
		Input   ResolvedTask `json:"input"`
	}
	var canonical []byte
	var err error
	switch request.Job {
	case GitHubWatcher:
		canonical, err = json.Marshal(watcherInputV1{Version: 1, Input: *request.Watch})
	case WikiCurator:
		canonical, err = json.Marshal(curatorInputV1{Version: 1, Input: *request.Resolved})
	default:
		return "", ErrInvalidInput
	}
	if err != nil {
		return "", fmt.Errorf("canonical input: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validateBuiltinDefinition(definition Definition) error {
	hasExactly := func(want ...Capability) bool {
		if len(definition.Capabilities) != len(want) {
			return false
		}
		actual := make(map[Capability]bool, len(definition.Capabilities))
		for _, capability := range definition.Capabilities {
			actual[capability] = true
		}
		for _, capability := range want {
			if !actual[capability] {
				return false
			}
		}
		return true
	}
	switch definition.Name {
	case GitHubWatcher:
		if definition.InputScope != watcherInputScope || definition.TokenBudget != 0 || definition.AllowedOutput != OutputSyncReceipt || !hasExactly(CapabilityGitHubRead) {
			return ErrInvalidDefinition
		}
	case WikiCurator:
		if definition.InputScope != curatorInputScope || definition.TokenBudget <= 0 || definition.AllowedOutput != OutputWikiRevisionProposal || !hasExactly(CapabilityInterpret, CapabilityWikiPropose) {
			return ErrInvalidDefinition
		}
	default:
		return ErrInvalidDefinition
	}
	return nil
}

func (r *Runner) execute(ctx context.Context, definition Definition, request RunRequest, receipt *Receipt) error {
	switch request.Job {
	case GitHubWatcher:
		if r.github == nil || definition.TokenBudget != 0 || definition.AllowedOutput != OutputSyncReceipt {
			return ErrInvalidDefinition
		}
		result, err := r.github.Check(ctx, *request.Watch)
		if err != nil {
			return err
		}
		if len(result.Cursor) > maxIdentifier {
			return ErrInvalidInput
		}
		receipt.Cursor, receipt.Changed = result.Cursor, result.Changed
		return nil
	case WikiCurator:
		if r.interpreter == nil || definition.TokenBudget <= 0 || definition.AllowedOutput != OutputWikiRevisionProposal {
			return ErrInvalidDefinition
		}
		proposal, tokens, err := r.interpreter.ProposeWikiRevision(ctx, *request.Resolved, definition.TokenBudget)
		if err != nil {
			return err
		}
		if tokens < 0 || tokens > definition.TokenBudget {
			return ErrBudget
		}
		if proposal.Project != request.Resolved.Project || proposal.TaskID != request.Resolved.TaskID || proposal.Page == "" || len(proposal.Page) > maxIdentifier || proposal.ProposedTitle == "" || len(proposal.ProposedTitle) > maxText || proposal.ProposedBody == "" || len(proposal.ProposedBody) > maxText || len(proposal.Basis) > maxText || proposal.BaseRevision < 0 || !proposal.RequiresReview {
			return ErrInvalidInput
		}
		receipt.TokensUsed, receipt.Proposal = tokens, &proposal
		return nil
	default:
		return ErrInvalidInput
	}
}

func validateInput(request RunRequest) error {
	validID := func(value string) bool {
		return value != "" && len(value) <= maxIdentifier && strings.TrimSpace(value) == value
	}
	switch request.Job {
	case GitHubWatcher:
		if request.Watch == nil || request.Resolved != nil || !validID(request.Watch.Project) || len(request.Watch.Cursor) > maxIdentifier {
			return ErrInvalidInput
		}
	case WikiCurator:
		v := request.Resolved
		if request.Watch != nil || v == nil || !validID(v.Project) || !validID(v.TaskID) || v.State != "resolved" || v.ResolutionRevision <= 0 || v.Title == "" || len(v.Title) > maxText || v.Resolution == "" || len(v.Resolution) > maxText || len(v.Evidence) > maxEvidence {
			return ErrInvalidInput
		}
		for _, evidence := range v.Evidence {
			if evidence == "" || len(evidence) > maxText {
				return ErrInvalidInput
			}
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func stableError(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, ErrBudget):
		return "budget_exceeded"
	case errors.Is(err, ErrInvalidInput):
		return "invalid_output"
	default:
		return "unavailable"
	}
}
