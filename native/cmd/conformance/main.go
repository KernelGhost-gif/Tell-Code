package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type testCase struct {
	Name string `json:"name"`
	Filename string `json:"filename"`
	SourceCode string `json:"source_code"`
	Files map[string]string `json:"files,omitempty"`
	Stdin string `json:"stdin,omitempty"`
	ExpectedStdout string `json:"expected_stdout"`
	ExpectedStderr string `json:"expected_stderr"`
	ExpectedExitCode int `json:"expected_exit_code"`
	MaxCallDepth int `json:"max_call_depth,omitempty"`
	MaxLoopIterations int `json:"max_loop_iterations,omitempty"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

type suite struct {
	SchemaVersion int `json:"schema_version"`
	Tests []testCase `json:"tests"`
}

func safeWrite(root, name, content string) error {
	clean := filepath.Clean(name)
	if filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("fixture path escapes temporary directory: %s", name)
	}
	target := filepath.Join(root, clean)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { return err }
	return os.WriteFile(target, []byte(content), 0o644)
}

func main() {
	fixtures := flag.String("fixtures", "../../conformance/runtime-v1.json", "conformance suite JSON")
	binary := flag.String("tell", "../../releases/tell-linux-amd64", "native Tell executable")
	flag.Parse()
	data, err := os.ReadFile(*fixtures)
	if err != nil { fatal(err) }
	var tests suite
	if err := json.Unmarshal(data, &tests); err != nil { fatal(err) }
	if tests.SchemaVersion != 1 { fatal(fmt.Errorf("unsupported conformance schema version %d", tests.SchemaVersion)) }
	path, err := filepath.Abs(*binary)
	if err != nil { fatal(err) }

	passed, failed := 0, 0
	for _, tc := range tests.Tests {
		filename := tc.Filename
		if filename == "" { filename = strings.ReplaceAll(tc.Name, " ", "-") + ".tell" }
		workdir, err := os.MkdirTemp("", "tell-conformance-")
		if err != nil { fatal(err) }
		if err := safeWrite(workdir, filename, tc.SourceCode); err != nil { _ = os.RemoveAll(workdir); fatal(err) }
		for name, content := range tc.Files {
			if err := safeWrite(workdir, name, content); err != nil { _ = os.RemoveAll(workdir); fatal(err) }
		}

		timeout := time.Duration(tc.TimeoutSeconds) * time.Second
		if timeout <= 0 { timeout = 10 * time.Second }
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		args := []string{"run", filename}
		if tc.MaxCallDepth > 0 { args = append(args, "--max-call-depth", fmt.Sprint(tc.MaxCallDepth)) }
		if tc.MaxLoopIterations > 0 { args = append(args, "--max-loop-iterations", fmt.Sprint(tc.MaxLoopIterations)) }
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = workdir
		cmd.Stdin = strings.NewReader(tc.Stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		runErr := cmd.Run()
		exitCode := 0
		if runErr != nil {
			if ctx.Err() != nil {
				exitCode = 124
				stderr.Reset()
				fmt.Fprint(&stderr, "conformance timeout")
			} else if exit, ok := runErr.(*exec.ExitError); ok {
				exitCode = exit.ExitCode()
			} else {
				exitCode = 127
				fmt.Fprint(&stderr, runErr)
			}
		}
		cancel()
		_ = os.RemoveAll(workdir)

		if stdout.String() == tc.ExpectedStdout && stderr.String() == tc.ExpectedStderr && exitCode == tc.ExpectedExitCode {
			passed++
			fmt.Println("PASS", tc.Name)
			continue
		}
		failed++
		fmt.Println("FAIL", tc.Name)
		fmt.Println("  stdout expected", fmt.Sprintf("%q", tc.ExpectedStdout), "got", fmt.Sprintf("%q", stdout.String()))
		fmt.Println("  stderr expected", fmt.Sprintf("%q", tc.ExpectedStderr), "got", fmt.Sprintf("%q", stderr.String()))
		fmt.Println("  exit expected", tc.ExpectedExitCode, "got", exitCode)
	}
	fmt.Println("Conformance:", passed, "passed,", failed, "failed.")
	if failed > 0 { os.Exit(1) }
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "conformance:", err)
	os.Exit(2)
}
