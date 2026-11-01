package tell

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxLoopIterations = 1_000_000

type Error struct{ Message string }

func (e *Error) Error() string              { return e.Message }
func fail(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type node struct {
	kind, text               string
	line                     int
	condition, name          string
	params                   []string
	body, otherwise, handler []node
}
type parser struct {
	lines    []string
	index    int
	filename string
}

func cleanLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(strings.ToLower(line), "note:") {
		return ""
	}
	quote := rune(0)
	escaped := false
	for i, r := range line {
		if quote != 0 {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
		} else if r == '\'' || r == '"' {
			quote = r
		} else if r == '#' {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}
func stripSentenceEnd(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, "."))
}
func (p *parser) block(stops map[string]bool) ([]node, string, error) {
	var out []node
	for p.index < len(p.lines) {
		raw := p.lines[p.index]
		lineNo := p.index + 1
		p.index++
		line := cleanLine(raw)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if stops[low] {
			return out, low, nil
		}
		if low == "end if." || low == "end while." || low == "end repeat." || low == "end loop." || low == "end function." || low == "end try." || low == "otherwise:" || low == "if something goes wrong:" {
			return nil, "", fail("unexpected block ending %q on line %d", line, lineNo)
		}
		if m := reIf.FindStringSubmatch(line); m != nil {
			body, stop, err := p.block(map[string]bool{"otherwise:": true, "end if.": true})
			if err != nil {
				return nil, "", err
			}
			var other []node
			if stop == "otherwise:" {
				other, stop, err = p.block(map[string]bool{"end if.": true})
				if err != nil {
					return nil, "", err
				}
			}
			if stop != "end if." {
				return nil, "", fail("if block on line %d needs 'End if.'", lineNo)
			}
			out = append(out, node{kind: "if", condition: strings.TrimSpace(m[1]), body: body, otherwise: other, line: lineNo})
			continue
		}
		if m := reWhile.FindStringSubmatch(line); m != nil {
			body, stop, err := p.block(map[string]bool{"end while.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end while." {
				return nil, "", fail("while block on line %d needs 'End while.'", lineNo)
			}
			out = append(out, node{kind: "while", condition: strings.TrimSpace(m[1]), body: body, line: lineNo})
			continue
		}
		if m := reRepeat.FindStringSubmatch(line); m != nil {
			body, stop, err := p.block(map[string]bool{"end repeat.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end repeat." {
				return nil, "", fail("repeat block on line %d needs 'End repeat.'", lineNo)
			}
			out = append(out, node{kind: "repeat", condition: strings.TrimSpace(m[1]), body: body, line: lineNo})
			continue
		}
		if m := reFor.FindStringSubmatch(line); m != nil {
			body, stop, err := p.block(map[string]bool{"end loop.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end loop." {
				return nil, "", fail("for-each block on line %d needs 'End loop.'", lineNo)
			}
			out = append(out, node{kind: "for", name: m[1], condition: strings.TrimSpace(m[2]), body: body, line: lineNo})
			continue
		}
		if m := reFunction.FindStringSubmatch(line); m != nil {
			name := m[1]
			tail := strings.TrimSpace(m[2])
			var params []string
			if strings.HasPrefix(tail, "(") && strings.HasSuffix(tail, ")") {
				inner := strings.TrimSpace(tail[1 : len(tail)-1])
				if inner != "" {
					for _, x := range strings.Split(inner, ",") {
						params = append(params, strings.TrimSpace(x))
					}
				}
			} else if strings.HasPrefix(strings.ToLower(tail), "with ") {
				params = splitAnd(tail[5:])
			} else if tail != "" {
				params = splitAnd(tail)
			}
			for _, x := range params {
				if !identifier(x) {
					return nil, "", fail("function parameter %q on line %d is not a valid name", x, lineNo)
				}
			}
			body, stop, err := p.block(map[string]bool{"end function.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end function." {
				return nil, "", fail("function %q on line %d needs 'End function.'", name, lineNo)
			}
			out = append(out, node{kind: "function", name: name, params: params, body: body, line: lineNo})
			continue
		}
		if low == "try:" {
			body, stop, err := p.block(map[string]bool{"if something goes wrong:": true, "end try.": true})
			if err != nil {
				return nil, "", err
			}
			var handler []node
			if stop == "if something goes wrong:" {
				handler, stop, err = p.block(map[string]bool{"end try.": true})
				if err != nil {
					return nil, "", err
				}
			}
			if stop != "end try." {
				return nil, "", fail("try block on line %d needs 'End try.'", lineNo)
			}
			out = append(out, node{kind: "try", body: body, handler: handler, line: lineNo})
			continue
		}
		out = append(out, node{kind: "statement", text: stripSentenceEnd(line), line: lineNo})
	}
	return out, "", nil
}
func parse(source, filename string) ([]node, error) {
	p := &parser{lines: strings.Split(source, "\n"), filename: filename}
	n, stop, err := p.block(map[string]bool{})
	if err != nil {
		return nil, err
	}
	if stop != "" {
		return nil, fail("unexpected %q", stop)
	}
	return n, nil
}

var (
	reIf         = regexp.MustCompile(`(?i)^if\s+(.+):\s*$`)
	reWhile      = regexp.MustCompile(`(?i)^while\s+(.+):\s*$`)
	reRepeat     = regexp.MustCompile(`(?i)^repeat\s+(.+?)\s+times\s*:\s*$`)
	reFor        = regexp.MustCompile(`(?i)^for each\s+([A-Za-z_]\w*)\s+in\s+(.+?)\s*:\s*$`)
	reFunction   = regexp.MustCompile(`(?i)^(?:to|define a function called)\s+([A-Za-z_]\w*)\s*(.*?)\s*:\s*$`)
	reIdentifier = regexp.MustCompile(`^[A-Za-z_]\w*$`)
)

func identifier(s string) bool   { return reIdentifier.MatchString(s) }
func splitAnd(s string) []string { return splitTop(s, " and ") }
func splitTop(s, sep string) []string {
	var out []string
	quote := byte(0)
	escaped := false
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
		}
		if c == ')' || c == ']' || c == '}' {
			depth--
		}
		if depth == 0 && i+len(sep) <= len(s) && strings.EqualFold(s[i:i+len(sep)], sep) {
			out = append(out, strings.TrimSpace(s[start:i]))
			i += len(sep) - 1
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

type env struct {
	values map[string]any
	parent *env
}

func newEnv(parent *env) *env { return &env{values: map[string]any{}, parent: parent} }
func (e *env) get(name string) (any, error) {
	if v, ok := e.values[name]; ok {
		return v, nil
	}
	if e.parent != nil {
		return e.parent.get(name)
	}
	return nil, fail("I don't know what %q means. Did you define it first?", name)
}
func (e *env) set(name string, v any) { e.values[name] = v }

type callable func([]any) (any, error)
type userFunction struct {
	name    string
	params  []string
	body    []node
	closure *env
	runtime *runtime
}
type signal struct {
	kind  string
	value any
}

func (s signal) Error() string { return s.kind }

type runtime struct {
	filename string
	global   *env
	reader   *bufio.Reader
	output   io.Writer
}

func newRuntime(filename string, input io.Reader, output io.Writer) *runtime {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	r := &runtime{filename: filename, reader: bufio.NewReader(input), output: output}
	r.global = newEnv(nil)
	r.installBuiltins(r.global)
	return r
}
func Check(source, filename string) error { _, err := parse(source, filename); return err }
func Run(source, filename string) error {
	return RunWithIO(source, filename, os.Stdin, os.Stdout)
}
func RunWithIO(source, filename string, input io.Reader, output io.Writer) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	r := newRuntime(filename, input, output)
	err = r.execute(nodes, r.global)
	if err != nil {
		var s signal
		if errors.As(err, &s) {
			switch s.kind {
			case "return":
				return fail("give back can only be used inside a function")
			case "break", "continue":
				return fail("loop control can only be used inside a loop")
			}
		}
	}
	return err
}
func (r *runtime) installBuiltins(e *env) {
	wrap := func(f func([]any) (any, error)) any { return callable(f) }
	e.set("len", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("len expects one argument")
		}
		switch v := a[0].(type) {
		case string:
			return float64(len([]rune(v))), nil
		case []any:
			return float64(len(v)), nil
		case map[string]any:
			return float64(len(v)), nil
		}
		return nil, fail("this value has no length")
	}))
	e.set("sum", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("sum expects one list")
		}
		xs, ok := a[0].([]any)
		if !ok {
			return nil, fail("sum expects a list")
		}
		var n float64
		for _, x := range xs {
			v, err := numberValue(x)
			if err != nil {
				return nil, err
			}
			n += v
		}
		return n, nil
	}))
	e.set("min", wrap(func(a []any) (any, error) { return numericExtreme(a, false) }))
	e.set("max", wrap(func(a []any) (any, error) { return numericExtreme(a, true) }))
	e.set("abs", wrap(unaryMath(math.Abs)))
	e.set("sqrt", wrap(unaryMath(math.Sqrt)))
	e.set("floor", wrap(unaryMath(math.Floor)))
	e.set("ceiling", wrap(unaryMath(math.Ceil)))
	e.set("round", wrap(func(a []any) (any, error) {
		if len(a) < 1 || len(a) > 2 {
			return nil, fail("round expects one or two arguments")
		}
		n, err := numberValue(a[0])
		if err != nil {
			return nil, err
		}
		digits := 0.0
		if len(a) == 2 {
			digits, _ = numberValue(a[1])
		}
		p := math.Pow10(int(digits))
		return math.Round(n*p) / p, nil
	}))
	e.set("number", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("number expects one argument")
		}
		return numberValue(a[0])
	}))
	e.set("int", e.values["number"])
	e.set("float", e.values["number"])
	e.set("str", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("str expects one argument")
		}
		return display(a[0]), nil
	}))
	e.set("bool", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("bool expects one argument")
		}
		return truthy(a[0]), nil
	}))
	e.set("first", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "first")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("first cannot be used on an empty list")
		}
		return xs[0], nil
	}))
	e.set("last", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "last")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("last cannot be used on an empty list")
		}
		return xs[len(xs)-1], nil
	}))
	e.set("average", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "average")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("average needs at least one value")
		}
		var s float64
		for _, x := range xs {
			n, e := numberValue(x)
			if e != nil {
				return nil, e
			}
			s += n
		}
		return s / float64(len(xs)), nil
	}))
	e.set("sorted", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "sorted")
		if err != nil {
			return nil, err
		}
		out := append([]any(nil), xs...)
		sort.SliceStable(out, func(i, j int) bool { return compare(out[i], out[j]) < 0 })
		return out, nil
	}))
	e.set("reversed", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "reversed")
		if err != nil {
			return nil, err
		}
		out := append([]any(nil), xs...)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out, nil
	}))
	e.set("range", wrap(func(a []any) (any, error) {
		if len(a) < 1 || len(a) > 3 {
			return nil, fail("range expects one to three arguments")
		}
		nums := make([]int, len(a))
		for i, x := range a {
			n, err := numberValue(x)
			if err != nil {
				return nil, err
			}
			nums[i] = int(n)
		}
		start, end, step := 0, nums[0], 1
		if len(nums) >= 2 {
			start, end = nums[0], nums[1]
		}
		if len(nums) == 3 {
			step = nums[2]
		}
		if step == 0 {
			return nil, fail("range step cannot be zero")
		}
		out := []any{}
		for i := start; (step > 0 && i < end) || (step < 0 && i > end); i += step {
			if len(out) >= maxLoopIterations {
				return nil, fail("range is too large")
			}
			out = append(out, float64(i))
		}
		return out, nil
	}))
	e.set("random_number", wrap(func(a []any) (any, error) {
		lo, hi := 0, 100
		if len(a) > 2 {
			return nil, fail("random_number expects zero, one, or two arguments")
		}
		if len(a) > 0 {
			n, er := numberValue(a[0])
			if er != nil {
				return nil, er
			}
			lo = int(n)
		}
		if len(a) > 1 {
			n, er := numberValue(a[1])
			if er != nil {
				return nil, er
			}
			hi = int(n)
		}
		if hi < lo {
			return nil, fail("random_number upper bound must not be smaller than lower bound")
		}
		return float64(lo + rand.Intn(hi-lo+1)), nil
	}))
	e.set("type_of", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("type_of expects one argument")
		}
		return typeName(a[0]), nil
	}))
}
func unaryMath(f func(float64) float64) func([]any) (any, error) {
	return func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("this function expects one argument")
		}
		n, e := numberValue(a[0])
		if e != nil {
			return nil, e
		}
		return f(n), nil
	}
}
func oneList(a []any, name string) ([]any, error) {
	if len(a) != 1 {
		return nil, fail("%s expects one list", name)
	}
	xs, ok := a[0].([]any)
	if !ok {
		return nil, fail("%s expects a list", name)
	}
	return xs, nil
}
func numericExtreme(a []any, wantMax bool) (any, error) {
	if len(a) == 1 {
		if xs, ok := a[0].([]any); ok {
			a = xs
		}
	}
	if len(a) == 0 {
		return nil, fail("min/max needs at least one value")
	}
	best, err := numberValue(a[0])
	if err != nil {
		return nil, err
	}
	for _, x := range a[1:] {
		n, e := numberValue(x)
		if e != nil {
			return nil, e
		}
		if (wantMax && n > best) || (!wantMax && n < best) {
			best = n
		}
	}
	return best, nil
}
func numberValue(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case string:
		f, e := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if e != nil {
			return 0, fail("%q is not a number", n)
		}
		return f, nil
	}
	return 0, fail("%s cannot be used as a number", typeName(v))
}
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "nothing"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "text"
	case []any:
		return "list"
	case map[string]any:
		return "dictionary"
	case callable, *userFunction:
		return "function"
	}
	return "value"
}
func display(v any) string {
	switch x := v.(type) {
	case nil:
		return "nothing"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		if math.IsInf(x, 1) {
			return "infinity"
		}
		if math.IsInf(x, -1) {
			return "-infinity"
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, v := range x {
			parts[i] = display(v)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		b, e := json.Marshal(x)
		if e == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	default:
		return fmt.Sprint(v)
	}
}
func compare(a, b any) int {
	if an, e := numberValue(a); e == nil {
		if bn, e2 := numberValue(b); e2 == nil {
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
			return 0
		}
	}
	as, bs := display(a), display(b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func (r *runtime) execute(nodes []node, e *env) error {
	for _, n := range nodes {
		if err := r.execNode(n, e); err != nil {
			var s signal
			if errors.As(err, &s) {
				return err
			}
			return fail("%s:%d: %v", r.filename, n.line, err)
		}
	}
	return nil
}
func (r *runtime) execNode(n node, e *env) error {
	switch n.kind {
	case "if":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		if truthy(v) {
			return r.execute(n.body, e)
		}
		return r.execute(n.otherwise, e)
	case "while":
		for i := 0; i < maxLoopIterations; i++ {
			v, err := r.eval(n.condition, e)
			if err != nil {
				return err
			}
			if !truthy(v) {
				return nil
			}
			err = r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					return nil
				}
				return err
			}
		}
		return fail("this loop ran over one million times; stopped to prevent a runaway program")
	case "repeat":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		f, err := numberValue(v)
		if err != nil {
			return err
		}
		count := int(f)
		if count < 0 || count > maxLoopIterations {
			return fail("repeat count must be between 0 and 1,000,000")
		}
		for i := 1; i <= count; i++ {
			e.set("_repeat_index", float64(i))
			err := r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					break
				}
				return err
			}
		}
		return nil
	case "for":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		items, ok := v.([]any)
		if !ok {
			if s, yes := v.(string); yes {
				items = []any{}
				for _, c := range s {
					items = append(items, string(c))
				}
			} else {
				return fail("for each needs a list or text value")
			}
		}
		for _, item := range items {
			e.set(n.name, item)
			e.set("_current_item", item)
			err := r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					break
				}
				return err
			}
		}
		return nil
	case "function":
		e.set(n.name, &userFunction{name: n.name, params: n.params, body: n.body, closure: e, runtime: r})
		return nil
	case "try":
		err := r.execute(n.body, e)
		if err == nil {
			return nil
		}
		var s signal
		if errors.As(err, &s) {
			return err
		}
		e.set("error_message", err.Error())
		return r.execute(n.handler, e)
	}
	return r.statement(n.text, e)
}

var (
	reLet     = regexp.MustCompile(`(?i)^let\s+([A-Za-z_]\w*)\s+be\s+(.+)$`)
	reSet     = regexp.MustCompile(`(?i)^set\s+([A-Za-z_]\w*)\s+to\s+(.+)$`)
	reSay     = regexp.MustCompile(`(?i)^(?:say|show|print)\s+(.+)$`)
	reAsk     = regexp.MustCompile(`(?i)^ask\s+(.+?)\s+and\s+(?:save the answer as|put the answer in)\s+([A-Za-z_]\w*)$`)
	reRead    = regexp.MustCompile(`(?i)^read the file\s+(.+?)\s+into\s+([A-Za-z_]\w*)$`)
	reWrite   = regexp.MustCompile(`(?i)^write\s+(.+?)\s+to the file\s+(.+)$`)
	reAppend  = regexp.MustCompile(`(?i)^append\s+(.+?)\s+to the file\s+(.+)$`)
	reUse     = regexp.MustCompile(`(?i)^use the\s+([A-Za-z_]+)\s+library(?:\s+as\s+([A-Za-z_]\w*))?$`)
	reCall    = regexp.MustCompile(`(?i)^(?:call|run)\s+([A-Za-z_]\w*)\s+with\s+(.+)$`)
	reReturn  = regexp.MustCompile(`(?i)^(?:give back|return)(?:\s+(.+))?$`)
	reSetItem = regexp.MustCompile(`(?i)^set (?:the )?item at position (.+?) in ([A-Za-z_]\w*) to (.+)$`)
	reSetKey  = regexp.MustCompile(`(?i)^set (?:the )?key (.+?) in ([A-Za-z_]\w*) to (.+)$`)
	reAdd     = regexp.MustCompile(`(?i)^add\s+(.+?)\s+to\s+([A-Za-z_]\w*)$`)
	reRemove  = regexp.MustCompile(`(?i)^remove\s+(.+?)\s+from\s+([A-Za-z_]\w*)$`)
	reRaise   = regexp.MustCompile(`(?i)^raise an error saying\s+(.+)$`)
)

func (r *runtime) statement(s string, e *env) error {
	if m := reLet.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[2], e)
		if err == nil {
			e.set(m[1], v)
		}
		return err
	}
	if m := reSet.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[2], e)
		if err == nil {
			e.set(m[1], v)
		}
		return err
	}
	if m := reSay.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		fmt.Fprintln(r.output, display(v))
		return nil
	}
	if m := reAsk.FindStringSubmatch(s); m != nil {
		p, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		fmt.Fprint(r.output, display(p))
		line, err := r.reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		e.set(m[2], line)
		e.set("_last_answer", line)
		return nil
	}
	if m := reRead.FindStringSubmatch(s); m != nil {
		path, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(fmt.Sprint(path))
		if err != nil {
			return err
		}
		e.set(m[2], string(data))
		return nil
	}
	if m := reWrite.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		path, err := r.eval(m[2], e)
		if err != nil {
			return err
		}
		return os.WriteFile(fmt.Sprint(path), []byte(display(v)), 0644)
	}
	if m := reAppend.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		path, err := r.eval(m[2], e)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(fmt.Sprint(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(display(v))
		return err
	}
	if m := reUse.FindStringSubmatch(s); m != nil {
		requested := strings.ToLower(m[1])
		alias := m[2]
		if alias == "" {
			alias = requested
		}
		module, err := moduleValue(requested)
		if err != nil {
			return err
		}
		e.set(alias, module)
		return nil
	}
	if m := reCall.FindStringSubmatch(s); m != nil {
		v, err := e.get(m[1])
		if err != nil {
			return err
		}
		args := []any{}
		for _, part := range splitAnd(m[2]) {
			x, er := r.eval(part, e)
			if er != nil {
				return er
			}
			args = append(args, x)
		}
		_, err = invoke(v, args)
		return err
	}
	if m := reReturn.FindStringSubmatch(s); m != nil {
		var v any
		if strings.TrimSpace(m[1]) != "" {
			x, err := r.eval(m[1], e)
			if err != nil {
				return err
			}
			v = x
		}
		return signal{kind: "return", value: v}
	}
	if strings.EqualFold(s, "stop this loop") || strings.EqualFold(s, "break out of this loop") {
		return signal{kind: "break"}
	}
	if strings.EqualFold(s, "skip to the next repetition") || strings.EqualFold(s, "continue to the next repetition") {
		return signal{kind: "continue"}
	}
	if m := reSetItem.FindStringSubmatch(s); m != nil {
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q is not a list", m[2])
		}
		ix, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		i, err := numberValue(ix)
		if err != nil {
			return err
		}
		val, err := r.eval(m[3], e)
		if err != nil {
			return err
		}
		idx := int(i)
		if idx < 0 {
			idx += len(xs)
		}
		if idx < 0 || idx >= len(xs) {
			return fail("list index out of range")
		}
		xs[idx] = val
		return nil
	}
	if m := reSetKey.FindStringSubmatch(s); m != nil {
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return fail("%q is not a dictionary", m[2])
		}
		key, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		val, err := r.eval(m[3], e)
		if err != nil {
			return err
		}
		obj[fmt.Sprint(key)] = val
		return nil
	}
	if m := reAdd.FindStringSubmatch(s); m != nil {
		val, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q does not support adding items", m[2])
		}
		e.values[m[2]] = append(xs, val)
		return nil
	}
	if m := reRemove.FindStringSubmatch(s); m != nil {
		val, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q does not support removing items", m[2])
		}
		for i, x := range xs {
			if reflect.DeepEqual(x, val) {
				e.values[m[2]] = append(xs[:i], xs[i+1:]...)
				return nil
			}
		}
		return fail("item %q was not found", display(val))
	}
	if m := reRaise.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		return fail("%s", display(v))
	}
	_, err := r.eval(s, e)
	if err != nil {
		return fail("I don't recognise this sentence: %q (%v)", s, err)
	}
	return nil
}
func invoke(v any, args []any) (any, error) {
	switch f := v.(type) {
	case callable:
		return f(args)
	case *userFunction:
		if len(args) != len(f.params) {
			return nil, fail("%q needs %d argument(s), but got %d", f.name, len(f.params), len(args))
		}
		local := newEnv(f.closure)
		for i, p := range f.params {
			local.set(p, args[i])
		}
		err := f.runtime.execute(f.body, local)
		if err != nil {
			var s signal
			if errors.As(err, &s) && s.kind == "return" {
				return s.value, nil
			}
			return nil, err
		}
		return nil, nil
	}
	return nil, fail("this value cannot be called")
}
func moduleValue(name string) (any, error) {
	mathModule := map[string]any{"pi": math.Pi, "e": math.E, "sqrt": callable(unaryMath(math.Sqrt)), "floor": callable(unaryMath(math.Floor)), "ceil": callable(unaryMath(math.Ceil)), "sin": callable(unaryMath(math.Sin)), "cos": callable(unaryMath(math.Cos)), "tan": callable(unaryMath(math.Tan)), "abs": callable(unaryMath(math.Abs)), "log": callable(unaryMath(math.Log)), "pow": callable(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("pow expects two arguments")
		}
		x, e := numberValue(a[0])
		if e != nil {
			return nil, e
		}
		y, e := numberValue(a[1])
		return math.Pow(x, y), e
	})}
	switch name {
	case "math", "mathematics":
		return mathModule, nil
	case "random":
		return map[string]any{"number": callable(func(a []any) (any, error) {
			lo, hi := 0.0, 1.0
			if len(a) > 0 {
				lo, _ = numberValue(a[0])
			}
			if len(a) > 1 {
				hi, _ = numberValue(a[1])
			}
			return lo + rand.Float64()*(hi-lo), nil
		}), "integer": callable(func(a []any) (any, error) {
			return newRuntime("", nil, nil).global.values["random_number"].(callable)(a)
		})}, nil
	case "json":
		return map[string]any{"loads": callable(func(a []any) (any, error) {
			if len(a) != 1 {
				return nil, fail("json.loads expects one text argument")
			}
			var v any
			err := json.Unmarshal([]byte(fmt.Sprint(a[0])), &v)
			return normalizeJSON(v), err
		}), "dumps": callable(func(a []any) (any, error) {
			if len(a) != 1 {
				return nil, fail("json.dumps expects one argument")
			}
			b, e := json.Marshal(a[0])
			return string(b), e
		})}, nil
	case "statistics":
		return map[string]any{"mean": callable(func(a []any) (any, error) { return newRuntime("", nil, nil).global.values["average"].(callable)(a) })}, nil
	case "datetime", "dates":
		return map[string]any{"now": callable(func(a []any) (any, error) { return time.Now().Format(time.RFC3339), nil }), "today": callable(func(a []any) (any, error) { return time.Now().Format("2006-01-02"), nil })}, nil
	default:
		return nil, fail("the %q library is not available in this version of the native runtime", name)
	}
}
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = normalizeJSON(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalizeJSON(x[k])
		}
	}
	return v
}

// Expression parser: a compact Pratt parser for Tell's expression language.
type token struct{ kind, text string }
type exprParser struct {
	tokens  []token
	pos     int
	runtime *runtime
	env     *env
}

func translateExpr(s string) string {
	for _, pair := range [][2]string{
		{"the current repetition number", "_repeat_index"},
		{"the current item", "_current_item"},
		{"the answer", "_last_answer"},
	} {
		s = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(pair[0])).ReplaceAllString(s, pair[1])
	}
	// Natural function-result and prefix phrases are rewritten before tokenizing.
	result := regexp.MustCompile(`(?i)^the result of\s+([A-Za-z_]\w*)\s+with\s+(.+)$`)
	if m := result.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		parts := splitAnd(m[2])
		return m[1] + "(" + strings.Join(parts, ",") + ")"
	}
	for _, p := range []struct{ re, replace string }{{`(?i)^the length of\s+(.+)$`, `len($1)`}, {`(?i)^the square root of\s+(.+)$`, `sqrt($1)`}, {`(?i)^the absolute value of\s+(.+)$`, `abs($1)`}, {`(?i)^the type of\s+(.+)$`, `type_of($1)`}} {
		if re := regexp.MustCompile(p.re); re.MatchString(strings.TrimSpace(s)) {
			s = re.ReplaceAllString(strings.TrimSpace(s), p.replace)
			break
		}
	}
	// Apply word operators only outside quoted strings.
	var out strings.Builder
	var segment strings.Builder
	quote := byte(0)
	escaped := false
	flush := func() {
		x := segment.String()
		segment.Reset()
		patterns := []struct{ from, to string }{{"is greater than or equal to", ">="}, {"is less than or equal to", "<="}, {"is not equal to", "!="}, {"isn't equal to", "!="}, {"is equal to", "=="}, {"is greater than", ">"}, {"is more than", ">"}, {"is less than", "<"}, {"is below", "<"}, {"is at least", ">="}, {"is at most", "<="}, {"is not in", "notin"}, {"is in", "in"}, {"to the power of", "**"}, {"divided by", "/"}, {"multiplied by", "*"}, {"plus", "+"}, {"minus", "-"}, {"times", "*"}, {"modulo", "%"}, {"isn't", "!="}, {"is not", "!="}, {" and ", " and "}, {" or ", " or "}}
		for _, p := range patterns {
			re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(p.from) + `([^A-Za-z0-9_]|$)`)
			x = re.ReplaceAllString(x, "${1}"+p.to+"${2}")
		}
		reIs := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])is([^A-Za-z0-9_]|$)`)
		x = reIs.ReplaceAllString(x, "${1}==${2}")
		repl := strings.NewReplacer("true", "true", "false", "false", "nothing", "nothing")
		_ = repl
		out.WriteString(x)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			out.WriteByte(c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			flush()
			quote = c
			out.WriteByte(c)
		} else {
			segment.WriteByte(c)
		}
	}
	flush()
	// Replace boolean/null words outside strings with canonical tokens.
	return out.String()
}
func lex(s string) ([]token, error) {
	s = translateExpr(s)
	var out []token
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		if c == '"' || c == '\'' {
			q := c
			i++
			var b strings.Builder
			esc := false
			closed := false
			for i < len(s) {
				x := s[i]
				i++
				if esc {
					switch x {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
						b.WriteByte(x)
					}
					esc = false
					continue
				}
				if x == '\\' {
					esc = true
					continue
				}
				if x == q {
					closed = true
					break
				}
				b.WriteByte(x)
			}
			if !closed {
				return nil, fail("unterminated text in expression")
			}
			out = append(out, token{"string", b.String()})
			continue
		}
		if (c >= '0' && c <= '9') || (c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9') {
			start := i
			i++
			for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
				i++
			}
			out = append(out, token{"number", s[start:i]})
			continue
		}
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' {
			start := i
			i++
			for i < len(s) && ((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= '0' && s[i] <= '9') || s[i] == '_') {
				i++
			}
			word := s[start:i]
			low := strings.ToLower(word)
			switch low {
			case "true", "false", "nothing", "and", "or", "not", "in", "notin":
				out = append(out, token{low, low})
			default:
				out = append(out, token{"ident", word})
			}
			continue
		}
		if i+1 < len(s) {
			two := s[i : i+2]
			if two == ">=" || two == "<=" || two == "==" || two == "!=" || two == "**" || two == "//" {
				out = append(out, token{two, two})
				i += 2
				continue
			}
		}
		if strings.ContainsRune("+-*/%<>()[]{}.,", rune(c)) {
			out = append(out, token{string(c), string(c)})
			i++
			continue
		}
		return nil, fail("unexpected character %q in expression", c)
	}
	out = append(out, token{"eof", ""})
	return out, nil
}
func (p *exprParser) peek() string { return p.tokens[p.pos].kind }
func (p *exprParser) take() token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}
func precedence(op string) int {
	switch op {
	case "or":
		return 1
	case "and":
		return 2
	case "==", "!=", "<", ">", "<=", ">=", "in", "notin":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "//", "%":
		return 5
	case "**":
		return 6
	}
	return 0
}
func (p *exprParser) expression(min int) (any, error) {
	left, err := p.prefix()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		prec := precedence(op)
		if prec < min || prec == 0 {
			break
		}
		p.take()
		next := prec + 1
		if op == "**" {
			next = prec
		}
		right, e := p.expression(next)
		if e != nil {
			return nil, e
		}
		left, e = p.binary(op, left, right)
		if e != nil {
			return nil, e
		}
	}
	return left, nil
}
func (p *exprParser) prefix() (any, error) {
	t := p.take()
	var v any
	var err error
	switch t.kind {
	case "number":
		v, err = strconv.ParseFloat(t.text, 64)
	case "string":
		v = p.runtime.interpolate(t.text, p.env)
	case "true":
		v = true
	case "false":
		v = false
	case "nothing":
		v = nil
	case "-", "+", "not":
		x, e := p.expression(7)
		if e != nil {
			return nil, e
		}
		if t.kind == "not" {
			v = !truthy(x)
		} else {
			n, e := numberValue(x)
			if e != nil {
				return nil, e
			}
			if t.kind == "-" {
				v = -n
			} else {
				v = n
			}
		}
	case "(":
		v, err = p.expression(1)
		if err == nil && p.peek() == ")" {
			p.take()
		} else if err == nil {
			err = fail("expected )")
		}
	case "[":
		xs := []any{}
		if p.peek() != "]" {
			for {
				item, e := p.expression(1)
				if e != nil {
					return nil, e
				}
				xs = append(xs, item)
				if p.peek() != "," {
					break
				}
				p.take()
			}
		}
		if p.peek() == "]" {
			p.take()
		} else {
			return nil, fail("expected ]")
		}
		v = xs
	case "{":
		obj := map[string]any{}
		if p.peek() != "}" {
			for {
				key, e := p.expression(1)
				if e != nil {
					return nil, e
				}
				if p.peek() != ":" {
					return nil, fail("expected : in dictionary")
				}
				p.take()
				val, e := p.expression(1)
				if e != nil {
					return nil, e
				}
				obj[fmt.Sprint(key)] = val
				if p.peek() != "," {
					break
				}
				p.take()
			}
		}
		if p.peek() == "}" {
			p.take()
		} else {
			return nil, fail("expected }")
		}
		v = obj
	case "ident":
		v, err = p.env.get(t.text)
	default:
		return nil, fail("expected a value, found %q", t.text)
	}
	if err != nil {
		return nil, err
	}
	return p.postfix(v)
}
func (p *exprParser) postfix(v any) (any, error) {
	for {
		switch p.peek() {
		case "(":
			p.take()
			args := []any{}
			if p.peek() != ")" {
				for {
					x, e := p.expression(1)
					if e != nil {
						return nil, e
					}
					args = append(args, x)
					if p.peek() != "," {
						break
					}
					p.take()
				}
			}
			if p.peek() != ")" {
				return nil, fail("expected ) after arguments")
			}
			p.take()
			var e error
			v, e = invoke(v, args)
			if e != nil {
				return nil, e
			}
		case ".":
			p.take()
			if p.peek() != "ident" {
				return nil, fail("expected a member name after .")
			}
			name := p.take().text
			obj, ok := v.(map[string]any)
			if !ok {
				return nil, fail("this value has no member %q", name)
			}
			member, ok := obj[name]
			if !ok {
				return nil, fail("unknown member %q", name)
			}
			v = member
		case "[":
			p.take()
			ix, e := p.expression(1)
			if e != nil {
				return nil, e
			}
			if p.peek() != "]" {
				return nil, fail("expected ] after index")
			}
			p.take()
			switch x := v.(type) {
			case []any:
				n, e := numberValue(ix)
				if e != nil {
					return nil, e
				}
				i := int(n)
				if i < 0 {
					i += len(x)
				}
				if i < 0 || i >= len(x) {
					return nil, fail("list index out of range")
				}
				v = x[i]
			case string:
				n, e := numberValue(ix)
				if e != nil {
					return nil, e
				}
				r := []rune(x)
				i := int(n)
				if i < 0 {
					i += len(r)
				}
				if i < 0 || i >= len(r) {
					return nil, fail("text index out of range")
				}
				v = string(r[i])
			case map[string]any:
				val, ok := x[fmt.Sprint(ix)]
				if !ok {
					return nil, fail("dictionary key %q was not found", display(ix))
				}
				v = val
			default:
				return nil, fail("this value cannot be indexed")
			}
		default:
			return v, nil
		}
	}
}
func (p *exprParser) binary(op string, a, b any) (any, error) {
	switch op {
	case "and":
		return truthy(a) && truthy(b), nil
	case "or":
		return truthy(a) || truthy(b), nil
	case "==":
		return reflect.DeepEqual(a, b), nil
	case "!=":
		return !reflect.DeepEqual(a, b), nil
	case "<":
		return compare(a, b) < 0, nil
	case ">":
		return compare(a, b) > 0, nil
	case "<=":
		return compare(a, b) <= 0, nil
	case ">=":
		return compare(a, b) >= 0, nil
	case "in", "notin":
		found := false
		switch x := b.(type) {
		case []any:
			for _, v := range x {
				if reflect.DeepEqual(a, v) {
					found = true
				}
			}
		case string:
			found = strings.Contains(x, fmt.Sprint(a))
		case map[string]any:
			_, found = x[fmt.Sprint(a)]
		}
		if op == "notin" {
			found = !found
		}
		return found, nil
	case "+":
		if x, ok := a.(string); ok {
			return x + display(b), nil
		}
		if xs, ok := a.([]any); ok {
			ys, ok := b.([]any)
			if !ok {
				return nil, fail("cannot add a list to this value")
			}
			return append(xs, ys...), nil
		}
		x, e := numberValue(a)
		if e != nil {
			return nil, e
		}
		y, e := numberValue(b)
		return x + y, e
	case "-", "*", "/", "//", "%", "**":
		x, e := numberValue(a)
		if e != nil {
			return nil, e
		}
		y, e := numberValue(b)
		if e != nil {
			return nil, e
		}
		switch op {
		case "-":
			return x - y, nil
		case "*":
			return x * y, nil
		case "/":
			if y == 0 {
				return nil, fail("division by zero")
			}
			return x / y, nil
		case "//":
			if y == 0 {
				return nil, fail("division by zero")
			}
			return math.Floor(x / y), nil
		case "%":
			if y == 0 {
				return nil, fail("division by zero")
			}
			return math.Mod(x, y), nil
		case "**":
			return math.Pow(x, y), nil
		}
	}
	return nil, fail("unsupported operator %q", op)
}
func (r *runtime) eval(s string, e *env) (any, error) {
	tokens, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &exprParser{tokens: tokens, runtime: r, env: e}
	v, err := p.expression(1)
	if err != nil {
		return nil, err
	}
	if p.peek() != "eof" {
		return nil, fail("unexpected %q in expression", p.tokens[p.pos].text)
	}
	return v, nil
}

var interpolation = regexp.MustCompile(`\{([^{}]+)\}`)

func (r *runtime) interpolate(text string, e *env) string {
	return interpolation.ReplaceAllStringFunc(text, func(match string) string {
		inner := strings.TrimSpace(match[1 : len(match)-1])
		v, err := r.eval(inner, e)
		if err != nil {
			return match
		}
		return display(v)
	})
}

// Keep path helpers platform-neutral and prevent an empty filename from silently targeting the cwd.
func cleanPath(path string) string { return filepath.Clean(path) }
