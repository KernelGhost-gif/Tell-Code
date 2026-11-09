package optimizer

import (
	"math"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/source"
)

type Optimizer interface { Optimize(ast.Expr) ast.Expr }
type Folder struct{}

func New() Optimizer { return Folder{} }

func (Folder) Optimize(expr ast.Expr) ast.Expr {
	switch node := expr.(type) {
	case *ast.Unary:
		right := (Folder{}).Optimize(node.Right)
		node.Right = right
		if literal, ok := right.(*ast.Literal); ok {
			switch node.Operator {
			case "-": if n, ok := numeric(literal.Value); ok { return &ast.Literal{Value:-n, SourceSpan:node.Span()} }
			case "+": if n, ok := numeric(literal.Value); ok { return &ast.Literal{Value:n, SourceSpan:node.Span()} }
			case "not": if b, ok := literal.Value.(bool); ok { return &ast.Literal{Value:!b, SourceSpan:node.Span()} }
			}
		}
	case *ast.Binary:
		node.Left = (Folder{}).Optimize(node.Left)
		node.Right = (Folder{}).Optimize(node.Right)
		left, lok := node.Left.(*ast.Literal); right, rok := node.Right.(*ast.Literal)
		if !lok || !rok { return node }
		if a, ok := numeric(left.Value); ok {
			b, ok := numeric(right.Value); if !ok { return node }
			var value float64
			switch node.Operator {
			case "+": value=a+b
			case "-": value=a-b
			case "*": value=a*b
			case "/": if b == 0 { return node }; value=a/b
			case "**": value=math.Pow(a,b)
			default: return node
			}
			if math.IsNaN(value) || math.IsInf(value, 0) { return node }
			return &ast.Literal{Value:value, SourceSpan:source.Span{Start:node.Span().Start, End:node.Span().End}}
		}
		if a, ok := left.Value.(string); ok && node.Operator == "+" {
			if b, ok := right.Value.(string); ok { return &ast.Literal{Value:a+b, SourceSpan:node.Span()} }
		}
	}
	return expr
}

func numeric(value any) (float64, bool) {
	switch n := value.(type) {
	case int: return float64(n), true
	case int64: return float64(n), true
	case float64: return n, true
	default: return 0, false
	}
}
