package model

import (
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

// TestParseExperiment_Search checks the policies.search values: exhaustive
// and exact are accepted, anything else is rejected.
func TestParseExperiment_Search(t *testing.T) {
	doc := func(search string) []byte {
		return []byte("apiVersion: experiment/v1\nmodel: ./sim.yaml\npolicies: {search: " + search + "}\nevaluator: exact\nruns: 1\nseed: 1\n")
	}
	for _, search := range []string{"exhaustive", "exact"} {
		exp, err := ParseExperiment(doc(search), engine.FormatYAML)
		require.NoError(t, err, search)
		require.Equal(t, search, exp.Policies.Search)
	}

	// max_policies is accepted with search: exact (and ignored by it).
	exp, err := ParseExperiment(append(doc("exact"), "max_policies: 3\n"...), engine.FormatYAML)
	require.NoError(t, err)
	require.Equal(t, 3, exp.MaxPolicies)

	for _, search := range []string{"grid", "Exact", "backward_induction"} {
		_, err := ParseExperiment(doc(search), engine.FormatYAML)
		var ve *engine.ValidationErrors
		require.ErrorAs(t, err, &ve, search)
		require.Equal(t, []engine.ValidationIssue{
			{Path: "$.policies.search", Code: engine.CodeInvalidValue, Message: "search must be exhaustive or exact"},
		}, ve.Issues(), search)
	}
}
