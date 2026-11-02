package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

func TestCreateProjectProducesRunnableProgramAndTest(t *testing.T) {
	parent := t.TempDir()
	directory, err := createProject(parent, "sample-app")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "tell.toml"))
	if err != nil || !strings.Contains(string(manifest), "name = \"sample-app\"") {
		t.Fatalf("manifest = %q, error = %v", manifest, err)
	}
	program, err := os.ReadFile(filepath.Join(directory, "main.tell"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := tell.RunWithIO(string(program), filepath.Join(directory, "main.tell"), bytes.NewReader(nil), &output); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "Hello from sample-app!\n"; got != want {
		t.Fatalf("program output = %q, want %q", got, want)
	}
	var tests, diagnostics bytes.Buffer
	if status := runTests(directory, tell.DefaultExecutionLimits(), &tests, &diagnostics); status != 0 || !strings.Contains(tests.String(), "1 passed, 0 failed") {
		t.Fatalf("project tests status=%d output=%q diagnostics=%q", status, tests.String(), diagnostics.String())
	}
	if _, err := createProject(parent, "sample-app"); err == nil {
		t.Fatal("expected an existing project directory to be preserved")
	}
}

func TestInitializeProjectPreservesExistingFiles(t *testing.T) {
	directory := t.TempDir()
	keep := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initializeProject(directory, "existing"); err != nil {
		t.Fatal(err)
	}
	if err := initializeProject(directory, "existing"); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	if content, err := os.ReadFile(keep); err != nil || string(content) != "keep me" {
		t.Fatalf("existing file content = %q, error = %v", content, err)
	}
	if _, err := createProject(directory, "../outside"); err == nil {
		t.Fatal("expected path traversal project name to be rejected")
	}
}
