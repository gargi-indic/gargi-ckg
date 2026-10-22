import re

with open("internal/sim/model/parse.go", "r") as f:
    text = f.read()

# remove empty block
text = re.sub(r'if len\(p\.Decisions\) == 0 && p\.Decisions != nil \{\s*//.*?\n\s*\}', '', text)

with open("internal/sim/model/parse.go", "w") as f:
    f.write(text)
