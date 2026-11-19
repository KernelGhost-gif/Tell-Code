package tell

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

const jitNumericSource = `to weighted(number: left, number: right) out number:
    give back left * 3 + right * 2
end function
`

func compiledJITFunction(t *testing.T) BytecodeFunction {
	t.Helper()
	artifact, err := Compile(jitNumericSource, "jit-numeric.tell")
	if err != nil {
		t.Fatalf("compile typed numeric function: %v", err)
	}
	var program BytecodeProgram
	if err := json.Unmarshal(artifact, &program); err != nil {
		t.Fatalf("decode compiled program: %v", err)
	}
	for _, function := range program.Functions {
		if function.Name == "weighted" {
			return function
		}
	}
	t.Fatal("compiled bytecode did not contain weighted")
	return BytecodeFunction{}
}

func TestNativeJITMatchesInterpreterForTypedNumericFunction(t *testing.T) {
	if !NativeJITAvailable() {
		t.Skip("native JIT backend is available only on linux/amd64")
	}
	function := compiledJITFunction(t)
	for _, engine := range []string{"interp", "jit"} {
		t.Run(engine, func(t *testing.T) {
			r := newRuntime("jit-numeric.tell", bytes.NewReader(nil), io.Discard, DefaultExecutionLimits())
			r.executionEngine = engine
			result, err := callBytecodeFunction(r, function, r.global, []BytecodeFunction{function}, nil, []any{4.0, 6.0}, nil, nil)
			if err != nil {
				t.Fatalf("%s execution failed: %v", engine, err)
			}
			number, err := numberValue(result)
			if err != nil {
				t.Fatalf("result is not numeric: %v", err)
			}
			if number != 24 {
				t.Fatalf("unexpected result: got %v, want 24", number)
			}
			if engine == "jit" {
				if r.jitStats.JITCompiledFunctions != 1 || r.jitStats.JITInvocations != 1 {
					t.Fatalf("native JIT was not used: %+v", r.jitStats)
				}
			}
			r.closeJIT()
		})
	}
}

func TestAutoEngineCompilesAfterHotThreshold(t *testing.T) {
	if !NativeJITAvailable() {
		t.Skip("native JIT backend is available only on linux/amd64")
	}
	function := compiledJITFunction(t)
	r := newRuntime("jit-hot.tell", bytes.NewReader(nil), io.Discard, DefaultExecutionLimits())
	r.executionEngine = "auto"
	r.jitHotThreshold = 3

	for index := 0; index < 3; index++ {
		result, err := callBytecodeFunction(r, function, r.global, []BytecodeFunction{function}, nil, []any{2.0, 8.0}, nil, nil)
		if err != nil {
			t.Fatalf("call %d failed: %v", index+1, err)
		}
		number, _ := numberValue(result)
		if number != 22 {
			t.Fatalf("call %d returned %v, want 22", index+1, number)
		}
		if index < 2 && r.jitStats.JITInvocations != 0 {
			t.Fatalf("JIT ran before hot threshold: %+v", r.jitStats)
		}
	}
	if r.jitStats.JITCompiledFunctions != 1 || r.jitStats.JITInvocations != 1 || r.jitStats.InterpretedCalls != 2 {
		t.Fatalf("unexpected tiering stats: %+v", r.jitStats)
	}
	r.closeJIT()
}

func TestNativeJITFallsBackForControlFlowFunctions(t *testing.T) {
	if !NativeJITAvailable() {
		t.Skip("native JIT backend is available only on linux/amd64")
	}
	source := `to absolute(number: value) out number:
    if value < 0:
        give back 0 - value
    end if
    give back value
end function
`
	artifact, err := Compile(source, "jit-fallback.tell")
	if err != nil {
		t.Fatalf("compile fallback function: %v", err)
	}
	var program BytecodeProgram
	if err := json.Unmarshal(artifact, &program); err != nil {
		t.Fatal(err)
	}
	var function BytecodeFunction
	found := false
	for _, candidate := range program.Functions {
		if candidate.Name == "absolute" {
			function, found = candidate, true
			break
		}
	}
	if !found {
		t.Fatal("compiled bytecode did not contain absolute")
	}
	r := newRuntime("jit-fallback.tell", bytes.NewReader(nil), io.Discard, DefaultExecutionLimits())
	r.executionEngine = "jit"
	result, err := callBytecodeFunction(r, function, r.global, program.Functions, program.Classes, []any{-9.0}, nil, nil)
	if err != nil {
		t.Fatalf("interpreter fallback failed: %v", err)
	}
	number, _ := numberValue(result)
	if number != 9 {
		t.Fatalf("unexpected fallback result: got %v, want 9", number)
	}
	if r.jitStats.JITCompiledFunctions != 0 || r.jitStats.InterpretedCalls != 1 {
		t.Fatalf("unsupported function did not fall back: %+v", r.jitStats)
	}
	r.closeJIT()
}

func TestRunBytecodeWithEngineReportsJITUse(t *testing.T) {
	if !NativeJITAvailable() {
		t.Skip("native JIT backend is available only on linux/amd64")
	}
	source := jitNumericSource + "say weighted(4, 6)\n"
	artifact, err := Compile(source, "jit-runner.tell")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	stats, err := RunBytecodeWithEngine(artifact, "jit-runner.tbc", bytes.NewReader(nil), &output, DefaultExecutionLimits(), "jit", 24)
	if err != nil {
		t.Fatalf("run bytecode with JIT: %v", err)
	}
	if output.String() != "24\n" {
		t.Fatalf("unexpected output: %q", output.String())
	}
	if stats.JITCompiledFunctions != 1 || stats.JITInvocations != 1 {
		t.Fatalf("expected one compiled function and one native invocation, got %+v", stats)
	}
}

func BenchmarkTypedNumericFunctionEngines(b *testing.B) {
	artifact, err := Compile(jitNumericSource, "jit-benchmark.tell")
	if err != nil {
		b.Fatal(err)
	}
	var program BytecodeProgram
	if err := json.Unmarshal(artifact, &program); err != nil {
		b.Fatal(err)
	}
	var function BytecodeFunction
	for _, candidate := range program.Functions {
		if candidate.Name == "weighted" {
			function = candidate
			break
		}
	}
	for _, engine := range []string{"interp", "jit", "auto"} {
		b.Run(engine, func(b *testing.B) {
			r := newRuntime("jit-benchmark.tell", bytes.NewReader(nil), io.Discard, DefaultExecutionLimits())
			r.executionEngine = engine
			r.jitHotThreshold = 24
			defer r.closeJIT()
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if _, err := callBytecodeFunction(r, function, r.global, program.Functions, program.Classes, []any{4.0, 6.0}, nil, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
