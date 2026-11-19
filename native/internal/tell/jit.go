package tell

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
)

// EngineStats reports how a bytecode run was tiered. InterpretedCalls includes
// warm-up calls in auto mode and functions that are not eligible for native JIT.
type EngineStats struct {
	JITCompiledFunctions int
	JITInvocations       int
	InterpretedCalls     int
}

// nativeCode owns one executable mapping. The platform backend is responsible
// for allocating it with write permissions, then switching it to read/execute.
type nativeCode struct {
	entry   uintptr
	mapping []byte
}

func NativeJITAvailable() bool { return nativeJITAvailable() }

func ValidExecutionEngine(engine string) bool { return validExecutionEngine(engine) }

func validExecutionEngine(engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "interp", "jit", "auto":
		return true
	default:
		return false
	}
}

func (r *runtime) runJITFunction(function BytecodeFunction, local *env) (any, bool, error) {
	if r.executionEngine == "" || r.executionEngine == "interp" {
		return nil, false, nil
	}
	keyData, err := json.Marshal(function)
	if err != nil {
		return nil, false, err
	}
	key := sha256.Sum256(keyData)

	if r.executionEngine == "auto" {
		r.jitCalls[key]++
		if r.jitCalls[key] < r.jitHotThreshold {
			r.jitStats.InterpretedCalls++
			return nil, false, nil
		}
	}
	if r.jitUnsupported[key] {
		r.jitStats.InterpretedCalls++
		return nil, false, nil
	}

	code, exists := r.jitCache[key]
	if !exists {
		code, err = compileNativeJIT(function)
		if err != nil {
			// Unsupported constructs are expected during tiered execution. Cache
			// the result so we do not repeatedly try compiling the same function.
			r.jitUnsupported[key] = true
			r.jitStats.InterpretedCalls++
			return nil, false, nil
		}
		r.jitCache[key] = code
		r.jitStats.JITCompiledFunctions++
	}

	arguments := make([]float64, len(function.Parameters))
	for index, name := range function.Parameters {
		value, exists := local.values[name]
		if !exists {
			value, err = local.get(name)
			if err != nil {
				return nil, true, err
			}
		}
		arguments[index], err = numberValue(value)
		if err != nil {
			return nil, true, fail("native JIT argument %q is not numeric: %v", name, err)
		}
	}
	if len(arguments) == 0 {
		// The machine-code ABI takes a pointer even for a zero-argument function.
		arguments = []float64{0}
	}
	result := callNativeJIT(code.entry, &arguments[0])
	r.jitStats.JITInvocations++
	return result, true, nil
}

func (r *runtime) closeJIT() {
	for key, code := range r.jitCache {
		releaseNativeJIT(code)
		delete(r.jitCache, key)
	}
}
