// Package appbackend composes application services with the established HTTP
// operations. It contains no product logic; DTOs are aliases of application
// types and this adapter only validates attested identity and maps errors.
package appbackend

import (
	"context"
	"errors"

	"codex-commons/internal/application"
	"codex-commons/internal/domain"
	"codex-commons/internal/httpapi"
)

type Adapter struct {
	httpapi.LegacyBackend
	home *application.Service
}

func New(legacy httpapi.LegacyBackend, home *application.Service) (*Adapter, error) {
	if legacy == nil || home == nil {
		return nil, errors.New("legacy backend and application service required")
	}
	return &Adapter{LegacyBackend: legacy, home: home}, nil
}

func (a *Adapter) GeneralHome(ctx context.Context, query httpapi.GeneralHomeQuery, meta httpapi.RequestMeta) (httpapi.GeneralHomeResult, error) {
	if meta.Actor == "" || meta.Session == "" || meta.Host == "" {
		return httpapi.GeneralHomeResult{}, httpapi.NewError(httpapi.CodeInvalid, "attested identity required")
	}
	out, err := a.home.GeneralHome(ctx, query)
	if err == nil {
		return out, nil
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return httpapi.GeneralHomeResult{}, httpapi.NewError(httpapi.CodeNotFound, "home source not found")
	case errors.Is(err, domain.ErrInvalid):
		return httpapi.GeneralHomeResult{}, httpapi.NewError(httpapi.CodeInvalid, "invalid home query")
	case errors.Is(err, domain.ErrUnavailable):
		return httpapi.GeneralHomeResult{}, httpapi.NewError(httpapi.CodeUnavailable, "home unavailable")
	default:
		return httpapi.GeneralHomeResult{}, err
	}
}

var _ httpapi.Backend = (*Adapter)(nil)
