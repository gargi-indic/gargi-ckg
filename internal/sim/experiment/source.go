package experiment

import (
	"fmt"
	"sort"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// PolicySource supplies the policies an experiment evaluates. FilesSource
// loads policy/v1 files; other sources (such as exhaustive search) plug in
// through the same interface.
type PolicySource interface {
	// Policies returns the policies to evaluate against the compiled model.
	// Policy IDs must be unique.
	Policies(m *model.CompiledModel) ([]sim.Policy, error)
}

// FilesSource is a PolicySource that loads and compiles policy/v1 files, as
// listed under policies.files of an experiment.
type FilesSource struct {
	// BaseDir is the directory relative file paths are resolved against.
	BaseDir string
	// Files lists the policy/v1 documents to load (.yaml, .yml or .json).
	Files []string
}

var _ PolicySource = FilesSource{}

// Policies parses every file with model.ParsePolicy and compiles it against
// m with model.CompilePolicy, returning the policies in file order. All
// problems are reported together as *engine.ValidationErrors, sorted by
// path, code and message. Issues of file i are located at
// "$.policies.files[i]" or, for issues inside the document, at
// "$.policies.files[i]:" followed by the path within the policy document.
// Two files that compile to the same policy ID are rejected with
// engine.CodeDuplicateID.
func (s FilesSource) Policies(m *model.CompiledModel) ([]sim.Policy, error) {
	errs := &engine.ValidationErrors{}
	filesAt := engine.PathRoot.Field("policies").Field("files")
	if len(s.Files) == 0 {
		errs.Add(filesAt.String(), engine.CodeMissingRequired, "no policy files")
		return nil, errs
	}

	policies := make([]sim.Policy, 0, len(s.Files))
	seen := make(map[string]int, len(s.Files))
	for i, file := range s.Files {
		at := filesAt.Index(i)
		_, data, format, ok := readDocument(s.BaseDir, file, at, errs)
		if !ok {
			continue
		}
		def, err := model.ParsePolicy(data, format)
		if err != nil {
			addPrefixed(errs, at.String(), err, engine.CodeInvalidFormat)
			continue
		}
		p, perrs := model.CompilePolicy(def, m)
		if perrs.Len() > 0 {
			addPrefixed(errs, at.String(), perrs, engine.CodeInvalidValue)
			continue
		}
		if j, dup := seen[p.ID()]; dup {
			errs.Add(at.String(), engine.CodeDuplicateID, fmt.Sprintf("policy id %q is also defined by policies.files[%d]", p.ID(), j))
			continue
		}
		seen[p.ID()] = i
		policies = append(policies, p)
	}
	if errs.Len() > 0 {
		return nil, sortIssues(errs)
	}
	return policies, nil
}

// sortIssues returns errs with its issues sorted by path, code and message.
func sortIssues(errs *engine.ValidationErrors) *engine.ValidationErrors {
	issues := errs.Issues()
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
	out := &engine.ValidationErrors{}
	for _, is := range issues {
		out.AddIssue(is)
	}
	return out
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
