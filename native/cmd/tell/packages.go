package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const lunaInstallURL = "https://github.com/Neonwave175/Luna"

//go:embed package_registry.json
var packageRegistryJSON []byte

type packageCatalogEntry struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Languages    []string `json:"languages"`
	Platforms    []string `json:"platforms"`
	Requirements []string `json:"requirements"`
	Validation   string   `json:"validation"`
	URL          string   `json:"url"`
	Config       string   `json:"config"`
	Compile      string   `json:"compile"`
	Install      string   `json:"install"`
	Executable   string   `json:"executable"`
}

func loadPackageCatalog() ([]packageCatalogEntry, error) {
	var entries []packageCatalogEntry
	if err := json.Unmarshal(packageRegistryJSON, &entries); err != nil {
		return nil, fmt.Errorf("read embedded package catalogue: %w", err)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := strings.ToLower(strings.TrimSpace(entry.Name))
		if name == "" || entry.URL == "" || entry.Compile == "" || entry.Install == "" || len(entry.Languages) == 0 {
			return nil, fmt.Errorf("invalid package catalogue entry %q: name, url, compile, install, and languages are required", entry.Name)
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate package catalogue entry %q", entry.Name)
		}
		seen[name] = true
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func packageCatalogIndex(entries []packageCatalogEntry) map[string]packageCatalogEntry {
	index := make(map[string]packageCatalogEntry, len(entries))
	for _, entry := range entries {
		index[strings.ToLower(entry.Name)] = entry
	}
	return index
}

func printPackageUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Tell package commands (curated catalogue, native registries, and Luna)")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Usage: tell pkg <command> [arguments]")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  search [query]             Search packages, descriptions, and languages")
	fmt.Fprintln(writer, "  info <name>                Show package details and build requirements")
	fmt.Fprintln(writer, "  add <name>                 Register a catalogue recipe with Luna")
	fmt.Fprintln(writer, "  install <name> [name...]   Install Tell, native-registry, or Luna packages")
	fmt.Fprintln(writer, "  native specs: cargo:, npm:, pip:, go:, gem:, composer:, nuget:, pub:, cabal:, mix:, coursier:, julia:, r:, opam:, mint:, luarocks:, conan:, deno:, bun:, nimble:, dub:")
	fmt.Fprintln(writer, "  registry search: tell pkg search <provider>:<query>")
	fmt.Fprintln(writer, "  remove <name>              Remove a package")
	fmt.Fprintln(writer, "  list                       List packages registered with Luna")
	fmt.Fprintln(writer, "  update                     Update installed packages")
	fmt.Fprintln(writer, "  sync [Lunafile]            Install packages from a Luna JSON manifest")
}

func runPackageCommand(args []string, stdout, stderr io.Writer) int {
	homeDir, _ := os.UserHomeDir()
	return runPackageCommandWithHome(
		args,
		stdout,
		stderr,
		exec.LookPath,
		func(command *exec.Cmd) error { return command.Run() },
		homeDir,
	)
}

func runPackageCommandWith(
	args []string,
	stdout, stderr io.Writer,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
) int {
	homeDir, _ := os.UserHomeDir()
	return runPackageCommandWithHome(args, stdout, stderr, lookPath, runCommand, homeDir)
}

func runPackageCommandWithHome(
	args []string,
	stdout, stderr io.Writer,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
	homeDir string,
) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printPackageUsage(stdout)
		return 0
	}

	command := strings.ToLower(args[0])
	operands := args[1:]

	catalog, err := loadPackageCatalog()
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: %v\n", err)
		return 1
	}
	catalogByName := packageCatalogIndex(catalog)

	switch command {
	case "search":
		if len(operands) > 1 {
			fmt.Fprintln(stderr, "tell pkg search: expected at most one query")
			return 2
		}
		if len(operands) == 1 {
			provider, query, recognized, parseErr := parseNativePackageQuery(operands[0])
			if parseErr != nil {
				fmt.Fprintf(stderr, "tell pkg search: %v\n", parseErr)
				return 2
			}
			if recognized {
				return runNativePackageSearch(provider, query, lookPath, runCommand, stdout, stderr)
			}
		}
		query := ""
		if len(operands) == 1 {
			query = strings.ToLower(strings.TrimSpace(operands[0]))
		}
		matches := 0
		for _, entry := range catalog {
			fields := []string{entry.Name, entry.Description, entry.Category}
			fields = append(fields, entry.Languages...)
			haystack := strings.ToLower(strings.Join(fields, " "))
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			fmt.Fprintf(stdout, "  %-18s %s [%s]\n", entry.Name, entry.Description, strings.Join(entry.Languages, ", "))
			matches++
		}
		if matches == 0 {
			fmt.Fprintln(stdout, "No catalogue packages matched.")
		}
		return 0

	case "info":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "tell pkg info: expected exactly one package name")
			return 2
		}
		entry, ok := catalogByName[strings.ToLower(operands[0])]
		if !ok {
			fmt.Fprintf(stderr, "tell pkg info: %q is not in Tell's catalogue; run 'tell pkg search'\n", operands[0])
			return 2
		}
		fmt.Fprintf(stdout, "%s — %s\n", entry.Name, entry.Description)
		fmt.Fprintf(stdout, "Category: %s\n", entry.Category)
		fmt.Fprintf(stdout, "Languages: %s\n", strings.Join(entry.Languages, ", "))
		fmt.Fprintf(stdout, "Platforms: %s\n", strings.Join(entry.Platforms, ", "))
		fmt.Fprintf(stdout, "Requirements: %s\n", strings.Join(entry.Requirements, ", "))
		fmt.Fprintf(stdout, "Validation: %s\n", entry.Validation)
		fmt.Fprintf(stdout, "Source: %s\n", entry.URL)
		fmt.Fprintf(stdout, "Executable: %s\n", entry.Executable)
		return 0

	case "add":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "tell pkg add: expected exactly one catalogue package name")
			return 2
		}
		entry, ok := catalogByName[strings.ToLower(operands[0])]
		if !ok {
			fmt.Fprintf(stderr, "tell pkg add: %q is not in Tell's catalogue; run 'tell pkg search'\n", operands[0])
			return 2
		}
		executable, lookupErr := lookPath("luna")
		if lookupErr != nil {
			fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
			return 127
		}
		return registerCatalogPackage(entry, executable, homeDir, stdout, stderr, runCommand)

	case "install":
		if len(operands) == 0 {
			fmt.Fprintln(stderr, "tell pkg install: expected at least one package name")
			printPackageUsage(stderr)
			return 2
		}
		lunaPackages := make([]string, 0, len(operands))
		for _, name := range operands {
			native, recognized, parseErr := parseNativePackageSpec(name)
			if parseErr != nil {
				fmt.Fprintf(stderr, "tell pkg install: %v\n", parseErr)
				return 2
			}
			if recognized {
				if status := runNativePackageAction("install", native, homeDir, lookPath, runCommand, stdout, stderr); status != 0 {
					return status
				}
				continue
			}
			lunaPackages = append(lunaPackages, name)
		}
		if len(lunaPackages) == 0 {
			return 0
		}
		executable, lookupErr := lookPath("luna")
		if lookupErr != nil {
			fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
			return 127
		}
		for _, name := range lunaPackages {
			if entry, ok := catalogByName[strings.ToLower(name)]; ok {
				if status := registerCatalogPackage(entry, executable, homeDir, stdout, stderr, runCommand); status != 0 {
					return status
				}
			}
		}
		return runLunaCommand(executable, append([]string{"install"}, lunaPackages...), stdout, stderr, runCommand)

	case "remove":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "tell pkg remove: expected exactly one package name")
			printPackageUsage(stderr)
			return 2
		}
		native, recognized, parseErr := parseNativePackageSpec(operands[0])
		if parseErr != nil {
			fmt.Fprintf(stderr, "tell pkg remove: %v\n", parseErr)
			return 2
		}
		if recognized {
			return runNativePackageAction("remove", native, homeDir, lookPath, runCommand, stdout, stderr)
		}
		return runLunaCommandFromPath([]string{"remove", operands[0]}, stdout, stderr, lookPath, runCommand)

	case "list":
		if len(operands) != 0 {
			fmt.Fprintf(stderr, "tell pkg %s: this command does not accept arguments\n", command)
			printPackageUsage(stderr)
			return 2
		}
		return runNativeAndLunaList(homeDir, lookPath, runCommand, stdout, stderr)

	case "update":
		if len(operands) != 0 {
			fmt.Fprintf(stderr, "tell pkg %s: this command does not accept arguments\n", command)
			printPackageUsage(stderr)
			return 2
		}
		if status := runTrackedNativeUpdates(homeDir, lookPath, runCommand, stdout, stderr); status != 0 {
			return status
		}
		if _, lookupErr := lookPath("luna"); lookupErr != nil {
			packages, loadErr := loadInstalledNativePackages(homeDir)
			if loadErr == nil && len(packages) > 0 {
				return 0
			}
			fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
			return 127
		}
		return runLunaCommandFromPath([]string{"update"}, stdout, stderr, lookPath, runCommand)

	case "sync":
		if len(operands) > 1 {
			fmt.Fprintln(stderr, "tell pkg sync: expected at most one manifest path")
			printPackageUsage(stderr)
			return 2
		}
		manifest := "Lunafile"
		if len(operands) == 1 {
			manifest = operands[0]
		}
		info, statErr := os.Stat(manifest)
		if statErr != nil {
			fmt.Fprintf(stderr, "tell pkg sync: cannot read manifest %q: %v\n", manifest, statErr)
			return 2
		}
		if !info.Mode().IsRegular() {
			fmt.Fprintf(stderr, "tell pkg sync: manifest %q is not a regular file\n", manifest)
			return 2
		}
		return runLunaCommandFromPath([]string{"sync", manifest}, stdout, stderr, lookPath, runCommand)

	default:
		fmt.Fprintf(stderr, "tell pkg: unknown command %q\n", args[0])
		printPackageUsage(stderr)
		return 2
	}
}

func registerCatalogPackage(
	entry packageCatalogEntry,
	executable, homeDir string,
	stdout, stderr io.Writer,
	runCommand func(*exec.Cmd) error,
) int {
	if strings.TrimSpace(homeDir) == "" {
		fmt.Fprintln(stderr, "tell pkg: could not determine the home directory")
		return 1
	}
	recipePath := filepath.Join(homeDir, ".local", "lunaorigin", entry.Name+".json")
	if _, err := os.Stat(recipePath); err == nil {
		fmt.Fprintf(stdout, "%s is already registered with Luna; keeping the existing recipe\n", entry.Name)
		return 0
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "tell pkg: cannot inspect Luna recipe %q: %v\n", recipePath, err)
		return 1
	}

	fmt.Fprintf(stdout, "Registering %s from Tell's catalogue with Luna\n", entry.Name)
	args := []string{"add", entry.Name, entry.URL, entry.Compile, entry.Config, entry.Install}
	return runLunaCommand(executable, args, stdout, stderr, runCommand)
}

func runLunaCommandFromPath(
	args []string,
	stdout, stderr io.Writer,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
) int {
	executable, err := lookPath("luna")
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
		return 127
	}
	return runLunaCommand(executable, args, stdout, stderr, runCommand)
}

func runLunaCommand(
	executable string,
	args []string,
	stdout, stderr io.Writer,
	runCommand func(*exec.Cmd) error,
) int {
	process := exec.Command(executable, args...)
	process.Stdout = stdout
	process.Stderr = stderr
	err := runCommand(process)
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	fmt.Fprintf(stderr, "tell pkg: could not start Luna: %v\n", err)
	return 1
}
