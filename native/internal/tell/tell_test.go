package tell

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedConformanceCorpus(t *testing.T) {
	data, err := os.ReadFile("../../../conformance/core.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Source   string `json:"source"`
		Output   string `json:"output"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			var output bytes.Buffer
			filename := testCase.Name + ".tell"
			if testCase.Filename != "" {
				filename = filepath.Join("../../../conformance", filepath.FromSlash(testCase.Filename))
			}
			err := RunWithIO(testCase.Source, filename, bytes.NewReader(nil), &output)
			if testCase.Error != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(testCase.Error)) {
					t.Fatalf("expected error containing %q, got %v", testCase.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(&output)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != testCase.Output {
				t.Fatalf("output mismatch: got %q, want %q", got, testCase.Output)
			}
		})
	}
}

func TestOptionalPythonModuleInterop(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python is optional for the native runtime")
	}
	t.Setenv("TELL_PYTHON_EXECUTABLE", python)
	var output bytes.Buffer
	source := `Use the Python library called statistics as stats.
Say stats.mean([1, 2, 3]).
Say stats.fmean(data=[4, 8]).`
	if err := RunWithIO(source, "python-interop.tell", bytes.NewReader(nil), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "2\n6\n"; got != want {
		t.Fatalf("Python interop output = %q, want %q", got, want)
	}
}
 
func TestOptionalJavaScriptModuleInterop(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is optional for the native runtime")
	}
	t.Setenv("TELL_NODE_EXECUTABLE", node)
	var output bytes.Buffer
	source := `Use the JavaScript library called node:path as paths.
Say paths.basename("/projects/tell/main.js").
Say paths.extname("/projects/tell/main.js").`
	if err := RunWithIO(source, "javascript-interop.tell", bytes.NewReader(nil), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "main.js\n.js\n"; got != want {
		t.Fatalf("JavaScript interop output = %q, want %q", got, want)
	}
}

func TestParserBuildsExpressionASTsForCommonStatements(t *testing.T) {
	nodes, err := parse("Let answer be 1 plus 2.\nSet answer to answer plus 1.\nSay answer.", "ast.tell")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].simple == nil || nodes[0].simple.kind != "let" || nodes[0].simple.expression == nil {
		t.Fatalf("expected a parsed let statement with an expression AST, got %#v", nodes)
	}
	if nodes[1].simple == nil || nodes[1].simple.kind != "set" || nodes[2].simple == nil || nodes[2].simple.kind != "print" {
		t.Fatalf("common statements were not represented as typed nodes: %#v", nodes)
	}
	memberAssignment, err := parse("Set box.name to \"Ada\".", "ast.tell")
	if err != nil || memberAssignment[0].simple == nil || memberAssignment[0].simple.kind != "set_member" || memberAssignment[0].simple.target == nil {
		t.Fatalf("member assignment did not produce a target AST: %#v, %v", memberAssignment, err)
	}
	constructed, err := parse("Let box be a new Box.", "ast.tell")
	if err != nil || constructed[0].simple == nil || constructed[0].simple.expression == nil {
		t.Fatalf("natural object construction did not produce a parsed statement: %#v, %v", constructed, err)
	}
	told, err := parse("Tell box to open with \"wide\".", "ast.tell")
	if err != nil || told[0].simple == nil || told[0].simple.kind != "expression" {
		t.Fatalf("Tell method sentence did not produce an expression AST: %#v, %v", told, err)
	}
}

func TestMultipleAssignmentValidatesBeforeMutating(t *testing.T) {
	runtime := newRuntime("test.tell", nil, &bytes.Buffer{}, DefaultExecutionLimits())
	runtime.global.set("first", float64(1))
	runtime.global.set("second", "before")
	runtime.global.types["second"] = "text"
	if err := runtime.statement("Set first, second to [2, 3]", runtime.global); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected a type mismatch, got %v", err)
	}
	if got, _ := runtime.global.get("first"); got != float64(1) {
		t.Fatalf("first target changed before validation completed: %v", got)
	}
	if got, _ := runtime.global.get("second"); got != "before" {
		t.Fatalf("second target changed after validation failed: %v", got)
	}
}

func TestModuleExampleRuns(t *testing.T) {
	filename := filepath.Join("../../../examples", "modules.tell")
	source, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := RunWithIO(string(source), filename, bytes.NewReader(nil), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "42\n"; got != want {
		t.Fatalf("module example output = %q, want %q", got, want)
	}
}

func TestCheckThatAssertionPassesAndFails(t *testing.T) {
	if _, err := runTest(t, "Check that 2 plus 2 is equal to 4.", ""); err != nil {
		t.Fatalf("true assertion failed: %v", err)
	}
	if _, err := runTest(t, "Check that 2 plus 2 is equal to 5.", ""); err == nil || !strings.Contains(err.Error(), "check failed") {
		t.Fatalf("expected a failed assertion, got %v", err)
	}
}

func FuzzExpressionParserDoesNotPanic(f *testing.F) {
	for _, seed := range []string{
		`2 plus 3 times 4`,
		`[1, {"key": values[1:]}]`,
		`"is equal to plus"`,
		`1e+`,
		`((((`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		tokens, err := lex(source)
		if err != nil {
			return
		}
		parser := &expressionParser{tokens: tokens}
		_, _ = parser.parse()
	})
}

func TestCircularModuleImportIsRejected(t *testing.T) {
	err := RunWithIO(`Use the module "cycle_a.tell" as a.`, "../../../conformance/modules/entry.tell", bytes.NewReader(nil), &bytes.Buffer{})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "circular module import") {
		t.Fatalf("expected circular module import error, got %v", err)
	}
}

func TestSessionKeepsValuesBetweenExecutions(t *testing.T) {
	var output bytes.Buffer
	session := NewSession(strings.NewReader(""), &output)
	if err := session.Exec(`Let greeting be "hello".`, "first.tell"); err != nil {
		t.Fatal(err)
	}
	if err := session.Exec(`Say greeting.`, "second.tell"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "hello\n"; got != want {
		t.Fatalf("session output = %q, want %q", got, want)
	}
}

func TestSessionEchoesExpressionResultsWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	session := NewSession(strings.NewReader(""), &output)
	session.SetEchoExpressions(true)
	if err := session.Exec("2 plus 2", "repl.tell"); err != nil {
		t.Fatal(err)
	}
	if err := session.Exec("_ plus 1", "repl.tell"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "4\n5\n"; got != want {
		t.Fatalf("REPL expression output = %q, want %q", got, want)
	}
}

func TestFormatterNormalizesIndentationAndPreservesProgram(t *testing.T) {
	source := "# top\nIf true:\nSay \"keep  \".\n# inside\n Otherwise:\n   Say \"skip\". # trailing\nEnd if.\n"
	want := "# top\nIf true:\n    Say \"keep  \".\n    # inside\nOtherwise:\n    Say \"skip\". # trailing\nEnd if.\n"
	formatted, err := Format(source, "format.tell")
	if err != nil {
		t.Fatal(err)
	}
	if formatted != want {
		t.Fatalf("formatted source = %q, want %q", formatted, want)
	}
	formattedAgain, err := Format(formatted, "format.tell")
	if err != nil || formattedAgain != formatted {
		t.Fatalf("formatter is not idempotent: output=%q err=%v", formattedAgain, err)
	}
	before, _ := runTest(t, source, "")
	after, _ := runTest(t, formatted, "")
	if before != after {
		t.Fatalf("formatter changed program output: before=%q after=%q", before, after)
	}
}

func TestRecursionLimitReturnsRuntimeError(t *testing.T) {
	source := `To recurse with value:
    Give back recurse(value plus 1).
End function.
Say recurse(0).`
	_, err := runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), "maximum function call depth exceeded") {
		t.Fatalf("expected recursion limit error, got %v", err)
	}
}

func TestExecutionLimitsCanBeConfigured(t *testing.T) {
	var output bytes.Buffer
	limits := ExecutionLimits{MaxFunctionCallDepth: 3, MaxLoopIterations: 2}
	session := NewSessionWithLimits(strings.NewReader(""), &output, limits)
	err := session.Exec("Repeat 3 times:\n    Say 1.\nEnd repeat.", "limit.tell")
	if err == nil || !strings.Contains(err.Error(), "repeat count must be between 0 and 2") {
		t.Fatalf("expected configured repeat limit error, got %v", err)
	}
	source := "To recurse with value:\n    Give back recurse(value plus 1).\nEnd function.\nSay recurse(0)."
	err = session.Exec(source, "limit.tell")
	if err == nil || !strings.Contains(err.Error(), "maximum function call depth exceeded") {
		t.Fatalf("expected configured recursion limit error, got %v", err)
	}
	var exactOutput bytes.Buffer
	exactLimit := NewSessionWithLimits(strings.NewReader(""), &exactOutput, ExecutionLimits{MaxFunctionCallDepth: 3, MaxLoopIterations: 2})
	err = exactLimit.Exec("Let count be 0.\nWhile count is less than 2:\n    Set count to count plus 1.\nEnd while.", "exact-limit.tell")
	if err != nil {
		t.Fatalf("loop completing at its limit must succeed, got %v", err)
	}
	err = exactLimit.Exec("For each item in [1, 2, 3]:\n    Say item.\nEnd loop.", "for-limit.tell")
	if err == nil || !strings.Contains(err.Error(), "for-each loop exceeded the configured limit of 2") {
		t.Fatalf("expected configured for-each limit error, got %v", err)
	}
}

func TestClassRuntimeStorageIsNotExposed(t *testing.T) {
	source := `Create a class called Box:
    When creating a Box with value:
        Remember value as this.value.
    End constructor.
End class.
Let box be a new Box with 3.
Say box.fields.`
	_, err := runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), `Box has no member "fields"`) {
		t.Fatalf("expected internal field access to fail, got %v", err)
	}
}

func runTest(t *testing.T, source, input string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	err := RunWithIO(source, "test.tell", strings.NewReader(input), &output)
	return output.String(), err
}

func TestVariablesExpressionsAndInterpolation(t *testing.T) {
	out, err := runTest(t, `Let name be "Ada".
Let score be 20.
Set score to score plus 2.
Say "{name} has {score} points.".`, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Ada has 22 points.\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestNamedArgumentValidation(t *testing.T) {
	source := "To add with left and right:\n    Give back left plus right.\nEnd function.\nSay add(1, left=2)."
	_, err := runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), "multiple values") {
		t.Fatalf("expected duplicate argument error, got %v", err)
	}
	source = "To add with left and right:\n    Give back left plus right.\nEnd function.\nSay add(other=2)."
	_, err = runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), "no parameter named") {
		t.Fatalf("expected unknown parameter error, got %v", err)
	}
}

func TestRuntimeErrorsIncludeSourceColumns(t *testing.T) {
	source := "If true:\n    Say 1 divided by 0.\nEnd if."
	_, err := runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), "test.tell:2:11:") {
		t.Fatalf("expected source file, line, and column in error, got %v", err)
	}
}

func TestExpressionLexerTracksUnicodeColumns(t *testing.T) {
	tokens, err := lex("café + 2")
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].kind != "ident" || tokens[0].text != "café" || tokens[0].column != 1 {
		t.Fatalf("unexpected unicode identifier token: %#v", tokens[0])
	}
	_, err = lex("é + @")
	if err == nil || !strings.Contains(err.Error(), "column 5") {
		t.Fatalf("expected location at Unicode-aware column 5, got %v", err)
	}
}

func TestConditionsAndLoops(t *testing.T) {
	source := `Let count be 0.
If count is equal to 0:
    Say "starting".
Otherwise:
    Say "wrong".
End if.
Repeat 3 times:
    Say "round {the current repetition number}".
End repeat.
While count is less than 2:
    Set count to count plus 1.
End while.`
	out, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "starting\nround 1\nround 2\nround 3\n"
	if out != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", out, want)
	}
}

func TestFunctionsAndRecursion(t *testing.T) {
	source := `To factorial with number:
    If number is at most 1:
        Give back 1.
    End if.
    Give back number times factorial(number minus 1).
End function.
Say "Result: {the result of factorial with 6}.".`
	out, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Result: 720.\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestInputAndTry(t *testing.T) {
	source := `Ask "Name? " and save the answer as name.
Try:
    Let broken be 10 divided by 0.
If something goes wrong:
    Say "Hello, {name}.".
End try.`
	out, err := runTest(t, source, "Ada\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Name? Hello, Ada.\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestCollectionsAndMathLibrary(t *testing.T) {
	source := `Let values be [2, 4, 6].
Add 8 to values.
Set the item at position 0 in values to 10.
Use the math library as maths.
Say "{the length of values} values; root is {maths.sqrt(81)}.".`
	out, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "4 values; root is 9.\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestJSONAndStatisticsLibraries(t *testing.T) {
	source := `Use the json library.
Use the statistics library as stats.
Let record be json.loads("{\"answer\": 42}").
Say record["answer"].
Say stats.mean([2, 4, 6]).`
	out, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "42\n4\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestFileReadWrite(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	out, err := runTest(t, `Write "native runtime" to the file "note.txt".
Read the file "note.txt" into contents.
Say contents.`, "")
	if err != nil {
		t.Fatal(err)
	}
	if out != "native runtime\n" {
		t.Fatalf("unexpected output: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "native runtime" {
		t.Fatalf("unexpected file contents: %q", data)
	}
}

func TestParserRejectsUnclosedBlock(t *testing.T) {
	if err := Check("If true:\n    Say 1.\n", "bad.tell"); err == nil {
		t.Fatal("expected parser error")
	}
}

func TestDivisionDiagnosticPointsAtOperator(t *testing.T) {
	_, err := runTest(t, "Say 1 divided by 0.", "")
	if err == nil {
		t.Fatal("expected division-by-zero error")
	}
	if !strings.Contains(err.Error(), "test.tell:1:7: division by zero") {
		t.Fatalf("expected operator source coordinate, got %v", err)
	}
}

func TestNothingMemberDiagnosticPointsAtAccess(t *testing.T) {
	source := "Let value be nothing.\nSay value.name."
	_, err := runTest(t, source, "")
	if err == nil {
		t.Fatal("expected null member access error")
	}
	if !strings.Contains(err.Error(), "test.tell:2:10:") {
		t.Fatalf("expected member access source coordinate, got %v", err)
	}
}

func TestControlFlowExpressionsAreParsedOnceAndKeepSourceSpans(t *testing.T) {
	source := "If 1 divided by 0:\n    Say \"unreachable\".\nEnd if."
	nodes, err := parse(source, "control-flow.tell")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].conditionExpr == nil {
		t.Fatalf("control-flow expression was not cached: %#v", nodes)
	}
	binary, ok := nodes[0].conditionExpr.(binaryExpression)
	if !ok || binary.column != 6 {
		t.Fatalf("condition AST operator span = %#v, want column 6", nodes[0].conditionExpr)
	}
	_, err = runTest(t, source, "")
	if err == nil || !strings.Contains(err.Error(), "test.tell:1:6: division by zero") {
		t.Fatalf("expected condition operator source location, got %v", err)
	}
}



func TestLocalModuleImportsUseTypedASTNodes(t *testing.T) {
	nodes, err := parse("Use the module \"math_helpers.tell\" as helpers.", "main.tell")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].simple == nil || nodes[0].simple.kind != "import_module" {
		t.Fatalf("local module import was not parsed as a typed node: %#v", nodes)
	}
	if len(nodes[0].simple.names) != 1 || nodes[0].simple.names[0] != "helpers" {
		t.Fatalf("module alias = %#v, want helpers", nodes[0].simple.names)
	}
	if _, ok := nodes[0].simple.expression.(literalExpression); !ok {
		t.Fatalf("module path is not a parsed string literal: %#v", nodes[0].simple.expression)
	}
}

func TestIndexedAssignmentsUseTypedASTNodesAndExecute(t *testing.T) {
	source := "Let values be [1, 2].\nSet values[1] to 9.\nSet the item at position 0 in values to 7.\nLet mapping be {\"name\": \"old\"}.\nSet mapping[\"name\"] to \"new\".\nSet the key \"name\" in mapping to \"updated\".\nSay values.\nSay mapping[\"name\"]."
	nodes, err := parse(source, "indexed-assignment.tell")
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{1, 2, 4, 5} {
		if nodes[index].simple == nil || nodes[index].simple.kind != "set_index" {
			t.Fatalf("statement %d was not parsed as set_index: %#v", index, nodes[index])
		}
	}
	output, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output, "[7, 9]\nupdated\n"; got != want {
		t.Fatalf("indexed assignment output = %q, want %q", got, want)
	}
}

func TestCallAndRunStatementsUseTypedASTNodesAndExecute(t *testing.T) {
	source := "To combine with left and right:\n    Say left plus right.\nEnd function.\nCall combine with 2 and 3.\nRun combine with 4 and 1."
	nodes, err := parse(source, "call-statements.tell")
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{1, 2} {
		if nodes[index].simple == nil || nodes[index].simple.kind != "call" {
			t.Fatalf("call statement %d was not parsed into typed call node: %#v", index, nodes[index])
		}
		if _, ok := nodes[index].simple.expression.(callExpression); !ok {
			t.Fatalf("call statement expression = %T, want callExpression", nodes[index].simple.expression)
		}
	}
	output, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output, "5\n5\n"; got != want {
		t.Fatalf("Call/Run output = %q, want %q", got, want)
	}
}

func TestRememberFieldStatementsUseTypedASTNodesAndExecute(t *testing.T) {
	nodes, err := parse("Remember value as this.name.", "remember.tell")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].simple == nil || nodes[0].simple.kind != "remember_field" {
		t.Fatalf("Remember was not parsed into a typed node: %#v", nodes)
	}
	if nodes[0].simple.names[0] != "name" {
		t.Fatalf("Remember field = %#v, want name", nodes[0].simple.names)
	}
	source := `Create a class called Counter:
    When creating a Counter with initial:
        Remember initial as this.value.
    End constructor.
    To increment:
        Remember this.value plus 1 as this.value.
    End function.
End class.
Let counter be a new Counter with 4.
Tell counter to increment.
Say counter.value.`
	output, err := runTest(t, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := output, "5\n"; got != want {
		t.Fatalf("Remember execution output = %q, want %q", got, want)
	}
}

func TestInteropImportsUseTypedASTNodes(t *testing.T) {
	cases := []struct{ source, kind, alias string }{
		{"Use the Python library called statistics as stats.", "import_python", "stats"},
		{"Use the JavaScript library called node:path as paths.", "import_javascript", "paths"},
		{"Use the math library as maths.", "import_library", "maths"},
	}
	for _, testCase := range cases {
		nodes, err := parse(testCase.source, "imports.tell")
		if err != nil {
			t.Fatalf("parse %q: %v", testCase.source, err)
		}
		if len(nodes) != 1 || nodes[0].simple == nil || nodes[0].simple.kind != testCase.kind {
			t.Fatalf("import %q was not parsed into %s node: %#v", testCase.source, testCase.kind, nodes)
		}
		if len(nodes[0].simple.names) != 1 || nodes[0].simple.names[0] != testCase.alias {
			t.Fatalf("import alias = %#v, want %q", nodes[0].simple.names, testCase.alias)
		}
	}
}

func TestCollectionMutationStatementsUseTypedASTNodes(t *testing.T) {
	nodes, err := parse("Let items be [1].\nAdd 2 to items.\nRemove 1 from items.", "mutations.tell")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d statements, want 3", len(nodes))
	}
	if nodes[1].simple == nil || nodes[1].simple.kind != "add" {
		t.Fatalf("Add was not parsed as a typed node: %#v", nodes[1])
	}
	if nodes[2].simple == nil || nodes[2].simple.kind != "remove" {
		t.Fatalf("Remove was not parsed as a typed node: %#v", nodes[2])
	}
}

func TestFileAndConsoleStatementsUseTypedASTNodes(t *testing.T) {
	source := "Ask \"Name? \" and save the answer as name.\nRead the file \"note.txt\" into contents.\nWrite name to the file \"copy.txt\".\nAppend \"!\" to the file \"copy.txt\"."
	nodes, err := parse(source, "typed-io.tell")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ask", "read_file", "write_file", "append_file"}
	if len(nodes) != len(want) {
		t.Fatalf("got %d I/O statement nodes, want %d", len(nodes), len(want))
	}
	for index, kind := range want {
		if nodes[index].simple == nil || nodes[index].simple.kind != kind {
			t.Fatalf("node %d = %#v, want typed kind %q", index, nodes[index], kind)
		}
	}

	var output bytes.Buffer
	if err := RunWithIO("Ask \"Name? \" and save the answer as name.\nSay name.", "ask.tell", strings.NewReader("Ada\n"), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "Name? Ada\n"; got != want {
		t.Fatalf("Ask output = %q, want %q", got, want)
	}
}
