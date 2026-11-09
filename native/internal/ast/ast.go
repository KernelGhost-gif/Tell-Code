package ast

import "github.com/KernelGhost-gif/Tell-Code/native/internal/source"

type Node interface {
	Span() source.Span
	Accept(Visitor) any
}

type Expr interface {
	Node
	expressionNode()
}

type Visitor interface {
	VisitLiteral(*Literal) any
	VisitName(*Name) any
	VisitUnary(*Unary) any
	VisitBinary(*Binary) any
}

type Literal struct { Value any; SourceSpan source.Span }
func (n *Literal) Span() source.Span { return n.SourceSpan }
func (n *Literal) Accept(v Visitor) any { return v.VisitLiteral(n) }
func (*Literal) expressionNode() {}

type Name struct { Identifier string; SourceSpan source.Span }
func (n *Name) Span() source.Span { return n.SourceSpan }
func (n *Name) Accept(v Visitor) any { return v.VisitName(n) }
func (*Name) expressionNode() {}

type Unary struct { Operator string; Right Expr; SourceSpan source.Span }
func (n *Unary) Span() source.Span { return n.SourceSpan }
func (n *Unary) Accept(v Visitor) any { return v.VisitUnary(n) }
func (*Unary) expressionNode() {}

type Binary struct { Left Expr; Operator string; Right Expr; SourceSpan source.Span }
func (n *Binary) Span() source.Span { return n.SourceSpan }
func (n *Binary) Accept(v Visitor) any { return v.VisitBinary(n) }
func (*Binary) expressionNode() {}

// Walk visits a node depth-first. Returning false from visit prunes that subtree.
func Walk(node Expr, visit func(Expr) bool) {
	if node == nil || !visit(node) { return }
	switch n := node.(type) {
	case *Unary:
		Walk(n.Right, visit)
	case *Binary:
		Walk(n.Left, visit)
		Walk(n.Right, visit)
	}
}
