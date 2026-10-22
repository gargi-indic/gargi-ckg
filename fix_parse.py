import re

# internal/sim/model/parse.go
with open("internal/sim/model/parse.go", "r") as f:
    text = f.read()

# Make ParseSimModel accumulate errors
text = text.replace("""	sim, err := bindSim(tree)
	if err != nil {
		return nil, sortErrors(err)
	}

	if err := validateSim(sim); err != nil {
		return nil, sortErrors(err)
	}

	return sim, nil""", """	errs := &engine.ValidationErrors{}
	sim, bindErr := bindSim(tree)
	if bindErr != nil {
		errs.Merge(bindErr.(*engine.ValidationErrors))
	}

	if valErr := validateSim(sim); valErr != nil {
		errs.Merge(valErr.(*engine.ValidationErrors))
	}

	if errs.Len() > 0 {
		return nil, sortErrors(errs)
	}
	return sim, nil""")

text = text.replace("""	pol, err := bindPolicy(tree)
	if err != nil {
		return nil, sortErrors(err)
	}

	if err := validatePolicy(pol); err != nil {
		return nil, sortErrors(err)
	}

	return pol, nil""", """	errs := &engine.ValidationErrors{}
	pol, bindErr := bindPolicy(tree)
	if bindErr != nil {
		errs.Merge(bindErr.(*engine.ValidationErrors))
	}

	if valErr := validatePolicy(pol); valErr != nil {
		errs.Merge(valErr.(*engine.ValidationErrors))
	}

	if errs.Len() > 0 {
		return nil, sortErrors(errs)
	}
	return pol, nil""")

text = text.replace("""	exp, err := bindExperiment(tree)
	if err != nil {
		return nil, sortErrors(err)
	}

	if exp.Evaluator == "" {
		exp.Evaluator = "monte_carlo"
	}
	if exp.Top == 0 {
		exp.Top = 5
	}

	if err := validateExperiment(exp); err != nil {
		return nil, sortErrors(err)
	}

	return exp, nil""", """	errs := &engine.ValidationErrors{}
	exp, bindErr := bindExperiment(tree)
	if bindErr != nil {
		errs.Merge(bindErr.(*engine.ValidationErrors))
	}

	if exp.Evaluator == "" {
		exp.Evaluator = "monte_carlo"
	}
	if exp.Top == 0 {
		exp.Top = 5
	}

	if valErr := validateExperiment(exp); valErr != nil {
		errs.Merge(valErr.(*engine.ValidationErrors))
	}

	if errs.Len() > 0 {
		return nil, sortErrors(errs)
	}
	return exp, nil""")

with open("internal/sim/model/parse.go", "w") as f:
    f.write(text)

# Now bind.go must ALWAYS return the struct (even if incomplete) instead of nil so validate doesn't panic.
with open("internal/sim/model/bind.go", "r") as f:
    bind = f.read()

bind = bind.replace("""	if errs.Len() > 0 {
		return nil, errs
	}
	return &sim, nil""", """	if errs.Len() > 0 {
		return &sim, errs
	}
	return &sim, nil""")

bind = bind.replace("""	if errs.Len() > 0 {
		return nil, errs
	}
	return &pol, nil""", """	if errs.Len() > 0 {
		return &pol, errs
	}
	return &pol, nil""")

bind = bind.replace("""	if errs.Len() > 0 {
		return nil, errs
	}
	return &exp, nil""", """	if errs.Len() > 0 {
		return &exp, errs
	}
	return &exp, nil""")

with open("internal/sim/model/bind.go", "w") as f:
    f.write(bind)
