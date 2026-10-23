package report

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSchema_ReportsPattern checks that the experiment/v1 schema's reports
// pattern accepts what ParseSpec accepts, for the canonical spellings.
func TestSchema_ReportsPattern(t *testing.T) {
	data, err := os.ReadFile("../../../schemas/experiment/v1/schema.json")
	require.NoError(t, err)
	var schema struct {
		Properties struct {
			Reports struct {
				Items struct {
					Pattern string `json:"pattern"`
				} `json:"items"`
			} `json:"reports"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(data, &schema))
	re := regexp.MustCompile(schema.Properties.Reports.Items.Pattern)
	for _, s := range []string{
		"regret", "robust", "pareto:cost:min,objective", "path_heatmap", "policy_tree",
		"voi:ai_score", "voi:review-2.v1", "flip_point:p_bad", "flip_point:p_bad:0.1,0.5", "flip_point:loss:-1e3,2E+3",
	} {
		_, err := ParseSpec(s)
		require.NoError(t, err, s)
		require.Regexp(t, re, s)
	}
	for _, s := range []string{"voi", "voi:", "voi:a:b", "flip_point", "flip_point:p-bad", "flip_point:p:1", "path_heatmap:x", "policy_tree:"} {
		_, err := ParseSpec(s)
		require.Error(t, err, s)
		require.NotRegexp(t, re, s)
	}
}

func TestParseSpec_ExplainKinds(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Spec
	}{
		{"path_heatmap", Spec{Kind: KindPathHeatmap}},
		{"policy_tree", Spec{Kind: KindPolicyTree}},
		{"voi:ai_score", Spec{Kind: KindVOI, Node: "ai_score"}},
		{"voi: review-2.v1 ", Spec{Kind: KindVOI, Node: "review-2.v1"}},
		{"flip_point:p_bad", Spec{Kind: KindFlipPoint, Param: "p_bad"}},
		{"flip_point:p_bad:0.1,0.5", Spec{Kind: KindFlipPoint, Param: "p_bad", HasRange: true, Lo: 0.1, Hi: 0.5}},
		{"flip_point: loss : -1e3 , 2E3", Spec{Kind: KindFlipPoint, Param: "loss", HasRange: true, Lo: -1000, Hi: 2000}},
	} {
		got, err := ParseSpec(tc.in)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
		require.Equal(t, 1, got.MinWorlds(), tc.in)
		require.Nil(t, got.Metrics(), tc.in)
		require.Equal(t, got.Kind != KindPathHeatmap, got.NeedsSolver(), tc.in)
	}
	require.False(t, Spec{Kind: KindRegret}.NeedsSolver())
	require.False(t, Spec{Kind: KindPareto}.NeedsSolver())

	for in, msg := range map[string]string{
		"path_heatmap:x":            `report "path_heatmap" takes no arguments`,
		"policy_tree:":              `report "policy_tree" takes no arguments`,
		"voi:":                      "voi needs a node",
		"voi:a b":                   `voi node "a b" must match`,
		"voi:a:b":                   `voi node "a:b" must match`,
		"flip_point":                "flip_point needs a world param",
		"flip_point::0,1":           "flip_point needs a world param",
		"flip_point:p.bad":          `flip_point param "p.bad" must match`,
		"flip_point:p_bad:0.1":      `flip_point range "0.1" must be <lo>,<hi>`,
		"flip_point:p_bad:0,1,2":    `flip_point range "0,1,2" must be <lo>,<hi>`,
		"flip_point:p_bad:x,1":      `flip_point bound "x" must be a finite number`,
		"flip_point:p_bad:0,Inf":    `flip_point bound "Inf" must be a finite number`,
		"flip_point:p_bad:NaN,1":    `flip_point bound "NaN" must be a finite number`,
		"flip_point:p_bad:0.5,0.5":  "flip_point range needs lo < hi, got 0.5,0.5",
		"flip_point:p_bad:0.5,0.25": "flip_point range needs lo < hi, got 0.5,0.25",
	} {
		_, err := ParseSpec(in)
		require.ErrorContains(t, err, msg, in)
	}
}

// treeSets is a two-decision example: d1 (options x, y) is followed by
// d2[a] after x and by d2[b] after y; d2 has options p, q.
func treeSets() []TreeSet {
	return []TreeSet{
		{InfoSet: "d1", Decision: "d1", Observed: map[string]any{}, ReachProbability: 1, Choice: "y", Options: []string{"x", "y"}, Values: []float64{1, 2}},
		{InfoSet: "d2[a]", Decision: "d2", Observed: map[string]any{"d1.output.choice": "a"}, Choice: "p", HasParent: true, ParentInfoSet: "d1", ParentOption: "x", Options: []string{"p", "q"}, Values: []float64{1, 0}},
		{InfoSet: "d2[b]", Decision: "d2", Observed: map[string]any{"d1.output.choice": "b"}, ReachProbability: 1, Choice: "q", HasParent: true, ParentInfoSet: "d1", ParentOption: "y", Options: []string{"p", "q"}, Values: []float64{0, 2}},
	}
}

func TestNewPolicyTree(t *testing.T) {
	tree, err := NewPolicyTree("w", "pol", 2, treeSets())
	require.NoError(t, err)
	data, err := json.Marshal(tree)
	require.NoError(t, err)
	require.JSONEq(t, `{"world":"w","policyId":"pol","ev":2,"roots":[{
		"infoSet":"d1","decision":"d1","observed":{},"reachProbability":1,"onPath":true,"choice":"y","actions":[
			{"option":"x","value":1,"children":[{"infoSet":"d2[a]","decision":"d2","observed":{"d1.output.choice":"a"},"reachProbability":0,"onPath":false,"choice":"p","actions":[
				{"option":"p","value":1,"children":[]},{"option":"q","value":0,"children":[]}]}]},
			{"option":"y","value":2,"children":[{"infoSet":"d2[b]","decision":"d2","observed":{"d1.output.choice":"b"},"reachProbability":1,"onPath":true,"choice":"q","actions":[
				{"option":"p","value":0,"children":[]},{"option":"q","value":2,"children":[]}]}]}]}]}`, string(data))

	empty, err := NewPolicyTree("w", "pol", 0, nil)
	require.NoError(t, err)
	data, err = json.Marshal(empty)
	require.NoError(t, err)
	require.JSONEq(t, `{"world":"w","policyId":"pol","ev":0,"roots":[]}`, string(data))
}

func TestNewPolicyTree_Errors(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func([]TreeSet) []TreeSet
		msg    string
	}{
		"duplicate": {func(s []TreeSet) []TreeSet { return append(s, s[1]) }, `duplicate information set "d2[a]"`},
		"values": {func(s []TreeSet) []TreeSet {
			s[0].Values = s[0].Values[:1]
			return s
		}, `information set "d1" has 2 options and 1 values`},
		"parent": {func(s []TreeSet) []TreeSet {
			s[1].ParentInfoSet = "ghost"
			return s
		}, `information set "d2[a]" follows unknown information set "ghost"`},
		"option": {func(s []TreeSet) []TreeSet {
			s[2].ParentOption = "z"
			return s
		}, `information set "d2[b]" follows unknown option "z" of "d1"`},
		"cycle": {func(s []TreeSet) []TreeSet {
			s[1].ParentInfoSet, s[1].ParentOption = "d2[b]", "p"
			s[2].ParentInfoSet, s[2].ParentOption = "d2[a]", "p"
			return s
		}, "2 of 3 information sets are not reachable from a root"},
	} {
		_, err := NewPolicyTree("w", "pol", 0, tc.mutate(treeSets()))
		require.ErrorContains(t, err, tc.msg, name)
	}
}
