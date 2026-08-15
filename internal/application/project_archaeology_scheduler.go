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
	closing    bool
}

func newArchaeologyScheduler(parent context.Context, service *Service, repository ArchaeologyNativeRepository, launcher ArchaeologyNativeLauncher, principal string) *ArchaeologyScheduler {
	ctx, cancel := context.WithCancel(parent)
	s := &ArchaeologyScheduler{service: service, repository: repository, launcher: launcher, principal: principal, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	s.wg.Add(1)
	go s.loop()
	return s
}
func (s *ArchaeologyScheduler) Available(ctx context.Context) error {
	if s == nil || s.launcher == nil {
		return domain.ErrUnavailable
	}
	return s.launcher.Available(ctx)
}
func (s *ArchaeologyScheduler) Launch(context.Context, domain.ArchaeologySession) error {
	return domain.ErrUnavailable
}
func (s *ArchaeologyScheduler) Wake() {
	if s == nil {
		return
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
	s.cancel()
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
	for s.ctx.Err() == nil {
		job, err := s.repository.ClaimArchaeologyNativeJob(s.ctx)
		if err != nil {
			return
		}
		s.launch(job)
	}
}
func (s *ArchaeologyScheduler) launch(job domain.ArchaeologyNativeJob) {
	session, err := s.repository.ArchaeologySession(s.ctx, s.principal)
	if err != nil {
		_ = s.repository.FailArchaeologyNativeStart(s.ctx, job.ID, domain.ArchaeologyLaunchResult{}, false)
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
		_ = s.repository.FailArchaeologyNativeStart(s.ctx, job.ID, domain.ArchaeologyLaunchResult{}, false)
		return
	}
	bound := make(chan struct{})
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
			if bindErr == nil {
				terminal.JobID = job.ID
				if terminal.Status == "unavailable" {
					_ = s.repository.LoseArchaeologyNativeTurn(s.ctx, job.ID, terminal.ThreadID, terminal.TurnID)
				} else {
					_ = s.repository.CompleteArchaeologyNativeTurn(s.ctx, terminal)
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
		_ = s.repository.FailArchaeologyNativeStart(markCtx, job.ID, result, result.State == "uncertain" || result.ThreadID != "")
		close(bound)
		// Once an exact turn exists, any later protocol/visibility failure is
		// post-acceptance. Persist uncertainty first, then interrupt that exact
		// turn once. Never retry the non-idempotent launch boundary.
		if result.ThreadID != "" && result.TurnID != "" {
			_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		}
		return
	}
	bindErr = s.repository.BindArchaeologyNativeIdentity(s.ctx, job.ID, result.ThreadID, result.CodexSessionID, result.TurnID)
	if bindErr != nil {
		close(bound)
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repository.FailArchaeologyNativeStart(markCtx, job.ID, result, true)
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
		close(bound)
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repository.FailArchaeologyNativeStart(markCtx, job.ID, result, true)
		_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		return
	}
	bindErr = s.repository.ActivateArchaeologyNativeJob(s.ctx, job.ID, result.ThreadID, result.TurnID)
	if bindErr != nil {
		close(bound)
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repository.FailArchaeologyNativeStart(markCtx, job.ID, result, true)
		_ = s.launcher.InterruptNative(markCtx, domain.ArchaeologyNativeJob{ID: job.ID, ThreadID: result.ThreadID, CodexSessionID: result.CodexSessionID, TurnID: result.TurnID})
		return
	}
	close(bound)
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
		if s.repository.UpdateArchaeologyNativeProgress(ctx, domain.ArchaeologyNativeProgress{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, PhaseLabel: label, SourcesExamined: input.SourcesExamined}) != nil {
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
		if err = s.repository.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: call.ThreadID, TurnID: call.TurnID, Digest: digest, Outcomes: outcomes}); err != nil {
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
	jobs, value, err := s.repository.CancelArchaeologyNativeBatch(ctx, principal, requestID, baseRevision)
	if err != nil {
		return value, err
	}
	for _, job := range jobs {
		if job.ThreadID == "" || job.TurnID == "" {
			continue
		}
		if interruptErr := s.launcher.InterruptNative(ctx, job); interruptErr != nil {
			_ = s.repository.LoseArchaeologyNativeTurn(ctx, job.ID, job.ThreadID, job.TurnID)
		}
	}
	s.Wake()
	return s.repository.ArchaeologySession(ctx, principal)
}
