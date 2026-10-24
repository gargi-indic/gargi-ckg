// Package schemas embeds the gargi schemas.
package schemas

import (
	"embed"
	"fmt"
	"path/filepath"
)

// FS contains the embedded JSON schemas for gargi.
//
//go:embed */v1/schema.json
var FS embed.FS

// Schema returns the contents of the schema for the given kind.
// The kind must be one of "sim", "graph", "policy", or "experiment".
// It returns an error if the kind is unknown or if the file cannot be read.
func Schema(kind string) ([]byte, error) {
	switch kind {
	case "sim", "graph", "policy", "experiment":
		path := filepath.Join(kind, "v1", "schema.json")
		return FS.ReadFile(path)
	default:
		return nil, fmt.Errorf("unknown schema kind: %q", kind)
	}
}
