// Package lexer scans Tell expression tokens without allocating token text.
// Tokens refer to byte ranges in the original source; call Token.Lexeme to view them.
package lexer

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/source"
)

type Kind uint8

const (
	Illegal Kind = iota
	EOF
	Identifier
	Number
	String
	True
	False
	Nothing
	Plus
	Minus
	Star
	Slash
	Power
	EqualEqual
	NotEqual
	Less
	LessEqual
	Greater
	GreaterEqual
	And
	Or
	Not
	LeftParen
	RightParen
)

type Token struct {
	Kind Kind
	Span source.Span
}

func (t Token) Lexeme(src string) string {
	if t.Span.Start.Offset < 0 || t.Span.End.Offset > len(src) || t.Span.End.Offset < t.Span.Start.Offset {
		return ""
	}
	return src[t.Span.Start.Offset:t.Span.End.Offset]
}

type Scanner interface { Next() Token }

type scanner struct {
	source string
	offset int
	line int
	column int
}

func New(src string) Scanner { return &scanner{source:src,line:1,column:1} }

func (s *scanner) position() source.Position {
	return source.Position{Offset:s.offset,Line:s.line,Column:s.column}
}
func (s *scanner) peek() rune {
	if s.offset>=len(s.source) { return 0 }
	r,_:=utf8.DecodeRuneInString(s.source[s.offset:])
	return r
}
func (s *scanner) advance() rune {
	r,width:=utf8.DecodeRuneInString(s.source[s.offset:])
	if width==0 { return 0 }
	s.offset+=width
	if r=='\n' { s.line++; s.column=1 } else { s.column++ }
	return r
}
func (s *scanner) token(kind Kind,start source.Position) Token {
	return Token{Kind:kind,Span:source.Span{Start:start,End:s.position()}}
}
func isIdentifierStart(r rune) bool { return r=='_' || unicode.IsLetter(r) }
func isIdentifierPart(r rune) bool { return r=='_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

func (s *scanner) Next() Token {
	for unicode.IsSpace(s.peek()) { s.advance() }
	start:=s.position()
	r:=s.peek()
	if r==0 { return s.token(EOF,start) }
	if isIdentifierStart(r) {
		for isIdentifierPart(s.peek()) { s.advance() }
		word:=s.source[start.Offset:s.offset]
		switch {
		case strings.EqualFold(word,"true"): return s.token(True,start)
		case strings.EqualFold(word,"false"): return s.token(False,start)
		case strings.EqualFold(word,"nothing"),strings.EqualFold(word,"null"),strings.EqualFold(word,"nil"): return s.token(Nothing,start)
		case strings.EqualFold(word,"and"): return s.token(And,start)
		case strings.EqualFold(word,"or"): return s.token(Or,start)
		case strings.EqualFold(word,"not"): return s.token(Not,start)
		default: return s.token(Identifier,start)
		}
	}
	if unicode.IsDigit(r) || (r=='.' && s.offset+1<len(s.source) && s.source[s.offset+1]>='0' && s.source[s.offset+1]<='9') {
		s.advance()
		for unicode.IsDigit(s.peek()) { s.advance() }
		if s.peek()=='.' { s.advance(); for unicode.IsDigit(s.peek()) { s.advance() } }
		if s.peek()=='e' || s.peek()=='E' {
			s.advance()
			if s.peek()=='+' || s.peek()=='-' { s.advance() }
			if !unicode.IsDigit(s.peek()) { return s.token(Illegal,start) }
			for unicode.IsDigit(s.peek()) { s.advance() }
		}
		return s.token(Number,start)
	}
	if r=='"' || r=='\'' {
		quote:=s.advance()
		escaped:=false
		for s.peek()!=0 {
			current:=s.advance()
			if escaped { escaped=false; continue }
			if current=='\\' { escaped=true; continue }
			if current==quote { return s.token(String,start) }
			if current=='\n' { return s.token(Illegal,start) }
		}
		return s.token(Illegal,start)
	}
	s.advance()
	switch r {
	case '+': return s.token(Plus,start)
	case '-': return s.token(Minus,start)
	case '*':
		if s.peek()=='*' { s.advance(); return s.token(Power,start) }
		return s.token(Star,start)
	case '/': return s.token(Slash,start)
	case '=': if s.peek()=='=' { s.advance(); return s.token(EqualEqual,start) }
	case '!': if s.peek()=='=' { s.advance(); return s.token(NotEqual,start) }
	case '<':
		if s.peek()=='=' { s.advance(); return s.token(LessEqual,start) }
		return s.token(Less,start)
	case '>':
		if s.peek()=='=' { s.advance(); return s.token(GreaterEqual,start) }
		return s.token(Greater,start)
	case '(': return s.token(LeftParen,start)
	case ')': return s.token(RightParen,start)
	}
	return s.token(Illegal,start)
}

func TokenError(src string,token Token) error {
	return fmt.Errorf("unexpected token %q at line %d, column %d",token.Lexeme(src),token.Span.Start.Line,token.Span.Start.Column)
}
