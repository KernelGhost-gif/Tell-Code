package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPackageCommandsForwardToLuna(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "packages.json")
	if err := os.WriteFile(manifest, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "install one package", args: []string{"install", "fmt"}, want: []string{"install", "fmt"}},
		{name: "install multiple packages", args: []string{"install", "fmt", "ripgrep"}, want: []string{"install", "fmt", "ripgrep"}},
		{name: "remove", args: []string{"remove", "fmt"}, want: []string{"remove", "fmt"}},
		{name: "list", args: []string{"list"}, want: []string{"list"}},
		{name: "update", args: []string{"update"}, want: []string{"update"}},
		{name: "sync manifest", args: []string{"sync", manifest}, want: []string{"sync", manifest}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			var output bytes.Buffer
			status := runPackageCommandWith(
				test.args,
				&output,
				&output,
				func(name string) (string, error) {
					if name != "luna" {
						t.Fatalf("looked up %q, want luna", name)
					}
					return "/fake/bin/luna", nil
				},
				func(command *exec.Cmd) error {
					got = append([]string(nil), command.Args[1:]...)
					_, _ = fmt.Fprintln(command.Stdout, "Luna output")
					return nil
				},
			)
			if status != 0 {
				t.Fatalf("status = %d, output = %q", status, output.String())
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Luna args = %#v, want %#v", got, test.want)
			}
			if !strings.Contains(output.String(), "Luna output") {
				t.Fatalf("stdout was not forwarded: %q", output.String())
			}
		})
	}
}

func TestPackageSyncDefaultsToLunafile(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "Lunafile"), []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	var got []string
	status := runPackageCommandWith(
		[]string{"sync"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) (string, error) { return "/fake/luna", nil },
		func(command *exec.Cmd) error {
			got = append([]string(nil), command.Args[1:]...)
			return nil
		},
	)
	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	if !reflect.DeepEqual(got, []string{"sync", "Lunafile"}) {
		t.Fatalf("Luna args = %#v", got)
	}
}

func TestPackageCommandReportsMissingLuna(t *testing.T) {
	var stderr bytes.Buffer
	status := runPackageCommandWith(
		[]string{"list"},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(*exec.Cmd) error { t.Fatal("must not run without Luna"); return nil },
	)
	if status != 127 || !strings.Contains(stderr.String(), lunaInstallURL) {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
}

func TestPackageCommandValidationDoesNotInvokeLuna(t *testing.T) {
	tests := [][]string{
		{"install"},
		{"remove"},
		{"remove", "one", "two"},
		{"list", "unexpected"},
		{"update", "unexpected"},
		{"sync", "one", "two"},
		{"unknown"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stderr bytes.Buffer
			status := runPackageCommandWith(
				args,
				&bytes.Buffer{},
				&stderr,
				func(string) (string, error) { t.Fatal("Luna lookup should not happen"); return "", errors.New("unexpected") },
				func(*exec.Cmd) error { t.Fatal("Luna must not run"); return nil },
			)
			if status != 2 || stderr.Len() == 0 {
				t.Fatalf("status = %d, stderr = %q", status, stderr.String())
			}
		})
	}
}

func TestPackageSyncRequiresARegularManifestFile(t *testing.T) {
	var stderr bytes.Buffer
	status := runPackageCommandWith(
		[]string{"sync", filepath.Join(t.TempDir(), "missing.json")},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, error) { t.Fatal("Luna lookup should not happen"); return "", nil },
		func(*exec.Cmd) error { t.Fatal("Luna must not run"); return nil },
	)
	if status != 2 || !strings.Contains(stderr.String(), "cannot read manifest") {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
}
