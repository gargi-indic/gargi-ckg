package explain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gargi-indic/gargi-flow/internal/sim/model"
)

// Explain renders p in plain language. The output is deterministic and ends
// with a newline.
func Explain(p *model.Policy) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Policy %s, version %s\n\n", p.ID, p.Version)

	if len(p.Params) > 0 {
		b.WriteString("Parameters\n")
		for _, name := range sortedKeys(p.Params) {
			fmt.Fprintf(&b, "  %s: %s\n", name, describeParam(p.Params[name]))
		}
		b.WriteString("\n")
	}

	for i, name := range sortedKeys(p.Decisions) {
		if i > 0 {
			b.WriteString("\n")
		}
		writeDecision(&b, name, p.Decisions[name])
	}
	return b.String()
}

// sortedKeys returns the keys of m in ascending order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// describeParam renders one parameter specification.
func describeParam(s model.ParamSpec) string {
	var b strings.Builder
	if s.Min != nil || s.Max != nil {
		b.WriteString("a number")
		if s.Min != nil {
			b.WriteString(" from " + formatFloat(*s.Min))
		}
		if s.Max != nil {
			b.WriteString(" to " + formatFloat(*s.Max))
		}
		if s.Step != nil {
			b.WriteString(" in steps of " + formatFloat(*s.Step))
		}
	} else {
		vals := make([]string, len(s.Values))
		for i, v := range s.Values {
			vals[i] = formatValue(v)
		}
		b.WriteString("one of " + strings.Join(vals, ", "))
	}
	if s.Default != nil {
		b.WriteString(" (default " + formatValue(s.Default) + ")")
	}
	return b.String()
}

// writeDecision renders one decision block.
func writeDecision(b *strings.Builder, name string, d model.Decision) {
	fmt.Fprintf(b, "Decision %s\n", name)

	if len(d.Rules) > 0 {
		if len(d.Rules) == 1 && isElse(d.Rules[0]) {
			fmt.Fprintf(b, "  Always choose %s.\n", d.Rules[0].Else)
		} else {
			for i, r := range d.Rules {
				if isElse(r) {
					fmt.Fprintf(b, "  %d. Otherwise, choose %s.\n", i+1, r.Else)
				} else {
					fmt.Fprintf(b, "  %d. If %s, choose %s.\n", i+1, rewriteCondition(r.When), r.Choose)
				}
			}
			if !isElse(d.Rules[len(d.Rules)-1]) {
				b.WriteString("  If no rule matches, the decision fails.\n")
			}
		}
	}

	if len(d.Table) > 0 {
		for i, row := range d.Table {
			parts := make([]string, 0, len(row.Observed))
			for _, k := range sortedKeys(row.Observed) {
				parts = append(parts, k+" is "+formatValue(row.Observed[k]))
			}
			fmt.Fprintf(b, "  %d. When %s, choose %s.\n", i+1, strings.Join(parts, " and "), row.Choose)
		}
		if d.Default != "" {
			fmt.Fprintf(b, "  Otherwise, choose %s.\n", d.Default)
		} else {
			b.WriteString("  If no row matches, the decision fails.\n")
		}
	}
}

// isElse reports whether r is an else rule.
func isElse(r model.Rule) bool { return r.When == "" && r.Else != "" }

// formatFloat formats f with the shortest representation that round-trips.
func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// formatValue renders a scalar value: strings in single quotes, numbers via
// formatFloat, booleans as true/false and nil as null.
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + x + "'"
	case bool:
		return strconv.FormatBool(x)
	case int:
		return formatFloat(float64(x))
	case int32:
		return formatFloat(float64(x))
	case int64:
		return formatFloat(float64(x))
	case uint:
		return formatFloat(float64(x))
	case uint64:
		return formatFloat(float64(x))
	case float32:
		return formatFloat(float64(x))
	case float64:
		return formatFloat(x)
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return formatFloat(f)
		}
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

// operators lists the rewritten operators; the two-character operators come
// before their one-character prefixes so the longer match wins.
var operators = []struct{ op, text string }{
	{"==", "is"},
	{"!=", "is not"},
	{">=", "is at least"},
	{"<=", "is at most"},
	{"&&", "and"},
	{"||", "or"},
	{">", "is above"},
	{"<", "is below"},
}

// rewriteCondition rewrites the CEL operators in cond into words. String
// literals (single or double quoted) are copied untouched.
func rewriteCondition(cond string) string {
	var out strings.Builder
	for i := 0; i < len(cond); {
		c := cond[i]
		if c == '\'' || c == '"' {
			end := literalEnd(cond, i)
			out.WriteString(cond[i:end])
			i = end
			continue
		}
		if text, n := matchOperator(cond[i:]); n > 0 {
			trimmed := strings.TrimRight(out.String(), " \t\r\n")
			out.Reset()
			out.WriteString(trimmed + " " + text + " ")
			i = skipSpace(cond, i+n)
			continue
		}
		if c == '!' {
			out.WriteString("not ")
			i = skipSpace(cond, i+1)
			continue
		}
		out.WriteByte(c)
		i++
	}
	return strings.TrimSpace(out.String())
}

// literalEnd returns the index just past the string literal that starts at
// cond[start], honouring backslash escapes. An unterminated literal extends
// to the end of cond.
func literalEnd(cond string, start int) int {
	quote := cond[start]
	for j := start + 1; j < len(cond); j++ {
		switch cond[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		}
	}
	return len(cond)
}

// matchOperator returns the replacement text and length of the operator at
// the start of s, or length 0 when s does not start with one.
func matchOperator(s string) (string, int) {
	for _, o := range operators {
		if strings.HasPrefix(s, o.op) {
			return o.text, len(o.op)
		}
	}
	return "", 0
}

// skipSpace returns the first index at or after i that is not ASCII
// whitespace.
func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return i
}
