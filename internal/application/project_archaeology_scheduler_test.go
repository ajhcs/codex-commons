package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestHistorianReportRejectsAgentSuppliedBoundProjectIdentity(t *testing.T) {
	s := &ArchaeologyScheduler{}
	body := []byte(`{"outcomes":[{"project_id":"other","title":"x"}]}`)
	response := s.handleTool(context.Background(), domain.ArchaeologyNativeJob{ID: "job", ProjectID: "bound"}, ArchaeologyNativeToolCall{ThreadID: "thread", TurnID: "turn", Tool: "commons_project_history_report", Arguments: body})
	if response.Success {
		t.Fatal("agent-supplied project_id was accepted")
	}
}

func TestHistorianReportRejectsUnknownEnvelopeFields(t *testing.T) {
	s := &ArchaeologyScheduler{}
	body := []byte(`{"outcomes":[],"extra":"not allowed"}`)
	response := s.handleTool(context.Background(), domain.ArchaeologyNativeJob{ID: "job", ProjectID: "bound"}, ArchaeologyNativeToolCall{ThreadID: "thread", TurnID: "turn", Tool: "commons_project_history_report", Arguments: body})
	if response.Success {
		t.Fatal("unknown report field was accepted")
	}
}

func TestNativeShellSupportsCanonicalPreviewWithoutImportingTasks(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "catalog-project", Name: "Catalog Project", PathLabel: "Catalog Project", HasGit: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"catalog-project"}, Depth: "standard", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	occurred := time.Now().UTC().Add(-time.Hour)
	service := New(repository, nil, nil)
	request := HistoricalImportRequest{
		SchemaVersion: 1, BatchID: "native-preview", SourceDigest: digest, CollisionPolicy: "current_wins",
		Tasks: []HistoricalTaskRequest{{
			Key: "done", Title: "Historical task", State: "done",
			Source: HistoricalSourceRequest{Kind: "repository_document", StableID: "doc", Digest: digest, OccurredAt: occurred},
			Attributions: []HistoricalAttributionRequest{{
				Session: "historical", Role: "implementer", Confidence: "verified",
				Source: HistoricalSourceRequest{Kind: "codex_session_uuidv7", StableID: "historical", Digest: digest, OccurredAt: occurred},
			}},
		}},
	}
	preview, err := service.PreviewHistoricalTaskImport(ctx, "catalog-project", request, ProjectCoreActor{})
	if err != nil || preview.Applied {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	var tasks int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 {
		t.Fatalf("preview imported tasks=%d", tasks)
	}
}

func TestArchaeologyViewHidesStartUntilLegacyCatalogIsRefreshed(t *testing.T) {
	value := domain.ArchaeologySession{State: "draft", Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"legacy"}}, Candidates: []domain.ArchaeologyCandidate{{ID: "legacy"}}}
	if archaeologyView(value).Controls.CanStart {
		t.Fatal("legacy unmapped candidate exposed Start")
	}
	value.Candidates[0].CanonicalProjectID = "legacy"
	if !archaeologyView(value).Controls.CanStart {
		t.Fatal("refreshed candidate did not expose Start")
	}
}

type bindFailureRepository struct {
	ArchaeologyNativeRepository
	failed    bool
	uncertain bool
}

func (r *bindFailureRepository) ArchaeologySession(context.Context, string) (domain.ArchaeologySession, error) {
	return domain.ArchaeologySession{Candidates: []domain.ArchaeologyCandidate{{ID: "candidate", Name: "Candidate"}}}, nil
}

func (r *bindFailureRepository) BindArchaeologyNativeJob(context.Context, string, string, string, string) error {
	return domain.ErrConflict
}

func (r *bindFailureRepository) FailArchaeologyNativeStart(_ context.Context, _ string, uncertain bool) error {
	r.failed = true
	r.uncertain = uncertain
	return nil
}

type acceptedThenBindFailureLauncher struct {
	ArchaeologyNativeLauncher
}

func (acceptedThenBindFailureLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}, nil
}

func TestNativeAcceptedTaskBecomesUncertainWhenDurableBindFails(t *testing.T) {
	repository := &bindFailureRepository{}
	scheduler := &ArchaeologyScheduler{
		service:    &Service{},
		repository: repository,
		launcher:   acceptedThenBindFailureLauncher{},
		principal:  domain.HumanLocalPrincipal,
		ctx:        context.Background(),
	}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if !repository.failed || !repository.uncertain {
		t.Fatalf("failed=%v uncertain=%v", repository.failed, repository.uncertain)
	}
}

type acceptedRequestLostResponseLauncher struct {
	ArchaeologyNativeLauncher
}

func (acceptedRequestLostResponseLauncher) LaunchNative(context.Context, domain.ArchaeologyNativeJob, domain.ArchaeologySession, domain.ArchaeologyCandidate, func(context.Context, ArchaeologyNativeToolCall) ArchaeologyNativeToolResponse, func(domain.ArchaeologyNativeTerminal)) (domain.ArchaeologyLaunchResult, error) {
	return domain.ArchaeologyLaunchResult{State: "uncertain"}, errors.New("response lost")
}

func TestNativeAcceptedRequestWithoutResponseBecomesUncertain(t *testing.T) {
	repository := &bindFailureRepository{}
	scheduler := &ArchaeologyScheduler{
		service:    &Service{},
		repository: repository,
		launcher:   acceptedRequestLostResponseLauncher{},
		principal:  domain.HumanLocalPrincipal,
		ctx:        context.Background(),
	}
	scheduler.launch(domain.ArchaeologyNativeJob{ID: "job", CandidateID: "candidate"})
	if !repository.failed || !repository.uncertain {
		t.Fatalf("failed=%v uncertain=%v", repository.failed, repository.uncertain)
	}
}

func TestArchaeologySchedulerCloseJoinsAcceptedCallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler := &ArchaeologyScheduler{ctx: ctx, cancel: cancel}
	if !scheduler.beginCallback() {
		t.Fatal("callback was rejected before close")
	}
	release := make(chan struct{})
	go func() {
		<-release
		scheduler.callbackWG.Done()
	}()
	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("close returned before the accepted callback")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("close did not join the callback")
	}
	if scheduler.beginCallback() {
		t.Fatal("callback accepted after close")
	}
}
