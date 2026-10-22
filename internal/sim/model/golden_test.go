// Package model implements definitions.
package model

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

func runParseTest(t *testing.T, inputPath string, parseFn func([]byte, engine.Format) (any, error)) {
	t.Helper()

	data, err := os.ReadFile(inputPath)
	require.NoError(t, err)

	format := engine.FormatJSON
	if strings.HasSuffix(inputPath, ".yaml") {
		format = engine.FormatYAML
	}

	res, err := parseFn(data, format)

	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	base = strings.TrimSuffix(base, ".input")

	goldenFile := base + ".errors.golden.json"

	if err == nil {
		if *update {
			goldenFile = base + ".golden.json"
			b, _ := json.MarshalIndent(res, "", "  ")
			_ = os.WriteFile(goldenFile, b, 0o644)
		} else {
			goldenFile = base + ".golden.json"
			expected, err := os.ReadFile(goldenFile)
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("failed reading golden file: %v", err)
			}
			if err == nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				require.JSONEq(t, string(expected), string(b))
			}
		}
		return
	}

	verrs, ok := err.(*engine.ValidationErrors)
	if !ok {
		t.Fatalf("expected *engine.ValidationErrors, got %T: %v", err, err)
	}

	b, _ := json.MarshalIndent(verrs, "", "  ")

	if *update {
		_ = os.WriteFile(goldenFile, b, 0o644)
	} else {
		expected, err := os.ReadFile(goldenFile)
		require.NoError(t, err, "golden file missing for errors")
		require.JSONEq(t, string(expected), string(b))
	}
}

func TestParseSimModel_Golden(t *testing.T) {
	files, err := filepath.Glob("testdata/sim/*.input.*")
	require.NoError(t, err)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			runParseTest(t, f, func(b []byte, f engine.Format) (any, error) {
				return ParseSimModel(b, f)
			})
		})
	}
}

func TestParsePolicy_Golden(t *testing.T) {
	files, err := filepath.Glob("testdata/policy/*.input.*")
	require.NoError(t, err)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			runParseTest(t, f, func(b []byte, f engine.Format) (any, error) {
				return ParsePolicy(b, f)
			})
		})
	}
}

func TestParseExperiment_Golden(t *testing.T) {
	files, err := filepath.Glob("testdata/experiment/*.input.*")
	require.NoError(t, err)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			runParseTest(t, f, func(b []byte, f engine.Format) (any, error) {
				return ParseExperiment(b, f)
			})
		})
	}
}
