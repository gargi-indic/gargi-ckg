package experiment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

// issueKeys returns the path and code of every issue (messages may name
// OS-specific file paths).
func issueKeys(errs *engine.ValidationErrors) []string {
	var out []string
	for _, is := range errs.Issues() {
		out = append(out, is.Path+" "+string(is.Code))
	}
	return out
}

func TestLoadFS_MatchesLoadFile(t *testing.T) {
	reg := registry(t)
	fromFile, err := LoadFile(filepath.Join(loanDir, "experiment.yaml"), reg)
	require.NoError(t, err)
	fromFS, err := LoadFS(os.DirFS(loanDir), "experiment.yaml", reg)
	require.NoError(t, err)
	require.Equal(t, fromFile.Def, fromFS.Def)
	require.Equal(t, fromFile.Worlds, fromFS.Worlds)
	require.Equal(t, fromFile.Model.Def, fromFS.Model.Def)
	require.Nil(t, fromFile.FS)
	require.NotNil(t, fromFS.FS)

	// policies.files are read from the file system as well.
	fromFS.Def.Policies = model.Policies{Files: []string{"./" + recessionFile}}
	policies, err := fromFS.FilesSource().Policies(fromFS.Model)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	require.Equal(t, recessionID, policies[0].ID())

	// A nil file system reads from the operating system, like LoadFile.
	viaNil, err := LoadFS(nil, filepath.Join(loanDir, "experiment.yaml"), reg)
	require.NoError(t, err)
	require.Equal(t, fromFile.Def, viaNil.Def)
}

func TestCompileFS_ErrorPathsMatchCompile(t *testing.T) {
	reg := registry(t)
	missingGraph := strings.Replace(readLoan(t, "sim.yaml"), "graph: ./graph.yaml", "graph: ./missing.yaml", 1)
	dir := t.TempDir()
	writeFile(t, dir, "sim.yaml", missingGraph)
	writeFile(t, dir, "bad.txt", "x")
	fsys := fstest.MapFS{
		"sim.yaml": {Data: []byte(missingGraph)},
		"bad.txt":  {Data: []byte("x")},
	}
	for name, def := range map[string]*model.Experiment{
		"missing model":   {Model: "./nope.yaml"},
		"bad extension":   {Model: "./bad.txt"},
		"missing graph":   {Model: "./sim.yaml"},
		"nil definition":  nil,
		"missing policy":  {Model: "./sim.yaml", Policies: model.Policies{Policy: "./p.yaml"}},
		"nested document": {Model: "sub/../sim.yaml"},
	} {
		_, osErrs := Compile(def, dir, reg)
		_, fsErrs := CompileFS(def, fsys, ".", reg)
		require.Positive(t, osErrs.Len(), name)
		require.Equal(t, issueKeys(osErrs), issueKeys(fsErrs), name)
	}

	// Missing policy files are reported at the same paths.
	exp, err := LoadFS(os.DirFS(loanDir), "experiment.yaml", reg)
	require.NoError(t, err)
	exp.Def.Policies = model.Policies{Files: []string{"./policies/absent.yaml"}}
	_, fsErr := exp.FilesSource().Policies(exp.Model)
	osSrc := FilesSource{BaseDir: loanDir, Files: exp.Def.Policies.Files}
	_, osErr := osSrc.Policies(exp.Model)
	var fsVE, osVE *engine.ValidationErrors
	require.ErrorAs(t, fsErr, &fsVE)
	require.ErrorAs(t, osErr, &osVE)
	require.Equal(t, issueKeys(osVE), issueKeys(fsVE))
}
