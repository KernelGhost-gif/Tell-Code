package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/diagnostic"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

var version = "0.3.7"

func main() {
	check := flag.Bool("check", false, "parse the program without running it")
	typeCheck := flag.Bool("types", false, "check statically knowable type annotations (native runtime)")
	showVersion := flag.Bool("version", false, "print the Tell version")
	quietREPL := flag.Bool("quiet-repl", false, "omit terminal prompts and banner (for IDE integrations)")
	buildOutput := flag.String("output", "", "write compiled bytecode to this path")
	defaults := tell.DefaultExecutionLimits()
	maxCallDepth := flag.Int("max-call-depth", defaults.MaxFunctionCallDepth, "maximum nested Tell function calls")
	maxLoopIterations := flag.Int("max-loop-iterations", defaults.MaxLoopIterations, "maximum iterations in one loop or range")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Tell %s - a sentence-first programming language\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: tell [run|check|test|build|format|repl|new|init] [options] <program.tell|program.tbc|->\n       tell --version\n\nOptions:\n")
		flag.PrintDefaults()
	}
	args := os.Args[1:]
	replMode := len(args) > 0 && args[0] == "repl"
	formatMode := len(args) > 0 && args[0] == "format"
	buildMode := len(args) > 0 && args[0] == "build"
	testMode := len(args) > 0 && args[0] == "test"
	projectMode := ""
	if len(args) > 0 && (args[0] == "new" || args[0] == "init") {
		projectMode = args[0]
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "repl" {
		args = args[1:]
	}
	if formatMode {
		args = args[1:]
	}
	if buildMode {
		args = args[1:]
		var options, positional []string
		for i := 0; i < len(args); i++ {
			arg := args[i]
			if arg == "--output" || arg == "-output" {
				options = append(options, arg)
				if i+1 < len(args) {
					i++
					options = append(options, args[i])
				}
			} else if strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "-output=") {
				options = append(options, arg)
			} else {
				positional = append(positional, arg)
			}
		}
		args = append(options, positional...)
	}
	if testMode {
		args = args[1:]
	}
	if len(args) > 0 && (args[0] == "run" || args[0] == "check") {
		command := args[0]
		args = args[1:]
		if command == "check" {
			args = append([]string{"--check"}, args...)
		}
	}
	if err := flag.CommandLine.Parse(args); err != nil {
		exitWithDiagnostic(err, "<command-line>", diagnostic.UsageError)
	}
	if *showVersion {
		fmt.Printf("Tell %s (native Go runtime)\n", version)
		return
	}
	if *maxCallDepth < 1 || *maxLoopIterations < 1 {
		exitWithDiagnostic(fmt.Errorf("execution limits must be positive integers"), "<command-line>", diagnostic.UsageError)
	}
	limits := tell.ExecutionLimits{MaxFunctionCallDepth: *maxCallDepth, MaxLoopIterations: *maxLoopIterations}
	if projectMode != "" {
		if projectMode == "new" && flag.NArg() != 1 {
			exitWithDiagnostic(fmt.Errorf("tell new expects one project name"), "<command-line>", diagnostic.UsageError)
		}
		if projectMode == "init" && flag.NArg() > 1 {
			exitWithDiagnostic(fmt.Errorf("tell init accepts at most one project name"), "<command-line>", diagnostic.UsageError)
		}
		name := ""
		if flag.NArg() == 1 {
			name = flag.Arg(0)
		}
		if projectMode == "new" {
			directory, err := createProject(".", name)
			if err != nil {
				exitWithDiagnostic(err, "<project>", diagnostic.IOError)
			}
			fmt.Printf("Created Tell project %q in %s\n", name, directory)
		} else {
			if name == "" {
				cwd, err := os.Getwd()
				if err != nil {
					exitWithDiagnostic(err, "<project>", diagnostic.IOError)
				}
				name = projectNameFromDirectory(cwd)
			}
			if err := initializeProject(".", name); err != nil {
				exitWithDiagnostic(err, "<project>", diagnostic.IOError)
			}
			fmt.Printf("Initialized Tell project %q in the current directory\n", name)
		}
		return
	}
	if replMode {
		runREPL(limits, *quietREPL)
		return
	}
	if testMode {
		if flag.NArg() > 1 {
			exitWithDiagnostic(fmt.Errorf("tell test accepts at most one file or directory"), "<command-line>", diagnostic.UsageError)
		}
		target := "."
		if flag.NArg() == 1 {
			target = flag.Arg(0)
		}
		if status := runTests(target, limits, os.Stdout, os.Stderr); status != 0 {
			os.Exit(status)
		}
		return
	}
	if flag.NArg() == 0 {
		flag.Usage()
		return
	}
	filename := flag.Arg(0)
	var data []byte
	var err error
	if filename == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(filename)
	}
	if err != nil {
		exitWithDiagnostic(err, filename, diagnostic.IOError)
	}
	if formatMode {
		displayName := filename
		if filename == "-" {
			displayName = "<stdin>"
		}
		formatted, err := tell.Format(string(data), displayName)
		if err != nil {
			exitWithDiagnostic(err, displayName, diagnostic.SyntaxError)
		}
		if *check {
			if string(data) != formatted {
				fmt.Fprintf(os.Stderr, "Needs formatting: %s\n", displayName)
				os.Exit(1)
			}
			fmt.Printf("Already formatted: %s\n", displayName)
			return
		}
		if filename == "-" {
			fmt.Print(formatted)
			return
		}
		if string(data) != formatted {
			info, statErr := os.Stat(filename)
			if statErr != nil {
				exitWithDiagnostic(statErr, filename, diagnostic.IOError)
			}
			if err := os.WriteFile(filename, []byte(formatted), info.Mode().Perm()); err != nil {
				exitWithDiagnostic(err, filename, diagnostic.IOError)
			}
		}
		fmt.Printf("Formatted: %s\n", displayName)
		return
	}
	if buildMode {
		if filename == "-" && *buildOutput == "" {
			exitWithDiagnostic(fmt.Errorf("tell build - requires --output"), "<command-line>", diagnostic.UsageError)
		}
		outputPath := *buildOutput
		if outputPath == "" {
			outputPath = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".tbc"
		}
		compiled, err := tell.Compile(string(data), filename)
		if err != nil {
			exitWithDiagnostic(err, filename, diagnostic.SyntaxError)
		}
		if err := tell.WriteBytecode(outputPath, compiled); err != nil {
			exitWithDiagnostic(err, outputPath, diagnostic.IOError)
		}
		fmt.Printf("Built: %s\n", outputPath)
		return
	}
	if strings.EqualFold(filepath.Ext(filename), ".tbc") {
		if err := tell.RunBytecodeWithLimits(data, filename, os.Stdin, os.Stdout, limits); err != nil {
			exitWithDiagnostic(err, filename, diagnostic.RuntimeError)
		}
		return
	}
	if *check {
		displayName := filename
		if filename == "-" {
			displayName = "<stdin>"
		}
		checkSource := tell.Check
		if *typeCheck {
			checkSource = tell.Analyze
		}
		if err := checkSource(string(data), displayName); err != nil {
			kind := diagnostic.SyntaxError
			if *typeCheck {
				kind = diagnostic.TypeError
			}
			exitWithDiagnostic(err, displayName, kind)
		}
		fmt.Printf("Looks good: %s\n", displayName)
		return
	}
	runName := filename
	if filename == "-" {
		runName = "<stdin>"
	}
	if err := tell.NewSessionWithLimits(os.Stdin, os.Stdout, limits).Exec(string(data), runName); err != nil {
		exitWithDiagnostic(err, runName, diagnostic.RuntimeError)
	}
}

func exitWithDiagnostic(err error, filename string, kind diagnostic.Kind) {
	os.Exit(reportDiagnostic(os.Stderr, err, filename, kind))
}

func runTests(target string, limits tell.ExecutionLimits, output, diagnostics io.Writer) int {
	files, err := discoverTestFiles(target)
	if err != nil {
		fmt.Fprintf(diagnostics, "tell test: %v\n", err)
		return 2
	}
	if len(files) == 0 {
		fmt.Fprintf(diagnostics, "tell test: no *_test.tell files found under %s\n", target)
		return 1
	}
	passed, failed := 0, 0
	for _, filename := range files {
		source, readErr := os.ReadFile(filename)
		if readErr != nil {
			failed++
			fmt.Fprintf(output, "FAIL %s\n", displayTestPath(filename))
			fmt.Fprintf(diagnostics, "  cannot read test file: %v\n", readErr)
			continue
		}
		var programOutput bytes.Buffer
		session := tell.NewSessionWithLimits(strings.NewReader(""), &programOutput, limits)
		runErr := session.Exec(string(source), filename)
		if runErr != nil {
			failed++
			fmt.Fprintf(output, "FAIL %s\n", displayTestPath(filename))
			if programOutput.Len() > 0 {
				_, _ = output.Write(programOutput.Bytes())
			}
			fmt.Fprintf(diagnostics, "  %v\n", runErr)
			continue
		}
		passed++
		fmt.Fprintf(output, "PASS %s\n", displayTestPath(filename))
	}
	fmt.Fprintf(output, "Ran %d Tell test file(s): %d passed, %d failed.\n", passed+failed, passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func discoverTestFiles(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(target), ".tell") {
			return nil, fmt.Errorf("test file must have a .tell extension")
		}
		absolute, err := filepath.Abs(target)
		if err != nil {
			return nil, err
		}
		return []string{absolute}, nil
	}
	files := []string{}
	excludedDirectories := map[string]bool{".git": true, ".venv": true, "__pycache__": true, "node_modules": true, "vendor": true, "build": true, "dist": true}
	err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != target {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || excludedDirectories[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), "_test.tell") {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			files = append(files, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func displayTestPath(filename string) string {
	if relative, err := filepath.Rel(".", filename); err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative)
	}
	return filename
}

func runREPL(limits tell.ExecutionLimits, quiet bool) {
	reader := bufio.NewReader(os.Stdin)
	session := tell.NewSessionWithLimits(reader, os.Stdout, limits)
	session.SetEchoExpressions(true)
	if !quiet {
		fmt.Printf("Tell Interactive Shell\nTell %s (native Go runtime)\nType :help for commands, :exit to leave.\n", version)
	}
	var pending []string
	for {
		prompt := ">>> "
		if len(pending) > 0 {
			prompt = "... "
		}
		if !quiet {
			fmt.Print(prompt)
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if len(pending) > 0 {
				if parseErr := tell.Check(strings.Join(pending, "\n"), "<repl>"); parseErr != nil {
					fmt.Fprintln(os.Stderr, parseErr)
				}
			}
			fmt.Println()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if len(pending) == 0 {
			switch strings.ToLower(strings.TrimSpace(line)) {
			case ":exit", "exit", "quit":
				return
			case ":help", "help":
				fmt.Println("Enter Tell statements or complete blocks. Bare expressions print their result. Commands: :help, :clear, :exit.")
				continue
			case ":clear":
				if !quiet {
					fmt.Print("\033[2J\033[H")
				}
				continue
			}
		}
		pending = append(pending, line)
		source := strings.Join(pending, "\n")
		parseErr := tell.Check(source, "<repl>")
		if parseErr != nil && strings.Contains(parseErr.Error(), " needs 'End ") {
			continue
		}
		if parseErr != nil {
			fmt.Fprintln(os.Stderr, parseErr)
			pending = nil
			continue
		}
		if err := session.Exec(source, "<repl>"); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		pending = nil
		if err != nil {
			return
		}
	}
}
