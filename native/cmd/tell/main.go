package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

var version = "0.2.0"

func main() {
	check := flag.Bool("check", false, "parse the program without running it")
	showVersion := flag.Bool("version", false, "print the Tell version")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Tell %s - a sentence-first programming language\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: tell [options] <program.tell>\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Printf("Tell %s (native Go runtime)\n", version)
		return
	}
	if flag.NArg() == 0 {
		flag.Usage()
		return
	}
	filename := flag.Arg(0)
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tell: %v\n", err)
		os.Exit(2)
	}
	if *check {
		if err := tell.Check(string(data), filename); err != nil {
			fmt.Fprintf(os.Stderr, "tell: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Looks good: %s\n", filepath.Clean(filename))
		return
	}
	if err := tell.Run(string(data), filename); err != nil {
		fmt.Fprintf(os.Stderr, "tell: %v\n", err)
		os.Exit(1)
	}
}
