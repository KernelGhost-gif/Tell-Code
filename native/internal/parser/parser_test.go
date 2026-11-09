package parser

import (
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
)

func TestPowerIsRightAssociative(t *testing.T) {
	expr,err:=New("2 ** 3 ** 2").Parse()
	if err!=nil { t.Fatal(err) }
	root,ok:=expr.(*ast.Binary)
	if !ok || root.Operator!="**" { t.Fatalf("unexpected root: %#v",expr) }
	if _,ok:=root.Right.(*ast.Binary); !ok { t.Fatalf("power should be right associative: %#v",root.Right) }
}

func TestMultiplicationBindsTighterThanAddition(t *testing.T) {
	expr,err:=New("2 + 3 * 4").Parse()
	if err!=nil { t.Fatal(err) }
	root:=expr.(*ast.Binary)
	if root.Operator!="+" { t.Fatalf("root op = %q",root.Operator) }
	if right,ok:=root.Right.(*ast.Binary); !ok || right.Operator!="*" { t.Fatalf("unexpected right expression: %#v",root.Right) }
}
