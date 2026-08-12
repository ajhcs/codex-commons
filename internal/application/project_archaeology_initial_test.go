package application

import (
	"context"
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
	var count int
	if err = repository.DB().QueryRowContext(ctx, `SELECT count(*) FROM archaeology_sessions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("GET persisted session count=%d err=%v", count, err)
	}
}
