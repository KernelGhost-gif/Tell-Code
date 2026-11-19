package tell

import (
	"crypto/sha256"
	"encoding/binary"
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

func assignJITFunctionKey(function *BytecodeFunction, namespace string, index int) {
	if function == nil {
		return
	}
	name := function.Name
	buffer := make([]byte, len(namespace)+1+8+len(name))
	position := copy(buffer, namespace)
	buffer[position] = 0
	position++
	binary.LittleEndian.PutUint64(buffer[position:position+8], uint64(index))
	position += 8
	copy(buffer[position:], name)
	function.jitKey = sha256.Sum256(buffer)
}

func assignBytecodeJITKeys(program *BytecodeProgram, namespace string) {
	if program == nil {
		return
	}
	for index := range program.Functions {
		assignJITFunctionKey(&program.Functions[index], namespace, index)
	}
}

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
	key := function.jitKey
	if key == ([32]byte{}) {
		keyData, err := json.Marshal(function)
		if err != nil {
			return nil, false, err
		}
		key = sha256.Sum256(keyData)
	}

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

	var arguments [7]float64
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
	// The machine-code ABI takes a pointer even for a zero-argument function.
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
