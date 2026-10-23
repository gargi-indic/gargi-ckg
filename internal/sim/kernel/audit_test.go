package kernel_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// auditRoots are the simulation source trees, relative to this package.
var auditRoots = []string{"..", "../../../pkg/sim"}

// forbiddenTimeFuncs are the wall-clock functions of package time that
// simulation code must not call.
var forbiddenTimeFuncs = map[string]bool{"Now": true, "Since": true, "Sleep": true}

// auditSource reports every wall-clock call and every math/rand (v1) import
// in one Go source file.
func auditSource(fset *token.FileSet, filename string, src any) ([]string, error) {
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}

	var violations []string
	timeNames := map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		switch path {
		case "math/rand":
			violations = append(violations, fmt.Sprintf("%s: imports math/rand (use math/rand/v2 streams from the sampler)", fset.Position(imp.Pos())))
		case "time":
			name := "time"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "." {
				// Dot import: the functions are called unqualified.
				ast.Inspect(f, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && forbiddenTimeFuncs[id.Name] {
							violations = append(violations, fmt.Sprintf("%s: calls time.%s", fset.Position(call.Pos()), id.Name))
						}
					}
					return true
				})
			} else if name != "_" {
				timeNames[name] = true
			}
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && timeNames[id.Name] && forbiddenTimeFuncs[sel.Sel.Name] {
			violations = append(violations, fmt.Sprintf("%s: uses time.%s", fset.Position(sel.Pos()), sel.Sel.Name))
		}
		return true
	})
	return violations, nil
}

// TestSim_NoWallClockAudit fails if any non-test Go file under internal/sim
// or pkg/sim reads the wall clock (time.Now, time.Since, time.Sleep) or
// imports the global math/rand (v1).
func TestSim_NoWallClockAudit(t *testing.T) {
	fset := token.NewFileSet()
	scanned := 0
	for _, root := range auditRoots {
		_, err := os.Stat(root)
		require.NoError(t, err, "audit root %s must exist", root)
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			violations, err := auditSource(fset, path, nil)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			scanned++
			for _, v := range violations {
				t.Error(v)
			}
			return nil
		})
		require.NoError(t, err)
	}
	// internal/sim/{kernel,behavior,model,sampler} and pkg/sim are all scanned.
	require.Greater(t, scanned, 15, "audit scanned suspiciously few files")
}

// TestSim_NoWallClockAudit_DetectsViolations checks that the scanner itself
// reports forbidden uses, including renamed and dot imports.
func TestSim_NoWallClockAudit_DetectsViolations(t *testing.T) {
	cases := map[string]struct {
		src  string
		want int
	}{
		"now": {
			src:  "package p\nimport \"time\"\nfunc f() time.Time { return time.Now() }\n",
			want: 1,
		},
		"since and sleep": {
			src:  "package p\nimport \"time\"\nfunc f(t time.Time) { time.Sleep(time.Since(t)) }\n",
			want: 2,
		},
		"renamed import": {
			src:  "package p\nimport clk \"time\"\nvar now = clk.Now\n",
			want: 1,
		},
		"dot import": {
			src:  "package p\nimport . \"time\"\nfunc f() { _ = Now() }\n",
			want: 1,
		},
		"math rand v1": {
			src:  "package p\nimport \"math/rand\"\nfunc f() int { return rand.Int() }\n",
			want: 1,
		},
		"clean": {
			src:  "package p\nimport (\n\t\"math/rand/v2\"\n\t\"time\"\n)\nfunc f(r *rand.Rand) time.Time { return time.Unix(int64(r.IntN(3)), 0) }\n",
			want: 0,
		},
		"other package named time-like": {
			src:  "package p\ntype clock struct{}\nfunc (clock) Now() int { return 0 }\nvar time clock\nfunc f() int { return time.Now() }\n",
			want: 0,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := auditSource(token.NewFileSet(), "probe.go", tc.src)
			require.NoError(t, err)
			require.Len(t, v, tc.want, "violations: %v", v)
		})
	}
}
