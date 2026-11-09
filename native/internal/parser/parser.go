package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/ast"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/lexer"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/source"
)

type Parser interface { Parse() (ast.Expr, error) }

type ParseError struct {
	Line int
	Column int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Message, e.Line, e.Column)
}

type parser struct {
	source string
	scanner lexer.Scanner
	current lexer.Token
	loaded bool
}

func New(src string) Parser { return &parser{source: src, scanner: lexer.New(src)} }

func (p *parser) peek() lexer.Token {
	if !p.loaded { p.current = p.scanner.Next(); p.loaded = true }
	return p.current
}
func (p *parser) take() lexer.Token { t := p.peek(); p.loaded = false; return t }
func (p *parser) fail(t lexer.Token, message string) error {
	return &ParseError{Line:t.Span.Start.Line, Column:t.Span.Start.Column, Message:message}
}

func precedence(k lexer.Kind) int {
	switch k {
	case lexer.Or: return 1
	case lexer.And: return 2
	case lexer.EqualEqual, lexer.NotEqual, lexer.Less, lexer.LessEqual, lexer.Greater, lexer.GreaterEqual: return 3
	case lexer.Plus, lexer.Minus: return 4
	case lexer.Star, lexer.Slash: return 5
	case lexer.Power: return 6
	default: return 0
	}
}

func (p *parser) Parse() (ast.Expr, error) {
	expr, err := p.parsePrecedence(1)
	if err != nil { return nil, err }
	if t := p.peek(); t.Kind != lexer.EOF { return nil, p.fail(t, "unexpected token "+strconv.Quote(t.Lexeme(p.source))) }
	return expr, nil
}

func (p *parser) parsePrecedence(min int) (ast.Expr, error) {
	left, err := p.parsePrefix()
	if err != nil { return nil, err }
	for {
		op := p.peek()
		level := precedence(op.Kind)
		if level == 0 || level < min { break }
		p.take()
		next := level + 1
		if op.Kind == lexer.Power { next = level }
		right, parseErr := p.parsePrecedence(next)
		if parseErr != nil { return nil, parseErr }
		left = &ast.Binary{Left:left, Operator:operatorName(op.Kind), Right:right,
			SourceSpan:source.Span{Start:left.Span().Start, End:right.Span().End}}
	}
	return left, nil
}

func operatorName(k lexer.Kind) string {
	switch k {
	case lexer.Plus: return "+"
	case lexer.Minus: return "-"
	case lexer.Star: return "*"
	case lexer.Slash: return "/"
	case lexer.Power: return "**"
	case lexer.EqualEqual: return "=="
	case lexer.NotEqual: return "!="
	case lexer.Less: return "<"
	case lexer.LessEqual: return "<="
	case lexer.Greater: return ">"
	case lexer.GreaterEqual: return ">="
	case lexer.And: return "and"
	case lexer.Or: return "or"
	default: return "?"
	}
}

func unquoteString(raw string) (string, error) {
	if len(raw) < 2 {
		return "", fmt.Errorf("string literal is too short")
	}
	if raw[0] == '"' {
		return strconv.Unquote(raw)
	}
	if raw[0] != 39 {
		return "", fmt.Errorf("unknown string delimiter")
	}

	body := raw[1 : len(raw)-1]
	var result strings.Builder
	for len(body) > 0 {
		r, _, tail, err := strconv.UnquoteChar(body, 39)
		if err != nil {
			return "", err
		}
		result.WriteRune(r)
		body = tail
	}
	return result.String(), nil
}
func (p *parser) parsePrefix() (ast.Expr, error) {
	t := p.take()
	switch t.Kind {
	case lexer.Number:
		raw := t.Lexeme(p.source)
		var value any
		if !strings.ContainsAny(raw, ".eE") {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil { value = n } else { value, err = strconv.ParseFloat(raw, 64); if err != nil { return nil, p.fail(t, "invalid number") } }
		} else {
			n, err := strconv.ParseFloat(raw, 64); if err != nil { return nil, p.fail(t, "invalid number") }; value = n
		}
		return &ast.Literal{Value:value, SourceSpan:t.Span}, nil
	case lexer.String:
		value, err := unquoteString(t.Lexeme(p.source))
		if err != nil { return nil, p.fail(t, "invalid string literal") }
		return &ast.Literal{Value:value, SourceSpan:t.Span}, nil
	case lexer.True: return &ast.Literal{Value:true, SourceSpan:t.Span}, nil
	case lexer.False: return &ast.Literal{Value:false, SourceSpan:t.Span}, nil
	case lexer.Nothing: return &ast.Literal{Value:nil, SourceSpan:t.Span}, nil
	case lexer.Identifier: return &ast.Name{Identifier:t.Lexeme(p.source), SourceSpan:t.Span}, nil
	case lexer.Minus, lexer.Plus, lexer.Not:
		right, err := p.parsePrecedence(7)
		if err != nil { return nil, err }
		op := operatorName(t.Kind); if t.Kind == lexer.Not { op = "not" }
		return &ast.Unary{Operator:op, Right:right, SourceSpan:source.Span{Start:t.Span.Start, End:right.Span().End}}, nil
	case lexer.LeftParen:
		expr, err := p.parsePrecedence(1)
		if err != nil { return nil, err }
		if close := p.take(); close.Kind != lexer.RightParen { return nil, p.fail(close, "expected ')'") }
		return expr, nil
	default:
		return nil, p.fail(t, "expected expression")
	}
}
