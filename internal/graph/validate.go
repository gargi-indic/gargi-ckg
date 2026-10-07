package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

var (
	identifierRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	typeNameRegex   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
)

// ValidateStructure checks the semantic structure and schema values of a GraphDefinition.
// It returns a non-nil *engine.ValidationErrors. If there are no errors, it will be empty.
func ValidateStructure(def *engine.GraphDefinition) *engine.ValidationErrors {
	errs := &engine.ValidationErrors{}

	if def == nil {
		errs.Add("$", engine.CodeInvalidType, "definition is nil")
		return errs
	}

	// 1. nodes non-empty
	if len(def.Nodes) == 0 {
		errs.Add("$.nodes", engine.CodeMissingRequired, "nodes must contain at least one node")
	}

	// 2. Value patterns
	if !identifierRegex.MatchString(def.ID) {
		errs.Add("$.id", engine.CodeInvalidValue, "invalid identifier pattern")
	}

	nodeIDs := make(map[string]int)
	duplicateNodeIDs := false
	danglingEdge := false
	selfLoop := false

	// Check Nodes
	for i, node := range def.Nodes {
		nodePath := fmt.Sprintf("$.nodes[%d]", i)
		if !identifierRegex.MatchString(string(node.ID)) {
			errs.Add(fmt.Sprintf("%s.id", nodePath), engine.CodeInvalidValue, "invalid identifier pattern")
		} else {
			if firstIdx, exists := nodeIDs[string(node.ID)]; exists {
				errs.Add(fmt.Sprintf("%s.id", nodePath), engine.CodeDuplicateID, fmt.Sprintf("duplicate of node at index %d", firstIdx))
				duplicateNodeIDs = true
			} else {
				nodeIDs[string(node.ID)] = i
			}
		}
		if !typeNameRegex.MatchString(node.Type) {
			errs.Add(fmt.Sprintf("%s.type", nodePath), engine.CodeInvalidValue, "invalid type name pattern")
		}
		if node.RetryPolicy != "" && !identifierRegex.MatchString(node.RetryPolicy) {
			errs.Add(fmt.Sprintf("%s.retryPolicy", nodePath), engine.CodeInvalidValue, "invalid identifier pattern")
		}
		if node.Compensation != "" && !identifierRegex.MatchString(node.Compensation) {
			errs.Add(fmt.Sprintf("%s.compensation", nodePath), engine.CodeInvalidValue, "invalid identifier pattern")
		}
	}

	// Check Edges
	seenEdges := make(map[string]int)
	for i, edge := range def.Edges {
		edgePath := fmt.Sprintf("$.edges[%d]", i)

		if !identifierRegex.MatchString(string(edge.From)) {
			errs.Add(fmt.Sprintf("%s.from", edgePath), engine.CodeInvalidValue, "invalid identifier pattern")
		}
		if !identifierRegex.MatchString(string(edge.To)) {
			errs.Add(fmt.Sprintf("%s.to", edgePath), engine.CodeInvalidValue, "invalid identifier pattern")
		}

		if _, exists := nodeIDs[string(edge.From)]; !exists && edge.From != "" {
			errs.Add(fmt.Sprintf("%s.from", edgePath), engine.CodeDanglingEdge, "references non-existent node")
			danglingEdge = true
		}
		if _, exists := nodeIDs[string(edge.To)]; !exists && edge.To != "" {
			errs.Add(fmt.Sprintf("%s.to", edgePath), engine.CodeDanglingEdge, "references non-existent node")
			danglingEdge = true
		}

		if edge.From == edge.To && edge.From != "" {
			errs.Add(edgePath, engine.CodeSelfLoop, "edge from and to must not be identical")
			selfLoop = true
		}

		edgeKey := fmt.Sprintf("%s->%s", edge.From, edge.To)
		if firstIdx, exists := seenEdges[edgeKey]; exists {
			errs.Add(edgePath, engine.CodeInvalidValue, fmt.Sprintf("duplicate edge of index %d", firstIdx))
		} else {
			seenEdges[edgeKey] = i
		}

		if edge.Condition != "" {
			if len(edge.Condition) < 1 || len(edge.Condition) > 4096 {
				errs.Add(fmt.Sprintf("%s.condition", edgePath), engine.CodeInvalidCondition, "condition length must be 1..4096")
			}
		}

		if edge.DataMapping != nil {
			var keys []string
			for k := range edge.DataMapping {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := edge.DataMapping[k]
				if k == "" {
					errs.Add(fmt.Sprintf("%s.dataMapping.%s", edgePath, k), engine.CodeInvalidValue, "empty key in data mapping")
				}
				if v == "" {
					errs.Add(fmt.Sprintf("%s.dataMapping.%s", edgePath, k), engine.CodeInvalidValue, "empty value in data mapping")
				}
			}
		}
	}

	// Check retryPolicies
	if def.RetryPolicies != nil {
		var rKeys []string
		for k := range def.RetryPolicies {
			rKeys = append(rKeys, k)
		}
		sort.Strings(rKeys)
		for _, k := range rKeys {
			if !identifierRegex.MatchString(k) {
				errs.Add(fmt.Sprintf("$.retryPolicies.%s", k), engine.CodeInvalidValue, "invalid identifier pattern for map key")
			}
			policy := def.RetryPolicies[k]
			pPath := fmt.Sprintf("$.retryPolicies.%s", k)
			if policy.MaxAttempts != nil && *policy.MaxAttempts < 1 {
				errs.Add(fmt.Sprintf("%s.maxAttempts", pPath), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.InitialIntervalMs != nil && *policy.InitialIntervalMs < 1 {
				errs.Add(fmt.Sprintf("%s.initialIntervalMs", pPath), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.BackoffCoefficient != nil && *policy.BackoffCoefficient < 1 {
				errs.Add(fmt.Sprintf("%s.backoffCoefficient", pPath), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.MaxIntervalMs != nil && *policy.MaxIntervalMs < 1 {
				errs.Add(fmt.Sprintf("%s.maxIntervalMs", pPath), engine.CodeInvalidValue, "must be >= 1")
			}
		}
	}

	// Check compensations
	if def.Compensations != nil {
		var cKeys []string
		for k := range def.Compensations {
			cKeys = append(cKeys, k)
		}
		sort.Strings(cKeys)
		for _, k := range cKeys {
			if !identifierRegex.MatchString(k) {
				errs.Add(fmt.Sprintf("$.compensations.%s", k), engine.CodeInvalidValue, "invalid identifier pattern for map key")
			}
			comp := def.Compensations[k]
			cPath := fmt.Sprintf("$.compensations.%s", k)
			if !typeNameRegex.MatchString(comp.Type) {
				errs.Add(fmt.Sprintf("%s.type", cPath), engine.CodeInvalidValue, "invalid type name pattern")
			}
		}
	}

	// Check failurePolicy
	if def.FailurePolicy != nil {
		if def.FailurePolicy.OnNodeFailure != "" {
			switch def.FailurePolicy.OnNodeFailure {
			case "fail_graph", "continue":
				// valid
			default:
				errs.Add("$.failurePolicy.onNodeFailure", engine.CodeInvalidValue, "must be fail_graph or continue")
			}
		}
		if def.FailurePolicy.OnCriticalFailure != "" {
			switch def.FailurePolicy.OnCriticalFailure {
			case "compensate", "fail_graph":
				// valid
			default:
				errs.Add("$.failurePolicy.onCriticalFailure", engine.CodeInvalidValue, "must be compensate or fail_graph")
			}
		}
	}

	// Graph structural checks only if there are no ID, Dangling Edge, or Self Loop errors and nodes exist.
	if !duplicateNodeIDs && !danglingEdge && !selfLoop && len(def.Nodes) > 0 {
		g, err := def.ToGraph()
		if err != nil {
			errs.Add("$", engine.CodeInvalidValue, err.Error())
		} else {
			if cycle := FindCycle(g); cycle != nil {
				cycleStrs := make([]string, len(cycle))
				for i, id := range cycle {
					cycleStrs[i] = string(id)
				}
				errs.Add("$.edges", engine.CodeCycle, fmt.Sprintf("cycle detected: %s", strings.Join(cycleStrs, " -> ")))
			} else {
				if len(Roots(g)) == 0 {
					errs.Add("$.nodes", engine.CodeNoRoot, "graph has no root nodes")
				}
				if len(Leaves(g)) == 0 {
					errs.Add("$.nodes", engine.CodeNoLeaf, "graph has no leaf nodes")
				}
			}
		}
	}

	// Sort the issues deterministically
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
