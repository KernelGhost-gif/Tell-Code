package lexer

import "testing"

func TestScannerTracksUTF8ByteOffsetsAndColumns(t *testing.T) {
	source:="café + 2\n"
	scanner:=New(source)
	first:=scanner.Next()
	if first.Kind!=Identifier || first.Lexeme(source)!="café" { t.Fatalf("unexpected first token: %#v",first) }
	if first.Span.Start.Line!=1 || first.Span.Start.Column!=1 || first.Span.End.Offset!=5 { t.Fatalf("unexpected span: %#v",first.Span) }
	plus:=scanner.Next()
	if plus.Kind!=Plus || plus.Span.Start.Column!=6 { t.Fatalf("unexpected plus token: %#v",plus) }
	number:=scanner.Next()
	if number.Kind!=Number || number.Span.Start.Line!=1 { t.Fatalf("unexpected number token: %#v",number) }
	eof:=scanner.Next()
	if eof.Kind!=EOF || eof.Span.Start.Line!=2 || eof.Span.Start.Column!=1 { t.Fatalf("unexpected EOF: %#v",eof) }
}
