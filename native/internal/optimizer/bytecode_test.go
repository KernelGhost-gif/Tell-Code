package optimizer

import (
	"encoding/binary"
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/bytecode"
)

func TestBytecodeOptimizerThreadsJumpsAndEliminatesDeadInstructions(t *testing.T) {
	chunk:=bytecode.NewChunk()
	chunk.Code=[]byte{
		byte(bytecode.OpJump),0,0,
		byte(bytecode.OpJump),0,0,
		byte(bytecode.OpReturn),
	}
	chunk.Lines=[]bytecode.LineEntry{
		{Offset:0,Line:1,Column:1},
		{Offset:3,Line:2,Column:1},
		{Offset:6,Line:3,Column:1},
	}
	// The first jump targets the second jump. The second jump targets Return.
	binary.BigEndian.PutUint16(chunk.Code[1:3],0)
	binary.BigEndian.PutUint16(chunk.Code[4:6],0)
	if err:=bytecode.Verify(chunk); err!=nil { t.Fatal(err) }

	optimized,err:=OptimizeBytecode(chunk)
	if err!=nil { t.Fatal(err) }
	if len(optimized.Code)!=4 { t.Fatalf("optimized code length = %d, want 4",len(optimized.Code)) }
	if bytecode.OpCode(optimized.Code[0])!=bytecode.OpJump { t.Fatalf("first opcode = %d",optimized.Code[0]) }
	if got:=binary.BigEndian.Uint16(optimized.Code[1:3]); got!=0 { t.Fatalf("threaded jump distance = %d, want 0",got) }
	if bytecode.OpCode(optimized.Code[3])!=bytecode.OpReturn { t.Fatalf("last opcode = %d, want return",optimized.Code[3]) }
	if err:=bytecode.Verify(optimized); err!=nil { t.Fatal(err) }
	if len(optimized.Lines)!=2 || optimized.Lines[1].Line!=3 { t.Fatalf("unexpected source map after DCE: %#v",optimized.Lines) }
}

func TestBytecodeOptimizerPreservesReachableConditionalPaths(t *testing.T) {
	chunk:=bytecode.NewChunk()
	chunk.Code=[]byte{
		byte(bytecode.OpConstant),0,0,
		byte(bytecode.OpJumpIfFalse),0,4,
		byte(bytecode.OpConstant),0,1,
		byte(bytecode.OpReturn),
		byte(bytecode.OpConstant),0,2,
		byte(bytecode.OpReturn),
	}
	chunk.Constants=[]any{true,"then","else"}
	chunk.Lines=[]bytecode.LineEntry{{Offset:0,Line:1,Column:1},{Offset:3,Line:2,Column:1},{Offset:6,Line:3,Column:1},{Offset:9,Line:4,Column:1},{Offset:10,Line:5,Column:1},{Offset:13,Line:6,Column:1}}
	if err:=bytecode.Verify(chunk); err!=nil { t.Fatal(err) }
	optimized,err:=OptimizeBytecode(chunk)
	if err!=nil { t.Fatal(err) }
	if len(optimized.Code)!=len(chunk.Code) { t.Fatalf("both conditional branches should remain reachable: got %d bytes, want %d",len(optimized.Code),len(chunk.Code)) }
}

func TestBytecodeOptimizerRemovesInstructionsAfterReturn(t *testing.T) {
	chunk:=bytecode.NewChunk()
	index,err:=chunk.AddConstant("unreachable")
	if err!=nil { t.Fatal(err) }
	chunk.Emit(bytecode.OpReturn,1,1)
	chunk.EmitOperand(bytecode.OpConstant,index,2,1)
	optimized,err:=OptimizeBytecode(chunk)
	if err!=nil { t.Fatal(err) }
	if len(optimized.Code)!=1 || bytecode.OpCode(optimized.Code[0])!=bytecode.OpReturn {
		t.Fatalf("unreachable instructions were not removed: %v",optimized.Code)
	}
}
