package vm

import (
	"context"
	"fmt"
	"math"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/bytecode"
)

type Kind uint8
const (
	Nothing Kind = iota
	Number
	Text
	Boolean
)

type Value struct {
	kind Kind
	number float64
	text string
	boolean bool
}

func From(value any) Value {
	switch v := value.(type) {
	case nil: return Value{kind:Nothing}
	case string: return Value{kind:Text, text:v}
	case bool: return Value{kind:Boolean, boolean:v}
	case int: return Value{kind:Number, number:float64(v)}
	case int64: return Value{kind:Number, number:float64(v)}
	case float64: return Value{kind:Number, number:v}
	default: return Value{kind:Nothing}
	}
}
func NumberValue(v float64) Value { return Value{kind:Number, number:v} }
func (v Value) Kind() Kind { return v.kind }
func (v Value) Interface() any {
	switch v.kind {
	case Number: return v.number
	case Text: return v.text
	case Boolean: return v.boolean
	default: return nil
	}
}
func (v Value) String() string {
	switch v.kind {
	case Number: return fmt.Sprint(v.number)
	case Text: return v.text
	case Boolean: if v.boolean { return "true" }; return "false"
	default: return "nothing"
	}
}

type VM interface {
	Execute(context.Context, *bytecode.Chunk, string, map[string]Value) (Value, error)
}

type Engine struct { MaxStack int }
func New() *Engine { return &Engine{MaxStack:1<<20} }

func (e *Engine) Execute(ctx context.Context, chunk *bytecode.Chunk, filename string, globals map[string]Value) (Value, error) {
	if chunk == nil || chunk.Version != 1 { return Value{}, fmt.Errorf("invalid or unsupported bytecode chunk") }
	if e.MaxStack <= 0 { return Value{}, fmt.Errorf("maximum stack must be positive") }
	stack := make([]Value, 0, 64)
	push := func(v Value) error {
		if len(stack) >= e.MaxStack { return fmt.Errorf("stack limit exceeded") }
		stack = append(stack, v); return nil
	}
	pop := func() (Value, error) {
		if len(stack)==0 { return Value{}, fmt.Errorf("bytecode stack underflow") }
		last:=stack[len(stack)-1]; stack=stack[:len(stack)-1]; return last,nil
	}
	readIndex := func(ip *int) (uint16,error) {
		if *ip+2>len(chunk.Code) { return 0,fmt.Errorf("truncated bytecode operand") }
		index:=uint16(chunk.Code[*ip])<<8|uint16(chunk.Code[*ip+1]); *ip+=2
		return index,nil
	}

	for ip:=0; ip<len(chunk.Code); {
		if err:=ctx.Err(); err!=nil { return Value{},err }
		offset:=ip
		op:=bytecode.OpCode(chunk.Code[ip]); ip++
		line,col:=chunk.LineAt(offset)
		fail:=func(message string) (Value,error) {
			return Value{},fmt.Errorf("[RuntimeError] File %q, line %d, col %d: %s",filename,line,col,message)
		}
		switch op {
		case bytecode.OpConstant:
			index,err:=readIndex(&ip); if err!=nil { return fail(err.Error()) }
			if int(index)>=len(chunk.Constants) { return fail("constant index out of range") }
			if err:=push(From(chunk.Constants[index])); err!=nil { return fail(err.Error()) }
		case bytecode.OpGetGlobal:
			index,err:=readIndex(&ip); if err!=nil { return fail(err.Error()) }
			if int(index)>=len(chunk.Constants) { return fail("global name index out of range") }
			name,ok:=chunk.Constants[index].(string); if !ok { return fail("global name constant is not text") }
			value,ok:=globals[name]; if !ok { return fail(fmt.Sprintf("undefined name %q",name)) }
			if err:=push(value); err!=nil { return fail(err.Error()) }
		case bytecode.OpNegate, bytecode.OpNot:
			value,err:=pop(); if err!=nil { return fail(err.Error()) }
			if op==bytecode.OpNegate {
				if value.kind!=Number { return fail("unary '-' requires a number") }
				value.number = -value.number
			} else { value=Value{kind:Boolean,boolean:!truthy(value)} }
			if err:=push(value); err!=nil { return fail(err.Error()) }
		case bytecode.OpReturn:
			if len(stack)==0 { return Value{kind:Nothing},nil }
			return pop()
		case bytecode.OpAdd, bytecode.OpSubtract, bytecode.OpMultiply, bytecode.OpDivide, bytecode.OpPower, bytecode.OpEqual:
			right,err:=pop(); if err!=nil { return fail(err.Error()) }
			left,err:=pop(); if err!=nil { return fail(err.Error()) }
			var result Value
			if op==bytecode.OpEqual {
				result=Value{kind:Boolean,boolean:left.kind==right.kind && left.Interface()==right.Interface()}
			} else if op==bytecode.OpAdd && left.kind==Text && right.kind==Text {
				result=Value{kind:Text,text:left.text+right.text}
			} else {
				if left.kind!=Number || right.kind!=Number { return fail("arithmetic operators require numbers") }
				var number float64
				switch op {
				case bytecode.OpAdd: number=left.number+right.number
				case bytecode.OpSubtract: number=left.number-right.number
				case bytecode.OpMultiply: number=left.number*right.number
				case bytecode.OpDivide: if right.number==0 { return fail("division by zero") }; number=left.number/right.number
				case bytecode.OpPower: number=math.Pow(left.number,right.number)
				}
				result=NumberValue(number)
			}
			if err:=push(result); err!=nil { return fail(err.Error()) }
		default:
			return fail(fmt.Sprintf("unknown opcode %d",op))
		}
	}
	return Value{kind:Nothing},nil
}

func truthy(v Value) bool {
	switch v.kind {
	case Nothing: return false
	case Boolean: return v.boolean
	case Number: return v.number!=0 && !math.IsNaN(v.number)
	case Text: return v.text!=""
	default: return false
	}
}
