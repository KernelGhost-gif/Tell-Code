package semantic

import (
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/parser"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/source"
)

func TestScopeResolvesLexicalParents(t *testing.T) {
	parent := NewScope(nil)
	if err := parent.Declare("outer","number",source.Span{}); err != nil { t.Fatal(err) }
	child := NewScope(parent)
	if _,ok := child.Resolve("outer"); !ok { t.Fatal("expected parent binding to resolve") }
	if err := child.Declare("inner","text",source.Span{}); err != nil { t.Fatal(err) }
	if _,ok := parent.Resolve("inner"); ok { t.Fatal("child binding leaked into parent") }
}

func TestAnalyzerRejectsUndefinedNames(t *testing.T) {
	expr,err := parser.New("absent + 1").Parse()
	if err != nil { t.Fatal(err) }
	err = NewAnalyzer().Analyze(expr,NewScope(nil))
	if err == nil { t.Fatal("expected undefined name error") }
	if _,ok := expr.(*ast.Binary); !ok { t.Fatal("parser did not return binary expression") }
}
