package diagnostic

import "fmt"

type Kind string

const (
	SyntaxError Kind = "SyntaxError"
	TypeError Kind = "TypeError"
	NameError Kind = "NameError"
	RuntimeError Kind = "RuntimeError"
	IOError Kind = "IOError"
	UsageError Kind = "UsageError"
)

func ExitCode(kind Kind) int {
	switch kind {
	case SyntaxError, UsageError: return 64
	case TypeError, NameError, RuntimeError: return 70
	case IOError: return 74
	default: return 70
	}
}

type Error struct {
	Kind Kind
	Filename string
	Line int
	Column int
	Message string
}

func (e *Error) Error() string {
	filename:=e.Filename
	if filename=="" { filename="<input>" }
	line,column:=e.Line,e.Column
	if line<1 { line=1 }
	if column<1 { column=1 }
	return fmt.Sprintf("[%s] File %q, line %d, col %d: %s",e.Kind,filename,line,column,e.Message)
}
func (e *Error) ExitCode() int { return ExitCode(e.Kind) }
