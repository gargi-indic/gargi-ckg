import re
with open('internal/sim/model/compile_test.go', 'r') as f:
    d = f.read()

# Add ai_score to TestPolicy_ElseFallthrough
d = d.replace("""
nodes:
  - id: ai_score
    type: no_op
  - id: n1
    type: decision
    config:
      options: ["approve", "reject", "review"]
      observes: ["ai_score.output.band"]
""", """
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
""")

# also check if the sim yaml in that test has the behavior for ai_score
d = d.replace("""
objective:
  name: profit
  value: "1.0"
  maximize: "mean"
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
""", """
objective:
  name: profit
  value: "1.0"
  maximize: "mean"
behaviors:
  ai_score:
    cost: 20
`
	s, err := ParseSimModel([]byte(simYaml), engine.FormatYAML)
""")

with open('internal/sim/model/compile_test.go', 'w') as f:
    f.write(d)
