package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"codex-commons/internal/domain"
)

type ArchaeologyNativeRepository interface {
	ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error)
	QueueArchaeologyNativeBatch(context.Context, domain.ArchaeologyNativeBatchRequest) (domain.ArchaeologySession, error)
	ClaimArchaeologyNativeJob(context.Context) (domain.ArchaeologyNativeJob, error)
	BindArchaeologyNativeJob(context.Context, string, string, string, string) error
	BindArchaeologyNativeIdentity(context.Context, string, string, string, string) error
	ActivateArchaeologyNativeJob(context.Context, string, string, string) error
	FailArchaeologyNativeStart(context.Context, string, domain.ArchaeologyLaunchResult, bool) error
	UpdateArchaeologyNativeProgress(context.Context, domain.ArchaeologyNativeProgress) error
	ReportArchaeologyNativeJob(context.Context, domain.ArchaeologyNativeReport) error
	CompleteArchaeologyNativeTurn(context.Context, domain.ArchaeologyNativeTerminal) error
	CancelArchaeologyNativeBatch(context.Context, string, string, int64) ([]domain.ArchaeologyNativeJob, domain.ArchaeologySession, error)
	LoseArchaeologyNativeTurn(context.Context, string, string, string) error
}

type ArchaeologyNativeToolCall struct {
	ThreadID, TurnID, Tool string
	Arguments              []byte
}
type ArchaeologyNativeToolResponse struct {
	Success bool
	Message string
}
type ArchaeologyNativeLauncher interface {
	Available(context.Context) error
	LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error)
	InterruptNative(context.Context, domain.ArchaeologyNativeJob) error
	FinalizeNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate, domain.ArchaeologyLaunchResult) error
}
type ArchaeologyNativeResolutionRepository interface {
	ResolveArchaeologyNativeUncertainty(context.Context, domain.ArchaeologyNativeResolution) (domain.ArchaeologySession, error)
}
type ArchaeologyNativeIdentityRepository interface {
	BindArchaeologyNativeUncertainty(context.Context, string, domain.ArchaeologyLaunchResult) error
}
type ArchaeologyNativeIdentityReconciler interface {
	RecoverNativeIdentity(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologyCandidate) (domain.ArchaeologyLaunchResult, bool, error)
}

// ArchaeologyNativePersistenceReconciler is the explicit, verified recovery
// seam for a scheduler persistence fault. A successful call must establish
// that the repository is readable and that any post-claim state is safe to
// resume. It intentionally does not prescribe a durable retry queue.
type ArchaeologyNativePersistenceReconciler interface {
	ReconcileArchaeologyNativePersistence(context.Context) error
}

// ErrArchaeologySchedulerPersistenceFault is deliberately generic. The
// scheduler retains the underlying error for process-local diagnostics but
// never exposes database or transport details as a public status value.
var ErrArchaeologySchedulerPersistenceFault = errors.New("archaeology scheduler persistence fault")

type ArchaeologySchedulerStatus struct {
	PersistenceFault bool   `json:"persistence_fault"`
	Error            string `json:"error,omitempty"`
}

type ArchaeologyScheduler struct {
	service    *Service
	repository ArchaeologyNativeRepository
	launcher   ArchaeologyNativeLauncher
	principal  string
	ctx        context.Context
	cancel     context.CancelFunc
	wake       chan struct{}
	wg         sync.WaitGroup
	callbackMu sync.Mutex
	callbackWG sync.WaitGroup
	drainMu    sync.Mutex
	stateMu    sync.RWMutex
	// persistenceFault is process-local by design. A restart must run the
	// existing durable reconciliation before constructing a new scheduler.
	persistenceFault error
	persistenceSeq   uint64
	closing          bool
}

func newArchaeologyScheduler(parent context.Context, service *Service, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string) *ArchaeologyScheduler {
	ctx, cancel := context.WithCancel(parent)
	s := &ArchaeologyScheduler{service: service, repository: repository, launcher: launcher, principal: principal, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	s.wg.Add(1)
	go s.loop()
	return s
}

func (s *ArchaeologyScheduler) persistenceBlocked() bool {
	if s == nil {
		return true
	}
	s.stateMu.RLock()
	blocked := s.persistenceFault != nil
	s.stateMu.RUnlock()
	return blocked
}

// recordPersistenceFailure latches the first post-claim repository failure.
// The underlying error is retained only in memory for diagnostics; callers
// observe ErrArchaeologySchedulerPersistenceFault instead.
func (s *ArchaeologyScheduler) recordPersistenceFailure(err error) error {
	if s == nil || err == nil {
		return err
	}
	s.stateMu.Lock()
	if s.persistenceFault == nil {
		s.persistenceFault = err
	}
	s.persistenceSeq++
	s.stateMu.Unlock()
	return err
}

func (s *ArchaeologyScheduler) PersistenceError() error {
	if s == nil {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.stateMu.RLock()
	faulted := s.persistenceFault != nil
	s.stateMu.RUnlock()
	if faulted {
		return ErrArchaeologySchedulerPersistenceFault
	}
	return nil
}

// PersistenceFault is an alias-shaped status accessor for callers that want
// to treat the latch as an error rather than inspect Status.
func (s *ArchaeologyScheduler) PersistenceFault() error {
	return s.PersistenceError()
}

func (s *ArchaeologyScheduler) Status() ArchaeologySchedulerStatus {
	if s == nil {
		return ArchaeologySchedulerStatus{PersistenceFault: true, Error: ErrArchaeologySchedulerPersistenceFault.Error()}
	}
	if s.PersistenceError() != nil {
		return ArchaeologySchedulerStatus{PersistenceFault: true, Error: ErrArchaeologySchedulerPersistenceFault.Error()}
	}
	return ArchaeologySchedulerStatus{}
}

// ClearPersistenceFault clears the in-memory latch only after the caller's
// verified reconciliation function succeeds. A bare reset is intentionally
// unavailable: the scheduler must not resume claiming against an unknown
// repository state.
func (s *ArchaeologyScheduler) ClearPersistenceFault(ctx context.Context, verify func(context.Context) error) error {
	if s == nil || ctx == nil || verify == nil {
		return domain.ErrInvalid
	}
	s.stateMu.RLock()
	sequence := s.persistenceSeq
	s.stateMu.RUnlock()
	if err := verify(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.persistenceSeq != sequence {
		return ErrArchaeologySchedulerPersistenceFault
	}
	s.persistenceFault = nil
	s.Wake()
	return nil
}

// ReconcilePersistence is the repository-backed clearing seam. Repositories
// that can perform a verified, read-only reconciliation may opt into
// ArchaeologyNativePersistenceReconciler; absent that capability, the safe
// result is ErrUnavailable and the latch remains set.
func (s *ArchaeologyScheduler) ReconcilePersistence(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return domain.ErrUnavailable
	}
	reconciler, ok := s.repository.(ArchaeologyNativePersistenceReconciler)
	if !ok {
		return domain.ErrUnavailable
	}
	return s.ClearPersistenceFault(ctx, reconciler.ReconcileArchaeologyNativePersistence)
}

func (s *ArchaeologyScheduler) Available(ctx context.Context) error {
	if s == nil || s.launcher == nil {
		return domain.ErrUnavailable
	}
	if s.persistenceBlocked() {
		return ErrArchaeologySchedulerPersistenceFault
	}
	return s.launcher.Available(ctx)
}
func (s *ArchaeologyScheduler) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}

// Wake is the bounded availability/recovery signal. Callers may invoke it
// from a Codex process-recovery callback or after queueing a batch; it never
// blocks and never performs a claim itself.
func (s *ArchaeologyScheduler) Wake() {
	if s == nil || s.wake == nil {
		return
	}
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *ArchaeologyScheduler) Close() {
	if s == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.callbackMu.Lock()
	s.closing = true
	s.callbackMu.Unlock()
	s.callbackWG.Wait()
}
func (s *ArchaeologyScheduler) beginCallback() bool {
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	if s.closing {
		return false
	}
	s.callbackWG.Add(1)
	return true
}
func (s *ArchaeologyScheduler) loop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.wake:
			s.drain()
		}
	}
}
func (s *ArchaeologyScheduler) drain() {
	if s == nil {
		return
	}
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	for s.ctx == nil || s.ctx.Err() == nil {
		if s.persistenceBlocked() {
			return
		}
		// Availability is deliberately checked for every individual claim. A
		// recovery attempt may be in progress or exhausted even when a prior
		// wake observed a usable launcher; queued rows must remain queued until
		// the next explicit availability wake.
		if err := s.Available(s.ctx); err != nil {
			return
		}
		job, err := s.repository.ClaimArchaeologyNativeJob(s.ctx)
		if err != nil {
			// ErrConflict is the store's bounded "nothing claimable" result,
			// including the durable uncertainty gate. Any other claim error may
			// leave the transaction or connection state unknown and must stop the
			// scheduler behind the same persistence fault latch as later writes.
			if !errors.Is(err, domain.ErrConflict) {
				s.recordPersistenceFailure(err)
			}
			return
		}
		s.launch(job)
	}
}

func (s *ArchaeologyScheduler) failStart(ctx context.Context, jobID string, result domain.ArchaeologyLaunchResult, uncertain bool) error {
	if s == nil || s.repository == nil {
		return s.recordPersistenceFailure(domain.ErrUnavailable)
	}
	return s.recordPersistenceFailure(s.repository.FailArchaeologyNativeStart(ctx, jobID, result, uncertain))
}

func (s *ArchaeologyScheduler) launch(job domain.ArchaeologyNativeJob) {
	session, err := s.repository.ArchaeologySession(s.ctx, s.principal)
	if err != nil {
		_ = s.failStart(s.ctx, job.ID, domain.ArchaeologyLaunchResult{}, false)
		return
	}
	var candidate domain.ArchaeologyCandidate
	found := false
	for _, item := range session.Candidates {
		if item.ID == job.CandidateID {
			candidate = item
			found = true
			break
		}
	}
	if !found {
		_ = s.failStart(s.ctx, job.ID, domain.ArchaeologyLaunchResult{}, false)
		return
	}
	bound := make(chan struct{})
	var boundOnce sync.Once
	closeBound := func() { boundOnce.Do(func() { close(bound) }) }
	var bindErr error
	onTool := func(ctx context.Context, call ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse {
		select {
		case <-ctx.Done():
			return ArchaeologyNativeToolResponse{}
		case <-bound:
		}
		if bindErr != nil || !s.beginCallback() {
			return ArchaeologyNativeToolResponse{}
		}
		defer s.callbackWG.Done()
		return s.handleTool(ctx, job, call)
	}
	onTerminal := func(terminal domain.ArchaeologyNativeTerminal) {
		if !s.beginCallback() {
			return
		}
		go func() {
			defer s.callbackWG.Done()
			select {
			case <-s.ctx.Done():
				return
			case <-bound:
			}
			// A prior persistence fault blocks new claims, but it must not
			// suppress a best-effort terminal write for this already-claimed job.
			// Attempt it and keep the fault latched if the repository still fails.
			if bindErr == nil {
				terminal.JobID = job.ID
				if terminal.Status == "unavailable" {
					_ = s.recordPersistenceFailure(s.repository.LoseArchaeologyNativeTurn(s.ctx, job.ID, terminal.ThreadID, terminal.TurnID))
				} else {
					_ = s.recordPersistenceFailure(s.repository.CompleteArchaeologyNativeTurn(s.ctx, terminal))
				}
				s.Wake()
			}
		}()
	}
	result, launchErr := s.launcher.LaunchNative(s.ctx, job, session, candidate, onTool, onTerminal)
	if launchErr != nil {
		bindErr = launchErr
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, result.State == "uncertain" || result.ThreadID != "")
		closeBound()
		// Once an exact turn exists, any later protocol/visibility failure is
		// post-acceptance. Persist uncertainty first, then interrupt that exact
		// turn once. Never retry the non-idempotent launch boundary.
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		}
		return
	}
	bindErr = s.repository.BindArchaeologyNativeIdentity(s.ctx, job.ID, result.ThreadID, result.CodexSessionID, result.TurnID)
	bindErr = s.recordPersistenceFailure(bindErr)
	if bindErr != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		// A task accepted by Codex but not durably bound must never become an
		// invisible orphan. Persist its exact identity above, then make one
		// best-effort interrupt of the known turn. The durable state remains
		// uncertain until a human reconciles it, regardless of interrupt result.
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{
				ID:             job.ID,
				ThreadID:       result.ThreadID,
				CodexSessionID: result.CodexSessionID,
				TurnID:         result.TurnID,
			})
		}
		return
	}
	bindErr = s.launcher.FinalizeNative(s.ctx, job, candidate, result)
	if bindErr != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		return
	}
	bindErr = s.repository.ActivateArchaeologyNativeJob(s.ctx, job.ID, result.ThreadID, result.TurnID)
	bindErr = s.recordPersistenceFailure(bindErr)
	if bindErr != nil {
		closeBound()
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.failStart(markCtx, job.ID, result, true)
		_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		return
	}
	closeBound()
}
func decodeStrictOne(body []byte, target any) error {
	return decodeStrictOneLimit(body, target, 64<<10)
}
func decodeStrictOneLimit(body []byte, target any, limit int) error {
	if len(body) == 0 || len(body) > limit {
		return domain.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return domain.ErrInvalid
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.ErrInvalid
	}
	return nil
}
func (s *ArchaeologyScheduler) handleTool(ctx context.Context, job domain.ArchaeologyNativeJob, call ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse {
	if call.ThreadID == "" || call.TurnID == "" {
		return ArchaeologyNativeToolResponse{}
	}
	switch call.Tool {
	case "commons_project_history_progress":
		var input struct {
			Phase           string `json:"phase"`
			SourcesExamined int    `json:"sources_examined"`
			Note            string `json:"note"`
		}
		limits, validPolicy := job.Policy.Limits()
		if decodeStrictOne(call.Arguments, &input) != nil || len(input.Note) > 500 || !validPolicy || input.SourcesExamined < 0 || input.SourcesExamined > limits.MaxSourcesExamined {
			return ArchaeologyNativeToolResponse{}
		}
		labels := map[string]string{"inspecting_sources": "Inspecting selected sources", "building_proposals": "Building history proposals", "ready_to_report": "Preparing review report"}
		label := labels[input.Phase]
		if label == "" {
			return ArchaeologyNativeToolResponse{}
		}
		if s.recordPersistenceFailure(s.repository.UpdateArchaeologyNativeProgress(ctx, domain.ArchaeologyNativeProgress{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, PhaseLabel: label, SourcesExamined: input.SourcesExamined})) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		return ArchaeologyNativeToolResponse{Success: true, Message: `{"accepted":true}`}
	case "commons_project_history_report":
		var envelope struct {
			Outcomes []json.RawMessage `json:"outcomes"`
		}
		limits, validPolicy := job.Policy.Limits()
		if decodeStrictOneLimit(call.Arguments, &envelope, domain.ArchaeologyNativeReportMaxBytes) != nil || !validPolicy || len(envelope.Outcomes) == 0 || len(envelope.Outcomes) > limits.MaxOutcomes {
			return ArchaeologyNativeToolResponse{}
		}
		for _, rawOutcome := range envelope.Outcomes {
			var fields map[string]json.RawMessage
			if decodeStrictOneLimit(rawOutcome, &fields, domain.ArchaeologyNativeProposalMaxBytes+16<<10) != nil {
				return ArchaeologyNativeToolResponse{}
			}
			if _, supplied := fields["project_id"]; supplied {
				return ArchaeologyNativeToolResponse{}
			}
		}
		var input struct {
			Outcomes []ArchaeologyOutcomeReportRequest `json:"outcomes"`
		}
		if decodeStrictOneLimit(call.Arguments, &input, domain.ArchaeologyNativeReportMaxBytes) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for index := range input.Outcomes {
			if input.Outcomes[index].HistoricalImport.BatchID != "" {
				return ArchaeologyNativeToolResponse{}
			}
			input.Outcomes[index].ProjectID = job.ProjectID
			input.Outcomes[index].HistoricalImport.BatchID = nativeHistoricalImportBatchID(job.ID, index)
		}
		outcomes, err := s.service.archaeologyReportOutcomes(ctx, input.Outcomes)
		if err != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for _, outcome := range outcomes {
			if outcome.ProjectID != job.ProjectID {
				return ArchaeologyNativeToolResponse{}
			}
		}
		digest := sha256.Sum256(call.Arguments)
		if err = s.recordPersistenceFailure(s.repository.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, Digest: digest, Outcomes: outcomes})); err != nil {
			return ArchaeologyNativeToolResponse{}
		}
		return ArchaeologyNativeToolResponse{Success: true, Message: `{"accepted":true,"canonical_apply":false}`}
	default:
		return ArchaeologyNativeToolResponse{}
	}
}

func nativeHistoricalImportBatchID(jobID string, index int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", jobID, index)))
	return fmt.Sprintf("native-%x", digest[:12])
}

func HistorianTitle(name, jobID string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "Project"
	}
	jobID = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, jobID))
	if jobID == "" {
		return ""
	}
	suffix := " · " + jobID
	prefix := "Project history · " + name
	maximum := 200 - len(suffix)
	if maximum <= 0 {
		return ""
	}
	if len(prefix) > maximum {
		prefix = prefix[:maximum]
		for !utf8.ValidString(prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix + suffix
}

func HistorianVisibleTitle(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "Project"
	}
	title := "Project history · " + name
	if len(title) > 200 {
		title = title[:200]
		for !utf8.ValidString(title) {
			title = title[:len(title)-1]
		}
	}
	return title
}
func HistorianPrompt(policy domain.ArchaeologyExecutionPolicy) string {
	limits, ok := policy.Limits()
	if !ok {
		return ""
	}
	allowed := make([]string, 0, 3)
	forbidden := make([]string, 0, 3)
	stableRules := make([]string, 0, 3)
	for _, kind := range []string{"git", "docs", "codex_history"} {
		if policy.Allows(kind) {
			allowed = append(allowed, kind)
			switch kind {
			case "git":
				stableRules = append(stableRules, "git objects as commit|tree|blob|tag:<40-or-64 lowercase hex> or refs as ref:refs/<name>, with no traversal, repeated slash, dot/hidden/.lock segments, @{, or trailing dot/slash")
			case "docs":
				stableRules = append(stableRules, "docs as normalized repository-relative paths with no absolute path, traversal, hidden/private credential filename, prompt text, or control characters")
			case "codex_history":
				stableRules = append(stableRules, "Codex history as task:<id> or thread:<id>, never a title, prompt, transcript, or path")
			}
		} else {
			forbidden = append(forbidden, kind)
		}
	}
	forbiddenText := "none"
	if len(forbidden) > 0 {
		forbiddenText = strings.Join(forbidden, ", ")
	}
	return fmt.Sprintf("Review the selected Codex project read-only for source-grounded historical Tasks and provenance. Execution depth is %s. The only admissible evidence kinds are: %s. Forbidden evidence kinds are: %s. Source selections govern evidence you may cite; they do not imply filesystem isolation. Never cite a disabled kind. Stable IDs for allowed evidence must be: %s. Commons binds project_id and historical_import.batch_id; do not supply either field. Hard limits: at most %d outcomes, %d provenance records and %d contributors per outcome, %d aliases, %d historical tasks, two attributions and one event per task, and %d examined sources. The complete report must be below 60 KiB and each historical_import below 32 KiB. Outcome titles, task keys, event keys, aliases, outer provenance records, and contributor session IDs must be unique in their applicable arrays. Every nested source must exactly match one outer provenance record. Alias sessions must be unique and cannot appear in any task attribution or event. An event session, when present, must exactly match an attribution session on that same task. Only report exact contributor session IDs observed in allowed evidence; never invent, rename, or infer a contributor, and each outer contributor must appear in the proposal's aliases, attributions, or events. Use only the two provided Commons project-history tools for progress and the final proposal. Do not write, apply, mutate, publish, expose credentials or prompts, include private data, or reproduce raw transcripts. A human must review every proposal before canonical import.", policy.Depth, strings.Join(allowed, ", "), forbiddenText, strings.Join(stableRules, "; "), limits.MaxOutcomes, limits.MaxProvenancePerOutcome, limits.MaxContributorsPerOutcome, limits.MaxHistoricalAliases, limits.MaxHistoricalTasks, limits.MaxSourcesExamined)
}

func (s *Service) ConfigureNativeProjectArchaeology(ctx context.Context, launcher ArchaeologyNativeLauncher, principal string) error {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || launcher == nil {
		return domain.ErrUnavailable
	}
	if err := reconcileArchaeologyNativeIdentities(ctx, repository, launcher, principal); err != nil {
		return err
	}
	s.archaeologyScheduler = newArchaeologyScheduler(ctx, s, repository, launcher, principal)
	s.archaeologyLauncher = s.archaeologyScheduler
	s.archaeologyScheduler.Wake()
	return nil
}

func reconcileArchaeologyNativeIdentities(ctx context.Context, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string) error {
	identityStore, storeOK := repository.(ArchaeologyNativeIdentityRepository)
	reconciler, launcherOK := launcher.(ArchaeologyNativeIdentityReconciler)
	if !storeOK || !launcherOK {
		return nil
	}
	value, err := repository.ArchaeologySession(ctx, principal)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	candidates := make(map[string]domain.ArchaeologyCandidate, len(value.Candidates))
	for _, candidate := range value.Candidates {
		candidates[candidate.ID] = candidate
	}
	checked := 0
	for _, batch := range value.NativeBatches {
		for _, job := range batch.Jobs {
			if checked >= domain.ArchaeologyNativeMaxProjects*2 {
				return nil
			}
			if job.State != "uncertain" || job.ThreadID != "" && job.TurnID != "" {
				continue
			}
			candidate, found := candidates[job.CandidateID]
			if !found {
				continue
			}
			checked++
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			launch, exact, lookupErr := reconciler.RecoverNativeIdentity(lookupCtx, job, candidate)
			cancel()
			if lookupErr != nil || !exact {
				// Lookup is best-effort and read-only. Zero, multiple, incomplete, or
				// unavailable matches all leave the global uncertainty gate closed.
				continue
			}
			if bindErr := identityStore.BindArchaeologyNativeUncertainty(ctx, job.ID, launch); bindErr != nil && !errors.Is(bindErr, domain.ErrConflict) {
				return bindErr
			}
		}
	}
	return nil
}
func (s *Service) CloseProjectArchaeology() {
	if s != nil && s.archaeologyScheduler != nil {
		s.archaeologyScheduler.Close()
	}
}

// NativeProjectArchaeologySchedulerStatus exposes only the scheduler's safe
// process-local status. It intentionally omits the underlying persistence
// error text.
func (s *Service) NativeProjectArchaeologySchedulerStatus() ArchaeologySchedulerStatus {
	if s == nil || s.archaeologyScheduler == nil {
		return ArchaeologySchedulerStatus{}
	}
	return s.archaeologyScheduler.Status()
}

// WakeNativeProjectArchaeologyScheduler forwards the bounded, nonblocking
// availability/recovery signal without exposing the scheduler instance.
func (s *Service) WakeNativeProjectArchaeologyScheduler() {
	if s == nil || s.archaeologyScheduler == nil {
		return
	}
	s.archaeologyScheduler.Wake()
}

// ReconcileNativeProjectArchaeologyPersistence delegates to the explicit
// repository reconciliation seam before allowing queued work to resume.
func (s *Service) ReconcileNativeProjectArchaeologyPersistence(ctx context.Context) error {
	if s == nil || s.archaeologyScheduler == nil {
		return domain.ErrUnavailable
	}
	return s.archaeologyScheduler.ReconcilePersistence(ctx)
}

func (s *Service) queueNativeProjectArchaeology(ctx context.Context, principal, requestID string, baseRevision int64, acknowledgeLargeBatch bool) (domain.ArchaeologySession, error) {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || s.archaeologyScheduler == nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	if err := s.archaeologyScheduler.Available(ctx); err != nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: principal, RequestID: requestID, BaseRevision: baseRevision, AcknowledgeLargeBatch: acknowledgeLargeBatch})
	if err == nil {
		s.archaeologyScheduler.Wake()
	}

	return value, err
}

func (s *Service) ResolveProjectArchaeologyUncertainty(ctx context.Context, principal, requestID string, input ArchaeologyResolutionRequest) (ArchaeologySession, error) {
	repository, ok := s.repository.(ArchaeologyNativeResolutionRepository)
	if !ok {
		return ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.ResolveArchaeologyNativeUncertainty(ctx, domain.ArchaeologyNativeResolution{Principal: principal, RequestID: requestID, BaseRevision: input.BaseRevision, JobID: input.JobID, ThreadID: input.ThreadID, TurnID: input.TurnID, Resolution: input.Resolution})
	return s.archaeologySessionView(value), err
}

func (s *ArchaeologyScheduler) Cancel(ctx context.Context, principal, requestID string, baseRevision int64) (domain.ArchaeologySession, error) {
	if s == nil || s.repository == nil || s.launcher == nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	if s.persistenceBlocked() {
		return domain.ArchaeologySession{}, ErrArchaeologySchedulerPersistenceFault
	}
	jobs, value, err := s.repository.CancelArchaeologyNativeBatch(ctx, principal, requestID, baseRevision)
	if err != nil {
		s.recordPersistenceFailure(err)
		return value, err
	}
	for _, job := range jobs {
		if job.ThreadID == "" || job.TurnID == "" {
			continue
		}
		if interruptErr := s.launcher.InterruptNative(ctx, job); interruptErr != nil {
			_ = s.recordPersistenceFailure(s.repository.LoseArchaeologyNativeTurn(ctx, job.ID, job.ThreadID, job.TurnID))
		}
	}
	s.Wake()
	return s.repository.ArchaeologySession(ctx, principal)
}
