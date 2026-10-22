import re

with open("internal/sim/model/types.go", "r") as f:
    text = f.read()

replacements = {
    "// Resource represents Resource.": "// Resource describes the constraints and capacity of a system resource.",
    "// Objective represents Objective.": "// Objective defines the goal to maximize or minimize and its constraints.",
    "// Behavior represents Behavior.": "// Behavior specifies the cost, duration, failure rates, and outputs of a node.",
    "// Abandon represents Abandon.": "// Abandon configures the probability of a task being abandoned.",
    "// Fail represents Fail.": "// Fail configures the probability and classification of a failure.",
    "// Bernoulli represents Bernoulli.": "// Bernoulli represents a Bernoulli distribution.",
    "// Normal represents Normal.": "// Normal represents a Normal (Gaussian) distribution.",
    "// Lognormal represents Lognormal.": "// Lognormal represents a Lognormal distribution.",
    "// Uniform represents Uniform.": "// Uniform represents a continuous Uniform distribution.",
    "// Triangular represents Triangular.": "// Triangular represents a Triangular distribution.",
    "// Decision represents Decision.": "// Decision contains the rules or table used to make a policy choice.",
    "// Rule represents Rule.": "// Rule represents a conditional choice in a policy decision.",
    "// TableRow represents TableRow.": "// TableRow represents a single observation and its corresponding choice.",
    "// Policies represents Policies.": "// Policies defines how policies are discovered or supplied to an experiment."
}

for old, new in replacements.items():
    text = text.replace(old, new)

with open("internal/sim/model/types.go", "w") as f:
    f.write(text)
