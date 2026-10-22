package graph

import "github.com/gargi-indic/gargi-flow/pkg/engine"

// DecodeStrict exposes the strict decoding functions to internal components without
// breaking encapsulation of graph parsing.
func DecodeStrict(data []byte, format engine.Format) (any, error) {
	switch format {
	case engine.FormatJSON:
		return decodeJSON(data)
	case engine.FormatYAML:
		return decodeYAML(data)
	default:
		return nil, newValidationError("$", engine.CodeInvalidFormat, "unsupported format")
	}
}
