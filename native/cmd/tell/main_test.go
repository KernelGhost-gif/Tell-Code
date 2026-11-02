package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

func TestRunTestsDiscoversAndReportsAssertions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "passes_test.tell"), []byte("Check that true."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.tell"), []byte("Check that false."), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".hidden"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden", "hidden_test.tell"), []byte("Check that false."), 0o600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	status := runTests(root, tell.DefaultExecutionLimits(), &output, &diagnostics)
	if status != 0 || !strings.Contains(output.String(), "1 passed, 0 failed") || diagnostics.Len() != 0 {
		t.Fatalf("passing test run status=%d output=%q diagnostics=%q", status, output.String(), diagnostics.String())
	}
	if err := os.WriteFile(filepath.Join(root, "fails_test.tell"), []byte("Check that false."), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostics.Reset()
	status = runTests(root, tell.DefaultExecutionLimits(), &output, &diagnostics)
	if status != 1 || !strings.Contains(output.String(), "1 failed") || !strings.Contains(diagnostics.String(), "check failed") {
		t.Fatalf("failing test run status=%d output=%q diagnostics=%q", status, output.String(), diagnostics.String())
	}
}
