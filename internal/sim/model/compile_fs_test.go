package model

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

func TestCompileFS_MatchesCompile(t *testing.T) {
	reg := setupRegistry()
	def, err := ParseSimModel([]byte(readLoan(t, "sim.yaml")), engine.FormatYAML)
	require.NoError(t, err)

	fromOS, errs := Compile(def, loanDir, reg)
	require.Zero(t, errs.Len(), "%v", errs)
	fromFS, errs := CompileFS(def, os.DirFS(loanDir), ".", reg)
	require.Zero(t, errs.Len(), "%v", errs)
	require.Equal(t, fromOS.Graph.ID, fromFS.Graph.ID)
	require.Equal(t, len(fromOS.Graph.Nodes), len(fromFS.Graph.Nodes))

	// Graph paths are slash-separated and resolved against baseDir.
	nested := fstest.MapFS{"models/loan/graph.yaml": {Data: []byte(readLoan(t, "graph.yaml"))}}
	_, errs = CompileFS(def, nested, "models/loan", reg)
	require.Zero(t, errs.Len(), "%v", errs)
}

func TestCompileFS_ErrorPathsMatchCompile(t *testing.T) {
	reg := setupRegistry()
	keys := func(errs *engine.ValidationErrors) []string {
		var out []string
		for _, is := range errs.Issues() {
			out = append(out, is.Path+" "+string(is.Code))
		}
		return out
	}
	loanSim := readLoan(t, "sim.yaml")
	for name, simDoc := range map[string]string{
		"missing graph": strings.Replace(loanSim, "graph: ./graph.yaml", "graph: ./missing.yaml", 1),
		"bad extension": strings.Replace(loanSim, "graph: ./graph.yaml", "graph: ./graph.txt", 1),
	} {
		def, err := ParseSimModel([]byte(simDoc), engine.FormatYAML)
		require.NoError(t, err, name)
		_, osErrs := Compile(def, loanDir, reg)
		_, fsErrs := CompileFS(def, os.DirFS(loanDir), ".", reg)
		require.Positive(t, osErrs.Len(), name)
		require.Equal(t, keys(osErrs), keys(fsErrs), name)
	}
}
