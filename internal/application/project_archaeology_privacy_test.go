package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/domain"
)

func TestArchaeologyBrowserJSONHasNoPackPromptRawPathOrTransportError(t *testing.T) {
	view := archaeologyView(domain.ArchaeologySession{
		State:        "draft",
		Config:       domain.ArchaeologyConfig{SelectedProjectIDs: []string{"project"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true}, MaxConcurrency: 1},
		Candidates:   []domain.ArchaeologyCandidate{{ID: "project", Name: "Project", PathLabel: "Project", RepositoryLabel: "acme/project"}},
		TaskLaunches: []domain.ArchaeologyTaskLaunch{{ProjectID: "project", State: "failed", Error: "rpc transport failed at /home/private/project with secret prompt"}},
		Handoff:      &domain.ArchaeologyHandoff{ID: "handoff", State: "ready_to_claim", PackJSON: `{"projects":[{"task_prompt":"secret body"}]}`, CreatedAt: time.Now()},
	})
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	for _, forbidden := range []string{`"pack"`, "task_prompt", "secret body", "/home/private", "rpc transport failed", "secret prompt", `~/`, `…/`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("browser JSON leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, "Codex could not start this task") {
		t.Fatalf("missing public error: %s", encoded)
	}
}
