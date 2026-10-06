package engine

import (
	"fmt"
	"strings"
)

// Path represents an RFC-9535-style normalized JSONPath.
type Path string

const (
	// Root is the root of the JSON document.
	Root Path = "$"
)

// Field appends a field access to the path.
// If the field name matches ^[A-Za-z_][A-Za-z0-9_]*$, it uses dot notation (e.g., .name).
// Otherwise, it uses bracket notation with single quotes (e.g., ['my-key'])
// where single quotes and backslashes inside the name are escaped.
func (p Path) Field(name string) Path {
	if isSimpleIdentifier(name) {
		return Path(string(p) + "." + name)
	}

	escaped := strings.ReplaceAll(name, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "'", "\\'")
	return Path(string(p) + "['" + escaped + "']")
}

// Index appends an array index access to the path (e.g., [3]).
func (p Path) Index(i int) Path {
	return Path(fmt.Sprintf("%s[%d]", p, i))
}

// String returns the string representation of the path.
func (p Path) String() string {
	return string(p)
}

func isSimpleIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
		} else {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
				return false
			}
		}
	}
	return true
}
