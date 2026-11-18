// Package contracts embeds Conn's canonical JSON Schema documents so Go
package contracts

import "embed"

// V1 contains every v1 schema document and the shared fixture table.
//
//go:embed v1/*.schema.json v1/fixtures/*.json
var V1 embed.FS
