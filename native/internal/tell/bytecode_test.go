package tell

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCompiled(t *testing.T, source string) string {
	t.Helper()
	artifact, err := Compile(source, "compiled.tell")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	var output bytes.Buffer
	if err := RunBytecode(artifact, "compiled.tbc", bytes.NewReader(nil), &output); err != nil {
		t.Fatalf("RunBytecode failed: %v", err)
	}
	return output.String()
}

func TestBytecodeCompilesAndRunsCoreProgram(t *testing.T) {
	source := `Let total be 0.
Let values be [1, 2, 3].
While total is less than 3:
    Set total to total plus 1.
End While.
If values[1] is equal to 2:
    Say "total={total}, item={values[1]}".
Otherwise:
    Say "wrong".
End If.`
	if got, want := runCompiled(t, source), "total=3, item=2\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeCompilesCSVStandardLibrary(t *testing.T) {
	source := `Use the csv library as tables.
Let rows be tables.parse("name,score\nAda,42\n").
Say rows[1][0].
Say tables.stringify(rows).`
	want := "Ada\nname,score\nAda,42\n\n"
	if got := runCompiled(t, source); got != want {
		t.Fatalf("compiled CSV output = %q, want %q", got, want)
	}
}

func TestBytecodeCheckThatAssertion(t *testing.T) {
	if got, want := runCompiled(t, "Check that 2 plus 2 is equal to 4.\nSay \"passed\"."), "passed\n"; got != want {
		t.Fatalf("asserting program output = %q, want %q", got, want)
	}
	artifact, err := Compile("Check that false.", "assertion.tell")
	if err != nil {
		t.Fatal(err)
	}
	if err := RunBytecode(artifact, "assertion.tbc", bytes.NewReader(nil), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "check failed") {
		t.Fatalf("expected a compiled assertion error, got %v", err)
	}
}

func TestBytecodeCompilesRepeatForEachBreakAndContinue(t *testing.T) {
	source := `Let total be 0.
Repeat 5 times:
    If the current repetition number is equal to 3:
        Skip to the next repetition.
    End If.
    Set total to total plus the current repetition number.
End repeat.
For each value in [1, 2, 3, 4]:
    If value is equal to 3:
        Stop this loop.
    End If.
    Set total to total plus value.
End loop.
Say total.`
	if got, want := runCompiled(t, source), "15\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeForEachIteratesUnicodeText(t *testing.T) {
	source := `For each letter in "aλ🙂":
    Say letter.
End loop.`
	if got, want := runCompiled(t, source), "a\nλ\n🙂\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeCompilesFunctionsNamedArgumentsAndRecursion(t *testing.T) {
	source := `To factorial(number):
    If number is at most 1:
        Give back 1.
    End If.
    Give back number times factorial(number minus 1).
End function.
Say factorial(number=6).`
	if got, want := runCompiled(t, source), "720\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeFunctionsCaptureLexicalEnvironment(t *testing.T) {
	source := `To make_adder(value):
    To add_to(other):
        Give back value plus other.
    End function.
    Give back add_to.
End function.
Let add_five be make_adder(5).
Say add_five(7).`
	if got, want := runCompiled(t, source), "12\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeStaticTypesRespectLocalAssignmentScope(t *testing.T) {
	source := `Let value as number be 1.
To change:
    Set value to "local".
    Say value.
End function.
change().
Say value.`
	if got, want := runCompiled(t, source), "local\n1\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeCompilesClassesInheritanceAndPropertyAccess(t *testing.T) {
	source := `Create a class called Animal:
    When creating an Animal with name:
        Remember name as this.name.
    End constructor.
    To speak:
        Say "{this.name} makes a sound.".
    End function.
End class.
Create a class called Dog based on Animal:
    To speak:
        Say "Woof, {this.name}!".
    End function.
End class.
Let pet as Animal be a new Dog with "Rex".
Tell pet to speak.
Set pet.name to "Milo".
Tell pet to speak.`
	if got, want := runCompiled(t, source), "Woof, Rex!\nWoof, Milo!\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeTryHandlerCatchesRuntimeAndRaisedErrors(t *testing.T) {
	for _, source := range []string{
		`Try:
    Let value be 1 divided by 0.
    Say "unreachable".
If something goes wrong:
    Say error_message.
End try.`,
		`Try:
    Raise an error saying "custom failure".
If something goes wrong:
    Say error_message.
End try.`,
	} {
		got := runCompiled(t, source)
		if !strings.Contains(got, "compiled.tbc:") || (!strings.Contains(got, "division by zero") && !strings.Contains(got, "custom failure")) {
			t.Fatalf("handler did not receive the Tell error message: %q", got)
		}
		if strings.Contains(got, "unreachable") {
			t.Fatalf("try body continued after an error: %q", got)
		}
	}
}

func TestBytecodeEmbedsCachesAndScopesLocalModules(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "math_helpers.tell")
	if err := os.WriteFile(modulePath, []byte(`Let multiplier be 3.
Let _private be "not exported".
To triple(value):
    Give back value times multiplier.
End function.
Say "module loaded".`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `Use the module "math_helpers.tell" as helpers.
Use the module "math_helpers.tell" as helpers_again.
Let multiplier be 100.
Say helpers.triple(7).
Say helpers_again.triple(3).`
	artifact, err := Compile(source, filepath.Join(root, "main.tell"))
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunBytecode(artifact, filepath.Join(root, "main.tbc"), bytes.NewReader(nil), &output); err != nil {
		t.Fatalf("RunBytecode failed after deleting source module: %v", err)
	}
	if got, want := output.String(), "module loaded\n21\n9\n"; got != want {
		t.Fatalf("compiled module output = %q, want %q", got, want)
	}
}

func TestBytecodeEmbedsTransitiveModulesAndRejectsCycles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inner.tell"), []byte("Let factor be 4."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outer.tell"), []byte(`Use the module "inner.tell" as inner.
To scale(value):
    Give back value times inner.factor.
End function.`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `Use the module "outer.tell" as outer.
Say outer.scale(6).`
	artifact, err := Compile(source, filepath.Join(root, "main.tell"))
	if err != nil {
		t.Fatalf("Compile with transitive module failed: %v", err)
	}
	var output bytes.Buffer
	if err := RunBytecode(artifact, filepath.Join(root, "main.tbc"), bytes.NewReader(nil), &output); err != nil {
		t.Fatalf("RunBytecode failed: %v", err)
	}
	if got, want := output.String(), "24\n"; got != want {
		t.Fatalf("transitive module output = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(root, "cycle_a.tell"), []byte(`Use the module "cycle_b.tell" as other.`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cycle_b.tell"), []byte(`Use the module "cycle_a.tell" as other.`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Compile(`Use the module "cycle_a.tell" as cycle.`, filepath.Join(root, "cycle_main.tell"))
	if err == nil || !strings.Contains(err.Error(), "circular module import") {
		t.Fatalf("expected a circular-module error, got %v", err)
	}
}

func TestBytecodeSupportsBuiltinsAndDictionaryIndex(t *testing.T) {
	source := `Let values be {"answer": 40}.
Say values["answer"] plus 2.
Say len([1, 2, 3]).`
	if got, want := runCompiled(t, source), "42\n3\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
}

func TestBytecodeFoldsConstantExpressions(t *testing.T) {
	artifact, err := Compile("Say 2 plus 3 times 4.", "constant-fold.tell")
	if err != nil {
		t.Fatal(err)
	}
	var program BytecodeProgram
	if err := json.Unmarshal(artifact, &program); err != nil {
		t.Fatal(err)
	}
	if len(program.Instructions) != 3 || program.Instructions[0].Op != "PUSH" || program.Instructions[0].Value != float64(14) {
		t.Fatalf("constant arithmetic was not folded: %#v", program.Instructions)
	}
	var output bytes.Buffer
	if err := RunBytecode(artifact, "constant-fold.tbc", bytes.NewReader(nil), &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "14\n" {
		t.Fatalf("folded program output = %q", output.String())
	}
}

func TestBytecodeConstantFoldingPreservesRuntimeErrorsAndShortCircuit(t *testing.T) {
	artifact, err := Compile("Say 1 divided by 0.", "division.tell")
	if err != nil {
		t.Fatal(err)
	}
	if err := RunBytecode(artifact, "division.tbc", bytes.NewReader(nil), &bytes.Buffer{}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "zero") {
		t.Fatalf("division error should remain a runtime error, got %v", err)
	}
	if got, want := runCompiled(t, "Say false and missing_value."), "false\n"; got != want {
		t.Fatalf("short-circuit output = %q, want %q", got, want)
	}
}

func TestBytecodePreservesRuntimeTypeAndConstantRules(t *testing.T) {
	if got, want := runCompiled(t, "Let constant answer as number be 42.\nSay answer."), "42\n"; got != want {
		t.Fatalf("compiled output = %q, want %q", got, want)
	}
	artifact, err := Compile("Let constant answer as number be 42.\nSet answer to 43.", "constant.tell")
	if err != nil {
		t.Fatal(err)
	}
	err = RunBytecode(artifact, "constant.tbc", bytes.NewReader(nil), &bytes.Buffer{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "constant") {
		t.Fatalf("expected constant reassignment failure, got %v", err)
	}
}

func TestBytecodeRejectsUnsupportedConstructs(t *testing.T) {
	_, err := Compile("Start a parallel task called worker.", "unsupported.tell")
	if err == nil || !strings.Contains(err.Error(), "unsupported statement") {
		t.Fatalf("expected a clear unsupported-statement error, got %v", err)
	}
}

func TestBytecodeRejectsMalformedInstructionsWithoutPanic(t *testing.T) {
	for _, program := range []BytecodeProgram{
		{Format: bytecodeFormat, Instructions: []BytecodeInstruction{{Op: "LIST", Count: -1}}},
		{Format: bytecodeFormat, Instructions: []BytecodeInstruction{{Op: "JUMP", Jump: 100}}},
		{Format: bytecodeFormat, Instructions: []BytecodeInstruction{{Op: "POP"}, {Op: "HALT"}}},
		{Format: bytecodeFormat, Instructions: []BytecodeInstruction{{Op: "PUSH"}, {Op: "JUMP_IF_FALSE", Jump: 3}, {Op: "PUSH"}, {Op: "POP"}, {Op: "HALT"}}},
	} {
		data, err := json.Marshal(program)
		if err != nil {
			t.Fatal(err)
		}
		if err := RunBytecode(data, "broken.tbc", bytes.NewReader(nil), &bytes.Buffer{}); err == nil {
			t.Fatal("expected malformed bytecode error")
		}
	}
}
