package bytecode

import "testing"

func TestJumpPatchAndVerify(t *testing.T) {
	chunk := NewChunk()
	operand, err := chunk.EmitJump(OpJump, 1, 1)
	if err != nil { t.Fatal(err) }
	chunk.Emit(OpReturn, 2, 1)
	if err := chunk.PatchJump(operand); err != nil { t.Fatal(err) }
	if err := Verify(chunk); err != nil { t.Fatal(err) }
}

func TestVerifyRejectsTruncatedOperand(t *testing.T) {
	chunk := NewChunk()
	chunk.Code = []byte{byte(OpConstant), 0}
	if err := Verify(chunk); err == nil { t.Fatal("expected truncated operand error") }
}

func TestVerifyRejectsOutOfRangeConstant(t *testing.T) {
	chunk := NewChunk()
	chunk.Code = []byte{byte(OpConstant), 0, 1, byte(OpReturn)}
	if err := Verify(chunk); err == nil { t.Fatal("expected invalid constant index") }
}

func TestVerifyRejectsUnknownOpcode(t *testing.T) {
	chunk := NewChunk()
	chunk.Code = []byte{255}
	if err := Verify(chunk); err == nil { t.Fatal("expected unknown opcode error") }
}
