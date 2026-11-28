// Package envvalue reads Conn configuration variables. An empty or
// whitespace-only value is treated as unset, because MCP clients such as Kiro
// may substitute an empty string for an unset ${VAR} placeholder.
package envvalue

import "strings"

// Lookup returns the trimmed value of name and whether it is set to
// something other than whitespace. A nil lookup reports not set.
func Lookup(lookup func(string) (string, bool), name string) (string, bool) {
	if lookup == nil {
		return "", false
	}
	value, ok := lookup(name)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}
