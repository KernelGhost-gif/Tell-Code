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

func TestSingleQuotedStrings(t *testing.T) {
	cases:=[]struct{source,want string}{
		{source:"'hello world'",want:"hello world"},
		{source:"'it\\'s fine'",want:"it's fine"},
		{source:"'say \"hi\"'",want:"say \"hi\""},
	}
	for _,tc:=range cases {
		expr,err:=New(tc.source).Parse()
		if err!=nil { t.Fatalf("parse %q: %v",tc.source,err) }
		literal,ok:=expr.(*ast.Literal)
		if !ok { t.Fatalf("expected literal for %q, got %T",tc.source,expr) }
		if got,ok:=literal.Value.(string); !ok || got!=tc.want {
			t.Fatalf("parse %q = %#v, want %q",tc.source,literal.Value,tc.want)
		}
	}
}
