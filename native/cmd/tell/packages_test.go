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

func TestPackageSearchAndInfoUseLanguageMetadataWithoutLuna(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{args: []string{"search", "haskell"}, want: []string{"hlint", "shellcheck"}},
		{args: []string{"search", "typescript"}, want: []string{"dprint", "prettier"}},
		{args: []string{"info", "staticcheck"}, want: []string{"Go", "Go 1.26+"}},
		{args: []string{"info", "black"}, want: []string{"Python", "Python 3.11+"}},
		{args: []string{"search", "kotlin"}, want: []string{"ktlint"}},
		{args: []string{"search", "r"}, want: []string{"air"}},
		{args: []string{"info", "csharpier"}, want: []string{"C#", ".NET 10"}},
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
		if status != 0 {
			t.Fatalf("args=%v status=%d output=%q stderr=%q", test.args, status, output.String(), diagnostics.String())
		}
		for _, wanted := range test.want {
			if !strings.Contains(strings.ToLower(output.String()), strings.ToLower(wanted)) {
				t.Fatalf("args=%v output=%q does not contain %q", test.args, output.String(), wanted)
			}
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

func TestPackageInstallRegistersLanguageSpecificRecipe(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	status := runPackageCommandWithHome(
		[]string{"install", "php-cs-fixer"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(string) (string, error) { return "/fake/bin/luna", nil },
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 || len(calls) != 2 {
		t.Fatalf("status=%d calls=%#v", status, calls)
	}
	if len(calls[0]) != 6 || calls[0][0] != "add" || calls[0][1] != "php-cs-fixer" {
		t.Fatalf("unexpected PHP recipe registration call: %#v", calls)
	}
	if calls[1][0] != "install" || calls[1][1] != "php-cs-fixer" {
		t.Fatalf("unexpected PHP install call: %#v", calls)
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

func TestEmbeddedPackageCatalogueIsValidAndUnique(t *testing.T) {
	entries, err := loadPackageCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 20 {
		t.Fatalf("catalogue only has %d entries; expected broad language coverage", len(entries))
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(entry.Name)
		if seen[key] {
			t.Fatalf("duplicate package: %s", entry.Name)
		}
		seen[key] = true
		if len(entry.Languages) == 0 || len(entry.Requirements) == 0 || entry.Validation == "" {
			t.Fatalf("package %s is missing language, requirement, or validation metadata", entry.Name)
		}
	}
}


func TestNativePackageInstallTracksAndRemoves(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	status := runPackageCommandWithHome(
		[]string{"install", "cargo:bat"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(name string) (string, error) {
			if name != "cargo" {
				return "", exec.ErrNotFound
			}
			return "/fake/cargo", nil
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 {
		t.Fatalf("native install status = %d", status)
	}
	packages, err := loadInstalledNativePackages(home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(packages, []nativePackage{{Provider: "cargo", Name: "bat"}}) {
		t.Fatalf("tracked packages = %#v", packages)
	}
	wantInstall := []string{"install", "--root", nativePackageRoot(home), "--locked", "bat"}
	if !reflect.DeepEqual(calls, [][]string{wantInstall}) {
		t.Fatalf("install calls = %#v, want %#v", calls, [][]string{wantInstall})
	}

	status = runPackageCommandWithHome(
		[]string{"remove", "cargo:bat"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(name string) (string, error) {
			if name != "cargo" {
				return "", exec.ErrNotFound
			}
			return "/fake/cargo", nil
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 {
		t.Fatalf("native removal status = %d", status)
	}
	packages, err = loadInstalledNativePackages(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 0 {
		t.Fatalf("packages remain after removal: %#v", packages)
	}
	wantRemove := []string{"uninstall", "--root", nativePackageRoot(home), "bat"}
	if !reflect.DeepEqual(calls[1], wantRemove) {
		t.Fatalf("remove args = %#v, want %#v", calls[1], wantRemove)
	}
}

func TestNativePackageInstallSupportsScopedNpmAndPythonTools(t *testing.T) {
	tests := []struct {
		spec       string
		lookups    map[string]string
		wantProgram string
		wantArgs   []string
	}{
		{
			spec: "npm:@biomejs/biome",
			lookups: map[string]string{"npm": "/fake/npm"},
			wantProgram: "/fake/npm",
			wantArgs: []string{"install", "--global", "--prefix", filepath.Join("HOME", ".local", "tell"), "@biomejs/biome"},
		},
		{
			spec: "pip:ruff",
			lookups: map[string]string{"uv": "/fake/uv"},
			wantProgram: "/fake/uv",
			wantArgs: []string{"tool", "install", "ruff"},
		},
		{
			spec: "go:golang.org/x/tools/gopls",
			lookups: map[string]string{"go": "/fake/go"},
			wantProgram: "/fake/go",
			wantArgs: []string{"install", "golang.org/x/tools/gopls@latest"},
		},
		{
			spec: "coursier:scalafmt",
			lookups: map[string]string{"cs": "/fake/cs"},
			wantProgram: "/fake/cs",
			wantArgs: []string{"install", "scalafmt", "--install-dir", filepath.Join("HOME", ".local", "tell", "bin")},
		},
		{
			spec: "julia:JSON3",
			lookups: map[string]string{"julia": "/fake/julia"},
			wantProgram: "/fake/julia",
			wantArgs: []string{"--startup-file=no", "--history-file=no", "-e", `using Pkg; Pkg.add("JSON3")`},
		},
		{
			spec: "r:styler",
			lookups: map[string]string{"Rscript": "/fake/Rscript"},
			wantProgram: "/fake/Rscript",
			wantArgs: []string{"--vanilla", "-e", `install.packages("styler", repos="https://cloud.r-project.org")`},
		},
		{
			spec: "opam:ocamlformat",
			lookups: map[string]string{"opam": "/fake/opam"},
			wantProgram: "/fake/opam",
			wantArgs: []string{"install", "--yes", "ocamlformat"},
		},
	}
	for _, test := range tests {
		t.Run(test.spec, func(t *testing.T) {
			home := t.TempDir()
			var got *exec.Cmd
			lookPath := func(name string) (string, error) {
				if path := test.lookups[name]; path != "" {
					return path, nil
				}
				return "", exec.ErrNotFound
			}
			status := runPackageCommandWithHome(
				[]string{"install", test.spec},
				&bytes.Buffer{},
				&bytes.Buffer{},
				lookPath,
				func(command *exec.Cmd) error {
					got = command
					return nil
				},
				home,
			)
			if status != 0 {
				t.Fatalf("status = %d", status)
			}
			if got == nil || got.Path != test.wantProgram {
				t.Fatalf("command = %#v, want executable %q", got, test.wantProgram)
			}
			expectedArgs := append([]string(nil), test.wantArgs...)
			for i := range expectedArgs {
				expectedArgs[i] = strings.ReplaceAll(expectedArgs[i], filepath.Join("HOME", ".local", "tell"), nativePackageRoot(home))
			}
			if !reflect.DeepEqual(got.Args[1:], expectedArgs) {
				t.Fatalf("args = %#v, want %#v", got.Args[1:], expectedArgs)
			}
			if test.spec == "go:golang.org/x/tools/gopls" {
				wantEnv := "GOBIN=" + filepath.Join(home, ".local", "tell", "bin")
				if !containsEnvironment(got.Env, wantEnv) {
					t.Fatalf("go install env does not contain %q", wantEnv)
				}
			}
			if test.spec == "pip:ruff" {
				packages, err := loadInstalledNativePackages(home)
				if err != nil {
					t.Fatal(err)
				}
				if len(packages) != 1 || packages[0].Manager != "uv" {
					t.Fatalf("Python manager was not persisted: %#v", packages)
				}
			}
		})
	}
}

func TestNativePackageSpecRejectsUnsafeNames(t *testing.T) {
	for _, spec := range []string{
		"npm:",
		"cargo:--help",
		"pip:ruff;touch",
		"go:module with spaces",
	} {
		if _, recognized, err := parseNativePackageSpec(spec); !recognized || err == nil {
			t.Fatalf("parseNativePackageSpec(%q) = recognized %v, err %v; want validation error", spec, recognized, err)
		}
	}
	if _, recognized, err := parseNativePackageSpec("custom:recipe"); recognized || err != nil {
		t.Fatalf("unknown provider should remain Luna-compatible, got recognized=%v err=%v", recognized, err)
	}
}

func TestNativeListWorksWithoutLuna(t *testing.T) {
	home := t.TempDir()
	if err := saveInstalledNativePackages(home, []nativePackage{{Provider: "go", Name: "golang.org/x/tools/gopls"}}); err != nil {
		t.Fatal(err)
	}
	var output, diagnostics bytes.Buffer
	status := runPackageCommandWithHome(
		[]string{"list"},
		&output,
		&diagnostics,
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(*exec.Cmd) error { t.Fatal("Luna should not be called when it is unavailable"); return nil },
		home,
	)
	if status != 0 || !strings.Contains(output.String(), "go:golang.org/x/tools/gopls") {
		t.Fatalf("status=%d output=%q stderr=%q", status, output.String(), diagnostics.String())
	}
}

func TestNativeUpdateRunsTrackedPackagesWithoutLuna(t *testing.T) {
	home := t.TempDir()
	if err := saveInstalledNativePackages(home, []nativePackage{
		{Provider: "cargo", Name: "bat"},
		{Provider: "npm", Name: "eslint"},
	}); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	status := runPackageCommandWithHome(
		[]string{"update"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(name string) (string, error) {
			switch name {
			case "cargo":
				return "/fake/cargo", nil
			case "npm":
				return "/fake/npm", nil
			default:
				return "", exec.ErrNotFound
			}
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	want := [][]string{
		{"install", "--root", nativePackageRoot(home), "--locked", "--force", "bat"},
		{"update", "--global", "--prefix", nativePackageRoot(home), "eslint"},
	}
	if status != 0 || !reflect.DeepEqual(calls, want) {
		t.Fatalf("status=%d calls=%#v want=%#v", status, calls, want)
	}
}

func containsEnvironment(environment []string, value string) bool {
	for _, item := range environment {
		if item == value {
			return true
		}
	}
	return false
}


func TestNativePipInstallPersistsPipxAndUsesItForUpdates(t *testing.T) {
	home := t.TempDir()
	var calls [][]string
	status := runPackageCommandWithHome(
		[]string{"install", "pip:ruff"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(name string) (string, error) {
			if name == "pipx" {
				return "/fake/pipx", nil
			}
			return "", exec.ErrNotFound
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 {
		t.Fatalf("pipx install status = %d", status)
	}
	packages, err := loadInstalledNativePackages(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Manager != "pipx" {
		t.Fatalf("pipx backend was not persisted: %#v", packages)
	}

	status = runPackageCommandWithHome(
		[]string{"update"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		func(name string) (string, error) {
			if name == "pipx" {
				return "/fake/pipx", nil
			}
			return "", exec.ErrNotFound
		},
		func(command *exec.Cmd) error {
			calls = append(calls, append([]string(nil), command.Args[1:]...))
			return nil
		},
		home,
	)
	if status != 0 {
		t.Fatalf("pipx update status = %d", status)
	}
	if !reflect.DeepEqual(calls, [][]string{{"install", "ruff"}, {"upgrade", "ruff"}}) {
		t.Fatalf("pipx calls = %#v", calls)
	}
}
