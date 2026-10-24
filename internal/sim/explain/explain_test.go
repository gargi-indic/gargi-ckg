package explain

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestGolden(t *testing.T) {
	for _, name := range []string{"recession-v1", "loan-base-best", "params"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name+".policy.yaml"))
			require.NoError(t, err)
			p, err := model.ParsePolicy(data, engine.FormatYAML)
			require.NoError(t, err)
			got := Explain(p)
			golden := filepath.Join("testdata", name+".golden")
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o600))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.Equal(t, string(want), got)
			require.True(t, strings.HasSuffix(got, "\n"))
			require.Equal(t, got, Explain(p), "deterministic")
		})
	}
}

func TestRewriteCondition(t *testing.T) {
	for in, want := range map[string]string{
		"a == 'x'":                  "a is 'x'",
		"a!=1":                      "a is not 1",
		"a>=1 && b<=2":              "a is at least 1 and b is at most 2",
		"a > 1 || b < 2":            "a is above 1 or b is below 2",
		"!done":                     "not done",
		"x && ! y":                  "x and not y",
		"a == 'p==q&&!r'":           "a is 'p==q&&!r'",
		`a == "<>=!"`:               `a is "<>=!"`,
		`a == 'it\'s >= 1' && b`:    `a is 'it\'s >= 1' and b`,
		"f(x) in [1, 2] && (y > 3)": "f(x) in [1, 2] and (y is above 3)",
	} {
		require.Equal(t, want, rewriteCondition(in), in)
	}
}

func TestFormatValue(t *testing.T) {
	require.Equal(t, "'s'", formatValue("s"))
	require.Equal(t, "5", formatValue(5))
	require.Equal(t, "5", formatValue(5.0))
	require.Equal(t, "0.3", formatValue(0.3))
	require.Equal(t, "true", formatValue(true))
	require.Equal(t, "null", formatValue(nil))
}

func TestExplain_TableWithoutDefault(t *testing.T) {
	// The parser requires a table default, so build the policy directly.
	p := &model.Policy{ID: "p", Version: "v", Decisions: map[string]model.Decision{
		"d": {Table: []model.TableRow{{Observed: map[string]any{"k": 1}, Choose: "a"}}},
	}}
	require.Equal(t, "Policy p, version v\n\nDecision d\n  1. When k is 1, choose a.\n  If no row matches, the decision fails.\n", Explain(p))
}
