package httpapi

import (
	"context"

	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
)

type HumanAccountBindingStore interface {
	GetHumanAccountBinding(context.Context) (domain.HumanAccountBinding, error)
	BindHumanAccount(context.Context, domain.BindHumanAccountRequest) (domain.HumanAccountBinding, error)
	UpdateHumanProfile(context.Context, domain.UpdateHumanProfileRequest) (domain.HumanAccountBinding, error)
}

type HumanAuthEventStore interface {
	RecordHumanAuthEvent(context.Context, domain.HumanAuthEventRequest) error
}

type CodexAuthConfig struct {
	Client            codexauth.Client
	BindingKey        [32]byte
	AllowFirstBindLAN bool
}
