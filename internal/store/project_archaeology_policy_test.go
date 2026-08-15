package store

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
)

func TestNativeStableIDsAreKindSpecificAndOpaque(t *testing.T) {
	valid := []struct{ kind, value string }{
		{"git", "commit:" + strings.Repeat("a", 40)},
		{"git", "tree:" + strings.Repeat("b", 64)},
		{"git", "ref:refs/heads/main"},
		{"docs", "docs/architecture/design.md"},
		{"docs", "README.md"},
		{"codex_history", "thread:019ff16064537540b19b8a2fec07505e"},
		{"codex_history", "task:task_12345678"},
	}
	for _, item := range valid {
		if !validNativeStableID(item.kind, item.value) {
			t.Errorf("valid %s stable ID rejected: %q", item.kind, item.value)
		}
	}
	invalid := []struct{ kind, value string }{
		{"git", "commit:abc"},
		{"git", "ref:refs/heads/../private"},
		{"git", "ref:refs/heads/main.lock"},
		{"docs", "/home/alice/private.md"},
		{"docs", "../../secrets.txt"},
		{"docs", ".env"},
		{"docs", "docs/.private/notes.md"},
		{"docs", "docs/prompt-injection.md"},
		{"docs", "C:/Users/alice/notes.md"},
		{"docs", "docs/token.txt"},
		{"docs", "docs/line\nbreak.md"},
		{"codex_history", "thread:short"},
		{"codex_history", "task:private prompt text"},
		{"codex_history", "thread:/home/alice/task"},
		{"unknown", "README.md"},
	}
	for _, item := range invalid {
		if validNativeStableID(item.kind, item.value) {
			t.Errorf("invalid %s stable ID accepted: %q", item.kind, item.value)
		}
	}
}

func TestNativeBatchSnapshotsEveryDepthAndSourceCombination(t *testing.T) {
	for _, depth := range []string{"quick", "standard", "deep"} {
		for mask := 1; mask < 8; mask++ {
			t.Run(fmt.Sprintf("%s-%d", depth, mask), func(t *testing.T) {
				ctx := context.Background()
				s, err := Open(ctx, ":memory:")
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "all", Name: "All", PathLabel: "All", HasGit: true, HasDocs: true, HasCodexHistory: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
				if err != nil {
					t.Fatal(err)
				}
				policy := domain.ArchaeologyExecutionPolicy{Depth: depth, Sources: domain.ArchaeologySources{Git: mask&1 != 0, Docs: mask&2 != 0, CodexHistory: mask&4 != 0}}
				value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"all"}, Depth: depth, Sources: policy.Sources, MaxConcurrency: 1}})
				if err != nil {
					t.Fatal(err)
				}
				value, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "start", BaseRevision: value.Revision})
				if err != nil {
					t.Fatal(err)
				}
				if len(value.NativeBatches) != 1 || !value.NativeBatches[0].PolicyAttested || !reflect.DeepEqual(value.NativeBatches[0].Policy, policy) {
					t.Fatalf("batch=%+v want policy=%+v", value.NativeBatches, policy)
				}
				job, err := s.ClaimArchaeologyNativeJob(ctx)
				if err != nil || !reflect.DeepEqual(job.Policy, policy) {
					t.Fatalf("job=%+v err=%v", job, err)
				}
			})
		}
	}
}

func TestCompletedNativeBatchPolicyRemainsImmutableAfterSessionReconfigure(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	value, err := s.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "immutable-discover"}, domain.ArchaeologyDiscovery{Candidates: []domain.ArchaeologyCandidate{{ID: "immutable", Name: "Immutable", PathLabel: "Immutable", HasGit: true, HasDocs: true, HasCodexHistory: true, DurationMinSeconds: 1, DurationMaxSeconds: 2, RelativeCost: "low"}}})
	if err != nil {
		t.Fatal(err)
	}
	first := domain.ArchaeologyExecutionPolicy{Depth: "standard", Sources: domain.ArchaeologySources{Git: true}}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "immutable-config-1", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"immutable"}, Depth: first.Depth, Sources: first.Sources, MaxConcurrency: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "immutable-start-1", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimArchaeologyNativeJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.BindArchaeologyNativeJob(ctx, job.ID, "thread-immutable", "session-immutable", "turn-immutable"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReportArchaeologyNativeJob(ctx, domain.ArchaeologyNativeReport{JobID: job.ID, ThreadID: "thread-immutable", TurnID: "turn-immutable", Digest: [32]byte{1}, Outcomes: []domain.ArchaeologyOutcome{nativeTestOutcome(t, "immutable")}}); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteArchaeologyNativeTurn(ctx, domain.ArchaeologyNativeTerminal{JobID: job.ID, ThreadID: "thread-immutable", TurnID: "turn-immutable", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	value, err = s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	second := domain.ArchaeologyExecutionPolicy{Depth: "deep", Sources: domain.ArchaeologySources{Docs: true, CodexHistory: true}}
	value, err = s.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "immutable-config-2", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"immutable"}, Depth: second.Depth, Sources: second.Sources, MaxConcurrency: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value.NativeBatches[0].Policy, first) || !reflect.DeepEqual(value.Config.Sources, second.Sources) || value.Config.Depth != second.Depth {
		t.Fatalf("session=%+v", value)
	}
	view, err := application.New(s, nil, nil).ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || view.Handoff == nil || view.Handoff.Depth != first.Depth || !view.Handoff.Sources.Git || view.Handoff.Sources.Docs || view.Handoff.Sources.CodexHistory || !view.Handoff.PolicyAttested {
		t.Fatalf("rendered handoff=%+v err=%v", view.Handoff, err)
	}
	if _, err = s.QueueArchaeologyNativeBatch(ctx, domain.ArchaeologyNativeBatchRequest{Principal: domain.HumanLocalPrincipal, RequestID: "immutable-start-2", BaseRevision: value.Revision}); err != nil {
		t.Fatal(err)
	}
	latest, err := s.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil || len(latest.NativeBatches) != 2 || !reflect.DeepEqual(latest.NativeBatches[0].Policy, second) || !reflect.DeepEqual(latest.NativeBatches[1].Policy, first) {
		t.Fatalf("batches=%+v err=%v", latest.NativeBatches, err)
	}
}
