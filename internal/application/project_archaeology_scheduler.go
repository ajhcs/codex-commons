package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	FailArchaeologyNativeStart(context.Context, string, bool) error
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
		_ = s.repository.FailArchaeologyNativeStart(s.ctx, job.ID, false)
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
		_ = s.repository.FailArchaeologyNativeStart(s.ctx, job.ID, false)
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
		_ = s.repository.FailArchaeologyNativeStart(context.Background(), job.ID, result.State == "uncertain" || result.ThreadID != "")
		close(bound)
		return
	}
	bindErr = s.repository.BindArchaeologyNativeJob(s.ctx, job.ID, result.ThreadID, result.CodexSessionID, result.TurnID)
	close(bound)
	if bindErr != nil {
		markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repository.FailArchaeologyNativeStart(markCtx, job.ID, true)
		return
	}
}
func decodeStrictOne(body []byte, target any) error {
	if len(body) == 0 || len(body) > 64<<10 {
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
		if decodeStrictOne(call.Arguments, &input) != nil || len(input.Note) > 500 {
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
		if decodeStrictOne(call.Arguments, &envelope) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for _, rawOutcome := range envelope.Outcomes {
			var fields map[string]json.RawMessage
			if decodeStrictOne(rawOutcome, &fields) != nil {
				return ArchaeologyNativeToolResponse{}
			}
			if _, supplied := fields["project_id"]; supplied {
				return ArchaeologyNativeToolResponse{}
			}
		}
		var input struct {
			Outcomes []ArchaeologyOutcomeReportRequest `json:"outcomes"`
		}
		if decodeStrictOne(call.Arguments, &input) != nil {
			return ArchaeologyNativeToolResponse{}
		}
		for index := range input.Outcomes {
			input.Outcomes[index].ProjectID = job.ProjectID
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

func HistorianTitle(name string) string {
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
func HistorianPrompt(_ domain.ArchaeologyCandidate) string {
	return "Review the selected Codex project for source-grounded historical Tasks and provenance. Use only the two provided Commons project-history tools for progress and the final proposal. Do not apply, mutate, publish, or expose credentials, prompts, private data, or raw transcripts. A human must review every proposal before canonical import."
}

func (s *Service) ConfigureNativeProjectArchaeology(ctx context.Context, launcher ArchaeologyNativeLauncher, principal string) error {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || launcher == nil {
		return domain.ErrUnavailable
	}
	s.archaeologyScheduler = newArchaeologyScheduler(ctx, s, repository, launcher, principal)
	s.archaeologyLauncher = s.archaeologyScheduler
	s.archaeologyScheduler.Wake()
	return nil
}
func (s *Service) CloseProjectArchaeology() {
	if s != nil && s.archaeologyScheduler != nil {
		s.archaeologyScheduler.Close()
	}
}
func (s *Service) queueNativeProjectArchaeology(ctx context.Context, principal, requestID string, baseRevision int64) (domain.ArchaeologySession, error) {
	repository, ok := s.repository.(ArchaeologyNativeRepository)
	if !ok || s.archaeologyScheduler == nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	if err := s.archaeologyScheduler.Available(ctx); err != nil {
		return domain.ArchaeologySession{}, domain.ErrUnavailable
	}
	value, err := repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: principal, RequestID: requestID, BaseRevision: baseRevision})
	if err == nil {
		s.archaeologyScheduler.Wake()
	}

	return value, err
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
