package types

import (
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/parser"
)

func TestInferNumericExpression(t *testing.T) {
	expr,err:=parser.New("2 + 3 * 4").Parse()
	if err!=nil { t.Fatal(err) }
	got,err:=NewChecker(nil).Infer(expr)
	if err!=nil { t.Fatal(err) }
	if got!=Number { t.Fatalf("got %q, want number",got) }
}

func TestRejectsTextSubtraction(t *testing.T) {
	expr,err:=parser.New("\"left\" - \"right\"").Parse()
	if err!=nil { t.Fatal(err) }
	if _,err:=NewChecker(nil).Infer(expr); err==nil { t.Fatal("expected type mismatch") }
}
