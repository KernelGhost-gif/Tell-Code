package main

import (
	"encoding/json"
	"io"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

type nativePackage struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Manager  string `json:"manager,omitempty"`
}

type nativeCommand struct {
	Program string
	Args    []string
	Env     []string
}

var nativePackageProviders = map[string]bool{
	"cargo":    true,
	"npm":      true,
	"pip":      true,
	"go":       true,
	"gem":      true,
	"composer": true,
	"nuget":    true,
	"pub":      true,
	"cabal":    true,
	"mix":      true,
}

func parseNativePackageSpec(spec string) (nativePackage, bool, error) {
	provider, name, found := strings.Cut(spec, ":")
	if !found {
		return nativePackage{}, false, nil
	}
	provider = strings.ToLower(provider)
	if !nativePackageProviders[provider] {
		return nativePackage{}, false, nil
	}
	name = strings.TrimSpace(name)
	if err := validateNativePackageName(name); err != nil {
		return nativePackage{}, true, fmt.Errorf("invalid %s package spec %q: %w", provider, spec, err)
	}
	return nativePackage{Provider: provider, Name: name}, true, nil
}

func validateNativePackageName(name string) error {
	if name == "" {
		return errors.New("package name is empty")
	}
	if len(name) > 256 {
		return errors.New("package name is too long")
	}
	if strings.HasPrefix(name, "-") {
		return errors.New("package names cannot start with a dash")
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return errors.New("package names cannot contain whitespace or control characters")
		}
		switch r {
		case ';', '|', '&', '`', '$', '<', '>', '\\', '\'', '"':
			return errors.New("package name contains a disallowed character")
		}
	}
	return nil
}

func nativePackageRoot(home string) string {
	return filepath.Join(home, ".local", "tell")
}

func nativePackageStorePath(home string) string {
	return filepath.Join(nativePackageRoot(home), "native-packages.json")
}

func loadInstalledNativePackages(home string) ([]nativePackage, error) {
	data, err := os.ReadFile(nativePackageStorePath(home))
	if errors.Is(err, os.ErrNotExist) {
		return []nativePackage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read native package state: %w", err)
	}
	var packages []nativePackage
	if err := json.Unmarshal(data, &packages); err != nil {
		return nil, fmt.Errorf("parse native package state: %w", err)
	}
	for _, pkg := range packages {
		if !nativePackageProviders[pkg.Provider] {
			return nil, fmt.Errorf("native package state contains unknown provider %q", pkg.Provider)
		}
		if err := validateNativePackageName(pkg.Name); err != nil {
			return nil, fmt.Errorf("native package state contains invalid %s package: %w", pkg.Provider, err)
		}
		if pkg.Manager != "" && (pkg.Provider != "pip" || (pkg.Manager != "uv" && pkg.Manager != "pipx")) {
			return nil, fmt.Errorf("native package state contains invalid manager %q for %s", pkg.Manager, pkg.Provider)
		}
	}
	return packages, nil
}

func saveInstalledNativePackages(home string, packages []nativePackage) error {
	path := nativePackageStorePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create native package state directory: %w", err)
	}
	data, err := json.MarshalIndent(packages, "", "  ")
	if err != nil {
		return fmt.Errorf("encode native package state: %w", err)
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write native package state: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("replace native package state: %w", err)
	}
	return nil
}

func updateNativePackageRecord(packages []nativePackage, target nativePackage, remove bool) []nativePackage {
	result := make([]nativePackage, 0, len(packages)+1)
	found := false
	for _, pkg := range packages {
		if pkg.Provider == target.Provider && pkg.Name == target.Name {
			found = true
			if target.Manager != "" {
				pkg.Manager = target.Manager
			}
			if remove {
				continue
			}
		}
		result = append(result, pkg)
	}
	if !remove && !found {
		result = append(result, target)
	}
	return result
}

func nativeCommandPlan(action string, pkg nativePackage, home string, lookPath func(string) (string, error)) ([]nativeCommand, error) {
	root := nativePackageRoot(home)
	binDir := filepath.Join(root, "bin")
	if strings.TrimSpace(home) == "" {
		return nil, errors.New("could not determine the home directory")
	}
	if action != "install" && action != "remove" && action != "update" {
		return nil, fmt.Errorf("unsupported native package action %q", action)
	}

	command := func(program string, args []string, env ...string) nativeCommand {
		return nativeCommand{Program: program, Args: args, Env: env}
	}
	switch pkg.Provider {
	case "cargo":
		if action == "remove" {
			return []nativeCommand{command("cargo", []string{"uninstall", "--root", root, pkg.Name})}, nil
		}
		args := []string{"install", "--root", root, "--locked"}
		if action == "update" {
			args = append(args, "--force")
		}
		args = append(args, pkg.Name)
		return []nativeCommand{command("cargo", args)}, nil

	case "npm":
		if action == "remove" {
			return []nativeCommand{command("npm", []string{"uninstall", "--global", "--prefix", root, pkg.Name})}, nil
		}
		if action == "update" {
			return []nativeCommand{command("npm", []string{"update", "--global", "--prefix", root, pkg.Name})}, nil
		}
		return []nativeCommand{command("npm", []string{"install", "--global", "--prefix", root, pkg.Name})}, nil

	case "pip":
		manager := pkg.Manager
		if manager == "" {
			manager = "uv"
			if _, err := lookPath(manager); err != nil {
				manager = "pipx"
			}
		}
		if manager != "uv" && manager != "pipx" {
			return nil, fmt.Errorf("unsupported Python CLI package manager %q", manager)
		}
		if _, err := lookPath(manager); err != nil {
			return nil, fmt.Errorf("Python CLI installs require %s on PATH", manager)
		}
		env := []string{"UV_TOOL_DIR=" + filepath.Join(root, "uv-tools"), "UV_TOOL_BIN_DIR=" + binDir,
			"PIPX_HOME=" + filepath.Join(root, "pipx"), "PIPX_BIN_DIR=" + binDir}
		switch {
		case manager == "uv" && action == "remove":
			return []nativeCommand{command(manager, []string{"tool", "uninstall", pkg.Name}, env...)}, nil
		case manager == "uv" && action == "update":
			return []nativeCommand{command(manager, []string{"tool", "upgrade", pkg.Name}, env...)}, nil
		case manager == "uv":
			return []nativeCommand{command(manager, []string{"tool", "install", pkg.Name}, env...)}, nil
		case action == "remove":
			return []nativeCommand{command(manager, []string{"uninstall", pkg.Name}, env...)}, nil
		case action == "update":
			return []nativeCommand{command(manager, []string{"upgrade", pkg.Name}, env...)}, nil
		default:
			return []nativeCommand{command(manager, []string{"install", pkg.Name}, env...)}, nil
		}

	case "go":
		if action == "remove" {
			return nil, nil // Go does not have an uninstall command; remove the recorded executable below.
		}
		module := pkg.Name
		if !strings.Contains(module, "@") {
			module += "@latest"
		}
		return []nativeCommand{command("go", []string{"install", module}, "GOBIN="+binDir)}, nil

	case "gem":
		gemDir := filepath.Join(root, "gems")
		env := []string{"GEM_HOME=" + gemDir, "GEM_PATH=" + gemDir}
		switch action {
		case "remove":
			return []nativeCommand{command("gem", []string{"uninstall", "--all", "--executables", "--force", "--install-dir", gemDir, pkg.Name}, env...)}, nil
		case "update":
			return []nativeCommand{command("gem", []string{"update", "--no-document", "--install-dir", gemDir, pkg.Name}, env...)}, nil
		default:
			return []nativeCommand{command("gem", []string{"install", "--no-document", "--install-dir", gemDir, "--bindir", binDir, pkg.Name}, env...)}, nil
		}

	case "composer":
		env := []string{"COMPOSER_HOME=" + filepath.Join(root, "composer")}
		if action == "remove" {
			return []nativeCommand{command("composer", []string{"global", "remove", pkg.Name}, env...)}, nil
		}
		if action == "update" {
			return []nativeCommand{command("composer", []string{"global", "update", pkg.Name}, env...)}, nil
		}
		return []nativeCommand{
			command("composer", []string{"global", "config", "bin-dir", binDir}, env...),
			command("composer", []string{"global", "require", pkg.Name}, env...),
		}, nil

	case "nuget":
		args := []string{"tool", "install", "--tool-path", binDir, pkg.Name}
		if action == "remove" {
			args = []string{"tool", "uninstall", "--tool-path", binDir, pkg.Name}
		} else if action == "update" {
			args = []string{"tool", "update", "--tool-path", binDir, pkg.Name}
		}
		return []nativeCommand{command("dotnet", args)}, nil

	case "pub":
		env := []string{"PUB_CACHE=" + filepath.Join(root, "pub-cache")}
		args := []string{"pub", "global", "activate", pkg.Name}
		if action == "remove" {
			args = []string{"pub", "global", "deactivate", pkg.Name}
		}
		return []nativeCommand{command("dart", args, env...)}, nil

	case "cabal":
		if action == "remove" {
			return nil, nil // Cabal has no portable uninstall command; remove the installed executable below.
		}
		return []nativeCommand{command("cabal", []string{"install", pkg.Name, "--installdir", binDir, "--overwrite-policy=always"})}, nil

	case "mix":
		if action == "remove" {
			return []nativeCommand{command("mix", []string{"archive.uninstall", pkg.Name})}, nil
		}
		return []nativeCommand{command("mix", []string{"archive.install", "hex", pkg.Name, "--force"})}, nil
	}
	return nil, fmt.Errorf("unsupported native package provider %q", pkg.Provider)
}

func runNativePackageAction(
	action string,
	pkg nativePackage,
	home string,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
	stdout, stderr io.Writer,
) int {
	if strings.TrimSpace(home) == "" {
		fmt.Fprintln(stderr, "tell pkg: could not determine the home directory")
		return 1
	}
	if err := os.MkdirAll(nativePackageRoot(home), 0o755); err != nil {
		fmt.Fprintf(stderr, "tell pkg: cannot create native package directory: %v\n", err)
		return 1
	}

	if action == "install" && pkg.Provider == "pip" && pkg.Manager == "" {
		if _, err := lookPath("uv"); err == nil {
			pkg.Manager = "uv"
		} else if _, err := lookPath("pipx"); err == nil {
			pkg.Manager = "pipx"
		} else {
			fmt.Fprintln(stderr, "tell pkg: Python CLI installs require either uv or pipx on PATH")
			return 127
		}
	}

	if action == "remove" && (pkg.Provider == "go" || pkg.Provider == "cabal") {
		binaryName := nativePackageBinaryName(pkg)
		binaryPath := filepath.Join(nativePackageRoot(home), "bin", binaryName)
		if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "tell pkg: cannot remove %s: %v\n", binaryPath, err)
			return 1
		}
	} else {
		plan, err := nativeCommandPlan(action, pkg, home, lookPath)
		if err != nil {
			fmt.Fprintf(stderr, "tell pkg: %v\n", err)
			return 127
		}
		for _, step := range plan {
			executable, err := lookPath(step.Program)
			if err != nil {
				fmt.Fprintf(stderr, "tell pkg: %s package operations require %s on PATH\n", action, step.Program)
				return 127
			}
			process := exec.Command(executable, step.Args...)
			process.Stdout = stdout
			process.Stderr = stderr
			if len(step.Env) > 0 {
				process.Env = mergeEnvironment(os.Environ(), step.Env)
			}
			if err := runCommand(process); err != nil {
				return reportPackageCommandError(err, stderr)
			}
		}
	}

	packages, err := loadInstalledNativePackages(home)
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: %v\n", err)
		return 1
	}
	packages = updateNativePackageRecord(packages, pkg, action == "remove")
	if err := saveInstalledNativePackages(home, packages); err != nil {
		fmt.Fprintf(stderr, "tell pkg: %v\n", err)
		return 1
	}

	switch action {
	case "install":
		fmt.Fprintf(stdout, "Installed %s:%s through its native package manager.\n", pkg.Provider, pkg.Name)
		fmt.Fprintf(stdout, "Add %s to PATH to run installed tools.\n", filepath.Join(nativePackageRoot(home), "bin"))
	case "remove":
		fmt.Fprintf(stdout, "Removed %s:%s.\n", pkg.Provider, pkg.Name)
	case "update":
		fmt.Fprintf(stdout, "Updated %s:%s.\n", pkg.Provider, pkg.Name)
	}
	return 0
}

func nativePackageBinaryName(pkg nativePackage) string {
	name := pkg.Name
	if at := strings.LastIndex(name, "@"); at > 0 {
		name = name[:at]
	}
	if slash := strings.LastIndexAny(name, "/:"); slash >= 0 {
		name = name[slash+1:]
	}
	return name
}

func mergeEnvironment(base, additions []string) []string {
	values := make(map[string]string, len(base)+len(additions))
	order := make([]string, 0, len(base)+len(additions))
	for _, item := range base {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for _, item := range additions {
		key, value, found := strings.Cut(item, "=")
		if !found {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}

func reportPackageCommandError(err error, stderr io.Writer) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	fmt.Fprintf(stderr, "tell pkg: package manager command failed: %v\n", err)
	return 1
}

func runTrackedNativeUpdates(
	home string,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
	stdout, stderr io.Writer,
) int {
	packages, err := loadInstalledNativePackages(home)
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: %v\n", err)
		return 1
	}
	for _, pkg := range packages {
		if status := runNativePackageAction("update", pkg, home, lookPath, runCommand, stdout, stderr); status != 0 {
			return status
		}
	}
	return 0
}

func runNativeAndLunaList(
	home string,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
	stdout, stderr io.Writer,
) int {
	packages, err := loadInstalledNativePackages(home)
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: %v\n", err)
		return 1
	}
	if len(packages) > 0 {
		fmt.Fprintln(stdout, "Native ecosystem packages:")
		for _, pkg := range packages {
			fmt.Fprintf(stdout, "  %s:%s [native]\n", pkg.Provider, pkg.Name)
		}
	}
	executable, err := lookPath("luna")
	if err != nil {
		if len(packages) > 0 {
			return 0
		}
		fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
		return 127
	}
	return runLunaCommand(executable, []string{"list"}, stdout, stderr, runCommand)
}
