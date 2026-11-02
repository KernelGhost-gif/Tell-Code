package tell

import (
	"strings"
	"testing"
)

func TestAnalyzeChecksKnownPrimitiveAnnotations(t *testing.T) {
	valid := `Let amount as number be 4 plus 5.
Set amount to amount times 2.
Let active as boolean be amount is greater than 0.`
	if err := Analyze(valid, "types.tell"); err != nil {
		t.Fatalf("valid annotations rejected: %v", err)
	}
	for _, source := range []string{
		`Let amount as number be "four".`,
		"Let amount as number be 4.\nSet amount to \"four\".",
		`Let amount as number be 2 is equal to 2.`,
	} {
		err := Analyze(source, "bad-types.tell")
		if err == nil || !strings.Contains(err.Error(), "type mismatch") {
			t.Errorf("expected compile-time annotation mismatch for %q, got %v", source, err)
		}
	}
}

func TestAnalyzeRecognizesTupleAnnotationsAndInferredTupleOperations(t *testing.T) {
	valid := `Let pair as tuple be (1, 2).
Let extended as tuple be pair plus (3,).
Let tail as tuple be extended[1:].`
	if err := Analyze(valid, "tuple-types.tell"); err != nil {
		t.Fatalf("valid tuple annotations rejected: %v", err)
	}
	if err := Analyze(`Let pair as tuple be [1, 2].`, "tuple-mismatch.tell"); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected tuple/list annotation mismatch, got %v", err)
	}
}

func TestAnalyzeLeavesDynamicExpressionsToRuntime(t *testing.T) {
	if err := Analyze(`Let result as number be dynamic_value.`, "dynamic.tell"); err != nil {
		t.Fatalf("an expression with unknown static type should be deferred: %v", err)
	}
}

func TestAnalyzeRejectsUnknownTypesAndHonorsCaseSensitiveClassNames(t *testing.T) {
	if err := Analyze(`Let value as mystery be dynamic_value.`, "unknown-type.tell"); err == nil || !strings.Contains(err.Error(), "unknown type annotation") {
		t.Fatalf("expected unknown type error, got %v", err)
	}
	program := `Create a class called Cat:
End class.
Let pet as Cat be dynamic_value.`
	if err := Analyze(program, "classes.tell"); err != nil {
		t.Fatalf("declared class annotation rejected: %v", err)
	}
	wrongCase := strings.Replace(program, "as Cat", "as cat", 1)
	if err := Analyze(wrongCase, "classes.tell"); err == nil || !strings.Contains(err.Error(), "unknown type annotation") {
		t.Fatalf("expected class-name case mismatch to be rejected, got %v", err)
	}
}

func TestCompileRunsSemanticAnalysis(t *testing.T) {
	if _, err := Compile(`Let amount as number be "wrong".`, "build-types.tell"); err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("expected compiler to reject statically mismatched annotation, got %v", err)
	}
}
