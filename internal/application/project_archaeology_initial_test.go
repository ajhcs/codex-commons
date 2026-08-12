package application

import (
	"context"
	"encoding/json"
	"testing"

	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

func TestProjectArchaeologyInitialReadIsNonMutatingDraft(t *testing.T) {
	ctx := context.Background()
	repository, err := commonsstore.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := New(repository, nil, nil)
	view, err := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "draft" || view.Discovery.State != "idle" || !view.Discovery.MetadataOnly || view.Config.MaxConcurrency != 2 || view.Revision != 0 {
		t.Fatalf("view=%+v", view)
	}
	if view.Capabilities.Discovery.Configured || view.Capabilities.Discovery.Available || view.Capabilities.Discovery.Mode != "allowlisted_metadata" || !view.Capabilities.HistorianHandoff.Configured || !view.Capabilities.HistorianHandoff.Available || view.Capabilities.Review.Available || view.Capabilities.CanonicalApply.Available {
		t.Fatalf("unconfigured capability facts=%+v", view.Capabilities)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Discovery struct {
			Candidates []json.RawMessage `json:"candidates"`
		} `json:"discovery"`
		Config struct {
			SelectedProjectIDs []string `json:"selected_project_ids"`
		} `json:"config"`
		Runs []json.RawMessage `json:"runs"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Config.SelectedProjectIDs == nil || payload.Discovery.Candidates == nil || payload.Runs == nil {
		t.Fatalf("virtual draft must serialize bounded collections as arrays: %s", body)
	}
	service.ConfigureProjectArchaeology(fakeArchaeologyDiscoverer{}, nil)
	configuredView, err := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil {
		t.Fatal(err)
	}
	if !configuredView.Capabilities.Discovery.Configured || !configuredView.Capabilities.Discovery.Available || configuredView.Capabilities.Discovery.Mode != "codex_known_metadata" {
		t.Fatalf("configured discovery capability=%+v", configuredView.Capabilities.Discovery)
	}
	var count int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("GET persisted session count=%d err=%v", count, err)
	}
}

func TestArchaeologyViewCopiesSelectedProjectIDs(t *testing.T) {
	source := domain.ArchaeologySession{Config: domain.ArchaeologyConfig{SelectedProjectIDs: []string{"project-a"}}}
	view := archaeologyView(source)
	view.Config.SelectedProjectIDs[0] = "view-only"
	if source.Config.SelectedProjectIDs[0] != "project-a" {
		t.Fatalf("view mutation changed domain state: %+v", source.Config.SelectedProjectIDs)
	}
	source.Config.SelectedProjectIDs[0] = "domain-only"
	if view.Config.SelectedProjectIDs[0] != "view-only" {
		t.Fatalf("domain mutation changed response view: %+v", view.Config.SelectedProjectIDs)
	}
}
