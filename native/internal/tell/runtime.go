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
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type env struct {
	values    map[string]any
	parent    *env
	constants map[string]bool
	types     map[string]string
}

func newEnv(parent *env) *env {
	return &env{values: map[string]any{}, parent: parent, constants: map[string]bool{}, types: map[string]string{}}
}
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
type namedCallable func([]any, map[string]any) (any, error)
type pythonModule struct{ name string }
type pythonFunction struct{ module, name string }
type userFunction struct {
	name    string
	params  []string
	body    []node
	closure *env
	runtime *runtime
}
type userClass struct {
	name        string
	methods     map[string]*userFunction
	constructor *userFunction
	base        *userClass
}
type userInstance struct {
	class  *userClass
	fields map[string]any
}
// callFrame records a live Tell function activation independently of the Go stack.
type callFrame struct {
	Function string
	Caller *env
	Locals *env
	CallSiteLine int
	CallSiteColumn int
}

type signal struct {
	kind  string
	value any
}

func (s signal) Error() string { return s.kind }

type runtime struct {
	filename        string
	global          *env
	reader          *bufio.Reader
	output          io.Writer
	echoResults     bool
	callDepth       int
	callFrames      []callFrame
	callSiteLine    int
	callSiteColumn  int
	limits          ExecutionLimits
	modules         map[string]map[string]any
	bytecodeModules map[string]BytecodeProgram
	loading         map[string]bool
}

func typeMatches(typeName string, value any, e *env) (bool, error) {
	switch strings.ToLower(typeName) {
	case "any":
		return true, nil
	case "nothing":
		return value == nil, nil
	case "boolean":
		_, ok := value.(bool)
		return ok, nil
	case "number":
		switch value.(type) {
		case float64, int, int64:
			return true, nil
		}
		return false, nil
	case "text":
		_, ok := value.(string)
		return ok, nil
	case "list":
		_, ok := value.([]any)
		return ok, nil
	case "tuple":
		_, ok := value.(tupleValue)
		return ok, nil
	case "set":
		_, ok := value.(setValue)
		return ok, nil
	case "dictionary":
		_, ok := value.(map[string]any)
		return ok, nil
	case "function":
		switch value.(type) {
		case callable, namedCallable, *userFunction:
			return true, nil
		}
		return false, nil
	case "class":
		switch value.(type) {
		case *userClass, *bytecodeClassValue:
			return true, nil
		default:
			return false, nil
		}
	default:
		candidate, err := e.get(typeName)
		if err != nil {
			return false, fail("unknown type annotation %q", typeName)
		}
		switch class := candidate.(type) {
		case *userClass:
			instance, ok := value.(*userInstance)
			if !ok {
				return false, nil
			}
			for current := instance.class; current != nil; current = current.base {
				if current == class {
					return true, nil
				}
			}
			return false, nil
		case *bytecodeClassValue:
			instance, ok := value.(*bytecodeInstance)
			if !ok {
				return false, nil
			}
			for current := instance.class; current != nil; current = current.base {
				if current == class {
					return true, nil
				}
			}
			return false, nil
		default:
			return false, fail("%q is not a type", typeName)
		}
	}
}

func (r *runtime) checkType(annotation string, value any, e *env, name string) error {
	match, err := typeMatches(annotation, value, e)
	if err != nil {
		return err
	}
	if !match {
		return fail("type mismatch for %q: expected %s, got %s", name, annotation, typeName(value))
	}
	return nil
}

func (r *runtime) declare(e *env, name string, value any, typeName string, constant bool) error {
	if e.constants[name] {
		return fail("constant %q cannot be redeclared", name)
	}
	if typeName != "" {
		if previous, exists := e.types[name]; exists && !strings.EqualFold(previous, typeName) {
			return fail("%q is already annotated as %s", name, previous)
		}
		if err := r.checkType(typeName, value, e, name); err != nil {
			return err
		}
		e.types[name] = typeName
	}
	if constant {
		e.constants[name] = true
	}
	e.set(name, value)
	return nil
}

func (r *runtime) assign(e *env, name string, value any) error {
	if e.constants[name] {
		return fail("constant %q cannot be reassigned", name)
	}
	if typeName, ok := e.types[name]; ok {
		if err := r.checkType(typeName, value, e, name); err != nil {
			return err
		}
	}
	e.set(name, value)
	return nil
}

func (r *runtime) assignMany(e *env, names []string, values []any, constant bool) error {
	if len(names) != len(values) {
		return fail("multiple assignment expected %d values, got %d", len(names), len(values))
	}
	for index, name := range names {
		if constant {
			if e.constants[name] {
				return fail("constant %q cannot be redeclared", name)
			}
			if annotation, ok := e.types[name]; ok {
				if err := r.checkType(annotation, values[index], e, name); err != nil {
					return err
				}
			}
			continue
		}
		if e.constants[name] {
			return fail("constant %q cannot be reassigned", name)
		}
		if annotation, ok := e.types[name]; ok {
			if err := r.checkType(annotation, values[index], e, name); err != nil {
				return err
			}
		}
	}
	for index, name := range names {
		if constant {
			if err := r.declare(e, name, values[index], "", true); err != nil {
				return err
			}
		} else {
			e.set(name, values[index])
		}
	}
	return nil
}

func newRuntime(filename string, input io.Reader, output io.Writer, limits ExecutionLimits) *runtime {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	defaults := DefaultExecutionLimits()
	if limits.MaxFunctionCallDepth <= 0 {
		limits.MaxFunctionCallDepth = defaults.MaxFunctionCallDepth
	}
	if limits.MaxLoopIterations <= 0 {
		limits.MaxLoopIterations = defaults.MaxLoopIterations
	}
	r := &runtime{filename: filename, reader: reader, output: output, limits: limits, modules: make(map[string]map[string]any), loading: make(map[string]bool)}
	r.global = newEnv(nil)
	r.installBuiltins(r.global)
	return r
}
func Check(source, filename string) error { _, err := parse(source, filename); return err }

// Session executes multiple source units in one persistent global environment.
type Session struct{ runtime *runtime }

// SetEchoExpressions makes bare expression statements display their result.
// Interactive shells enable this; file execution leaves it disabled.
func (s *Session) SetEchoExpressions(enabled bool) { s.runtime.echoResults = enabled }

func NewSession(input io.Reader, output io.Writer) *Session {
	return NewSessionWithLimits(input, output, DefaultExecutionLimits())
}

func NewSessionWithLimits(input io.Reader, output io.Writer, limits ExecutionLimits) *Session {
	return &Session{runtime: newRuntime("<memory>", input, output, limits)}
}

func (s *Session) Exec(source, filename string) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	r := s.runtime
	r.filename = filename
	err = r.execute(nodes, r.global)
	if err != nil {
		var signalErr signal
		if errors.As(err, &signalErr) {
			switch signalErr.kind {
			case "return":
				return fail("give back can only be used inside a function")
			case "break", "continue":
				return fail("loop control can only be used inside a loop")
			}
		}
	}
	return err
}

func Run(source, filename string) error {
	return RunWithIO(source, filename, os.Stdin, os.Stdout)
}
func RunWithIO(source, filename string, input io.Reader, output io.Writer) error {
	return NewSession(input, output).Exec(source, filename)
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
		case tupleValue:
			return float64(len(v)), nil
		case setValue:
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
	e.set("tuple", wrap(func(a []any) (any, error) {
		if len(a) == 0 {
			return tupleValue{}, nil
		}
		if len(a) != 1 {
			return nil, fail("tuple expects zero or one iterable")
		}
		switch values := a[0].(type) {
		case []any:
			return tupleValue(append([]any(nil), values...)), nil
		case tupleValue:
			return tupleValue(append([]any(nil), values...)), nil
		case setValue:
			return tupleValue(append([]any(nil), values...)), nil
		case string:
			items := make(tupleValue, 0, utf8.RuneCountInString(values))
			for _, character := range values {
				items = append(items, string(character))
			}
			return items, nil
		default:
			return nil, fail("tuple expects a list, tuple, set, or text")
		}
	}))
	e.set("set", wrap(func(a []any) (any, error) {
		if len(a) == 0 {
			return setValue{}, nil
		}
		if len(a) != 1 {
			return nil, fail("set expects zero or one iterable")
		}
		switch values := a[0].(type) {
		case []any:
			return newSetValue(values), nil
		case tupleValue:
			return newSetValue([]any(values)), nil
		case setValue:
			return newSetValue([]any(values)), nil
		case string:
			items := make([]any, 0, utf8.RuneCountInString(values))
			for _, character := range values {
				items = append(items, string(character))
			}
			return newSetValue(items), nil
		default:
			return nil, fail("set expects a list, tuple, set, or text")
		}
	}))
	setOperation := func(name string, operation string) any {
		return wrap(func(args []any) (any, error) {
			if len(args) != 2 {
				return nil, fail("%s expects two sets", name)
			}
			left, leftOK := args[0].(setValue)
			right, rightOK := args[1].(setValue)
			if !leftOK || !rightOK {
				return nil, fail("%s expects two sets", name)
			}
			result := make([]any, 0, len(left)+len(right))
			switch operation {
			case "union":
				result = append(result, left...)
				result = append(result, right...)
			case "intersection":
				for _, value := range left {
					if setContains(right, value) {
						result = append(result, value)
					}
				}
			case "difference":
				for _, value := range left {
					if !setContains(right, value) {
						result = append(result, value)
					}
				}
			}
			return newSetValue(result), nil
		})
	}
	e.set("union", setOperation("union", "union"))
	e.set("intersection", setOperation("intersection", "intersection"))
	e.set("difference", setOperation("difference", "difference"))
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
	e.set("enumerate", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "enumerate")
		if err != nil {
			return nil, err
		}
		out := make([]any, len(xs))
		for index, value := range xs {
			out[index] = []any{float64(index), value}
		}
		return out, nil
	}))
	e.set("map", wrap(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("map expects a function and a list")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("map expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("map expects a list as its second argument")
		}
		out := make([]any, len(values))
		for index, value := range values {
			mapped, err := invokeNamed(a[0], []any{value}, nil)
			if err != nil {
				return nil, err
			}
			out[index] = mapped
		}
		return out, nil
	}))
	e.set("filter", wrap(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("filter expects a function and a list")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("filter expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("filter expects a list as its second argument")
		}
		out := make([]any, 0, len(values))
		for _, value := range values {
			keep, err := invokeNamed(a[0], []any{value}, nil)
			if err != nil {
				return nil, err
			}
			if truthy(keep) {
				out = append(out, value)
			}
		}
		return out, nil
	}))
	e.set("reduce", wrap(func(a []any) (any, error) {
		if len(a) != 3 {
			return nil, fail("reduce expects a function, a list, and an initial value")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("reduce expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("reduce expects a list as its second argument")
		}
		result := a[2]
		for _, value := range values {
			next, err := invokeNamed(a[0], []any{result, value}, nil)
			if err != nil {
				return nil, err
			}
			result = next
		}
		return result, nil
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
			if len(out) >= r.limits.MaxLoopIterations {
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
		f, e := parseFloatPortable(n)
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
	case tupleValue:
		return len(x) > 0
	case setValue:
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
	case tupleValue:
		return "tuple"
	case setValue:
		return "set"
	case map[string]any:
		return "dictionary"
	case callable, namedCallable, *userFunction:
		return "function"
	case pythonFunction:
		return "function"
	case pythonModule:
		return "module"
	case javascriptFunction:
		return "function"
	case javascriptModule:
		return "module"
	case *userClass:
		return "class"
	case *userInstance:
		return v.(*userInstance).class.name
	case *bytecodeClassValue:
		return "class"
	case *bytecodeInstance:
		return v.(*bytecodeInstance).class.name
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
		if x == 0 {
			return "0"
		}
		if math.IsNaN(x) {
			return "NaN"
		}
		if math.IsInf(x, 1) {
			return "infinity"
		}
		if math.IsInf(x, -1) {
			return "-infinity"
		}
		if math.Trunc(x) == x {
			return strconv.FormatFloat(x, 'f', -1, 64)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, v := range x {
			parts[i] = display(v)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case tupleValue:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = display(item)
		}
		if len(parts) == 1 {
			return "(" + parts[0] + ",)"
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case setValue:
		if len(x) == 0 {
			return "set()"
		}
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = display(item)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case map[string]any:
		b, e := json.Marshal(x)
		if e == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	case pythonModule:
		return "<Python module " + x.name + ">"
	case javascriptModule:
		return "<JavaScript module " + x.name + ">"
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

