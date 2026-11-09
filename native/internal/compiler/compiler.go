package compiler

import (
	"context"
	"fmt"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/bytecode"
)

type Compiler interface { Compile(context.Context, ast.Expr) (*bytecode.Chunk, error) }

type compiler struct { chunk *bytecode.Chunk }

func New() Compiler { return &compiler{} }

func (c *compiler) Compile(ctx context.Context, expression ast.Expr) (*bytecode.Chunk, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	c.chunk = bytecode.NewChunk()
	if err := c.emitExpr(ctx, expression); err != nil { return nil, err }
	c.chunk.Emit(bytecode.OpReturn, expression.Span().End.Line, expression.Span().End.Column)
	return c.chunk, nil
}

func (c *compiler) emitExpr(ctx context.Context, expression ast.Expr) error {
	if err := ctx.Err(); err != nil { return err }
	switch node := expression.(type) {
	case *ast.Literal:
		index, err := c.chunk.AddConstant(node.Value); if err != nil { return err }
		c.chunk.EmitOperand(bytecode.OpConstant, index, node.Span().Start.Line, node.Span().Start.Column)
	case *ast.Name:
		index, err := c.chunk.AddConstant(node.Identifier); if err != nil { return err }
		c.chunk.EmitOperand(bytecode.OpGetGlobal, index, node.Span().Start.Line, node.Span().Start.Column)
	case *ast.Unary:
		if err := c.emitExpr(ctx, node.Right); err != nil { return err }
		op := bytecode.OpNegate
		if node.Operator == "not" { op = bytecode.OpNot } else if node.Operator == "+" { return nil }
		c.chunk.Emit(op, node.Span().Start.Line, node.Span().Start.Column)
	case *ast.Binary:
		if err := c.emitExpr(ctx, node.Left); err != nil { return err }
		if err := c.emitExpr(ctx, node.Right); err != nil { return err }
		var op bytecode.OpCode
		switch node.Operator {
		case "+": op=bytecode.OpAdd
		case "-": op=bytecode.OpSubtract
		case "*": op=bytecode.OpMultiply
		case "/": op=bytecode.OpDivide
		case "**": op=bytecode.OpPower
		case "==": op=bytecode.OpEqual
		default: return fmt.Errorf("bytecode compiler does not support operator %q", node.Operator)
		}
		c.chunk.Emit(op, node.Span().Start.Line, node.Span().Start.Column)
	default:
		return fmt.Errorf("bytecode compiler does not support AST node %T", expression)
	}
	return nil
}
