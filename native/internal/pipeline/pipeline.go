// Package pipeline wires the modular expression front end into a small VM execution path.
// It is deliberately an expression vertical slice; the production statement runtime is migrated separately.
package pipeline

import (
	"context"
	"fmt"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/compiler"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/diagnostic"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/optimizer"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/parser"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/semantic"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/types"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/vm"
)

type Result struct {
	Value vm.Value
	OptimizedSourceExpression any
}

func Run(ctx context.Context, source, filename string, globals map[string]vm.Value) (Result, error) {
	if err:=ctx.Err(); err!=nil { return Result{},err }
	expression,err:=parser.New(source).Parse()
	if err!=nil {
		return Result{},&diagnostic.Error{Kind:diagnostic.SyntaxError,Filename:filename,Line:1,Column:1,Message:err.Error()}
	}

	scope:=semantic.NewScope(nil)
	symbolTypes:=make(map[string]types.Type,len(globals))
	for name,value:=range globals {
		typ:=types.Any
		switch value.Kind() {
		case vm.Number: typ=types.Number
		case vm.Text: typ=types.Text
		case vm.Boolean: typ=types.Boolean
		case vm.Nothing: typ=types.Nothing
		}
		_ = scope.Declare(name,string(typ),expression.Span())
		symbolTypes[name]=typ
	}
	if err:=semantic.NewAnalyzer().Analyze(expression,scope); err!=nil {
		return Result{},&diagnostic.Error{Kind:diagnostic.NameError,Filename:filename,Line:expression.Span().Start.Line,Column:expression.Span().Start.Column,Message:err.Error()}
	}
	if _,err:=types.NewChecker(symbolTypes).Infer(expression); err!=nil {
		return Result{},&diagnostic.Error{Kind:diagnostic.TypeError,Filename:filename,Line:expression.Span().Start.Line,Column:expression.Span().Start.Column,Message:err.Error()}
	}

	optimized:=optimizer.New().Optimize(expression)
	chunk,err:=compiler.New().Compile(ctx,optimized)
	if err!=nil { return Result{},fmt.Errorf("compile Tell expression: %w",err) }
	value,err:=vm.New().Execute(ctx,chunk,filename,globals)
	if err!=nil { return Result{},err }
	return Result{Value:value,OptimizedSourceExpression:optimized},nil
}
