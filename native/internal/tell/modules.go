package tell

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (r *runtime) loadTellModule(path string) (map[string]any, error) {
	if !strings.EqualFold(filepath.Ext(path), ".tell") {
		return nil, fail("Tell modules must use the .tell extension")
	}
	absolute, err := filepath.Abs(filepath.Join(filepath.Dir(r.filename), path))
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	if cached, ok := r.modules[absolute]; ok {
		return cached, nil
	}
	if r.loading[absolute] {
		return nil, fail("circular module import detected for %q", absolute)
	}
	source, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fail("cannot load Tell module %q: %v", path, err)
	}
	nodes, err := parse(string(source), absolute)
	if err != nil {
		return nil, err
	}
	r.loading[absolute] = true
	defer delete(r.loading, absolute)
	moduleEnv := newEnv(r.global)
	previousFilename := r.filename
	r.filename = absolute
	err = r.execute(nodes, moduleEnv)
	r.filename = previousFilename
	if err != nil {
		return nil, err
	}
	exports := make(map[string]any)
	for name, value := range moduleEnv.values {
		if !strings.HasPrefix(name, "_") {
			exports[name] = value
		}
	}
	r.modules[absolute] = exports
	return exports, nil
}

func (f *userFunction) call(args []any, receiver *userInstance) (any, error) {
	return f.callNamed(args, nil, receiver)
}

func (f *userFunction) callNamed(args []any, named map[string]any, receiver *userInstance) (any, error) {
	variadicIndex := -1
	for index, parameter := range f.params {
		_, _, _, variadic, ok := parameterParts(parameter)
		if !ok {
			return nil, fail("invalid parameter definition %q in %s", parameter, f.name)
		}
		if variadic {
			variadicIndex = index
		}
	}
	if len(args) > len(f.params) && variadicIndex < 0 {
		return nil, fail("%q accepts %d argument(s), but got %d positional arguments", f.name, len(f.params), len(args))
	}
	parameterIndex := make(map[string]int, len(f.params))
	bindings := make(map[string]any, len(f.params))
	parameterNames := make([]string, len(f.params))
	defaults := make([]string, len(f.params))
	for index, parameter := range f.params {
		name, defaultValue, hasDefault, variadic, ok := parameterParts(parameter)
		if !ok {
			return nil, fail("invalid parameter definition %q in %s", parameter, f.name)
		}
		parameterNames[index] = name
		parameterIndex[name] = index
		if hasDefault {
			defaults[index] = defaultValue
		}
		if variadic {
			if index < len(args) {
				bindings[name] = append([]any(nil), args[index:]...)
			} else {
				bindings[name] = []any{}
			}
		} else if index < len(args) {
			bindings[name] = args[index]
		}
	}
	for name, value := range named {
		index, exists := parameterIndex[name]
		if !exists {
			return nil, fail("%q has no parameter named %q", f.name, name)
		}
		_, _, _, variadic, _ := parameterParts(f.params[index])
		if variadic {
			return nil, fail("%q variadic parameter %q cannot be supplied by name", f.name, name)
		}
		if index < len(args) {
			return nil, fail("%q got multiple values for %q", f.name, name)
		}
		bindings[name] = value
	}
	for index, parameter := range parameterNames {
		if _, exists := bindings[parameter]; !exists && defaults[index] == "" {
			return nil, fail("%q is missing argument %q", f.name, parameter)
		}
	}
	if f.runtime.callDepth >= f.runtime.limits.MaxFunctionCallDepth {
		return nil, fail("maximum function call depth (%d) exceeded", f.runtime.limits.MaxFunctionCallDepth)
	}
	local := newEnv(f.closure)
	for name, value := range bindings {
		local.set(name, value)
	}
	if receiver != nil {
		local.set("this", receiver)
	}
	caller := f.runtime.global
	if len(f.runtime.callFrames) > 0 {
		caller = f.runtime.callFrames[len(f.runtime.callFrames)-1].Locals
	}
	frame := callFrame{
		Function: f.name,
		Caller: caller,
		Locals: local,
		CallSiteLine: f.runtime.callSiteLine,
		CallSiteColumn: f.runtime.callSiteColumn,
	}
	f.runtime.callFrames = append(f.runtime.callFrames, frame)
	f.runtime.callDepth++
	defer func() {
		f.runtime.callFrames = f.runtime.callFrames[:len(f.runtime.callFrames)-1]
		f.runtime.callDepth--
	}()
	for index, parameter := range parameterNames {
		if _, exists := bindings[parameter]; exists {
			continue
		}
		value, err := f.runtime.eval(defaults[index], local)
		if err != nil {
			return nil, err
		}
		local.set(parameter, value)
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

func invoke(v any, args []any) (any, error) {
	return invokeNamed(v, args, nil)
}

func isCallableValue(value any) bool {
	switch value.(type) {
	case callable, namedCallable, *userFunction, *userClass, *bytecodeClassValue, pythonFunction, javascriptFunction:
		return true
	default:
		return false
	}
}

func invokeNamed(v any, args []any, named map[string]any) (any, error) {
	switch f := v.(type) {
	case pythonFunction:
		return callPython(f.module, f.name, args, named)
	case javascriptFunction:
		return callJavaScript(f.module, f.name, args, named)
	case namedCallable:
		return f(args, named)
	case callable:
		if len(named) != 0 {
			return nil, fail("named arguments are not supported by this function")
		}
		return f(args)
	case *userFunction:
		return f.callNamed(args, named, nil)
	case *userClass:
		instance := &userInstance{class: f, fields: make(map[string]any)}
		if f.constructor == nil {
			if len(args) > 0 || len(named) > 0 {
				return nil, fail("class %q has no constructor and accepts no arguments", f.name)
			}
			return instance, nil
		}
		if _, err := f.constructor.callNamed(args, named, instance); err != nil {
			return nil, err
		}
		return instance, nil
	}
	return nil, fail("this value cannot be called")
}

func (c *userClass) method(name string) (*userFunction, bool) {
	if method, ok := c.methods[name]; ok {
		return method, true
	}
	if c.base != nil {
		return c.base.method(name)
	}
	return nil, false
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
			return newRuntime("", nil, nil, DefaultExecutionLimits()).global.values["random_number"].(callable)(a)
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
	case "csv":
		return map[string]any{
			"parse": callable(func(a []any) (any, error) {
				if len(a) != 1 {
					return nil, fail("csv.parse expects one text argument")
				}
				text, ok := a[0].(string)
				if !ok {
					return nil, fail("csv.parse expects one text argument")
				}
				reader := csv.NewReader(strings.NewReader(text))
				reader.FieldsPerRecord = -1
				rows := make([]any, 0)
				for {
					record, err := reader.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, fail("csv.parse: %v", err)
					}
					if len(record) == 0 {
						continue
					}
					row := make([]any, len(record))
					for i, cell := range record {
						row[i] = cell
					}
					rows = append(rows, row)
				}
				return rows, nil
			}),
			"stringify": callable(func(a []any) (any, error) {
				if len(a) != 1 {
					return nil, fail("csv.stringify expects one list of rows")
				}
				rows, ok := a[0].([]any)
				if !ok {
					return nil, fail("csv.stringify expects one list of rows")
				}
				var output bytes.Buffer
				writer := csv.NewWriter(&output)
				for rowIndex, value := range rows {
					row, ok := value.([]any)
					if !ok {
						return nil, fail("csv.stringify row %d must be a list", rowIndex+1)
					}
					record := make([]string, len(row))
					for column, cell := range row {
						text, ok := cell.(string)
						if !ok {
							return nil, fail("csv.stringify row %d must contain only text", rowIndex+1)
						}
						record[column] = text
					}
					if err := writer.Write(record); err != nil {
						return nil, err
					}
				}
				writer.Flush()
				if err := writer.Error(); err != nil {
					return nil, err
				}
				return output.String(), nil
			}),
		}, nil
	case "statistics":
		return map[string]any{"mean": callable(func(a []any) (any, error) {
			return newRuntime("", nil, nil, DefaultExecutionLimits()).global.values["average"].(callable)(a)
		})}, nil
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

// token carries source offsets and one-based diagnostic coordinates.
