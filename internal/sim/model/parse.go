package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/graph"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

func newValidationError(path string, code engine.ErrorCode, message string) error {
	errs := &engine.ValidationErrors{}
	errs.Add(path, code, message)
	return errs
}

// ParseSimModel parses a simulation definition from bytes.
func ParseSimModel(data []byte, format engine.Format) (*Sim, error) {
	tree, err := graph.DecodeStrict(data, format)
	if err != nil {
		return nil, err
	}

	m, ok := tree.(map[string]any)
	if !ok {
		return nil, newValidationError("$", engine.CodeInvalidType, "root must be an object")
	}

	if apiVersion, ok := m["apiVersion"].(string); !ok || apiVersion != "sim/v1" {
		return nil, newValidationError("$.apiVersion", engine.CodeUnsupportedVersion, fmt.Sprintf("unsupported apiVersion: %v", m["apiVersion"]))
	}

	// We use json.Marshal + json.Unmarshal to strictly decode into the struct.
	// But before doing that, we need to enforce additionalProperties: false manually or strictly.
	// We'll enforce strict decoding by unmarshalling with DisallowUnknownFields.
	b, err := json.Marshal(tree)
	if err != nil {
		return nil, newValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("failed to re-encode tree: %v", err))
	}

	var sim Sim
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sim); err != nil {
		return nil, mapJSONErrorToValidation(err, "$")
	}

	// Post validation
	if err := validateSim(&sim); err != nil {
		return nil, err
	}

	return &sim, nil
}

// ParsePolicy parses a policy definition from bytes.
func ParsePolicy(data []byte, format engine.Format) (*Policy, error) {
	tree, err := graph.DecodeStrict(data, format)
	if err != nil {
		return nil, err
	}

	m, ok := tree.(map[string]any)
	if !ok {
		return nil, newValidationError("$", engine.CodeInvalidType, "root must be an object")
	}

	if apiVersion, ok := m["apiVersion"].(string); !ok || apiVersion != "policy/v1" {
		return nil, newValidationError("$.apiVersion", engine.CodeUnsupportedVersion, fmt.Sprintf("unsupported apiVersion: %v", m["apiVersion"]))
	}

	b, err := json.Marshal(tree)
	if err != nil {
		return nil, newValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("failed to re-encode tree: %v", err))
	}

	var pol Policy
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pol); err != nil {
		return nil, mapJSONErrorToValidation(err, "$")
	}

	return &pol, nil
}

// ParseExperiment parses an experiment definition from bytes.
func ParseExperiment(data []byte, format engine.Format) (*Experiment, error) {
	tree, err := graph.DecodeStrict(data, format)
	if err != nil {
		return nil, err
	}

	m, ok := tree.(map[string]any)
	if !ok {
		return nil, newValidationError("$", engine.CodeInvalidType, "root must be an object")
	}

	if apiVersion, ok := m["apiVersion"].(string); !ok || apiVersion != "experiment/v1" {
		return nil, newValidationError("$.apiVersion", engine.CodeUnsupportedVersion, fmt.Sprintf("unsupported apiVersion: %v", m["apiVersion"]))
	}

	b, err := json.Marshal(tree)
	if err != nil {
		return nil, newValidationError("$", engine.CodeInvalidFormat, fmt.Sprintf("failed to re-encode tree: %v", err))
	}

	var exp Experiment
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&exp); err != nil {
		return nil, mapJSONErrorToValidation(err, "$")
	}

	// Apply defaults
	if exp.Evaluator == "" {
		exp.Evaluator = "monte_carlo"
	}
	if exp.Top == 0 {
		exp.Top = 5
	}

	return &exp, nil
}

// mapJSONErrorToValidation converts json decoder errors into ValidationErrors
func mapJSONErrorToValidation(err error, basePath string) error {
	var typeErr *json.UnmarshalTypeError
	if reflect.TypeOf(err).String() == "*json.UnmarshalTypeError" {
		typeErr = err.(*json.UnmarshalTypeError)
		path := basePath
		if typeErr.Field != "" {
			path = engine.Path(basePath).Field(typeErr.Field).String()
		}
		return newValidationError(path, engine.CodeInvalidType, fmt.Sprintf("expected %s, got %s", typeErr.Type, typeErr.Value))
	}

	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		field = strings.Trim(field, `"`)
		return newValidationError(basePath, engine.CodeUnknownField, fmt.Sprintf("unknown field '%s'", field))
	}

	return newValidationError(basePath, engine.CodeInvalidFormat, fmt.Sprintf("decode error: %v", err))
}

func validateSim(s *Sim) error {
	errs := &engine.ValidationErrors{}
	if len(s.Worlds) == 0 {
		errs.Add("$.worlds", engine.CodeMissingRequired, "must have at least one world")
	}

	for name, vs := range s.Latents {
		if c := checkValueSpec(vs); c != 1 {
			errs.Add(fmt.Sprintf("$.latents.%s", name), engine.CodeInvalidValue, "value spec must have exactly one key")
		}
	}
	for bName, b := range s.Behaviors {
		for outName, vs := range b.Output {
			if c := checkValueSpec(vs); c != 1 {
				errs.Add(fmt.Sprintf("$.behaviors.%s.output.%s", bName, outName), engine.CodeInvalidValue, "value spec must have exactly one key")
			}
		}
	}

	if s.Objective.Maximize != "mean" {
		errs.Add("$.objective.maximize", engine.CodeInvalidValue, "objective.maximize must be 'mean'")
	}

	if errs.Len() > 0 {
		return errs
	}
	return nil
}

func checkValueSpec(vs ValueSpec) int {
	c := 0
	if vs.Fixed != nil {
		c++
	}
	if vs.Choice != nil {
		c++
	}
	if vs.Bernoulli != nil {
		c++
	}
	if vs.Normal != nil {
		c++
	}
	if vs.Lognormal != nil {
		c++
	}
	if vs.Uniform != nil {
		c++
	}
	if vs.Triangular != nil {
		c++
	}
	if vs.Empirical != nil {
		c++
	}
	if vs.Expr != "" {
		c++
	}
	return c
}
