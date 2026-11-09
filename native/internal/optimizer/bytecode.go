package optimizer

import (
	"encoding/binary"
	"fmt"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/bytecode"
)

type instruction struct {
	offset int
	size int
	op bytecode.OpCode
	target int
	hasTarget bool
}

// OptimizeBytecode threads chains of unconditional jumps and removes instructions
// that are no longer reachable from the first instruction. It returns a new chunk.
func OptimizeBytecode(input *bytecode.Chunk) (*bytecode.Chunk,error) {
	if err:=bytecode.Verify(input); err!=nil { return nil,fmt.Errorf("verify input bytecode: %w",err) }
	if len(input.Code)==0 {
		out:=&bytecode.Chunk{Version:input.Version,Constants:append([]any(nil),input.Constants...)}
		return out,nil
	}

	instructions:=make([]instruction,0,len(input.Code))
	byOffset:=make(map[int]int,len(input.Code))
	for ip:=0; ip<len(input.Code); {
		offset:=ip
		op:=bytecode.OpCode(input.Code[ip])
		size:=1
		switch op {
		case bytecode.OpConstant,bytecode.OpGetGlobal,bytecode.OpJump,bytecode.OpJumpIfFalse:
			size=3
		}
		inst:=instruction{offset:offset,size:size,op:op}
		if op==bytecode.OpJump || op==bytecode.OpJumpIfFalse {
			inst.hasTarget=true
			distance:=int(binary.BigEndian.Uint16(input.Code[offset+1:offset+3]))
			inst.target=offset+size+distance
		}
		byOffset[offset]=len(instructions)
		instructions=append(instructions,inst)
		ip+=size
	}

	// Thread branch destinations through any chain of unconditional jumps.
	for i:=range instructions {
		inst:=&instructions[i]
		if !inst.hasTarget { continue }
		seen:=make(map[int]bool)
		for {
			if seen[inst.target] { break }
			seen[inst.target]=true
			nextIndex,ok:=byOffset[inst.target]
			if !ok { break }
			next:=instructions[nextIndex]
			if next.op!=bytecode.OpJump { break }
			inst.target=next.target
		}
	}

	// Compute reachability using the threaded control-flow graph.
	reachable:=make(map[int]bool,len(instructions))
	work:=[]int{instructions[0].offset}
	for len(work)>0 {
		offset:=work[len(work)-1]
		work=work[:len(work)-1]
		if offset==len(input.Code) || reachable[offset] { continue }
		index,ok:=byOffset[offset]
		if !ok { return nil,fmt.Errorf("control flow reached non-instruction offset %d",offset) }
		reachable[offset]=true
		inst:=instructions[index]
		switch inst.op {
		case bytecode.OpReturn:
		case bytecode.OpJump:
			work=append(work,inst.target)
		case bytecode.OpJumpIfFalse:
			work=append(work,inst.target,inst.offset+inst.size)
		default:
			work=append(work,inst.offset+inst.size)
		}
	}

	out:=&bytecode.Chunk{
		Version:input.Version,
		Constants:append([]any(nil),input.Constants...),
		Code:make([]byte,0,len(input.Code)),
		Lines:make([]bytecode.LineEntry,0,len(input.Lines)),
	}
	newOffset:=make(map[int]int,len(reachable))
	for _,inst:=range instructions {
		if !reachable[inst.offset] { continue }
		newOffset[inst.offset]=len(out.Code)
		line,column:=input.LineAt(inst.offset)
		out.Lines=append(out.Lines,bytecode.LineEntry{Offset:len(out.Code),Line:line,Column:column})
		out.Code=append(out.Code,input.Code[inst.offset:inst.offset+inst.size]...)
	}

	for _,inst:=range instructions {
		if !reachable[inst.offset] || !inst.hasTarget { continue }
		dest:=len(out.Code)
		if inst.target!=len(input.Code) {
			mapped,ok:=newOffset[inst.target]
			if !ok { return nil,fmt.Errorf("reachable branch target %d was removed",inst.target) }
			dest=mapped
		}
		source:=newOffset[inst.offset]
		distance:=dest-(source+inst.size)
		if distance<0 || distance>int(^uint16(0)) {
			return nil,fmt.Errorf("optimized jump at %d has unsupported distance %d",source,distance)
		}
		binary.BigEndian.PutUint16(out.Code[source+1:source+3],uint16(distance))
	}

	if err:=bytecode.Verify(out); err!=nil { return nil,fmt.Errorf("verify optimized bytecode: %w",err) }
	return out,nil
}
