package graph

import (
	"fmt"
	"sort"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

func bindDefinition(tree any) (*engine.GraphDefinition, error) {
	errs := &engine.ValidationErrors{}
	path := engine.PathRoot

	if tree == nil {
		errs.Add(string(path), engine.CodeInvalidType, "expected object, got null")
		return nil, errs.Err()
	}

	rootMap, ok := tree.(map[string]any)
	if !ok {
		errs.Add(string(path), engine.CodeInvalidType, fmt.Sprintf("expected object, got %T", tree))
		return nil, errs.Err()
	}

	def := &engine.GraphDefinition{}
	bindGraphDefinition(rootMap, def, path, errs)

	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	return def, nil
}

func bindGraphDefinition(m map[string]any, def *engine.GraphDefinition, path engine.Path, errs *engine.ValidationErrors) {
	keys := sortedKeys(m)

	hasAPIVersion := false
	hasID := false
	hasNodes := false

	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "apiVersion":
			hasAPIVersion = true
			if s, ok := bindString(v, p, errs); ok {
				if s != "graph/v1" {
					errs.Add(string(p), engine.CodeUnsupportedVersion, fmt.Sprintf("unsupported API version: %s", s))
				}
				def.APIVersion = s
			}
		case "id":
			hasID = true
			if s, ok := bindString(v, p, errs); ok {
				def.ID = s
			}
		case "name":
			if s, ok := bindString(v, p, errs); ok {
				def.Name = s
			}
		case "description":
			if s, ok := bindString(v, p, errs); ok {
				def.Description = s
			}
		case "nodes":
			hasNodes = true
			if arr, ok := bindArray(v, p, errs); ok {
				def.Nodes = bindNodeDefinitions(arr, p, errs)
			}
		case "edges":
			if arr, ok := bindArray(v, p, errs); ok {
				def.Edges = bindEdgeDefinitions(arr, p, errs)
			}
		case "retryPolicies":
			if obj, ok := bindObject(v, p, errs); ok {
				def.RetryPolicies = bindRetryPolicies(obj, p, errs)
			}
		case "compensations":
			if obj, ok := bindObject(v, p, errs); ok {
				def.Compensations = bindCompensations(obj, p, errs)
			}
		case "failurePolicy":
			if obj, ok := bindObject(v, p, errs); ok {
				def.FailurePolicy = bindFailurePolicy(obj, p, errs)
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}

	if !hasAPIVersion {
		errs.Add(string(path.Field("apiVersion")), engine.CodeMissingRequired, "missing required field 'apiVersion'")
	}
	if !hasID {
		errs.Add(string(path.Field("id")), engine.CodeMissingRequired, "missing required field 'id'")
	}
	if !hasNodes {
		errs.Add(string(path.Field("nodes")), engine.CodeMissingRequired, "missing required field 'nodes'")
	}
}

func bindNodeDefinitions(arr []any, path engine.Path, errs *engine.ValidationErrors) []engine.NodeDefinition {
	var nodes []engine.NodeDefinition
	for i, item := range arr {
		p := path.Index(i)
		if obj, ok := bindObject(item, p, errs); ok {
			nodes = append(nodes, bindNodeDefinition(obj, p, errs))
		}
	}
	return nodes
}

func bindNodeDefinition(m map[string]any, path engine.Path, errs *engine.ValidationErrors) engine.NodeDefinition {
	var n engine.NodeDefinition
	keys := sortedKeys(m)
	hasID := false
	hasType := false

	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "id":
			hasID = true
			if s, ok := bindString(v, p, errs); ok {
				n.ID = engine.NodeID(s)
			}
		case "type":
			hasType = true
			if s, ok := bindString(v, p, errs); ok {
				n.Type = s
			}
		case "config":
			if obj, ok := bindObject(v, p, errs); ok {
				n.Config = obj
			}
		case "retryPolicy":
			if s, ok := bindString(v, p, errs); ok {
				n.RetryPolicy = s
			}
		case "compensation":
			if s, ok := bindString(v, p, errs); ok {
				n.Compensation = s
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}

	if !hasID {
		errs.Add(string(path.Field("id")), engine.CodeMissingRequired, "missing required field 'id'")
	}
	if !hasType {
		errs.Add(string(path.Field("type")), engine.CodeMissingRequired, "missing required field 'type'")
	}
	return n
}

func bindEdgeDefinitions(arr []any, path engine.Path, errs *engine.ValidationErrors) []engine.EdgeDefinition {
	var edges []engine.EdgeDefinition
	for i, item := range arr {
		p := path.Index(i)
		if obj, ok := bindObject(item, p, errs); ok {
			edges = append(edges, bindEdgeDefinition(obj, p, errs))
		}
	}
	return edges
}

func bindEdgeDefinition(m map[string]any, path engine.Path, errs *engine.ValidationErrors) engine.EdgeDefinition {
	var e engine.EdgeDefinition
	keys := sortedKeys(m)
	hasFrom := false
	hasTo := false

	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "from":
			hasFrom = true
			if s, ok := bindString(v, p, errs); ok {
				e.From = engine.NodeID(s)
			}
		case "to":
			hasTo = true
			if s, ok := bindString(v, p, errs); ok {
				e.To = engine.NodeID(s)
			}
		case "condition":
			if s, ok := bindString(v, p, errs); ok {
				e.Condition = s
			}
		case "dataMapping":
			if obj, ok := bindObject(v, p, errs); ok {
				e.DataMapping = bindStringMap(obj, p, errs)
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}

	if !hasFrom {
		errs.Add(string(path.Field("from")), engine.CodeMissingRequired, "missing required field 'from'")
	}
	if !hasTo {
		errs.Add(string(path.Field("to")), engine.CodeMissingRequired, "missing required field 'to'")
	}
	return e
}

func bindRetryPolicies(m map[string]any, path engine.Path, errs *engine.ValidationErrors) map[string]engine.RetryPolicy {
	policies := make(map[string]engine.RetryPolicy)
	keys := sortedKeys(m)
	for _, k := range keys {
		p := path.Field(k)
		if obj, ok := bindObject(m[k], p, errs); ok {
			policies[k] = bindRetryPolicy(obj, p, errs)
		}
	}
	return policies
}

func bindRetryPolicy(m map[string]any, path engine.Path, errs *engine.ValidationErrors) engine.RetryPolicy {
	var rp engine.RetryPolicy
	keys := sortedKeys(m)
	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "maxAttempts":
			if val, ok := bindInt(v, p, errs); ok {
				rp.MaxAttempts = &val
			}
		case "initialIntervalMs":
			if val, ok := bindInt(v, p, errs); ok {
				rp.InitialIntervalMs = &val
			}
		case "backoffCoefficient":
			if val, ok := bindFloat(v, p, errs); ok {
				rp.BackoffCoefficient = &val
			}
		case "maxIntervalMs":
			if val, ok := bindInt(v, p, errs); ok {
				rp.MaxIntervalMs = &val
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}
	return rp
}

func bindCompensations(m map[string]any, path engine.Path, errs *engine.ValidationErrors) map[string]engine.CompensationDefinition {
	comps := make(map[string]engine.CompensationDefinition)
	keys := sortedKeys(m)
	for _, k := range keys {
		p := path.Field(k)
		if obj, ok := bindObject(m[k], p, errs); ok {
			comps[k] = bindCompensationDefinition(obj, p, errs)
		}
	}
	return comps
}

func bindCompensationDefinition(m map[string]any, path engine.Path, errs *engine.ValidationErrors) engine.CompensationDefinition {
	var c engine.CompensationDefinition
	keys := sortedKeys(m)
	hasType := false

	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "type":
			hasType = true
			if s, ok := bindString(v, p, errs); ok {
				c.Type = s
			}
		case "config":
			if obj, ok := bindObject(v, p, errs); ok {
				c.Config = obj
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}
	if !hasType {
		errs.Add(string(path.Field("type")), engine.CodeMissingRequired, "missing required field 'type'")
	}
	return c
}

func bindFailurePolicy(m map[string]any, path engine.Path, errs *engine.ValidationErrors) *engine.FailurePolicy {
	fp := &engine.FailurePolicy{}
	keys := sortedKeys(m)
	for _, k := range keys {
		v := m[k]
		p := path.Field(k)
		switch k {
		case "onNodeFailure":
			if s, ok := bindString(v, p, errs); ok {
				fp.OnNodeFailure = s
			}
		case "onCriticalFailure":
			if s, ok := bindString(v, p, errs); ok {
				fp.OnCriticalFailure = s
			}
		default:
			errs.Add(string(p), engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", k))
		}
	}
	return fp
}

func bindString(v any, p engine.Path, errs *engine.ValidationErrors) (string, bool) {
	if s, ok := v.(string); ok {
		return s, true
	}
	errs.Add(string(p), engine.CodeInvalidType, fmt.Sprintf("expected string, got %T", v))
	return "", false
}

func bindInt(v any, p engine.Path, errs *engine.ValidationErrors) (int, bool) {
	if f, ok := v.(float64); ok {
		if float64(int(f)) == f {
			return int(f), true
		}
		errs.Add(string(p), engine.CodeInvalidType, "expected integer, got float")
		return 0, false
	}
	errs.Add(string(p), engine.CodeInvalidType, fmt.Sprintf("expected integer, got %T", v))
	return 0, false
}

func bindFloat(v any, p engine.Path, errs *engine.ValidationErrors) (float64, bool) {
	if f, ok := v.(float64); ok {
		return f, true
	}
	errs.Add(string(p), engine.CodeInvalidType, fmt.Sprintf("expected float, got %T", v))
	return 0, false
}

func bindArray(v any, p engine.Path, errs *engine.ValidationErrors) ([]any, bool) {
	if arr, ok := v.([]any); ok {
		return arr, true
	}
	errs.Add(string(p), engine.CodeInvalidType, fmt.Sprintf("expected array, got %T", v))
	return nil, false
}

func bindObject(v any, p engine.Path, errs *engine.ValidationErrors) (map[string]any, bool) {
	if obj, ok := v.(map[string]any); ok {
		return obj, true
	}
	errs.Add(string(p), engine.CodeInvalidType, fmt.Sprintf("expected object, got %T", v))
	return nil, false
}

func bindStringMap(m map[string]any, path engine.Path, errs *engine.ValidationErrors) map[string]string {
	sm := make(map[string]string)
	keys := sortedKeys(m)
	for _, k := range keys {
		p := path.Field(k)
		if s, ok := bindString(m[k], p, errs); ok {
			sm[k] = s
		}
	}
	return sm
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
