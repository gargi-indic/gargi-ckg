package graph

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

var (
	identifierRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	typeNameRegex   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
)

// ValidateStructure checks the semantic structure and schema values of a GraphDefinition.
// It returns a non-nil *engine.ValidationErrors. If there are no errors, it will be empty.

// ValidateStructure checks the semantic structure and schema values of a GraphDefinition.
// It returns a non-nil *engine.ValidationErrors. If there are no errors, it will be empty.
func ValidateStructure(def *engine.GraphDefinition) *engine.ValidationErrors {
	errs := &engine.ValidationErrors{}

	if def == nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidType, "definition is nil")
		return errs
	}

	// 1. nodes non-empty
	if len(def.Nodes) == 0 {
		errs.Add(engine.PathRoot.Field("nodes").String(), engine.CodeMissingRequired, "nodes must contain at least one node")
	}

	// 2. Value patterns
	if !identifierRegex.MatchString(def.ID) {
		errs.Add(engine.PathRoot.Field("id").String(), engine.CodeInvalidValue, "invalid identifier pattern")
	}

	nodeIDs := make(map[string]int)
	duplicateNodeIDs := false
	danglingEdge := false
	selfLoop := false
	badIDs := false

	// Check Nodes
	for i, node := range def.Nodes {
		nodePath := engine.PathRoot.Field("nodes").Index(i)

		if !identifierRegex.MatchString(string(node.ID)) {
			errs.Add(nodePath.Field("id").String(), engine.CodeInvalidValue, "invalid identifier pattern")
			badIDs = true
		} else {
			if firstIdx, exists := nodeIDs[string(node.ID)]; exists {
				errs.Add(nodePath.Field("id").String(), engine.CodeDuplicateID, fmt.Sprintf("duplicate of node at index %d", firstIdx))
				duplicateNodeIDs = true
			} else {
				nodeIDs[string(node.ID)] = i
			}
		}

		if !typeNameRegex.MatchString(node.Type) {
			errs.Add(nodePath.Field("type").String(), engine.CodeInvalidValue, "invalid type name pattern")
		}

		if node.StartToCloseTimeoutMs != nil && *node.StartToCloseTimeoutMs < 1 {
			errs.Add(nodePath.Field("startToCloseTimeoutMs").String(), engine.CodeInvalidValue, "must be >= 1")
		}

		if node.ScheduleToCloseTimeoutMs != nil && *node.ScheduleToCloseTimeoutMs < 1 {
			errs.Add(nodePath.Field("scheduleToCloseTimeoutMs").String(), engine.CodeInvalidValue, "must be >= 1")
		}

		if node.JoinMode != "" && !node.JoinMode.Valid() {
			errs.Add(nodePath.Field("joinMode").String(), engine.CodeInvalidValue, "joinMode must be all_of or any_of")
		}

		if node.RetryPolicy != "" && !identifierRegex.MatchString(node.RetryPolicy) {
			errs.Add(nodePath.Field("retryPolicy").String(), engine.CodeInvalidValue, "invalid identifier pattern")
		}

		if node.Compensation != "" && !identifierRegex.MatchString(node.Compensation) {
			errs.Add(nodePath.Field("compensation").String(), engine.CodeInvalidValue, "invalid identifier pattern")
		}
	}

	validNodeIDs := make([]string, 0, len(def.Nodes))
	for _, n := range def.Nodes {
		if n.ID != "" && identifierRegex.MatchString(string(n.ID)) {
			validNodeIDs = append(validNodeIDs, string(n.ID))
		}
	}
	sort.Strings(validNodeIDs)

	// Check Edges
	type edgeKey struct {
		from engine.NodeID
		to   engine.NodeID
	}
	seenEdges := make(map[edgeKey]int)

	for i, edge := range def.Edges {
		edgePath := engine.PathRoot.Field("edges").Index(i)

		if !identifierRegex.MatchString(string(edge.From)) {
			errs.Add(edgePath.Field("from").String(), engine.CodeInvalidValue, "invalid identifier pattern")
			badIDs = true
		}

		if !identifierRegex.MatchString(string(edge.To)) {
			errs.Add(edgePath.Field("to").String(), engine.CodeInvalidValue, "invalid identifier pattern")
			badIDs = true
		}

		if identifierRegex.MatchString(string(edge.From)) {
			if _, exists := nodeIDs[string(edge.From)]; !exists && edge.From != "" {
				errs.Add(edgePath.Field("from").String(), engine.CodeDanglingEdge, "references non-existent node")
				danglingEdge = true
			}
		}

		if identifierRegex.MatchString(string(edge.To)) {
			if _, exists := nodeIDs[string(edge.To)]; !exists && edge.To != "" {
				errs.Add(edgePath.Field("to").String(), engine.CodeDanglingEdge, "references non-existent node")
				danglingEdge = true
			}
		}

		if edge.From == edge.To && edge.From != "" {
			errs.Add(edgePath.String(), engine.CodeSelfLoop, "edge from and to must not be identical")
			selfLoop = true
		}

		ek := edgeKey{from: edge.From, to: edge.To}
		if firstIdx, exists := seenEdges[ek]; exists {
			errs.Add(edgePath.String(), engine.CodeInvalidValue, fmt.Sprintf("duplicate edge of index %d", firstIdx))
		} else {
			seenEdges[ek] = i
		}

		if edge.Condition != "" {
			if utf8.RuneCountInString(edge.Condition) > 4096 {
				errs.Add(edgePath.Field("condition").String(), engine.CodeInvalidCondition, "condition length must be 1..4096 runes")
			} else {
				if err := ValidateCondition(edge.Condition, validNodeIDs...); err != nil {
					errs.Add(edgePath.Field("condition").String(), engine.CodeInvalidCondition, err.Error())
				}
			}
		}

		if edge.DataMapping != nil {
			keys := make([]string, 0, len(edge.DataMapping))
			for k := range edge.DataMapping {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, k := range keys {
				v := edge.DataMapping[k]
				mapPath := edgePath.Field("dataMapping").Field(k).String()

				if k == "" {
					errs.Add(mapPath, engine.CodeInvalidValue, "empty key in data mapping")
				}
				if v == "" {
					errs.Add(mapPath, engine.CodeInvalidValue, "empty value in data mapping")
				}
			}
		}
	}

	// Check retryPolicies
	if def.RetryPolicies != nil {
		rKeys := make([]string, 0, len(def.RetryPolicies))
		for k := range def.RetryPolicies {
			rKeys = append(rKeys, k)
		}
		sort.Strings(rKeys)
		for _, k := range rKeys {
			pPath := engine.PathRoot.Field("retryPolicies").Field(k)
			if !identifierRegex.MatchString(k) {
				errs.Add(pPath.String(), engine.CodeInvalidValue, "invalid identifier pattern for map key")
			}

			policy := def.RetryPolicies[k]
			if policy.MaxAttempts != nil && *policy.MaxAttempts < 1 {
				errs.Add(pPath.Field("maxAttempts").String(), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.InitialIntervalMs != nil && *policy.InitialIntervalMs < 1 {
				errs.Add(pPath.Field("initialIntervalMs").String(), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.BackoffCoefficient != nil && *policy.BackoffCoefficient < 1 {
				errs.Add(pPath.Field("backoffCoefficient").String(), engine.CodeInvalidValue, "must be >= 1")
			}
			if policy.MaxIntervalMs != nil && *policy.MaxIntervalMs < 1 {
				errs.Add(pPath.Field("maxIntervalMs").String(), engine.CodeInvalidValue, "must be >= 1")
			}
		}
	}

	// Check compensations
	if def.Compensations != nil {
		cKeys := make([]string, 0, len(def.Compensations))
		for k := range def.Compensations {
			cKeys = append(cKeys, k)
		}
		sort.Strings(cKeys)
		for _, k := range cKeys {
			cPath := engine.PathRoot.Field("compensations").Field(k)
			if !identifierRegex.MatchString(k) {
				errs.Add(cPath.String(), engine.CodeInvalidValue, "invalid identifier pattern for map key")
			}

			comp := def.Compensations[k]
			if !typeNameRegex.MatchString(comp.Type) {
				errs.Add(cPath.Field("type").String(), engine.CodeInvalidValue, "invalid type name pattern")
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
				errs.Add(engine.PathRoot.Field("failurePolicy").Field("onNodeFailure").String(), engine.CodeInvalidValue, "must be fail_graph or continue")
			}
		}
		if def.FailurePolicy.OnCriticalFailure != "" {
			switch def.FailurePolicy.OnCriticalFailure {
			case "compensate", "fail_graph":
				// valid
			default:
				errs.Add(engine.PathRoot.Field("failurePolicy").Field("onCriticalFailure").String(), engine.CodeInvalidValue, "must be compensate or fail_graph")
			}
		}
	}

	// Graph structural checks only if there are no ID, Dangling Edge, or Self Loop errors and nodes exist.
	if !badIDs && !duplicateNodeIDs && !danglingEdge && !selfLoop && len(def.Nodes) > 0 {
		g, err := def.ToGraph()
		if err != nil {
			errs.Add(engine.PathRoot.String(), engine.CodeInvalidValue, err.Error())
		} else {
			if cycle := FindCycle(g); cycle != nil {
				cycleStrs := make([]string, len(cycle))
				for i, id := range cycle {
					cycleStrs[i] = string(id)
				}
				errs.Add(engine.PathRoot.Field("edges").String(), engine.CodeCycle, fmt.Sprintf("cycle detected: %s", strings.Join(cycleStrs, " -> ")))
			} else {
				if len(Roots(g)) == 0 {
					errs.Add(engine.PathRoot.Field("nodes").String(), engine.CodeNoRoot, "graph has no root nodes")
				}
				if len(Leaves(g)) == 0 {
					errs.Add(engine.PathRoot.Field("nodes").String(), engine.CodeNoLeaf, "graph has no leaf nodes")
				}
			}
		}
	}

	// Sort the issues deterministically
	// Issues returns a fresh copy, which is safe to mutate.
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
