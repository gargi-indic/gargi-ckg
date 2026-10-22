import re
with open("internal/sim/model/bind.go", "r") as f:
    text = f.read()

# Why didn't bind.go return errors for 'oops' string?
# Because `oops` is a string! `p: oops` evaluates to a string. And CEL strings are allowed for weights/parameters.
# `isNumberOrCEL` returns true for strings. So `bad_duration` is a string, it's accepted by bind!
# But `validateSim` should catch `bad_duration`! Wait, if `bind.go` returned ANY error, `ParseSimModel` immediately returned!
# ParseSimModel:
# sim, err := bindSim(tree)
# if err != nil { return nil, sortErrors(err) }
#
# If `bindSim` returns early, it won't run `validateSim`.
# So we only see the ONE error `unknown_field 'extra'`.

# Let's fix ParseSimModel, ParsePolicy, and ParseExperiment to run validation EVEN IF bind returns errors, to accumulate them.
# We will do this by letting bind return a non-nil struct if possible, and merging errors. Or `bind` can just merge errors and then validate.
# Wait, bindSim returns `(*Sim, error)`. If we change it to return `(*Sim, *engine.ValidationErrors)`. Then we can merge them.
