package examples_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/internal/nodes"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExampleGraphs_Validation(t *testing.T) {
	parser := graph.NewParser()
	reg := engine.NewRegistry()
	require.NoError(t, nodes.RegisterDefaults(reg, nodes.Dependencies{}))

	exampleFiles := []string{
		"linear_pipeline.json",
		"linear_pipeline.yaml",
		"fanout_fanin.json",
		"fanout_fanin.yaml",
		"conditional_branch.json",
		"conditional_branch.yaml",
		"saga_compensation.yaml",
		"wait_signal.yaml",
	}

	for _, file := range exampleFiles {
		t.Run(file, func(t *testing.T) {
			path := filepath.Join(".", file)
			data, err := os.ReadFile(path)
			require.NoError(t, err, "failed to read example file")

			format := engine.FormatJSON
			if strings.HasSuffix(file, ".yaml") || strings.HasSuffix(file, ".yml") {
				format = engine.FormatYAML
			}

			// 1. Parser verification
			def, err := parser.Parse(data, format)
			require.NoError(t, err, "graph definition should parse successfully")
			require.NotNil(t, def)

			// 2. Structural schema validation
			structErrs := graph.ValidateStructure(def)
			assert.Equal(t, 0, structErrs.Len(), "structural validation errors: %v", structErrs)

			// 3. Semantic validation against default registry
			semErrs := graph.ValidateSemantics(def, reg)
			assert.Equal(t, 0, semErrs.Len(), "semantic validation errors: %v", semErrs)

			// 4. Graph topology & cycle validation
			g, err := def.ToGraph()
			require.NoError(t, err, "graph topology validation should pass")
			require.NotNil(t, g)
		})
	}
}
