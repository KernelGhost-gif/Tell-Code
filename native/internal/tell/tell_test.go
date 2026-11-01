package tell

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
