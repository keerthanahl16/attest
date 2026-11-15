// Package migrations embeds SQL schema files applied at process startup.
package migrations

import "embed"

// FS holds every *.sql migration applied in lexical order.
//
//go:embed *.sql
var FS embed.FS
