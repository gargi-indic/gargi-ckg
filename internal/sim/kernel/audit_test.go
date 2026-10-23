package kernel_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSim_NoWallClockAudit(t *testing.T) {
	fset := token.NewFileSet()

	err := filepath.Walk("../../..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if strings.Contains(path, "/sim") && !strings.Contains(path, "testdata") {
				return nil // We want to parse this directory
			} else if path == "../../.." || path == "../../.."+"/internal" {
				return nil
			}
			if !strings.HasPrefix(path, "../../../internal/sim") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		if !strings.Contains(path, "internal/sim") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		for _, imp := range node.Imports {
			impPath := imp.Path.Value
			if impPath == `"math/rand"`  {
				t.Errorf("File %s imports math/rand", path)
			}
		}

		fullNode, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("failed to parse %s fully: %v", path, err)
		}

		ast.Inspect(fullNode, func(n ast.Node) bool {
			if goStmt, ok := n.(*ast.GoStmt); ok {
				t.Errorf("File %s contains naked goroutine at line %d", path, fset.Position(goStmt.Pos()).Line)
			}

			if callExpr, ok := n.(*ast.CallExpr); ok {
				if selExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := selExpr.X.(*ast.Ident); ok {
						if ident.Name == "time" && (selExpr.Sel.Name == "Now" || selExpr.Sel.Name == "Since" || selExpr.Sel.Name == "Sleep") {
							t.Errorf("File %s calls time.%s() at line %d", path, selExpr.Sel.Name, fset.Position(callExpr.Pos()).Line)
						}
					}
				}
			}
			return true
		})

		return nil
	})

	if err != nil {
		t.Fatalf("walk failed: %v", err)
	}
}
