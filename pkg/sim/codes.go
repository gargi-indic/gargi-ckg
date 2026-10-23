package sim

import "github.com/gargi-indic/gargi-flow/pkg/engine"

// Simulation Engine codes represent public evaluation identifiers for engine checks.
const (
	CodeInvalidObservePath        engine.ErrorCode = "invalid_observe_path"
	CodeObservesNonAncestor       engine.ErrorCode = "observes_non_ancestor"
	CodeBehaviorMissing           engine.ErrorCode = "behavior_missing"
	CodeBehaviorUnknownNode       engine.ErrorCode = "behavior_unknown_node"
	CodeInvalidExpression         engine.ErrorCode = "invalid_expression"
	CodePolicyUnobservedReference engine.ErrorCode = "policy_unobserved_reference"
	CodePolicyUnknownOption       engine.ErrorCode = "policy_unknown_option"
	CodePolicyMissingDecision     engine.ErrorCode = "policy_missing_decision"
	CodePolicyUnknownDecision     engine.ErrorCode = "policy_unknown_decision"
	CodeModelNotDiscrete          engine.ErrorCode = "model_not_discrete"
	CodeTooManyPolicies           engine.ErrorCode = "too_many_policies"
)
