//go:build !linux || !amd64

package tell

import "fmt"

func nativeJITAvailable() bool { return false }

func compileNativeJIT(function BytecodeFunction) (*nativeCode, error) {
	return nil, fmt.Errorf("native JIT backend is currently available only on linux/amd64")
}

func callNativeJIT(entry uintptr, arguments *float64) float64 { return 0 }

func releaseNativeJIT(code *nativeCode) {}
