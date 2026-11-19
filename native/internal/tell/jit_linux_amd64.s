//go:build linux && amd64

#include "textflag.h"

// callNativeJIT bridges Go's stack-based ABI to the System V amd64 ABI used by
// the generated leaf functions. Generated code receives *float64 in RDI and
// returns a float64 in XMM0.
TEXT ·callNativeJIT(SB), NOSPLIT, $0-24
	MOVQ entry+0(FP), AX
	MOVQ arguments+8(FP), DI
	CALL AX
	MOVSD X0, ret+16(FP)
	RET
