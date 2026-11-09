package semantic

import (
	"fmt"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/source"
)

type Symbol struct {
	Name string
	Type string
	Span source.Span
}

type Scope struct {
	parent *Scope
	symbols map[string]Symbol
}

func NewScope(parent *Scope) *Scope {
	return &Scope{parent:parent, symbols:make(map[string]Symbol)}
}

func (s *Scope) Declare(name, typ string, span source.Span) error {
	if _, exists := s.symbols[name]; exists {
		return fmt.Errorf("duplicate declaration %q at line %d, column %d", name, span.Start.Line, span.Start.Column)
	}
	s.symbols[name] = Symbol{Name:name, Type:typ, Span:span}
	return nil
}

func (s *Scope) Resolve(name string) (Symbol, bool) {
	for scope := s; scope != nil; scope = scope.parent {
		if symbol, ok := scope.symbols[name]; ok { return symbol, true }
	}
	return Symbol{}, false
}

type Analyzer struct { Builtins map[string]string }

func NewAnalyzer() *Analyzer {
	return &Analyzer{Builtins:map[string]string{
		"true":"boolean", "false":"boolean", "nothing":"nothing",
		"len":"function", "type_of":"function", "number":"function",
		"abs":"function", "round":"function", "sqrt":"function",
	}}
}

// Analyze resolves every name in an expression against lexical scopes and built-ins.
func (a *Analyzer) Analyze(expr ast.Expr, scope *Scope) error {
	if scope == nil { scope = NewScope(nil) }
	var result error
	ast.Walk(expr, func(node ast.Expr) bool {
		if result != nil { return false }
		name, ok := node.(*ast.Name)
		if !ok { return true }
		if _, found := scope.Resolve(name.Identifier); found { return true }
		if _, found := a.Builtins[name.Identifier]; found { return true }
		span := name.Span()
		result = fmt.Errorf("undefined name %q at line %d, column %d", name.Identifier, span.Start.Line, span.Start.Column)
		return false
	})
	return result
}
