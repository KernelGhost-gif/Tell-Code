package source

// Position is a 1-based source coordinate. Offset is a zero-based UTF-8 byte offset.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Span is a half-open source range [Start, End).
type Span struct {
	Start Position
	End   Position
}

// Valid reports whether the range is initialized and ordered.
func (s Span) Valid() bool {
	return s.Start.Offset >= 0 && s.End.Offset >= s.Start.Offset &&
		s.Start.Line >= 1 && s.Start.Column >= 1 &&
		s.End.Line >= 1 && s.End.Column >= 1
}
