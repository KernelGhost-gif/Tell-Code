package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/diagnostic"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

func TestDiagnosticForLegacyRuntimeLocation(t *testing.T) {
	err := errors.New("sample.tell:3:7: division by zero")
	got := diagnosticFor(err, "fallback.tell", diagnostic.RuntimeError)
	want := "[RuntimeError] File \"sample.tell\", line 3, col 7: division by zero"
	if got.Error() != want {
		t.Fatalf("got %q, want %q", got.Error(), want)
	}
	if got.ExitCode() != 70 {
		t.Fatalf("exit code = %d, want 70", got.ExitCode())
	}
}

func TestDiagnosticForSyntaxTokenLocation(t *testing.T) {
	err := errors.New("expected a value, found \")\" at line 2, column 9")
	got := diagnosticFor(err, "bad.tell", diagnostic.SyntaxError)
	want := "[SyntaxError] File \"bad.tell\", line 2, col 9: expected a value, found \")\""
	if got.Error() != want {
		t.Fatalf("got %q, want %q", got.Error(), want)
	}
	if got.ExitCode() != 64 {
		t.Fatalf("exit code = %d, want 64", got.ExitCode())
	}
}

func TestDiagnosticClassifiesNamesAndTypes(t *testing.T) {
	name := diagnosticFor(errors.New("I don't know what missing means"), "x.tell", diagnostic.RuntimeError)
	if name.Kind != diagnostic.NameError || name.ExitCode() != 70 {
		t.Fatalf("unexpected name diagnostic: %#v", name)
	}
	typ := diagnosticFor(errors.New("type mismatch for age"), "x.tell", diagnostic.RuntimeError)
	if typ.Kind != diagnostic.TypeError || typ.ExitCode() != 70 {
		t.Fatalf("unexpected type diagnostic: %#v", typ)
	}
}

func TestDiagnosticForHonorsExplicitSyntaxKind(t *testing.T) {
	err := &tell.Error{Kind: "SyntaxError", Message: `expected a value, found ")" at line 1, column 12`}
	got := diagnosticFor(err, "syntax-error.tell", diagnostic.RuntimeError)
	want := `[SyntaxError] File "syntax-error.tell", line 1, col 12: expected a value, found ")"`
	if got.Error() != want {
		t.Fatalf("diagnostic = %q, want %q", got.Error(), want)
	}
	if got.ExitCode() != 64 {
		t.Fatalf("exit code = %d, want 64", got.ExitCode())
	}
}

func TestReportDiagnosticWritesOnlyFormattedDiagnostic(t *testing.T) {
	var output bytes.Buffer
	code := reportDiagnostic(&output, errors.New("division by zero"), "x.tell", diagnostic.RuntimeError)
	if code != 70 {
		t.Fatalf("exit code = %d, want 70", code)
	}
	want := "[RuntimeError] File \"x.tell\", line 1, col 1: division by zero\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}


func TestDiagnosticClassifiesNumericOperatorTypeErrors(t *testing.T) {
	got := diagnosticFor(errors.New("operator - requires numeric operands, got text and number"), "types.tell", diagnostic.RuntimeError)
	if got.Kind != diagnostic.TypeError {
		t.Fatalf("expected TypeError, got %s", got.Kind)
	}
}
