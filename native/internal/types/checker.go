package types

import (
	"fmt"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
)

type Type string

const (
	Any Type = "any"
	Nothing Type = "nothing"
	Boolean Type = "boolean"
	Number Type = "number"
	Text Type = "text"
	Function Type = "function"
)

type Checker struct { Symbols map[string]Type }

func NewChecker(symbols map[string]Type) *Checker {
	copyOf := make(map[string]Type, len(symbols))
	for name, typ := range symbols { copyOf[name] = typ }
	return &Checker{Symbols:copyOf}
}

func (c *Checker) Infer(expr ast.Expr) (Type, error) {
	switch node := expr.(type) {
	case *ast.Literal:
		switch node.Value.(type) {
		case nil: return Nothing, nil
		case bool: return Boolean, nil
		case string: return Text, nil
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64: return Number, nil
		default: return Any, nil
		}
	case *ast.Name:
		if typ, ok := c.Symbols[node.Identifier]; ok { return typ, nil }
		return Any, nil
	case *ast.Unary:
		right, err := c.Infer(node.Right); if err != nil { return Any, err }
		switch node.Operator {
		case "+", "-":
			if right != Number && right != Any { return Any, fmt.Errorf("operator %s requires number, got %s", node.Operator, right) }
			return Number, nil
		case "not":
			return Boolean, nil
		}
	case *ast.Binary:
		left, err := c.Infer(node.Left); if err != nil { return Any, err }
		right, err := c.Infer(node.Right); if err != nil { return Any, err }
		switch node.Operator {
		case "+": if left == Text && right == Text { return Text, nil }; fallthrough
		case "-", "*", "/", "**":
			if (left == Number || left == Any) && (right == Number || right == Any) { return Number, nil }
			return Any, fmt.Errorf("operator %s requires numeric operands, got %s and %s", node.Operator, left, right)
		case "and", "or", "==", "!=", "<", "<=", ">", ">=":
			return Boolean, nil
		}
	}
	return Any, nil
}
