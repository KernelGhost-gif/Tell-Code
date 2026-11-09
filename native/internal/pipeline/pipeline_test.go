package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/vm"
)

func TestRunArithmetic(t *testing.T) {
	result,err:=Run(context.Background(),"2 + 3 * 4","test.tell",nil)
	if err!=nil { t.Fatal(err) }
	if got:=result.Value.String(); got!="14" { t.Fatalf("got %q, want 14",got) }
}

func TestRunReadsGlobal(t *testing.T) {
	result,err:=Run(context.Background(),"answer + 1","test.tell",map[string]vm.Value{"answer":vm.From(41)})
	if err!=nil { t.Fatal(err) }
	if got:=result.Value.String(); got!="42" { t.Fatalf("got %q, want 42",got) }
}

func TestRunDivisionByZero(t *testing.T) {
	_,err:=Run(context.Background(),"10 / 0","sample.tell",nil)
	if err==nil || !strings.Contains(err.Error(),"division by zero") { t.Fatalf("unexpected error: %v",err) }
}

func TestRunRejectsUnknownName(t *testing.T) {
	_,err:=Run(context.Background(),"missing + 1","sample.tell",nil)
	if err==nil || !strings.Contains(err.Error(),"NameError") { t.Fatalf("unexpected error: %v",err) }
}

func TestSyntaxDiagnosticUsesTokenCoordinates(t *testing.T) {
	_,err:=Run(context.Background(),"2 + )","loc.tell",nil)
	if err==nil { t.Fatal("expected syntax error") }
	want:=`[SyntaxError] File "loc.tell", line 1, col 5: expected expression`
	if got:=err.Error(); got!=want { t.Fatalf("got %q, want %q",got,want) }
}

func TestRunHonoursContext(t *testing.T) {
	ctx,cancel:=context.WithCancel(context.Background()); cancel()
	if _,err:=Run(ctx,"1 + 2","sample.tell",nil); err==nil { t.Fatal("expected context cancellation") }
}
