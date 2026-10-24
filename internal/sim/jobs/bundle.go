package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/sim/experiment"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// NewRegistry returns a node registry able to compile simulation models:
// the default reference nodes plus a decision node whose chooser the kernel
// replaces with the simulated policy (the same registry as `gargi sim run
// --local`).
func NewRegistry() (*engine.Registry, error) {
	reg := engine.NewRegistry()
	if err := nodes.RegisterDefaults(reg, nodes.Dependencies{}); err != nil {
		return nil, err
	}
	if err := nodes.RegisterDecision(reg, firstOption{}); err != nil {
		return nil, err
	}
	return reg, nil
}

// firstOption is a placeholder decision chooser; simulations never call it.
type firstOption struct{}

func (firstOption) Choose(_ context.Context, _ engine.NodeID, _ map[string]any, options []string) (string, error) {
	if len(options) == 0 {
		return "", errors.New("decision has no options")
	}
	return options[0], nil
}

// ParseFormat returns the document format named by f ("yaml" or "json",
// case-insensitive); an empty f selects YAML.
func ParseFormat(f string) (engine.Format, error) {
	if f == "" {
		return engine.FormatYAML, nil
	}
	return engine.ParseFormat(f)
}

// SimFileName returns the path under which BundleFS serves the bundle's
// sim/v1 document: "sim.yaml" or "sim.json".
func SimFileName(format engine.Format) string {
	return "sim." + string(format)
}

// BundleFS is a read-only fs.FS over the documents of a model bundle.
//
// The sim/v1 document is served at SimFileName(format). Any other path
// resolves by base name: the base name of the sim document's graph field
// serves Graph, and a base name that is a key of Policies serves that
// policy. Everything else does not exist. Base-name resolution lets an
// experiment keep the relative paths it was written with (for example
// "./policies/recession-v1.yaml").
type BundleFS struct {
	simName   string
	sim       []byte
	graphBase string
	graph     []byte
	policies  map[string][]byte
}

var (
	_ fs.FS         = (*BundleFS)(nil)
	_ fs.ReadFileFS = (*BundleFS)(nil)
)

// NewBundleFS returns the BundleFS of b, whose sim document has the given
// format. The graph is resolvable only when the sim document parses.
func NewBundleFS(b sim.ModelBundle, format engine.Format) *BundleFS {
	fsys := &BundleFS{simName: SimFileName(format), sim: b.Sim, graph: b.Graph, policies: b.Policies}
	if def, err := model.ParseSimModel(b.Sim, format); err == nil && def.Graph != "" {
		fsys.graphBase = path.Base(filepath.ToSlash(def.Graph))
	}
	return fsys
}

func (b *BundleFS) lookup(name string) ([]byte, bool) {
	if name == b.simName {
		return b.sim, true
	}
	base := path.Base(name)
	if b.graphBase != "" && base == b.graphBase {
		return b.graph, true
	}
	data, ok := b.policies[base]
	return data, ok
}

// ReadFile implements fs.ReadFileFS. It returns a copy of the document.
func (b *BundleFS) ReadFile(name string) ([]byte, error) {
	data, ok := b.lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return slices.Clone(data), nil
}

// Open implements fs.FS.
func (b *BundleFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	data, ok := b.lookup(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &bundleFile{name: path.Base(name), Reader: bytes.NewReader(data), size: int64(len(data))}, nil
}

// bundleFile is an open BundleFS document.
type bundleFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *bundleFile) Stat() (fs.FileInfo, error) { return f, nil }
func (f *bundleFile) Close() error               { return nil }
func (f *bundleFile) Name() string               { return f.name }
func (f *bundleFile) Size() int64                { return f.size }
func (f *bundleFile) Mode() fs.FileMode          { return 0o444 }
func (f *bundleFile) ModTime() time.Time         { return time.Time{} }
func (f *bundleFile) IsDir() bool                { return false }
func (f *bundleFile) Sys() any                   { return nil }

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

// addIssues copies the issues of err into errs, each path prefixed by
// prefix and a colon when prefix is set. An error that is not a validation
// error becomes one issue at prefix (or the root) with code.
func addIssues(errs *engine.ValidationErrors, prefix string, err error, code engine.ErrorCode) {
	var ve *engine.ValidationErrors
	if errors.As(err, &ve) && ve != nil && ve.Len() > 0 {
		for _, is := range ve.Issues() {
			p := is.Path
			if prefix != "" {
				p = prefix + ":" + p
			}
			errs.Add(p, is.Code, is.Message)
		}
		return
	}
	at := prefix
	if at == "" {
		at = engine.PathRoot.String()
	}
	errs.Add(at, code, err.Error())
}

// policyFormat returns the format implied by the extension of a policy key.
func policyFormat(key string) (engine.Format, bool) {
	switch strings.ToLower(path.Ext(key)) {
	case ".yaml", ".yml":
		return engine.FormatYAML, true
	case ".json":
		return engine.FormatJSON, true
	default:
		return "", false
	}
}

// ValidateModel parses and compiles the sim/v1 document of b against its
// graph (model.CompileFS over NewBundleFS) and parses and compiles every
// policy of b against the compiled model. It returns the compiled model and
// nil on success, or every problem found, sorted by path, code and message.
//
// Issues of the sim and graph documents have the paths `gargi model
// validate` reports ("$.behaviors.x", "$.graph:$.nodes[0]"). Issues of a
// policy are reported at "$.policies['<key>']", or inside the document at
// "$.policies['<key>']:" followed by the path within the policy. Policy
// keys must be file base names with a .yaml, .yml or .json extension.
func ValidateModel(b sim.ModelBundle, reg *engine.Registry) (*model.CompiledModel, *engine.ValidationErrors) {
	errs := &engine.ValidationErrors{}
	policiesAt := engine.PathRoot.Field("policies")
	keys := make([]string, 0, len(b.Policies))
	for k := range b.Policies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	formats := make(map[string]engine.Format, len(keys))
	for _, k := range keys {
		at := policiesAt.Field(k).String()
		if k == "" || k != path.Base(k) || k == "." || k == ".." || strings.Contains(k, "\\") {
			errs.Add(at, engine.CodeInvalidValue, "policy key must be a file base name")
			continue
		}
		f, ok := policyFormat(k)
		if !ok {
			errs.Add(at, engine.CodeInvalidFormat, fmt.Sprintf("unsupported policy file extension %q", path.Ext(k)))
			continue
		}
		formats[k] = f
	}

	format, err := ParseFormat(b.Format)
	if err != nil {
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidFormat, err.Error())
		return nil, sortIssues(errs)
	}
	def, err := model.ParseSimModel(b.Sim, format)
	if err != nil {
		addIssues(errs, "", err, engine.CodeInvalidFormat)
		return nil, sortIssues(errs)
	}
	m, merrs := model.CompileFS(def, NewBundleFS(b, format), ".", reg)
	if merrs.Len() > 0 {
		addIssues(errs, "", merrs, engine.CodeInvalidValue)
		return nil, sortIssues(errs)
	}
	for _, k := range keys {
		f, ok := formats[k]
		if !ok {
			continue
		}
		at := policiesAt.Field(k).String()
		pdef, err := model.ParsePolicy(b.Policies[k], f)
		if err != nil {
			addIssues(errs, at, err, engine.CodeInvalidFormat)
			continue
		}
		if _, perrs := model.CompilePolicy(pdef, m); perrs.Len() > 0 {
			addIssues(errs, at, perrs, engine.CodeInvalidValue)
		}
	}
	if errs.Len() > 0 {
		return nil, sortIssues(errs)
	}
	return m, nil
}

// parseExperiment parses the experiment/v1 document data with its model
// field replaced by simName: the experiment always evaluates the bundle it
// is submitted against, so whatever model it names (or none) is ignored.
func parseExperiment(data []byte, format engine.Format, simName string) (*model.Experiment, error) {
	tree, err := graph.DecodeStrict(data, format)
	if err != nil {
		return nil, err
	}
	doc, ok := tree.(map[string]any)
	if !ok {
		return model.ParseExperiment(data, format)
	}
	if named, _ := doc["model"].(string); named == "" {
		// The document has no usable model field: add one and parse the
		// equivalent JSON document instead.
		doc["model"] = simName
		js, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		data, format = js, engine.FormatJSON
	}
	def, err := model.ParseExperiment(data, format)
	if err != nil {
		return nil, err
	}
	def.Model = simName
	return def, nil
}

// CompileExperiment compiles the experiment/v1 document definition (in
// format, "yaml" or "json"; empty means YAML) against the model bundle b,
// entirely in memory: the experiment's model field is ignored in favor of
// b, the sim document's graph resolves to b.Graph, and policies.files and
// policies.policy resolve to b.Policies by base name (see BundleFS). The
// compiled experiment reads policy files from the bundle when it runs.
// Validation problems are returned as *engine.ValidationErrors, sorted,
// with the paths experiment.Compile reports.
func CompileExperiment(b sim.ModelBundle, definition []byte, format string, reg *engine.Registry) (*experiment.CompiledExperiment, error) {
	expFormat, err := ParseFormat(format)
	if err != nil {
		errs := &engine.ValidationErrors{}
		errs.Add(engine.PathRoot.String(), engine.CodeInvalidFormat, err.Error())
		return nil, errs
	}
	simFormat, err := ParseFormat(b.Format)
	if err != nil {
		errs := &engine.ValidationErrors{}
		errs.Add(engine.PathRoot.Field("model").String(), engine.CodeInvalidFormat, err.Error())
		return nil, errs
	}
	simName := SimFileName(simFormat)
	def, err := parseExperiment(definition, expFormat, simName)
	if err != nil {
		return nil, err
	}
	exp, cerrs := experiment.CompileFS(def, NewBundleFS(b, simFormat), ".", reg)
	if cerrs.Len() > 0 {
		return nil, cerrs
	}
	return exp, nil
}
