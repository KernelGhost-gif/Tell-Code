package main

import (
	"embed"
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
	Platforms    []string `json:"platforms"`
	Requirements []string `json:"requirements"`
	URL          string   `json:"url"`
	Config       string   `json:"config"`
	Compile      string   `json:"compile"`
	Install      string   `json:"install"`
	Executable   string   `json:"executable"`
}

// Keep the embed import explicit: the catalogue is shipped inside the Tell binary.
var _ embed.FS

func loadPackageCatalog() ([]packageCatalogEntry, error) {
	var entries []packageCatalogEntry
	if err := json.Unmarshal(packageRegistryJSON, &entries); err != nil {
		return nil, fmt.Errorf("read embedded package catalogue: %w", err)
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := strings.ToLower(strings.TrimSpace(entry.Name))
		if name == "" || entry.URL == "" || entry.Compile == "" || entry.Install == "" {
			return nil, fmt.Errorf("invalid package catalogue entry %q: name, url, compile, and install are required", entry.Name)
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
	fmt.Fprintln(writer, "Tell package commands (catalogue + Luna)")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Usage: tell pkg <command> [arguments]")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  search [query]             Search Tell's curated package catalogue")
	fmt.Fprintln(writer, "  info <name>                Show package details and requirements")
	fmt.Fprintln(writer, "  add <name>                 Register a catalogue recipe with Luna")
	fmt.Fprintln(writer, "  install <name> [name...]   Register catalogue entries if needed, then install")
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
		query := ""
		if len(operands) == 1 {
			query = strings.ToLower(strings.TrimSpace(operands[0]))
		}
		matches := 0
		for _, entry := range catalog {
			haystack := strings.ToLower(strings.Join([]string{entry.Name, entry.Description, entry.Category}, " "))
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			fmt.Fprintf(stdout, "  %-12s %s [%s]\n", entry.Name, entry.Description, strings.Join(entry.Platforms, ", "))
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
		fmt.Fprintf(stdout, "Platforms: %s (CI validation currently runs on Ubuntu Linux)\n", strings.Join(entry.Platforms, ", "))
		fmt.Fprintf(stdout, "Requirements: %s\n", strings.Join(entry.Requirements, ", "))
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
		executable, lookupErr := lookPath("luna")
		if lookupErr != nil {
			fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
			return 127
		}
		for _, name := range operands {
			if entry, ok := catalogByName[strings.ToLower(name)]; ok {
				if status := registerCatalogPackage(entry, executable, homeDir, stdout, stderr, runCommand); status != 0 {
					return status
				}
			}
		}
		return runLunaCommand(executable, append([]string{"install"}, operands...), stdout, stderr, runCommand)

	case "remove":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "tell pkg remove: expected exactly one package name")
			printPackageUsage(stderr)
			return 2
		}
		return runLunaCommandFromPath([]string{"remove", operands[0]}, stdout, stderr, lookPath, runCommand)

	case "list", "update":
		if len(operands) != 0 {
			fmt.Fprintf(stderr, "tell pkg %s: this command does not accept arguments\n", command)
			printPackageUsage(stderr)
			return 2
		}
		return runLunaCommandFromPath([]string{command}, stdout, stderr, lookPath, runCommand)

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
