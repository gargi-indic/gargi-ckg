package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

// ValidateSemantics validates node types and their configurations against a node registry,
// as well as references to retry policies and compensations.
func ValidateSemantics(def *engine.GraphDefinition, reg *engine.Registry) *engine.ValidationErrors {
	errs := &engine.ValidationErrors{}

	if def == nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidType, "definition is nil")
		return errs
	}

	if reg == nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, "registry is nil")
		return errs
	}

	// 1. Check Nodes
	for i, node := range def.Nodes {
		nodePath := engine.PathRoot.Field("nodes").Index(i)

		// type validation
		validateNodeConfig(reg, nodePath, node.Type, node.Config, errs)

		// unresolved references
		if node.RetryPolicy != "" {
			if _, ok := def.RetryPolicies[node.RetryPolicy]; !ok {
				errs.Add(nodePath.Field("retryPolicy").String(), engine.CodeUnresolvedReference, fmt.Sprintf("retry policy %q is not defined in retryPolicies", node.RetryPolicy))
			}
		}

		if node.Compensation != "" {
			if _, ok := def.Compensations[node.Compensation]; !ok {
				errs.Add(nodePath.Field("compensation").String(), engine.CodeUnresolvedReference, fmt.Sprintf("compensation %q is not defined in compensations", node.Compensation))
			}
		}
	}

	// 2. Check Compensations
	if def.Compensations != nil {
		cKeys := make([]string, 0, len(def.Compensations))
		for k := range def.Compensations {
			cKeys = append(cKeys, k)
		}
		sort.Strings(cKeys)
		for _, k := range cKeys {
			cPath := engine.PathRoot.Field("compensations").Field(k)
			comp := def.Compensations[k]
			validateNodeConfig(reg, cPath, comp.Type, comp.Config, errs)
		}
	}

	return sortValidationErrors(errs)
}

func validateNodeConfig(reg *engine.Registry, path engine.Path, typeName string, config map[string]any, errs *engine.ValidationErrors) {
	if typeName == "" {
		return
	}
	if !reg.Has(typeName) {
		errs.Add(path.Field("type").String(), engine.CodeUnknownNodeType, fmt.Sprintf("unknown node type %q", typeName))
	} else {
		if err := reg.ValidateConfig(typeName, config); err != nil {
			// Extract concise message by stripping the "invalid node config: " prefix.
			// We only strip the exact prefix to avoid accidentally losing context.
			msg := err.Error()
			prefix := engine.ErrInvalidConfig.Error() + ": "
			msg = strings.TrimPrefix(msg, prefix)
			errs.Add(path.Field("config").String(), engine.CodeInvalidNodeConfig, msg)
		}
	}
}

func sortValidationErrors(errs *engine.ValidationErrors) *engine.ValidationErrors {
	issues := errs.Issues()
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})

	sortedErrs := &engine.ValidationErrors{}
	for _, iss := range issues {
		sortedErrs.AddIssue(iss)
	}

	return sortedErrs
}
