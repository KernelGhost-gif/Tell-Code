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
		{name: "install one unlisted package", args: []string{"install", "fmt"}, want: []string{"install", "fmt"}},
		{name: "install multiple unlisted packages", args: []string{"install", "fmt", "ripgrep"}, want: []string{"install", "fmt", "ripgrep"}},
		{name: "remove", args: []string{"remove", "fmt"}, want: []string{"remove", "fmt"}},
		{name: "list", args: []string{"list"}, want: []string{"list"}},
		{name: "update", args: []string{"update"}, want: []string{"update"}},
		{name: "sync manifest", args: []string{"sync", manifest}, want: []string{"sync", manifest}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			var output bytes.Buffer
			status := runPackageCommandWithHome(
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
				t.TempDir(),
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

func TestPackageSearchAndInfoDoNotRequireLuna(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"search", "benchmark"}, want: "hyperfine"},
		{args: []string{"info", "rg"}, want: "ripgrep"},
	} {
		var output, diagnostics bytes.Buffer
		status := runPackageCommandWithHome(
			test.args,
			&output,
			&diagnostics,
			func(string) (string, error) { t.Fatal("catalogue commands must not require Luna"); return "", nil },
			func(*exec.Cmd) error { t.Fatal("catalogue commands must not launch Luna"); return nil },
			t.TempDir(),
		)
		if status != 0 || !strings.Contains(strings.ToLower(output.String()), strings.ToLower(test.want)) {
			t.Fatalf("args=%v status=%d output=%q stderr=%q", test.args, status, output.String(), diagnostics.String())
		}
	}
}

func TestPackageInstallRegistersCatalogueEntryBeforeInstalling(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	var output bytes.Buffer
	status := runPackageCommandWithHome(
		[]string{"install", "tokei"},
		&output,
		&output,
		func(name string) (string, error) {
			if name != "luna" {
				t.Fatalf("looked up %q, want luna", name)
			}
			return "/fake/bin/luna", nil
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	want := [][]string{
		{"add", "tokei", "https://github.com/XAMPPRocky/tokei.git", "cargo build --release --locked", "", "mkdir -p ~/.local/luna && cp target/release/tokei ~/.local/luna/tokei"},
		{"install", "tokei"},
	}
	if status != 0 || !reflect.DeepEqual(calls, want) {
		t.Fatalf("status=%d calls=%#v want=%#v output=%q", status, calls, want, output.String())
	}
}

func TestPackageInstallKeepsExistingCustomRecipe(t *testing.T) {
	home := t.TempDir()
	origin := filepath.Join(home, ".local", "lunaorigin")
	if err := os.MkdirAll(origin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origin, "tokei.json"), []byte("{\"custom\":true}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	status := runPackageCommandWithHome(
		[]string{"install", "tokei"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) (string, error) { return "/fake/bin/luna", nil },
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 || !reflect.DeepEqual(calls, [][]string{{"install", "tokei"}}) {
		t.Fatalf("status=%d calls=%#v", status, calls)
	}
}

func TestPackageCommandReportsMissingLuna(t *testing.T) {
	var stderr bytes.Buffer
	status := runPackageCommandWithHome(
		[]string{"list"},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(*exec.Cmd) error { t.Fatal("must not run without Luna"); return nil },
		t.TempDir(),
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
			status := runPackageCommandWithHome(
				args,
				&bytes.Buffer{},
				&stderr,
				func(string) (string, error) { t.Fatal("Luna lookup should not happen"); return "", errors.New("unexpected") },
				func(*exec.Cmd) error { t.Fatal("Luna must not run"); return nil },
				t.TempDir(),
			)
			if status != 2 || stderr.Len() == 0 {
				t.Fatalf("status = %d, stderr = %q", status, stderr.String())
			}
		})
	}
}

func TestPackageSyncRequiresARegularManifestFile(t *testing.T) {
	var stderr bytes.Buffer
	status := runPackageCommandWithHome(
		[]string{"sync", filepath.Join(t.TempDir(), "missing.json")},
		&bytes.Buffer{},
		&stderr,
		func(string) (string, error) { t.Fatal("Luna lookup should not happen"); return "", nil },
		func(*exec.Cmd) error { t.Fatal("Luna must not run"); return nil },
		t.TempDir(),
	)
	if status != 2 || !strings.Contains(stderr.String(), "cannot read manifest") {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
}
