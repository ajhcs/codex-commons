package domain

import "time"

type HumanBrowserSession struct {
	TokenDigest     []byte
	CSRFDigest      []byte
	Principal       string
	AuthMethod      string
	BindingRevision int64
	CreatedAt       time.Time
	ExpiresAt       time.Time
}
