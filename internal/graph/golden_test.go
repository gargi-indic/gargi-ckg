package graph_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-ckg/internal/graph"
	"github.com/gargi-indic/gargi-ckg/pkg/engine"
)

import "flag"

var updateGolden = flag.Bool("update", false, "update golden files")

func TestGoldenDefinitions(t *testing.T) {
	parser := graph.NewParser()

	// 1. Valid Example Test
	t.Run("ValidExample", func(t *testing.T) {
		jsonPath := "../../schemas/graph/v1/example.json"
		yamlPath := "../../schemas/graph/v1/example.yaml"
		goldenPath := "testdata/definition/example.golden.json"

		jsonData, err := os.ReadFile(jsonPath)
		if err != nil {
			t.Fatalf("failed to read json example: %v", err)
		}
		yamlData, err := os.ReadFile(yamlPath)
		if err != nil {
			t.Fatalf("failed to read yaml example: %v", err)
		}

		jsonModel, err := parser.Parse(jsonData, engine.FormatJSON)
		if err != nil {
			t.Fatalf("unexpected error parsing JSON: %v", err)
		}

		yamlModel, err := parser.Parse(yamlData, engine.FormatYAML)
		if err != nil {
			t.Fatalf("unexpected error parsing YAML: %v", err)
		}

		if !reflect.DeepEqual(jsonModel, yamlModel) {
			t.Errorf("JSON and YAML models do not match")
		}

		modelJSON, err := json.MarshalIndent(jsonModel, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal model: %v", err)
		}

		if *updateGolden {
			if err := os.WriteFile(goldenPath, modelJSON, 0o644); err != nil {
				t.Fatalf("failed to write golden file: %v", err)
			}
		}

		goldenData, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("failed to read golden file: %v", err)
		}

		if string(modelJSON) != string(goldenData) {
			t.Errorf("model JSON does not match golden file")
		}

		g, err := jsonModel.ToGraph()
		if err != nil {
			t.Fatalf("ToGraph failed: %v", err)
		}
		if len(g.Nodes()) != 5 {
			t.Errorf("expected 5 nodes, got %d", len(g.Nodes()))
		}
	})

	// 2. Negative Cases
	files, err := os.ReadDir("testdata/definition")
	if err != nil {
		t.Fatalf("failed to read testdata directory: %v", err)
	}

	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".input.json") && !strings.HasSuffix(name, ".input.yaml") {
			continue
		}

		baseName := strings.TrimSuffix(strings.TrimSuffix(name, ".input.json"), ".input.yaml")
		isJSON := strings.HasSuffix(name, ".json")
		format := engine.FormatYAML
		if isJSON {
			format = engine.FormatJSON
		}

		t.Run(baseName, func(t *testing.T) {
			inputPath := filepath.Join("testdata/definition", name)
			goldenPath := filepath.Join("testdata/definition", baseName+".errors.golden.json")

			inputData, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("failed to read input file: %v", err)
			}

			_, err = parser.Parse(inputData, format)
			if err == nil {
				t.Fatalf("expected error, got none for %s", inputPath)
			}

			var valErrs *engine.ValidationErrors
			if !errors.As(err, &valErrs) {
				t.Fatalf("expected *engine.ValidationErrors, got %T: %v", err, err)
			}

			errJSON, err := json.MarshalIndent(valErrs, "", "  ")
			if err != nil {
				t.Fatalf("failed to marshal errors: %v", err)
			}

			if *updateGolden {
				if err := os.WriteFile(goldenPath, errJSON, 0o644); err != nil {
					t.Fatalf("failed to write golden file: %v", err)
				}
			}

			goldenData, err := os.ReadFile(goldenPath)
			if err != nil {
				if os.IsNotExist(err) {
					t.Fatalf("golden file not found: %s", goldenPath)
				}
				t.Fatalf("failed to read golden file: %v", err)
			}

			if string(errJSON) != string(goldenData) {
				t.Errorf("errors JSON does not match golden file.\nGot:\n%s\nWant:\n%s", string(errJSON), string(goldenData))
			}
		})
	}
}
