package migrations

import "embed"

// FS contains the ordered SQL migrations used by the local store.
//
//go:embed *.sql
var FS embed.FS
