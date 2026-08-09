package domain

import "errors"

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrInvalid        = errors.New("invalid")
	ErrFutureRevision = errors.New("future revision")
	ErrUnavailable    = errors.New("unavailable")
)
