package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"gopkg.in/yaml.v3"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

const (
	// MaxDefinitionBytes is the maximum allowed size of a definition file (4 MiB).
	MaxDefinitionBytes = 4 << 20
	// MaxDefinitionDepth is the maximum allowed nesting depth of a definition.
	MaxDefinitionDepth = 64
)

// Parser implements engine.DefinitionParser.
type Parser struct{}

// NewParser creates a new Parser instance.
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses a raw byte slice into an engine.GraphDefinition according to the specified format.
func (p *Parser) Parse(data []byte, format engine.Format) (*engine.GraphDefinition, error) {
	if len(data) == 0 || len(bytes.TrimSpace(data)) == 0 {
		return nil, NewValidationError("$", engine.CodeInvalidFormat, "input is empty or whitespace only")
	}
	if len(data) > MaxDefinitionBytes {
		return nil, NewValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("input exceeds maximum size of %d bytes", MaxDefinitionBytes))
	}

	var tree any
	var err error

	switch format {
	case engine.FormatJSON:
		tree, err = DecodeJSON(data)
	case engine.FormatYAML:
		tree, err = DecodeYAML(data)
	default:
		return nil, NewValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("unsupported format: %s", format))
	}

	if err != nil {
		return nil, err
	}

	return bindDefinition(tree)
}

// DecodeJSON decodes json.
func DecodeJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	tree, err := parseJSONValue(dec, engine.PathRoot, 0)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, NewValidationError("$", engine.CodeInvalidFormat, "unexpected end of JSON input")
		}
		return nil, err
	}

	// Check for trailing data
	_, err = dec.Token()
	if err != io.EOF {
		return nil, NewValidationError("$", engine.CodeInvalidFormat, "trailing data after JSON document")
	}

	return tree, nil
}

func parseJSONValue(dec *json.Decoder, path engine.Path, depth int) (any, error) {
	if depth > MaxDefinitionDepth {
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("maximum nesting depth of %d exceeded", MaxDefinitionDepth))
	}

	t, err := dec.Token()
	if err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return nil, NewValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("JSON syntax error at offset %d: %v", syntaxErr.Offset, err))
		}
		return nil, NewValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("JSON decode error: %v", err))
	}

	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			return parseJSONObject(dec, path, depth+1)
		case '[':
			return parseJSONArray(dec, path, depth+1)
		}
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("unexpected JSON delimiter: %v", v))
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("invalid number format: %v", v))
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, "non-finite float values are not allowed")
		}
		return f, nil
	case string, bool, nil:
		return v, nil
	default:
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("unexpected JSON token type: %T", v))
	}
}

func parseJSONObject(dec *json.Decoder, path engine.Path, depth int) (map[string]any, error) {
	obj := make(map[string]any)
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("error reading JSON object key: %v", err))
		}
		key, ok := t.(string)
		if !ok {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, "JSON object key must be a string")
		}

		fieldPath := path.Field(key)
		if _, exists := obj[key]; exists {
			return nil, NewValidationError(string(fieldPath), engine.CodeInvalidFormat, fmt.Sprintf("duplicate key '%s'", key))
		}

		val, err := parseJSONValue(dec, fieldPath, depth)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	// Consume closing '}'
	_, err := dec.Token()
	if err != nil {
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("error reading JSON object close: %v", err))
	}
	return obj, nil
}

func parseJSONArray(dec *json.Decoder, path engine.Path, depth int) ([]any, error) {
	var arr []any
	idx := 0
	for dec.More() {
		val, err := parseJSONValue(dec, path.Index(idx), depth)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
		idx++
	}
	// Consume closing ']'
	_, err := dec.Token()
	if err != nil {
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("error reading JSON array close: %v", err))
	}
	return arr, nil
}

// DecodeYAML decodes yaml.
func DecodeYAML(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return nil, NewValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("YAML decode error: %v", err))
	}

	// Check for multiple documents
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, NewValidationError("$", engine.CodeInvalidFormat, "multiple YAML documents are not allowed")
	}

	if len(root.Content) == 0 {
		return nil, nil // empty document
	}

	return normalizeYAMLNode(root.Content[0], engine.PathRoot, 0)
}

func normalizeYAMLNode(node *yaml.Node, path engine.Path, depth int) (any, error) {
	if depth > MaxDefinitionDepth {
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("maximum nesting depth of %d exceeded", MaxDefinitionDepth))
	}

	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return normalizeYAMLNode(node.Content[0], path, depth)
	case yaml.MappingNode:
		return normalizeYAMLMapping(node, path, depth+1)
	case yaml.SequenceNode:
		return normalizeYAMLSequence(node, path, depth+1)
	case yaml.ScalarNode:
		return normalizeYAMLScalar(node, path)
	case yaml.AliasNode:
		return nil, NewValidationError(string(path), engine.CodeInvalidFormat, "YAML aliases are not supported")
	default:
		return nil, NewValidationError(string(path), engine.CodeInvalidType, fmt.Sprintf("unsupported YAML node kind: %v", node.Kind))
	}
}

func normalizeYAMLMapping(node *yaml.Node, path engine.Path, depth int) (map[string]any, error) {
	obj := make(map[string]any)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		if keyNode.Kind != yaml.ScalarNode || (keyNode.Tag != "!!str" && keyNode.Tag != "") {
			return nil, NewValidationError(string(path), engine.CodeInvalidType, "YAML object keys must be strings")
		}
		key := keyNode.Value
		fieldPath := path.Field(key)

		if _, exists := obj[key]; exists {
			// duplicate keys might be caught by yaml.v3, but we double-check just in case.
			// Currently yaml.v3 throws an error on duplicate keys by default in strict mode, but we parse generic trees.
			// Let's enforce it here if it slips through.
			return nil, NewValidationError(string(fieldPath), engine.CodeInvalidFormat, fmt.Sprintf("duplicate key '%s'", key))
		}

		val, err := normalizeYAMLNode(valNode, fieldPath, depth)
		if err != nil {
			return nil, err
		}
		obj[key] = val
	}
	return obj, nil
}

func normalizeYAMLSequence(node *yaml.Node, path engine.Path, depth int) ([]any, error) {
	var arr []any
	for i, n := range node.Content {
		val, err := normalizeYAMLNode(n, path.Index(i), depth)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	return arr, nil
}

func normalizeYAMLScalar(node *yaml.Node, path engine.Path) (any, error) {
	switch node.Tag {
	case "!!str", "":
		return node.Value, nil
	case "!!int", "!!float":
		var f float64
		// Use yaml unmarshal into float64 to parse correctly and handle nans/infs
		err := node.Decode(&f)
		if err != nil {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("invalid number format: %v", node.Value))
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, "non-finite float values are not allowed")
		}
		return f, nil
	case "!!bool":
		var b bool
		err := node.Decode(&b)
		if err != nil {
			return nil, NewValidationError(string(path), engine.CodeInvalidFormat, fmt.Sprintf("invalid boolean format: %v", node.Value))
		}
		return b, nil
	case "!!null":
		return nil, nil
	default:
		// Any other tags like timestamp, binary, etc.
		return nil, NewValidationError(string(path), engine.CodeInvalidType, fmt.Sprintf("unsupported YAML scalar tag: %s", node.Tag))
	}
}

// NewValidationError helper to create a ValidationErrors containing one issue.
func NewValidationError(path string, code engine.ErrorCode, message string) error {
	errs := &engine.ValidationErrors{}
	errs.Add(path, code, message)
	return errs
}
