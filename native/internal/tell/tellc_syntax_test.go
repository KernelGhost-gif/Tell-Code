package tell

import (
	"bytes"
	"strings"
	"testing"
)

func TestTellCSyntax(t *testing.T) {
	source := `to factorial(number: n) out number:
    if n = 0:
        return 1
    end if
    return n * factorial(n - 1)
end function
let number result be factorial(5)
let bool correct be result = 120
say correct
for each value in [1, 2, 3]:
    if value = 2:
        continue
    end if
    say value
end for`
	var out bytes.Buffer
	if err := RunWithIO(source, "tellc.tell", strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "true\n1\n3\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
