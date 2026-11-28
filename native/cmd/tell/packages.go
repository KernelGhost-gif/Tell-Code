package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const lunaInstallURL = "https://github.com/Neonwave175/Luna"

func printPackageUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Tell package commands (powered by Luna)")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Usage: tell pkg <command> [arguments]")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Commands:")
	fmt.Fprintln(writer, "  install <name> [name...]  Install one or more packages")
	fmt.Fprintln(writer, "  remove <name>             Remove a package")
	fmt.Fprintln(writer, "  list                      List registered packages")
	fmt.Fprintln(writer, "  update                    Update installed packages")
	fmt.Fprintln(writer, "  sync [Lunafile]           Install packages from a Luna JSON manifest")
}

func runPackageCommand(args []string, stdout, stderr io.Writer) int {
	return runPackageCommandWith(
		args,
		stdout,
		stderr,
		exec.LookPath,
		func(command *exec.Cmd) error { return command.Run() },
	)
}

func runPackageCommandWith(
	args []string,
	stdout, stderr io.Writer,
	lookPath func(string) (string, error),
	runCommand func(*exec.Cmd) error,
) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printPackageUsage(stdout)
		return 0
	}

	command := strings.ToLower(args[0])
	operands := args[1:]
	lunaArgs := []string{command}

	switch command {
	case "install":
		if len(operands) == 0 {
			fmt.Fprintln(stderr, "tell pkg install: expected at least one package name")
			printPackageUsage(stderr)
			return 2
		}
		lunaArgs = append(lunaArgs, operands...)
	case "remove":
		if len(operands) != 1 {
			fmt.Fprintln(stderr, "tell pkg remove: expected exactly one package name")
			printPackageUsage(stderr)
			return 2
		}
		lunaArgs = append(lunaArgs, operands...)
	case "list", "update":
		if len(operands) != 0 {
			fmt.Fprintf(stderr, "tell pkg %s: this command does not accept arguments\n", command)
			printPackageUsage(stderr)
			return 2
		}
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
		info, err := os.Stat(manifest)
		if err != nil {
			fmt.Fprintf(stderr, "tell pkg sync: cannot read manifest %q: %v\n", manifest, err)
			return 2
		}
		if !info.Mode().IsRegular() {
			fmt.Fprintf(stderr, "tell pkg sync: manifest %q is not a regular file\n", manifest)
			return 2
		}
		lunaArgs = append(lunaArgs, manifest)
	default:
		fmt.Fprintf(stderr, "tell pkg: unknown command %q\n", args[0])
		printPackageUsage(stderr)
		return 2
	}

	executable, err := lookPath("luna")
	if err != nil {
		fmt.Fprintf(stderr, "tell pkg: Luna was not found on PATH. Install it from %s and try again.\n", lunaInstallURL)
		return 127
	}

	process := exec.Command(executable, lunaArgs...)
	process.Stdout = stdout
	process.Stderr = stderr
	err = runCommand(process)
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
