package tell

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestSharedOperators(t *testing.T) {
	source, err := os.ReadFile("../../../TellC/tests/fixtures/operators.tell")
	if err != nil {
		t.Fatal(err)
	}
	const want = "2\n7\n5\n-6\n16\n-2\n13\ntrue\nfalse\ntrue\ntrue\nfalse\ntrue\ninvalid shift\nshift out of range\n"
	var out bytes.Buffer
	if err := RunWithIO(string(source), "operators.tell", strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != want {
		t.Fatalf("interpreter = %q, want %q", out.String(), want)
	}
	if got := runCompiled(t, string(source)); got != want {
		t.Fatalf("bytecode = %q, want %q", got, want)
	}
}

func TestInvalidBitwiseOperands(t *testing.T) {
	for _, source := range []string{"say 1 << -1", "say 1 << 64", "say 1.5 & 1", "say 1 << 53", "say ~9007199254740991"} {
		t.Run(source, func(t *testing.T) {
			if err := RunWithIO(source, "invalid.tell", strings.NewReader(""), &bytes.Buffer{}); err == nil {
				t.Fatal("expected error")
			}
			artifact, err := Compile(source, "invalid.tell")
			if err != nil {
				return
			}
			if err = RunBytecode(artifact, "invalid.tbc", strings.NewReader(""), &bytes.Buffer{}); err == nil {
				t.Fatal("expected bytecode error")
			}
		})
	}
}
