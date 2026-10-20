// Package migrations embeds the goose SQL migrations so tests and tools apply the same files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
