package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/KernelGhost-gif/Tell-Code/native/internal/diagnostic"
	"github.com/KernelGhost-gif/Tell-Code/native/internal/tell"
)

var (
	runtimeLocation = regexp.MustCompile("^(.*):([0-9]+):([0-9]+):\\s*(.*)$")
	tokenLocation   = regexp.MustCompile("(?i)(?:at )?line ([0-9]+), column ([0-9]+)")
	lineLocation    = regexp.MustCompile("(?i)line ([0-9]+)")
)

// diagnosticFor translates legacy runtime errors into the stable CLI contract.
// The library continues to return errors; only the CLI formats and classifies them.
func diagnosticFor(err error, filename string, defaultKind diagnostic.Kind) *diagnostic.Error {
	if err == nil {
		return nil
	}
	var existing *diagnostic.Error
	if errors.As(err, &existing) {
		return existing
	}

	message := err.Error()
	line, column := 1, 1
	var runtimeError *tell.Error
	explicitKind := ""
	if errors.As(err, &runtimeError) {
		explicitKind = runtimeError.Kind
	}
	if match := runtimeLocation.FindStringSubmatch(message); match != nil {
		if match[1] != "" {
			filename = match[1]
		}
		fmt.Sscan(match[2], &line)
		fmt.Sscan(match[3], &column)
		message = match[4]
	} else if match := tokenLocation.FindStringSubmatch(message); match != nil {
		fmt.Sscan(match[1], &line)
		fmt.Sscan(match[2], &column)
		message = tokenLocation.ReplaceAllString(message, "")
		message = strings.TrimSpace(strings.TrimSuffix(message, "at"))
	} else if defaultKind == diagnostic.SyntaxError {
		if match := lineLocation.FindStringSubmatch(message); match != nil {
			fmt.Sscan(match[1], &line)
			message = lineLocation.ReplaceAllString(message, "")
			message = strings.TrimSpace(message)
		}
	}

	kind := defaultKind
	switch explicitKind {
	case "SyntaxError":
		kind = diagnostic.SyntaxError
	case "IOError":
		kind = diagnostic.IOError
	}
	folded := strings.ToLower(message)
	if strings.Contains(folded, "type mismatch") ||
		strings.Contains(folded, "type annotation") ||
		strings.Contains(folded, "requires a number") ||
		strings.Contains(folded, "requires numeric operands") {
		kind = diagnostic.TypeError
	} else if strings.Contains(folded, "i don't know what") ||
		strings.Contains(folded, "undefined name") ||
		strings.Contains(folded, "unknown name") {
		kind = diagnostic.NameError
	}
	if filename == "" {
		filename = "<input>"
	}
	return &diagnostic.Error{
		Kind: kind, Filename: filename, Line: line, Column: column, Message: message,
	}
}

func reportDiagnostic(writer io.Writer, err error, filename string, kind diagnostic.Kind) int {
	diagnosticError := diagnosticFor(err, filename, kind)
	if diagnosticError == nil {
		return 0
	}
	_, _ = fmt.Fprintln(writer, diagnosticError.Error())
	return diagnosticError.ExitCode()
}
