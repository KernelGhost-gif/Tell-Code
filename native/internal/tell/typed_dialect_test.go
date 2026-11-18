package tell

import (
    "bytes"
    "encoding/json"
    "strings"
    "testing"
)

const typedFactorialSource = `to factorial(number: n) out number:
    if n <= 1:
        give back 1
    end if
    give back n * factorial(n - 1)
end function
let number result be factorial(5)
say result
`

func TestTypedDialectSourceExecution(t *testing.T) {
    var output bytes.Buffer
    if err := RunWithIO(typedFactorialSource, "typed-factorial.tell", strings.NewReader(""), &output); err != nil {
        t.Fatalf("typed program failed: %v", err)
    }
    if got := output.String(); got != "120\n" {
        t.Fatalf("unexpected output: %q", got)
    }
}

func TestTypedDialectRejectsRuntimeParameterMismatch(t *testing.T) {
    source := `to echo(number: value) out number:
    give back value
end function
say echo("not a number")
`
    var output bytes.Buffer
    err := RunWithIO(source, "typed-argument.tell", strings.NewReader(""), &output)
    if err == nil || !strings.Contains(err.Error(), "type mismatch") {
        t.Fatalf("expected runtime parameter type mismatch, got %v", err)
    }
}

func TestTypedDialectRejectsRuntimeReturnMismatch(t *testing.T) {
    source := `to wrong(number: value) out number:
    give back "not a number"
end function
say wrong(1)
`
    var output bytes.Buffer
    err := RunWithIO(source, "typed-return.tell", strings.NewReader(""), &output)
    if err == nil || !strings.Contains(err.Error(), "type mismatch") {
        t.Fatalf("expected runtime return type mismatch, got %v", err)
    }
}

func TestTypedDialectRejectsMissingReturn(t *testing.T) {
    source := `to incomplete(number: value) out number:
    say value
end function
say incomplete(1)
`
    var output bytes.Buffer
    err := RunWithIO(source, "typed-missing-return.tell", strings.NewReader(""), &output)
    if err == nil || !strings.Contains(err.Error(), "must return a value") {
        t.Fatalf("expected missing return error, got %v", err)
    }
}

func TestValidateTypedProgramRequiresDeclarations(t *testing.T) {
    valid := `to add(number: left, number: right) out number:
    give back left + right
end function
let number result be add(20, 22)
say result
`
    if err := ValidateTypedProgram(valid, "valid-typed.tell"); err != nil {
        t.Fatalf("valid typed source rejected: %v", err)
    }

    untypedVariable := "let count be 3\nsay count\n"
    if err := ValidateTypedProgram(untypedVariable, "untyped-variable.tell"); err == nil || !strings.Contains(err.Error(), "needs an explicit type") {
        t.Fatalf("expected untyped variable to be rejected, got %v", err)
    }

    untypedFunction := `to identity(value) out number:
    give back value
end function
`
    if err := ValidateTypedProgram(untypedFunction, "untyped-function.tell"); err == nil || !strings.Contains(err.Error(), "needs an explicit type") {
        t.Fatalf("expected untyped parameter to be rejected, got %v", err)
    }
}

func TestTypedDialectStaticCallChecks(t *testing.T) {
    source := `to add(number: left, number: right) out number:
    give back left + right
end function
let number result be add("wrong", 2)
say result
`
    if err := ValidateTypedProgram(source, "static-call-mismatch.tell"); err == nil || !strings.Contains(err.Error(), "type mismatch") {
        t.Fatalf("expected compile-time argument mismatch, got %v", err)
    }
}

func TestTypedDialectBytecodePreservesSignatures(t *testing.T) {
    compiled, err := Compile(typedFactorialSource, "typed-factorial.tell")
    if err != nil {
        t.Fatalf("typed source failed to compile: %v", err)
    }
    var output bytes.Buffer
    if err := RunBytecode(compiled, "typed-factorial.tbc", strings.NewReader(""), &output); err != nil {
        t.Fatalf("typed bytecode failed: %v", err)
    }
    if got := output.String(); got != "120\n" {
        t.Fatalf("unexpected bytecode output: %q", got)
    }

    dynamicArgument := `to echo(number: value) out number:
    give back value
end function
say echo(1)
`
    compiled, err = Compile(dynamicArgument, "typed-bytecode-mismatch.tell")
    if err != nil {
        t.Fatalf("valid typed bytecode failed to compile: %v", err)
    }
    var program BytecodeProgram
    if err := json.Unmarshal(compiled, &program); err != nil {
        t.Fatalf("could not decode compiled program: %v", err)
    }
    changed := false
    for index := range program.Instructions {
        if program.Instructions[index].Op == "PUSH" {
            program.Instructions[index].Value = "not a number"
            changed = true
            break
        }
    }
    if !changed {
        t.Fatal("compiled call had no PUSH argument instruction to mutate")
    }
    tampered, err := json.Marshal(program)
    if err != nil {
        t.Fatalf("could not encode test bytecode: %v", err)
    }
    output.Reset()
    err = RunBytecode(tampered, "typed-bytecode-mismatch.tbc", strings.NewReader(""), &output)
    if err == nil || !strings.Contains(err.Error(), "type mismatch") {
        t.Fatalf("expected bytecode parameter type mismatch, got %v", err)
    }
}
