package orchestrator

import (
	"errors"
	"time"

	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"go.temporal.io/sdk/temporal"
)

const (
	// ErrorTypeRetryable indicates the error is retryable.
	ErrorTypeRetryable = "gargi.retryable"
	// ErrorTypeNonRetryable indicates the error is not retryable.
	ErrorTypeNonRetryable = "gargi.non_retryable"
	// ErrorTypeCritical indicates a severe failure that must trigger Saga compensation workflows.
	ErrorTypeCritical = "gargi.critical"

	// DefaultRetryInitialInterval is the default initial interval for activity retries.
	DefaultRetryInitialInterval = 1 * time.Second
	// DefaultRetryBackoffCoefficient is the default backoff coefficient for activity retries.
	DefaultRetryBackoffCoefficient = 2.0
	// DefaultRetryMaxInterval is the default max interval for activity retries.
	DefaultRetryMaxInterval = 100 * time.Second
	// DefaultRetryMaxAttempts is the default max attempts for activity retries.
	DefaultRetryMaxAttempts = 1
)

// BuildActivityRetryPolicy converts engine.RetryPolicy into temporal.RetryPolicy.
func BuildActivityRetryPolicy(
	nodePolicy *engine.RetryPolicy,
	defaultPolicy *engine.RetryPolicy,
) *temporal.RetryPolicy {
	rp := &temporal.RetryPolicy{
		InitialInterval:        DefaultRetryInitialInterval,
		BackoffCoefficient:     DefaultRetryBackoffCoefficient,
		MaximumInterval:        DefaultRetryMaxInterval,
		MaximumAttempts:        DefaultRetryMaxAttempts,
		NonRetryableErrorTypes: []string{ErrorTypeNonRetryable, ErrorTypeCritical},
	}

	applyPolicy := func(p *engine.RetryPolicy) {
		if p == nil {
			return
		}
		if p.InitialIntervalMs != nil && *p.InitialIntervalMs > 0 {
			rp.InitialInterval = time.Duration(*p.InitialIntervalMs) * time.Millisecond
		}
		if p.BackoffCoefficient != nil && *p.BackoffCoefficient > 0 {
			rp.BackoffCoefficient = *p.BackoffCoefficient
		}
		if p.MaxIntervalMs != nil && *p.MaxIntervalMs > 0 {
			rp.MaximumInterval = time.Duration(*p.MaxIntervalMs) * time.Millisecond
		}
		if p.MaxAttempts != nil && *p.MaxAttempts > 0 {
			rp.MaximumAttempts = int32(*p.MaxAttempts)
		}
	}

	applyPolicy(defaultPolicy)
	applyPolicy(nodePolicy)

	return rp
}

// ResolveNodeRetryPolicy resolves the effective temporal.RetryPolicy for a node definition.
func ResolveNodeRetryPolicy(
	nodeDef *engine.NodeDefinition,
	retryPolicies map[string]engine.RetryPolicy,
) *temporal.RetryPolicy {
	var defPol *engine.RetryPolicy
	if p, ok := retryPolicies["default"]; ok {
		defPol = &p
	}

	var nodePol *engine.RetryPolicy
	if nodeDef != nil && nodeDef.RetryPolicy != "" {
		if p, ok := retryPolicies[nodeDef.RetryPolicy]; ok {
			nodePol = &p
		}
	}

	return BuildActivityRetryPolicy(nodePol, defPol)
}

// WrapTemporalError maps engine error classifications to Temporal application errors.
func WrapTemporalError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return err
	}

	class := engine.ClassOf(err)
	switch class {
	case engine.ClassRetryable:
		return temporal.NewApplicationErrorWithCause(err.Error(), ErrorTypeRetryable, err)
	case engine.ClassCritical:
		return temporal.NewNonRetryableApplicationError(err.Error(), ErrorTypeCritical, err)
	case engine.ClassNonRetryable:
		fallthrough
	default:
		return temporal.NewNonRetryableApplicationError(err.Error(), ErrorTypeNonRetryable, err)
	}
}

// IsCriticalError checks whether an error represents engine.ClassCritical.
func IsCriticalError(err error) bool {
	if err == nil {
		return false
	}
	if engine.IsCritical(err) {
		return true
	}

	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		if appErr.Type() == ErrorTypeCritical {
			return true
		}
	}

	return false
}
