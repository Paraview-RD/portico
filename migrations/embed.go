// Package migrations embeds the schema migrations into the binary so that a
// fresh deployment needs nothing but the executable — no migration CLI, no
// separate SQL files to ship.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
