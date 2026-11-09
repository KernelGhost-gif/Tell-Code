package bytecode

import (
	"encoding/binary"
	"fmt"
)

type OpCode byte

const (
	OpConstant OpCode = iota + 1
	OpGetGlobal
	OpAdd
	OpSubtract
	OpMultiply
	OpDivide
	OpPower
	OpNegate
	OpNot
	OpEqual
	OpReturn
)

type LineEntry struct { Offset int; Line int; Column int }

type Chunk struct {
	Version uint16
	Code []byte
	Constants []any
	Lines []LineEntry
}

func NewChunk() *Chunk { return &Chunk{Version:1} }

func (c *Chunk) AddConstant(value any) (uint16, error) {
	if len(c.Constants) >= 1<<16 { return 0, fmt.Errorf("constant pool exceeds 65535 entries") }
	index := uint16(len(c.Constants))
	c.Constants = append(c.Constants, value)
	return index, nil
}

func (c *Chunk) Emit(op OpCode, line, column int) {
	c.Lines = append(c.Lines, LineEntry{Offset:len(c.Code), Line:line, Column:column})
	c.Code = append(c.Code, byte(op))
}

func (c *Chunk) EmitOperand(op OpCode, operand uint16, line, column int) {
	c.Emit(op, line, column)
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], operand)
	c.Code = append(c.Code, data[:]...)
}

func (c *Chunk) LineAt(offset int) (int, int) {
	line, column := 1, 1
	for _, entry := range c.Lines {
		if entry.Offset > offset { break }
		line, column = entry.Line, entry.Column
	}
	return line, column
}
