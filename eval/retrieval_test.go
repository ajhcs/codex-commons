package eval_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
	"codex-commons/internal/store"
)

type retrievalScenario struct {
	name, query, wantTitle, actionMarker string
}

var retrievalScenarios = []retrievalScenario{
	{"avoid duplicate migration", "schema lock retry", "Schema lock retry is already implemented", "reuse the bounded retry helper"},
	{"honor release gate", "release checksum gate", "Release checksum gate", "stop the release on mismatch"},
	{"route unresolved ownership", "upload ownership unresolved", "Upload ownership unresolved", "ask the storage owner before implementation"},
}

func seedRetrieval(t testing.TB) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrieval.sqlite"), store.WithClock(func() time.Time {
		return time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err := s.CreateProject(ctx, domain.Project{ID: "commons-lab", Name: "Commons Lab", Purpose: "retrieval evaluation"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTopic(ctx, domain.Topic{ID: "commons-lab", ProjectID: "commons-lab", Name: "Commons Lab"}); err != nil {
		t.Fatal(err)
	}
	posts := []struct{ key, kind, title, body string }{
		{"P-retry", "finding", "Schema lock retry is already implemented", "reuse the bounded retry helper; do not write another migration loop"},
		{"P-release", "decision", "Release checksum gate", "stop the release on mismatch and escalate the artifact discrepancy"},
		{"P-owner", "question", "Upload ownership unresolved", "ask the storage owner before implementation; the path boundary is undecided"},
		{"P-noise", "notice", "Routine schema and upload update", "release notes mention retries but do not change the next action"},
	}
	for _, p := range posts {
		_, err := s.Post(ctx, domain.PostRequest{TopicID: "commons-lab", Kind: p.kind, Title: p.title, Body: p.body, Basis: "fixed evaluation evidence", ActorID: "eval", SessionID: "S-eval", RequestID: p.key})
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func runScenario(ctx context.Context, s *store.Store, scenario retrievalScenario) (commands, results, tokens int, elapsed time.Duration, err error) {
	started := time.Now()
	hits, err := s.Search(ctx, "commons-lab", scenario.query, 5)
	commands++
	results = len(hits)
	if err != nil {
		return commands, results, 0, time.Since(started), err
	}
	var selected string
	for _, hit := range hits {
		if hit.Title == scenario.wantTitle {
			selected = hit.Ref
			break
		}
	}
	if selected == "" {
		return commands, results, estimatedTokens(hits), time.Since(started), fmt.Errorf("target %q not found in %+v", scenario.wantTitle, hits)
	}
	opened, err := s.Open(ctx, selected)
	commands++
	tokens = estimatedTokens(hits) + estimatedTokens(opened)
	if err == nil && !strings.Contains(opened.Body, scenario.actionMarker) {
		err = fmt.Errorf("opened %s without action marker %q", selected, scenario.actionMarker)
	}
	return commands, results, tokens, time.Since(started), err
}

func estimatedTokens(value any) int {
	payload, _ := json.Marshal(value)
	return (len(payload) + 2) / 3
}

func TestActionChangingRetrieval(t *testing.T) {
	s := seedRetrieval(t)
	var latencies []time.Duration
	for _, scenario := range retrievalScenarios {
		commands, results, tokens, elapsed, err := runScenario(context.Background(), s, scenario)
		if err != nil {
			t.Errorf("%s: %v", scenario.name, err)
			continue
		}
		latencies = append(latencies, elapsed)
		if commands > 2 || results > 5 || tokens > 840 {
			t.Errorf("%s: commands=%d results=%d tokens=%d", scenario.name, commands, results, tokens)
		}
		t.Logf("%s: found/opened %q commands=%d results=%d tokens_est=%d latency=%s", scenario.name, scenario.wantTitle, commands, results, tokens, elapsed)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	if len(latencies) > 0 && latencies[len(latencies)-1] > 50*time.Millisecond {
		t.Fatalf("fixed-scenario retrieval latency exceeded 50ms: %s", latencies[len(latencies)-1])
	}
}

func BenchmarkActionChangingRetrieval(b *testing.B) {
	s := seedRetrieval(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scenario := retrievalScenarios[i%len(retrievalScenarios)]
		commands, _, tokens, _, err := runScenario(ctx, s, scenario)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(commands), "commands/op")
		b.ReportMetric(float64(tokens), "tokens_est/op")
	}
}
