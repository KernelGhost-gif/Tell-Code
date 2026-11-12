package diagnostic

import "testing"

func TestErrorFormattingAndExitCodes(t *testing.T) {
	err := &Error{Kind:RuntimeError, Filename:"demo.tell", Line:4, Column:7, Message:"division by zero"}
	want := "[RuntimeError] File \"demo.tell\", line 4, col 7: division by zero"
	if got := err.Error(); got != want { t.Fatalf("got %q, want %q", got, want) }
	if got := err.ExitCode(); got != 70 { t.Fatalf("got exit code %d, want 70", got) }
	if got := (&Error{Kind:SyntaxError}).ExitCode(); got != 64 { t.Fatalf("syntax exit code = %d", got) }
	if got := (&Error{Kind:IOError}).ExitCode(); got != 74 { t.Fatalf("I/O exit code = %d", got) }
}
