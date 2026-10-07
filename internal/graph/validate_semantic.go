package graph

import (
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
		if node.Type != "" {
			if !reg.Has(node.Type) {
				errs.Add(nodePath.Field("type").String(), engine.CodeUnknownNodeType, "unknown node type")
			} else {
				if err := reg.ValidateConfig(node.Type, node.Config); err != nil {
					// Extract concise message by stripping the "invalid node config: " prefix.
					// We only strip the exact prefix to avoid accidentally losing context.
					msg := err.Error()
					prefix := engine.ErrInvalidConfig.Error() + ": "
					msg = strings.TrimPrefix(msg, prefix)
					errs.Add(nodePath.Field("config").String(), engine.CodeInvalidNodeConfig, msg)
				}
			}
		}

		// unresolved references
		if node.RetryPolicy != "" {
			if _, ok := def.RetryPolicies[node.RetryPolicy]; !ok {
				errs.Add(nodePath.Field("retryPolicy").String(), engine.CodeUnresolvedReference, "unresolved reference")
			}
		}

		if node.Compensation != "" {
			if _, ok := def.Compensations[node.Compensation]; !ok {
				errs.Add(nodePath.Field("compensation").String(), engine.CodeUnresolvedReference, "unresolved reference")
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

			if comp.Type != "" {
				if !reg.Has(comp.Type) {
					errs.Add(cPath.Field("type").String(), engine.CodeUnknownNodeType, "unknown node type")
				} else {
					if err := reg.ValidateConfig(comp.Type, comp.Config); err != nil {
						msg := err.Error()
						prefix := engine.ErrInvalidConfig.Error() + ": "
						msg = strings.TrimPrefix(msg, prefix)
						errs.Add(cPath.Field("config").String(), engine.CodeInvalidNodeConfig, msg)
					}
				}
			}
		}
	}

	// Sort the issues deterministically like ValidateStructure
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
