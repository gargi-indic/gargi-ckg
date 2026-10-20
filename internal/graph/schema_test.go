package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"gopkg.in/yaml.v3"
)

// normalizeNumbers recursively converts json.Number or float64 back to float64/int/etc,
// ensuring the structure derived from YAML can be deeply compared to one from JSON.
// Since json.Unmarshal into any uses float64 for all numbers, we normalize both to match JSON's representation.
func normalizeNumbers(v any) any {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any)
		for k, v := range val {
			res[k] = normalizeNumbers(v)
		}
		return res
	case map[any]any:
		res := make(map[string]any)
		for k, v := range val {
			res[fmt.Sprintf("%v", k)] = normalizeNumbers(v)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, v := range val {
			res[i] = normalizeNumbers(v)
		}
		return res
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return val
	}
}

func readSchema(t *testing.T, path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("failed to parse %s as JSON: %v", path, err)
	}
	return schema
}

func readJSON(t *testing.T, path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse %s as JSON: %v", path, err)
	}
	return doc
}

func readYAML(t *testing.T, path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse %s as YAML: %v", path, err)
	}
	return doc
}

func TestSchemaAndExamplesParse(t *testing.T) {
	schema := readSchema(t, "../../schemas/graph/v1/schema.json")
	if schema == nil {
		t.Fatal("schema is nil")
	}

	exampleJSON := readJSON(t, "../../schemas/graph/v1/example.json")
	exampleYAML := readYAML(t, "../../schemas/graph/v1/example.yaml")

	normJSON := normalizeNumbers(exampleJSON)
	normYAML := normalizeNumbers(exampleYAML)

	if !reflect.DeepEqual(normJSON, normYAML) {
		t.Errorf("example.json and example.yaml are not deeply equal")

		jsonBytes, _ := json.MarshalIndent(normJSON, "", "  ")
		yamlBytes, _ := json.MarshalIndent(normYAML, "", "  ")
		t.Logf("JSON: %s", jsonBytes)
		t.Logf("YAML: %s", yamlBytes)
	}
}

// conformanceChecker is a test-only limited JSON Schema validator.
type conformanceChecker struct {
	t      *testing.T
	root   map[string]any
	schema map[string]any
}

func (c *conformanceChecker) validate(doc any, schema map[string]any, path string) []string {
	var errs []string

	if ref, ok := schema["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/") {
			errs = append(errs, fmt.Sprintf("%s: unsupported ref %s", path, ref))
			return errs
		}
		parts := strings.Split(ref[2:], "/")
		resolved := c.root
		for _, part := range parts {
			if next, ok := resolved[part].(map[string]any); ok {
				resolved = next
			} else {
				errs = append(errs, fmt.Sprintf("%s: failed to resolve ref %s", path, ref))
				return errs
			}
		}
		return c.validate(doc, resolved, path)
	}

	if typ, ok := schema["type"].(string); ok {
		switch typ {
		case "object":
			if doc == nil {
				return errs
			}
			obj, ok := doc.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected object, got %T", path, doc))
				return errs
			}

			reqs, _ := schema["required"].([]any)
			for _, r := range reqs {
				reqStr := r.(string)
				if _, ok := obj[reqStr]; !ok {
					errs = append(errs, fmt.Sprintf("%s: missing required property %s", path, reqStr))
				}
			}

			props, hasProps := schema["properties"].(map[string]any)
			patternProps, hasPatternProps := schema["patternProperties"].(map[string]any)

			addlPropsVal, hasAddlProps := schema["additionalProperties"]
			addlPropsAllowed := true
			if hasAddlProps {
				if b, ok := addlPropsVal.(bool); ok && !b {
					addlPropsAllowed = false
				}
			}

			for k, v := range obj {
				matched := false
				if hasProps {
					if propSchema, ok := props[k].(map[string]any); ok {
						matched = true
						errs = append(errs, c.validate(v, propSchema, path+"."+k)...)
					}
				}
				if !matched && hasPatternProps {
					for pat, patSchemaVal := range patternProps {
						matchedRegex, _ := regexp.MatchString(pat, k)
						if matchedRegex {
							matched = true
							if patSchema, ok := patSchemaVal.(map[string]any); ok {
								errs = append(errs, c.validate(v, patSchema, path+"."+k)...)
							}
						}
					}
				}
				if !matched && !addlPropsAllowed {
					errs = append(errs, fmt.Sprintf("%s: additional property not allowed: %s", path, k))
				}
			}

		case "array":
			if doc == nil {
				return errs
			}
			arr, ok := doc.([]any)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected array, got %T", path, doc))
				return errs
			}
			if minItems, ok := schema["minItems"].(float64); ok {
				if len(arr) < int(minItems) {
					errs = append(errs, fmt.Sprintf("%s: array length %d < minItems %d", path, len(arr), int(minItems)))
				}
			}
			if items, ok := schema["items"].(map[string]any); ok {
				for i, v := range arr {
					errs = append(errs, c.validate(v, items, fmt.Sprintf("%s[%d]", path, i))...)
				}
			}

		case "string":
			if doc == nil {
				return errs
			}
			str, ok := doc.(string)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected string, got %T", path, doc))
				return errs
			}
			if pat, ok := schema["pattern"].(string); ok {
				matched, _ := regexp.MatchString(pat, str)
				if !matched {
					errs = append(errs, fmt.Sprintf("%s: string '%s' does not match pattern '%s'", path, str, pat))
				}
			}
			if minLen, ok := schema["minLength"].(float64); ok {
				if len(str) < int(minLen) {
					errs = append(errs, fmt.Sprintf("%s: string length %d < minLength %d", path, len(str), int(minLen)))
				}
			}
			if maxLen, ok := schema["maxLength"].(float64); ok {
				if len(str) > int(maxLen) {
					errs = append(errs, fmt.Sprintf("%s: string length %d > maxLength %d", path, len(str), int(maxLen)))
				}
			}
			if enum, ok := schema["enum"].([]any); ok {
				found := false
				for _, e := range enum {
					if str == e.(string) {
						found = true
						break
					}
				}
				if !found {
					errs = append(errs, fmt.Sprintf("%s: string '%s' not in enum %v", path, str, enum))
				}
			}

		case "integer":
			if doc == nil {
				return errs
			}
			num, ok := doc.(float64)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected integer, got %T", path, doc))
				return errs
			}
			if float64(int(num)) != num {
				errs = append(errs, fmt.Sprintf("%s: expected integer, got float", path))
			}
			if minVal, ok := schema["minimum"].(float64); ok {
				if num < minVal {
					errs = append(errs, fmt.Sprintf("%s: value %v < minimum %v", path, num, minVal))
				}
			}

		case "number":
			if doc == nil {
				return errs
			}
			num, ok := doc.(float64)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s: expected number, got %T", path, doc))
				return errs
			}
			if minVal, ok := schema["minimum"].(float64); ok {
				if num < minVal {
					errs = append(errs, fmt.Sprintf("%s: value %v < minimum %v", path, num, minVal))
				}
			}
		}
	}

	if constVal, ok := schema["const"]; ok {
		if doc != constVal {
			errs = append(errs, fmt.Sprintf("%s: value %v does not match const %v", path, doc, constVal))
		}
	}

	return errs
}

func TestExampleConformsToSchema(t *testing.T) {
	schema := readSchema(t, "../../schemas/graph/v1/schema.json")
	example := readJSON(t, "../../schemas/graph/v1/example.json")

	checker := &conformanceChecker{
		t:      t,
		root:   schema,
		schema: schema,
	}

	errs := checker.validate(example, schema, "$")
	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("Validation error: %s", e)
		}
	}
}

func TestNegativeCases(t *testing.T) {
	schema := readSchema(t, "../../schemas/graph/v1/schema.json")
	example := readJSON(t, "../../schemas/graph/v1/example.json")

	checker := &conformanceChecker{
		t:      t,
		root:   schema,
		schema: schema,
	}

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "unknown top-level key",
			mutate: func(m map[string]any) {
				m["unknown"] = "value"
			},
			wantErr: "additional property not allowed: unknown",
		},
		{
			name: "bad apiVersion",
			mutate: func(m map[string]any) {
				m["apiVersion"] = "graph/v2"
			},
			wantErr: "value graph/v2 does not match const graph/v1",
		},
		{
			name: "node id with a space",
			mutate: func(m map[string]any) {
				nodes := m["nodes"].([]any)
				n0 := nodes[0].(map[string]any)
				n0["id"] = "bad id"
			},
			wantErr: "does not match pattern",
		},
		{
			name: "maxAttempts 0",
			mutate: func(m map[string]any) {
				rp := m["retryPolicies"].(map[string]any)
				sr := rp["standard_retry"].(map[string]any)
				sr["maxAttempts"] = 0.0
			},
			wantErr: "< minimum 1",
		},
		{
			name: "edge missing 'to'",
			mutate: func(m map[string]any) {
				edges := m["edges"].([]any)
				e0 := edges[0].(map[string]any)
				delete(e0, "to")
			},
			wantErr: "missing required property to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// deep copy
			data, _ := json.Marshal(example)
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("failed to unmarshal JSON: %v", err)
			}

			tt.mutate(doc)

			errs := checker.validate(doc, schema, "$")
			if len(errs) == 0 {
				t.Fatalf("expected error containing %q, got none", tt.wantErr)
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.wantErr) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, errs)
			}
		})
	}
}

func checkSchemaStructure(t *testing.T, schema map[string]any, root map[string]any, path string) {
	if ref, ok := schema["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("%s: non-local ref %s", path, ref)
		} else {
			parts := strings.Split(ref[2:], "/")
			resolved := root
			for _, part := range parts {
				if next, ok := resolved[part].(map[string]any); ok {
					resolved = next
				} else {
					t.Errorf("%s: unresolvable ref %s", path, ref)
					break
				}
			}
		}
	}

	if typ, ok := schema["type"].(string); ok && typ == "object" {
		if addl, ok := schema["additionalProperties"].(bool); !ok || addl {
			if !strings.HasSuffix(path, ".config") {
				t.Errorf("%s: object does not have additionalProperties: false", path)
			}
		}

		if reqs, ok := schema["required"].([]any); ok {
			props, hasProps := schema["properties"].(map[string]any)
			for _, req := range reqs {
				reqStr := req.(string)
				if !hasProps {
					t.Errorf("%s: required property %s but no properties defined", path, reqStr)
				} else if _, ok := props[reqStr]; !ok {
					t.Errorf("%s: required property %s not found in properties", path, reqStr)
				}
			}
		}
	}

	if props, ok := schema["properties"].(map[string]any); ok {
		for k, v := range props {
			if vMap, ok := v.(map[string]any); ok {
				checkSchemaStructure(t, vMap, root, path+".properties."+k)
			}
		}
	}

	if patternProps, ok := schema["patternProperties"].(map[string]any); ok {
		for k, v := range patternProps {
			if vMap, ok := v.(map[string]any); ok {
				checkSchemaStructure(t, vMap, root, path+".patternProperties."+k)
			}
		}
	}

	if items, ok := schema["items"].(map[string]any); ok {
		checkSchemaStructure(t, items, root, path+".items")
	}

	if defs, ok := schema["$defs"].(map[string]any); ok {
		for k, v := range defs {
			if vMap, ok := v.(map[string]any); ok {
				checkSchemaStructure(t, vMap, root, path+".$defs."+k)
			}
		}
	}
}

func TestSchemaStructuralSanity(t *testing.T) {
	schema := readSchema(t, "../../schemas/graph/v1/schema.json")
	checkSchemaStructure(t, schema, schema, "$")
}

type fakeSignalSource struct{}

func (f *fakeSignalSource) Wait(_ context.Context, _ engine.ExecutionID, _ engine.NodeID, _ string) (map[string]any, error) {
	return nil, nil
}

func TestExampleNodesInstantiate(t *testing.T) {
	example := readJSON(t, "../../schemas/graph/v1/example.json")
	reg := engine.NewRegistry()

	deps := nodes.Dependencies{
		Signals: &fakeSignalSource{},
	}
	if err := nodes.RegisterDefaults(reg, deps); err != nil {
		t.Fatalf("failed to register defaults: %v", err)
	}

	nodesList, ok := example["nodes"].([]any)
	if !ok {
		t.Fatalf("nodes not found or not an array")
	}

	for _, n := range nodesList {
		nodeMap := n.(map[string]any)
		typ := nodeMap["type"].(string)
		config, ok := nodeMap["config"].(map[string]any)
		if !ok {
			config = nil
		}

		_, err := reg.Instantiate(typ, config)
		if err != nil {
			t.Errorf("failed to instantiate node %s of type %s: %v", nodeMap["id"], typ, err)
		}
	}

	compensations, ok := example["compensations"].(map[string]any)
	if ok {
		for name, c := range compensations {
			compMap := c.(map[string]any)
			typ := compMap["type"].(string)
			config, ok := compMap["config"].(map[string]any)
			if !ok {
				config = nil
			}

			_, err := reg.Instantiate(typ, config)
			if err != nil {
				t.Errorf("failed to instantiate compensation %s of type %s: %v", name, typ, err)
			}
		}
	}
}
