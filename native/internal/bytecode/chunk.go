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
	OpJump
	OpJumpIfFalse
	OpReturn
)

type LineEntry struct {
	Offset int
	Line int
	Column int
}

type Chunk struct {
	Version uint16
	Code []byte
	Constants []any
	Lines []LineEntry
}

func NewChunk() *Chunk { return &Chunk{Version:1} }

func (c *Chunk) AddConstant(value any) (uint16,error) {
	if len(c.Constants)>=1<<16 { return 0,fmt.Errorf("constant pool exceeds 65535 entries") }
	index:=uint16(len(c.Constants))
	c.Constants=append(c.Constants,value)
	return index,nil
}

func (c *Chunk) Emit(op OpCode,line,column int) {
	c.Lines=append(c.Lines,LineEntry{Offset:len(c.Code),Line:line,Column:column})
	c.Code=append(c.Code,byte(op))
}

func (c *Chunk) EmitOperand(op OpCode,operand uint16,line,column int) {
	c.Emit(op,line,column)
	var data [2]byte
	binary.BigEndian.PutUint16(data[:],operand)
	c.Code=append(c.Code,data[:]...)
}

// EmitJump reserves a big-endian uint16 forward distance and returns its operand offset.
func (c *Chunk) EmitJump(op OpCode,line,column int) (int,error) {
	if op!=OpJump && op!=OpJumpIfFalse { return 0,fmt.Errorf("opcode %d is not a jump",op) }
	c.Emit(op,line,column)
	operandOffset:=len(c.Code)
	c.Code=append(c.Code,0,0)
	return operandOffset,nil
}

// PatchJump patches a forward jump to the current end of the chunk.
func (c *Chunk) PatchJump(operandOffset int) error {
	if operandOffset<1 || operandOffset+2>len(c.Code) || c.Code[operandOffset-1]!=byte(OpJump) && c.Code[operandOffset-1]!=byte(OpJumpIfFalse) {
		return fmt.Errorf("invalid jump patch offset %d",operandOffset)
	}
	distance:=len(c.Code)-(operandOffset+2)
	if distance>int(^uint16(0)) { return fmt.Errorf("jump distance %d exceeds bytecode limit",distance) }
	binary.BigEndian.PutUint16(c.Code[operandOffset:operandOffset+2],uint16(distance))
	return nil
}

func (c *Chunk) LineAt(offset int) (int,int) {
	line,column:=1,1
	for _,entry:=range c.Lines {
		if entry.Offset>offset { break }
		line,column=entry.Line,entry.Column
	}
	return line,column
}

// Verify validates instruction boundaries, operands, constant references, and jump targets.
func Verify(c *Chunk) error {
	if c==nil || c.Version!=1 { return fmt.Errorf("invalid or unsupported bytecode chunk") }
	boundaries:=make(map[int]bool,len(c.Code))
	type jump struct { target int; offset int }
	var jumps []jump
	for ip:=0; ip<len(c.Code); {
		boundaries[ip]=true
		offset:=ip
		op:=OpCode(c.Code[ip]); ip++
		switch op {
		case OpConstant,OpGetGlobal,OpJump,OpJumpIfFalse:
			if ip+2>len(c.Code) { return fmt.Errorf("truncated operand at byte %d",offset) }
			operand:=int(binary.BigEndian.Uint16(c.Code[ip:ip+2]))
			ip+=2
			if op==OpConstant || op==OpGetGlobal {
				if operand>=len(c.Constants) { return fmt.Errorf("constant index %d out of range at byte %d",operand,offset) }
				if op==OpGetGlobal { if _,ok:=c.Constants[operand].(string); !ok { return fmt.Errorf("global name constant %d is not text",operand) } }
			} else {
				jumps=append(jumps,jump{target:ip+operand,offset:offset})
			}
		case OpAdd,OpSubtract,OpMultiply,OpDivide,OpPower,OpNegate,OpNot,OpEqual,OpReturn:
		default:
			return fmt.Errorf("unknown opcode %d at byte %d",op,offset)
		}
	}
	boundaries[len(c.Code)]=true
	for _,entry:=range jumps {
		if entry.target>len(c.Code) || !boundaries[entry.target] {
			return fmt.Errorf("invalid jump target %d at byte %d",entry.target,entry.offset)
		}
	}
	return nil
}
