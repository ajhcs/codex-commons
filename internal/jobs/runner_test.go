package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type fakeKill map[JobName]bool

func (k fakeKill) Disabled(name JobName) bool { return k[name] }

type fakeGitHub struct {
	mu     sync.Mutex
	calls  int
	result SyncResult
	err    error
}

type cancelingGitHub struct{ cancel context.CancelFunc }

func (f cancelingGitHub) Check(context.Context, WatchInput) (SyncResult, error) {
	f.cancel()
	return SyncResult{Cursor: "must-not-succeed"}, nil
}

type unavailableStore struct{}

func (unavailableStore) Acquire(context.Context, Definition, string, string, time.Time, time.Time) (AcquireResult, error) {
	return AcquireResult{}, errors.New("forum store unavailable")
}
func (unavailableStore) Finish(context.Context, Receipt) error { return nil }

func (f *fakeGitHub) Check(ctx context.Context, input WatchInput) (SyncResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeGitHub) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeInterpreter struct {
	mu       sync.Mutex
	calls    int
	proposal WikiProposal
	tokens   int
	err      error
}

func (f *fakeInterpreter) ProposeWikiRevision(ctx context.Context, task ResolvedTask, budget int) (WikiProposal, int, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.proposal, f.tokens, f.err
}

func (f *fakeInterpreter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newTestRunner(t *testing.T, clock *fakeClock, store StateStore, github GitHubReader, interpreter Interpreter, kill KillSwitch, granted CapabilitySet) *Runner {
	t.Helper()
	runner, err := NewRunner(clock, store, github, interpreter, kill, granted,
		WatcherDefinition(time.Minute, 5*time.Second),
		CuratorDefinition(time.Minute, 5*time.Second, 100),
	)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestUnchangedWatcherNeverCallsInterpreterAndEmitsSmallReceipt(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	github := &fakeGitHub{result: SyncResult{Cursor: "etag-1", Changed: false}}
	interpreter := &fakeInterpreter{}
	runner := newTestRunner(t, clock, NewMemoryStore(), github, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})

	receipt, err := runner.Run(context.Background(), RunRequest{RunID: "watch-1", Job: GitHubWatcher, Watch: &WatchInput{Project: "commons", Cursor: "etag-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "succeeded" || receipt.Changed || receipt.Cursor != "etag-1" || receipt.Proposal != nil || receipt.TokensUsed != 0 {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if interpreter.callCount() != 0 {
		t.Fatalf("unchanged watcher made %d interpretive calls", interpreter.callCount())
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 260 {
		t.Fatalf("unchanged receipt is not tiny: %d bytes: %s", len(encoded), encoded)
	}
	t.Logf("unchanged receipt size: %d bytes", len(encoded))
}

func TestCuratorOnlyAcceptsResolvedTaskAndRequiresReview(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	interpreter := &fakeInterpreter{tokens: 42, proposal: WikiProposal{Project: "commons", TaskID: "task-1", Page: "architecture", BaseRevision: 3, ProposedTitle: "Architecture", ProposedBody: "Proposed change", Basis: "task:task-1@7", RequiresReview: true}}
	runner := newTestRunner(t, clock, NewMemoryStore(), &fakeGitHub{}, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})

	unresolved := RunRequest{RunID: "curate-0", Job: WikiCurator, Resolved: &ResolvedTask{Project: "commons", TaskID: "task-1", State: "active", ResolutionRevision: 7, Title: "T", Resolution: "done"}}
	if _, err := runner.Run(context.Background(), unresolved); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unresolved task error = %v", err)
	}
	if interpreter.callCount() != 0 {
		t.Fatal("interpreter called for unresolved task")
	}

	resolved := *unresolved.Resolved
	resolved.State = "resolved"
	receipt, err := runner.Run(context.Background(), RunRequest{RunID: "curate-1", Job: WikiCurator, Resolved: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Proposal == nil || !receipt.Proposal.RequiresReview || receipt.TokensUsed != 42 || receipt.OutputType != OutputWikiRevisionProposal {
		t.Fatalf("unexpected proposal receipt: %+v", receipt)
	}
	if interpreter.callCount() != 1 {
		t.Fatalf("interpretive calls = %d", interpreter.callCount())
	}
}

func TestBudgetCapabilityKillSwitchAndOutageFailClosed(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	task := &ResolvedTask{Project: "p", TaskID: "t", State: "resolved", ResolutionRevision: 1, Title: "T", Resolution: "R"}

	t.Run("missing capability", func(t *testing.T) {
		interpreter := &fakeInterpreter{}
		runner := newTestRunner(t, &fakeClock{now: base}, NewMemoryStore(), &fakeGitHub{}, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true})
		_, err := runner.Run(context.Background(), RunRequest{RunID: "r", Job: WikiCurator, Resolved: task})
		if !errors.Is(err, ErrCapability) || interpreter.callCount() != 0 {
			t.Fatalf("error=%v calls=%d", err, interpreter.callCount())
		}
	})

	t.Run("kill switch", func(t *testing.T) {
		github := &fakeGitHub{}
		runner := newTestRunner(t, &fakeClock{now: base}, NewMemoryStore(), github, &fakeInterpreter{}, fakeKill{GitHubWatcher: true}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
		_, err := runner.Run(context.Background(), RunRequest{RunID: "r", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}})
		if !errors.Is(err, ErrDisabled) || github.callCount() != 0 {
			t.Fatalf("error=%v calls=%d", err, github.callCount())
		}
	})

	t.Run("token budget", func(t *testing.T) {
		interpreter := &fakeInterpreter{tokens: 101, proposal: WikiProposal{Project: "p", TaskID: "t", Page: "x", ProposedBody: "x", RequiresReview: true}}
		runner := newTestRunner(t, &fakeClock{now: base}, NewMemoryStore(), &fakeGitHub{}, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
		receipt, err := runner.Run(context.Background(), RunRequest{RunID: "r", Job: WikiCurator, Resolved: task})
		if !errors.Is(err, ErrBudget) || receipt.Status != "failed" || receipt.Error != "budget_exceeded" || receipt.Proposal != nil {
			t.Fatalf("receipt=%+v error=%v", receipt, err)
		}
	})

	t.Run("github outage", func(t *testing.T) {
		github := &fakeGitHub{err: errors.New("github unavailable")}
		interpreter := &fakeInterpreter{}
		runner := newTestRunner(t, &fakeClock{now: base}, NewMemoryStore(), github, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
		receipt, err := runner.Run(context.Background(), RunRequest{RunID: "r", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}})
		if err == nil || receipt.Status != "failed" || receipt.Error != "unavailable" || interpreter.callCount() != 0 {
			t.Fatalf("receipt=%+v error=%v interpretive calls=%d", receipt, err, interpreter.callCount())
		}
	})

	t.Run("forum store outage", func(t *testing.T) {
		github := &fakeGitHub{}
		interpreter := &fakeInterpreter{}
		runner := newTestRunner(t, &fakeClock{now: base}, unavailableStore{}, github, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
		_, err := runner.Run(context.Background(), RunRequest{RunID: "r", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}})
		if err == nil || github.callCount() != 0 || interpreter.callCount() != 0 {
			t.Fatalf("error=%v github calls=%d interpretive calls=%d", err, github.callCount(), interpreter.callCount())
		}
	})
}

func TestIdempotencyMinimumIntervalLeaseAndCancellation(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: base}
	store := NewMemoryStore()
	github := &fakeGitHub{result: SyncResult{Cursor: "c1"}}
	runner := newTestRunner(t, clock, store, github, &fakeInterpreter{}, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
	request := RunRequest{RunID: "same", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}}

	first, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runner.Run(context.Background(), request)
	if err != nil || replay != first || github.callCount() != 1 {
		t.Fatalf("replay=%+v error=%v calls=%d", replay, err, github.callCount())
	}
	changed := request
	changed.Watch = &WatchInput{Project: "other", Cursor: "different"}
	if _, err := runner.Run(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed watcher replay error=%v", err)
	}
	if github.callCount() != 1 {
		t.Fatalf("changed watcher replay reached GitHub: calls=%d", github.callCount())
	}
	_, err = runner.Run(context.Background(), RunRequest{RunID: "new", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}})
	if !errors.Is(err, ErrTooSoon) {
		t.Fatalf("minimum interval error = %v", err)
	}
	clock.advance(time.Minute)
	if _, err := runner.Run(context.Background(), RunRequest{RunID: "new", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}}); err != nil {
		t.Fatal(err)
	}

	leaseStore := NewMemoryStore()
	definition := WatcherDefinition(0, time.Minute)
	if acquired, err := leaseStore.Acquire(context.Background(), definition, "a", "digest-a", base, base.Add(time.Minute)); err != nil || !acquired.Acquired {
		t.Fatalf("first acquire=%+v error=%v", acquired, err)
	}
	if _, err := leaseStore.Acquire(context.Background(), definition, "b", "digest-b", base, base.Add(time.Minute)); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("competing lease error=%v", err)
	}
	if acquired, err := leaseStore.Acquire(context.Background(), definition, "b", "digest-b", base.Add(time.Minute), base.Add(2*time.Minute)); err != nil || !acquired.Acquired {
		t.Fatalf("expired lease acquire=%+v error=%v", acquired, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	before := github.callCount()
	if _, err := runner.Run(canceled, RunRequest{RunID: "cancel", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error=%v", err)
	}
	if github.callCount() != before {
		t.Fatal("canceled run reached GitHub reader")
	}

	cancelDuring, cancelDuringRun := context.WithCancel(context.Background())
	clock.advance(time.Minute)
	cancelRunner := newTestRunner(t, clock, NewMemoryStore(), cancelingGitHub{cancel: cancelDuringRun}, &fakeInterpreter{}, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
	receipt, err := cancelRunner.Run(cancelDuring, RunRequest{RunID: "cancel-during", Job: GitHubWatcher, Watch: &WatchInput{Project: "p"}})
	if !errors.Is(err, context.Canceled) || receipt.Status != "failed" || receipt.Error != "canceled" {
		t.Fatalf("during-run cancellation receipt=%+v error=%v", receipt, err)
	}
}

func TestCuratorIdempotencyBindsSemanticInput(t *testing.T) {
	base := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	interpreter := &fakeInterpreter{tokens: 3, proposal: WikiProposal{Project: "p", TaskID: "t", Page: "page", ProposedTitle: "Title", ProposedBody: "Body", RequiresReview: true}}
	runner := newTestRunner(t, &fakeClock{now: base}, NewMemoryStore(), &fakeGitHub{}, interpreter, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
	request := RunRequest{RunID: "curator-idempotency", Job: WikiCurator, Resolved: &ResolvedTask{Project: "p", TaskID: "t", State: "resolved", ResolutionRevision: 1, Title: "Task", Resolution: "Done", Evidence: []string{"post:1"}}}

	first, err := runner.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := runner.Run(context.Background(), request)
	if err != nil || replay != first || interpreter.callCount() != 1 {
		t.Fatalf("identical replay=%+v error=%v calls=%d", replay, err, interpreter.callCount())
	}
	changed := request
	changed.Resolved = &ResolvedTask{Project: "p", TaskID: "t", State: "resolved", ResolutionRevision: 2, Title: "Task", Resolution: "Different", Evidence: []string{"post:2"}}
	if _, err := runner.Run(context.Background(), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed curator replay error=%v", err)
	}
	if interpreter.callCount() != 1 {
		t.Fatalf("changed curator replay reached interpreter: calls=%d", interpreter.callCount())
	}
}

func TestWatcherIdempotencyBindsProjectAndCursorIndependently(t *testing.T) {
	base := WatchInput{Project: "p", Cursor: "cursor-1"}
	for _, test := range []struct {
		name    string
		changed WatchInput
	}{
		{name: "project", changed: WatchInput{Project: "other", Cursor: "cursor-1"}},
		{name: "cursor", changed: WatchInput{Project: "p", Cursor: "cursor-2"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			github := &fakeGitHub{result: SyncResult{Cursor: "next"}}
			runner := newTestRunner(t, &fakeClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}, NewMemoryStore(), github, &fakeInterpreter{}, fakeKill{}, CapabilitySet{CapabilityGitHubRead: true, CapabilityInterpret: true, CapabilityWikiPropose: true})
			if _, err := runner.Run(context.Background(), RunRequest{RunID: "field-binding", Job: GitHubWatcher, Watch: &base}); err != nil {
				t.Fatal(err)
			}
			if _, err := runner.Run(context.Background(), RunRequest{RunID: "field-binding", Job: GitHubWatcher, Watch: &test.changed}); !errors.Is(err, ErrIdempotencyConflict) {
				t.Fatalf("changed %s error=%v", test.name, err)
			}
			if github.callCount() != 1 {
				t.Fatalf("changed %s reached GitHub: calls=%d", test.name, github.callCount())
			}
		})
	}
}

func TestDefinitionsRejectRecursiveAndWakeCapabilities(t *testing.T) {
	for _, forbidden := range []Capability{CapabilityCreateJobs, CapabilityWakeAgents} {
		definition := WatcherDefinition(time.Minute, time.Second)
		definition.Capabilities = append(definition.Capabilities, forbidden)
		if err := definition.Validate(); !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("capability %q accepted: %v", forbidden, err)
		}
	}
	definition := WatcherDefinition(time.Minute, time.Second)
	definition.Capabilities = append(definition.Capabilities, CapabilityInterpret)
	if _, err := NewRunner(&fakeClock{}, NewMemoryStore(), &fakeGitHub{}, &fakeInterpreter{}, fakeKill{}, CapabilitySet{}, definition, CuratorDefinition(time.Minute, time.Second, 1)); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("watcher accepted expanded capabilities: %v", err)
	}
}
