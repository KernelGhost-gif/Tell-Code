//go:build linux && amd64

package tell

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"syscall"
	"unsafe"
)

func nativeJITAvailable() bool { return true }

//go:noescape
func callNativeJIT(entry uintptr, arguments *float64) float64

func releaseNativeJIT(code *nativeCode) {
	if code == nil || len(code.mapping) == 0 {
		return
	}
	_ = syscall.Munmap(code.mapping)
	code.mapping = nil
	code.entry = 0
}

func compileNativeJIT(function BytecodeFunction) (*nativeCode, error) {
	if function.Receiver || function.HasVariadic || len(function.Defaults) != 0 {
		return nil, fmt.Errorf("methods, variadic parameters, and default arguments are not supported by the native JIT")
	}
	if !strings.EqualFold(function.ReturnType, "number") && !strings.EqualFold(function.ReturnType, "decimal") {
		return nil, fmt.Errorf("native JIT currently requires a number or decimal return type")
	}
	if len(function.ParameterTypes) != len(function.Parameters) || len(function.Parameters) > 7 {
		return nil, fmt.Errorf("native JIT requires up to seven explicitly typed numeric parameters")
	}
	parameterIndex := make(map[string]int, len(function.Parameters))
	for index, name := range function.Parameters {
		typeName := strings.ToLower(strings.TrimSpace(function.ParameterTypes[index]))
		if typeName != "number" && typeName != "decimal" {
			return nil, fmt.Errorf("native JIT parameter %q is not numeric", name)
		}
		parameterIndex[name] = index
	}

	code := make([]byte, 0, len(function.Instructions)*12)
	stackDepth := 0
	returned := false
	for _, instruction := range function.Instructions {
		if returned {
			break
		}
		switch instruction.Op {
		case "PUSH":
			number, ok := numericConstant(instruction.Value)
			if !ok {
				return nil, fmt.Errorf("native JIT only accepts numeric constants")
			}
			if stackDepth >= 7 {
				return nil, fmt.Errorf("native JIT expression exceeds seven live numeric values")
			}
			register := byte(stackDepth)
			code = append(code, 0x48, 0xB8) // movabs rax, imm64
			var bits [8]byte
			binary.LittleEndian.PutUint64(bits[:], math.Float64bits(number))
			code = append(code, bits[:]...)
			code = append(code, 0x66, 0x48, 0x0F, 0x6E, 0xC0|(register<<3)) // movq xmmN, rax
			stackDepth++
		case "LOAD":
			index, ok := parameterIndex[instruction.Name]
			if !ok {
				return nil, fmt.Errorf("native JIT cannot load non-parameter %q", instruction.Name)
			}
			if stackDepth >= 7 {
				return nil, fmt.Errorf("native JIT expression exceeds seven live numeric values")
			}
			register := byte(stackDepth)
			displacement := int32(index * 8)
			code = append(code, 0xF2, 0x0F, 0x10, 0x87|(register<<3)) // movsd xmmN, [rdi+disp32]
			var bits [4]byte
			binary.LittleEndian.PutUint32(bits[:], uint32(displacement))
			code = append(code, bits[:]...)
			stackDepth++
		case "UNARY":
			if stackDepth < 1 {
				return nil, fmt.Errorf("native JIT encountered unary stack underflow")
			}
			register := byte(stackDepth - 1)
			switch instruction.Name {
			case "+":
				// Unary plus leaves the value unchanged.
			case "-":
				// Reserve xmm7 as a scratch register for the IEEE-754 sign bit.
				code = append(code, 0x48, 0xB8)
				var bits [8]byte
				binary.LittleEndian.PutUint64(bits[:], uint64(1)<<63)
				code = append(code, bits[:]...)
				code = append(code, 0x66, 0x48, 0x0F, 0x6E, 0xF8) // movq xmm7, rax
				code = append(code, 0x66, 0x0F, 0x57, 0xC0|(register<<3)|0x07) // xorpd xmmN, xmm7
			default:
				return nil, fmt.Errorf("native JIT does not support unary operator %q", instruction.Name)
			}
		case "BINARY":
			if stackDepth < 2 {
				return nil, fmt.Errorf("native JIT encountered binary stack underflow")
			}
			left := byte(stackDepth - 2)
			right := byte(stackDepth - 1)
			var opcode byte
			switch instruction.Name {
			case "+":
				opcode = 0x58 // addsd
			case "-":
				opcode = 0x5C // subsd
			case "*":
				opcode = 0x59 // mulsd
			default:
				return nil, fmt.Errorf("native JIT does not support binary operator %q", instruction.Name)
			}
			code = append(code, 0xF2, 0x0F, opcode, 0xC0|(left<<3)|right)
			stackDepth--
		case "RETURN":
			if stackDepth != 1 {
				return nil, fmt.Errorf("native JIT return requires exactly one numeric value")
			}
			code = append(code, 0xC3) // ret; result is already in xmm0
			returned = true
		default:
			return nil, fmt.Errorf("native JIT does not support %s instructions", instruction.Op)
		}
	}
	if !returned {
		return nil, fmt.Errorf("native JIT function has no return")
	}

	mapping, err := syscall.Mmap(
		-1, 0, len(code),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANON,
	)
	if err != nil {
		return nil, fmt.Errorf("allocate native code page: %w", err)
	}
	copy(mapping, code)
	if err := syscall.Mprotect(mapping, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(mapping)
		return nil, fmt.Errorf("make native code executable: %w", err)
	}
	entry := uintptr(unsafe.Pointer(&mapping[0]))
	return &nativeCode{entry: entry, mapping: mapping}, nil
}

func numericConstant(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	default:
		return 0, false
	}
}
