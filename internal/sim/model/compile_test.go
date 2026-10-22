package model

import (
	"context"
	"testing"
	"path/filepath"
	"os"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
)

type dummyChooser struct{}
func (dummyChooser) Choose(ctx context.Context, decision engine.NodeID, obs map[string]any, options []string) (string, error) {
	return "", nil
}

func setupRegistry() *engine.Registry {
	reg := engine.NewRegistry()
	nodes.RegisterDefaults(reg, nodes.Dependencies{})
	nodes.RegisterDecision(reg, dummyChooser{})
	reg.Register("no_op", func(any) (engine.NodeInterface, error) { return nil, nil }, nil)
	return reg
}

func TestCompile_LoanModelAndRecession(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: loan_graph
nodes:
  - id: ai_score
    type: http_call
    config:
      url: http://test.com
      method: GET
  - id: route
    type: decision
    config:
      options: ["approve", "reject", "human_review"]
      observes: ["ai_score.output.band"]
  - id: human_review
    type: wait_for_signal
    config:
      signal: my_signal
  - id: final
    type: decision
    config:
      options: ["approve", "reject"]
      observes: ["human_review.output.result"]
  - id: approve
    type: no_op
  - id: reject
    type: no_op
edges:
  - from: ai_score
    to: route
  - from: route
    to: human_review
    condition: "nodes['route'].output.choice == 'human_review'"
  - from: route
    to: approve
    condition: "nodes['route'].output.choice == 'approve'"
  - from: route
    to: reject
    condition: "nodes['route'].output.choice == 'reject'"
  - from: human_review
    to: final
  - from: final
    to: approve
    condition: "nodes['final'].output.choice == 'approve'"
  - from: final
    to: reject
    condition: "nodes['final'].output.choice == 'reject'"
`
	err := os.WriteFile(filepath.Join(baseDir, "loan.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: loan_sim
graph: loan.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "run.cost * -1"
  maximize: "mean"
behaviors:
  ai_score:
    cost: 20
    output:
      band:
        choice:
          high: 0.2
          low: 0.8
  human_review:
    cost: 100
    output:
      result:
        choice:
          pass: 0.5
          fail: 0.5
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	m, errs := Compile(s, baseDir, reg)
	require.Nil(t, errs)
	require.NotNil(t, m)

	discrete, reasons := m.Discrete()
	assert.True(t, discrete)
	assert.Empty(t, reasons)

	aiBandSupport, ok := m.ObservedSupport("ai_score.output.band")
	assert.True(t, ok)
	assert.Equal(t, []any{"high", "low"}, aiBandSupport)

	humanResultSupport, ok := m.ObservedSupport("human_review.output.result")
	assert.True(t, ok)
	assert.Equal(t, []any{"fail", "pass"}, humanResultSupport)

	polYaml := `
apiVersion: policy/v1
id: recession-v1
version: "1.0"
decisions:
  route:
    rules:
      - when: "ai_score.output.band == 'high'"
        choose: "reject"
      - when: "ai_score.output.band == 'low'"
        choose: "human_review"
  final:
    rules:
      - when: "human_review.output.result == 'pass'"
        choose: "approve"
      - when: "human_review.output.result == 'fail'"
        choose: "reject"
`
	p, err := ParsePolicy([]byte(polYaml), engine.FormatYAML)
	require.NoError(t, err)

	compPol, errs := CompilePolicy(p, m)
	require.Nil(t, errs)
	require.NotNil(t, compPol)

	obs1 := sim.Observation{"ai_score.output.band": "high"}
	choice, err := compPol.Choose(context.Background(), "route", obs1, nil)
	require.NoError(t, err)
	assert.Equal(t, "reject", choice)

	obs2 := sim.Observation{"ai_score.output.band": "low"}
	choice, err = compPol.Choose(context.Background(), "route", obs2, nil)
	require.NoError(t, err)
	assert.Equal(t, "human_review", choice)
}

func TestCompile_CELTypeError(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: test
nodes:
  - id: n1
    type: no_op
`
	err := os.WriteFile(filepath.Join(baseDir, "test.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: test_sim
graph: test.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "'hello'" # invalid type, should be double/int/dyn
  maximize: "mean"
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	_, errs := Compile(s, baseDir, reg)
	require.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "invalid output type")
}

func TestCompile_LatentReferenceError(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: test
nodes:
  - id: n1
    type: no_op
`
	err := os.WriteFile(filepath.Join(baseDir, "test.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: test_sim
graph: test.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "1.0"
  maximize: "mean"
latents:
  l1:
    expr: "latent.bad" # invalid, latent can only reference world
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	_, errs := Compile(s, baseDir, reg)
	require.NotNil(t, errs)
	assert.Contains(t, errs.Error(), "undeclared reference to 'latent'")
}

func TestCompilePolicy_NegativeErrors(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: loan_graph
nodes:
  - id: ai_score
    type: http_call
    config:
      url: http://test.com
      method: GET
  - id: route
    type: decision
    config:
      options: ["approve", "reject"]
      observes: ["ai_score.output.band"]
  - id: approve
    type: no_op
  - id: reject
    type: no_op
edges:
  - from: ai_score
    to: route
  - from: route
    to: approve
    condition: "nodes['route'].output.choice == 'approve'"
  - from: route
    to: reject
    condition: "nodes['route'].output.choice == 'reject'"
`
	err := os.WriteFile(filepath.Join(baseDir, "loan.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: loan_sim
graph: loan.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "run.cost * -1"
  maximize: "mean"
behaviors:
  ai_score:
    cost: 20
    output:
      band:
        choice:
          high: 0.2
          low: 0.8
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	m, errs := Compile(s, baseDir, reg)
	require.Nil(t, errs)

	polYaml := `
apiVersion: policy/v1
id: recession-v1
version: "1.0"
decisions:
  route:
    rules:
      - when: "ai_score.output.other == 'high'"
        choose: "reject"
`
	p, err := ParsePolicy([]byte(polYaml), engine.FormatYAML)
	require.NoError(t, err)

	_, pErrs := CompilePolicy(p, m)
	require.NotNil(t, pErrs)
	assert.Contains(t, pErrs.Error(), "unobserved reference")

	polYaml2 := `
apiVersion: policy/v1
id: recession-v1
version: "1.0"
decisions:
  route:
    rules:
      - when: "ai_score.output.band == 'high'"
        choose: "fake_option"
`
	p2, err := ParsePolicy([]byte(polYaml2), engine.FormatYAML)
	require.NoError(t, err)

	_, pErrs2 := CompilePolicy(p2, m)
	require.NotNil(t, pErrs2)
	assert.Contains(t, pErrs2.Error(), "unknown option")

	polYaml3 := `
apiVersion: policy/v1
id: recession-v1
version: "1.0"
decisions:
  route:
    rules:
      - when: "ai_score['output'].band == 'high'"
        choose: "approve"
`
	p3, err := ParsePolicy([]byte(polYaml3), engine.FormatYAML)
	require.NoError(t, err)

	_, pErrs3 := CompilePolicy(p3, m)
	require.NotNil(t, pErrs3)
	assert.Contains(t, pErrs3.Error(), "complex select or index access not allowed")

	polYaml4 := `
apiVersion: policy/v1
id: recession-v1
version: "1.0"
decisions:
  route:
    rules:
      - when: "ai_score.output.bandit == 'x'"
        choose: "approve"
`
	p4, err := ParsePolicy([]byte(polYaml4), engine.FormatYAML)
	require.NoError(t, err)

	_, pErrs4 := CompilePolicy(p4, m)
	require.NotNil(t, pErrs4)
	assert.Contains(t, pErrs4.Error(), "unobserved reference")
}

func TestPolicy_ElseFallthrough(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: test
nodes:
  - id: ai_score
    type: http_call
    config:
      url: http://test.com
      method: GET
  - id: n1
    type: decision
    config:
      options: ["approve", "reject", "review"]
      observes: ["ai_score.output.band"]
edges:
  - from: ai_score
    to: n1
`
	err := os.WriteFile(filepath.Join(baseDir, "test.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: test_sim
graph: test.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "1.0"
  maximize: "mean"
behaviors:
  ai_score:
    cost: 20
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	m, _ := Compile(s, baseDir, reg)

	polYaml := `
apiVersion: policy/v1
id: else_test
version: "1.0"
decisions:
  n1:
    rules:
      - when: "has(ai_score.output.band) && ai_score.output.band == 'high'"
        choose: "reject"
      - else: "review"
`
	p, err := ParsePolicy([]byte(polYaml), engine.FormatYAML)
	require.NoError(t, err)

	compPol, errs := CompilePolicy(p, m)
	require.Nil(t, errs)

	// screen=skip with band nil -> review
	obs := sim.Observation{"ai_score.output.band": nil}
	choice, err := compPol.Choose(context.Background(), "n1", obs, nil)
	require.NoError(t, err)
	assert.Equal(t, "review", choice)
}

func TestTablePolicy_Types(t *testing.T) {
	baseDir := t.TempDir()
	graphYaml := `
apiVersion: graph/v1
id: test
nodes:
  - id: ai_score
    type: http_call
    config:
      url: http://test.com
      method: GET
  - id: n1
    type: decision
    config:
      options: ["approve", "reject", "review"]
      observes: ["ai_score.output.band"]
edges:
  - from: ai_score
    to: n1
`
	err := os.WriteFile(filepath.Join(baseDir, "test.graph.yaml"), []byte(graphYaml), 0644)
	require.NoError(t, err)

	simYaml := `
apiVersion: sim/v1
id: test_sim
graph: test.graph.yaml
worlds:
  base:
    p_bad: 0.2
objective:
  name: profit
  value: "1.0"
  maximize: "mean"
behaviors:
  ai_score:
    cost: 20
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
	require.NoError(t, err)

	reg := setupRegistry()
	m, _ := Compile(s, baseDir, reg)

	polYaml := `
apiVersion: policy/v1
id: test
version: "1.0"
decisions:
  n1:
    table:
      - observed:
          ai_score.output.band: 1
        choose: "approve"
    default: "reject"
`
	p, err := ParsePolicy([]byte(polYaml), engine.FormatYAML)
	require.NoError(t, err)

	compPol, errs := CompilePolicy(p, m)
	require.Nil(t, errs)

	// test numeric cross matching (float from JSON)
	obs := sim.Observation{"ai_score.output.band": float64(1.0)}
	choice, err := compPol.Choose(context.Background(), "n1", obs, nil)
	require.NoError(t, err)
	assert.Equal(t, "approve", choice)

	// fallback default
	obs2 := sim.Observation{"ai_score.output.band": 2}
	choice2, err := compPol.Choose(context.Background(), "n1", obs2, nil)
	require.NoError(t, err)
	assert.Equal(t, "reject", choice2)
}
