package tell

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxLoopIterations = 1_000_000
const maxFunctionCallDepth = 64

// ExecutionLimits bounds loops, range construction, and nested Tell calls.
type ExecutionLimits struct {
	MaxFunctionCallDepth int
	MaxLoopIterations    int
}

func DefaultExecutionLimits() ExecutionLimits {
	return ExecutionLimits{MaxFunctionCallDepth: maxFunctionCallDepth, MaxLoopIterations: maxLoopIterations}
}

type Error struct{ Message string }

// tupleValue is an immutable, ordered Tell sequence distinct from mutable lists.
type tupleValue []any

func (e *Error) Error() string              { return e.Message }
func fail(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type node struct {
	kind, text               string
	line, column             int
	condition, name, base    string
	params                   []string
	body, otherwise, handler []node
	simple                   *simpleStatement
}

type simpleStatement struct {
	kind, source  string
	names         []string
	annotation    string
	constant      bool
	expression    expression
	target        expression
	hasExpression bool
}
type parser struct {
	lines    []string
	index    int
	filename string
}

func cleanLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(strings.ToLower(line), "note:") {
		return ""
	}
	quote := rune(0)
	escaped := false
	for i, r := range line {
		if quote != 0 {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
		} else if r == '\'' || r == '"' {
			quote = r
		} else if r == '#' {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}
func stripSentenceEnd(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(strings.TrimSuffix(s, "."))
}

func blockHeader(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasSuffix(line, ":") {
		return "", false
	}
	return strings.TrimSpace(line[:len(line)-1]), true
}

func prefixFold(text, prefix string) (string, bool) {
	if len(text) < len(prefix) || !strings.EqualFold(text[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(text[len(prefix):]), true
}

func splitFoldOnce(text, delimiter string) (string, string, bool) {
	quote := byte(0)
	escaped := false
	depth := 0
	for i := 0; i+len(delimiter) <= len(text); i++ {
		c := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.EqualFold(text[i:i+len(delimiter)], delimiter) {
			return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+len(delimiter):]), true
		}
	}
	return "", "", false
}

func firstWord(text string) (string, string) {
	text = strings.TrimSpace(text)
	end := 0
	for end < len(text) {
		r, width := utf8.DecodeRuneInString(text[end:])
		if !identifierRune(r) {
			break
		}
		end += width
	}
	return text[:end], strings.TrimSpace(text[end:])
}

func parseClassHeader(header string) (name, base string, ok bool) {
	rest, ok := prefixFold(header, "create a class called ")
	if !ok {
		return "", "", false
	}
	name, tail, hasBase := splitFoldOnce(rest, " based on ")
	if !hasBase {
		name = rest
	} else {
		base = tail
	}
	if !identifier(name) || (hasBase && !identifier(base)) {
		return "", "", false
	}
	return name, base, true
}

func parseParameterText(source string) (string, bool) {
	if remaining, ok := prefixFold(source, "all remaining values as "); ok {
		if !identifier(remaining) {
			return "", false
		}
		return "*" + remaining, true
	}
	name, value, hasDefault := splitFoldOnce(source, " defaulting to ")
	if !hasDefault {
		name = strings.TrimSpace(source)
		return name, identifier(name)
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !identifier(name) || value == "" {
		return "", false
	}
	return name + "=" + value, true
}

func parameterParts(parameter string) (name, defaultValue string, hasDefault, variadic, ok bool) {
	if strings.HasPrefix(parameter, "*") {
		name = strings.TrimPrefix(parameter, "*")
		return name, "", false, true, identifier(name)
	}
	name, defaultValue, hasDefault = splitFoldOnce(parameter, "=")
	name = strings.TrimSpace(name)
	defaultValue = strings.TrimSpace(defaultValue)
	if hasDefault {
		ok = identifier(name) && defaultValue != ""
	} else {
		name = strings.TrimSpace(parameter)
		ok = identifier(name)
	}
	return name, defaultValue, hasDefault, false, ok
}

func validParameters(parameters []string) bool {
	seen := make(map[string]bool, len(parameters))
	hasDefault := false
	variadicSeen := false
	for index, parameter := range parameters {
		name, _, optional, variadic, ok := parameterParts(parameter)
		if !ok || seen[name] || variadicSeen || (hasDefault && !optional && !variadic) || (variadic && index != len(parameters)-1) {
			return false
		}
		seen[name] = true
		hasDefault = hasDefault || optional
		variadicSeen = variadic
	}
	return true
}

func parseLoopNames(source string) ([]string, bool) {
	names := splitTop(source, ",")
	if len(names) == 0 {
		return nil, false
	}
	seen := make(map[string]bool, len(names))
	for index, name := range names {
		names[index] = strings.TrimSpace(name)
		if !identifier(names[index]) || seen[names[index]] {
			return nil, false
		}
		seen[names[index]] = true
	}
	return names, true
}

func parseSimpleStatement(source string, line, column int) *simpleStatement {
	parseExpression := func(expressionText string) (expression, bool) {
		expressionText = strings.TrimSpace(expressionText)
		if expressionText == "" {
			return nil, false
		}
		offset := strings.LastIndex(source, expressionText)
		if offset < 0 {
			offset = 0
		}
		parsed, err := parseTellExpressionAt(expressionText, line, column+utf8.RuneCountInString(source[:offset]))
		return parsed, err == nil
	}
	if rest, ok := prefixFold(source, "let "); ok {
		constant := false
		if tail, isConstant := prefixFold(rest, "constant "); isConstant {
			rest, constant = tail, true
		}
		binding, expressionText, separated := splitFoldOnce(rest, " be ")
		if separated {
			name, annotation, annotated := splitFoldOnce(binding, " as ")
			if !annotated {
				name = binding
			}
			if strings.Contains(binding, ",") && !annotated {
				if names, valid := parseLoopNames(binding); valid && len(names) > 1 {
					if parsed, validExpression := parseExpression(expressionText); validExpression {
						return &simpleStatement{kind: "let", names: names, constant: constant, expression: parsed, hasExpression: true}
					}
				}
			} else if identifier(name) {
				if parsed, validExpression := parseExpression(expressionText); validExpression {
					return &simpleStatement{kind: "let", names: []string{name}, annotation: annotation, constant: constant, expression: parsed, hasExpression: true}
				}
			}
		}
	}
	if rest, ok := prefixFold(source, "set "); ok {
		name, expressionText, separated := splitFoldOnce(rest, " to ")
		if separated {
			if strings.Contains(name, ",") {
				if names, valid := parseLoopNames(name); valid && len(names) > 1 {
					if parsed, validExpression := parseExpression(expressionText); validExpression {
						return &simpleStatement{kind: "set", names: names, expression: parsed, hasExpression: true}
					}
				}
			} else if identifier(name) {
				if parsed, validExpression := parseExpression(expressionText); validExpression {
					return &simpleStatement{kind: "set", names: []string{name}, expression: parsed, hasExpression: true}
				}
			} else if parsedTarget, validTarget := parseExpression(name); validTarget {
				if _, isMember := parsedTarget.(memberExpression); isMember {
					if parsedValue, validValue := parseExpression(expressionText); validValue {
						return &simpleStatement{kind: "set_member", source: name, target: parsedTarget, expression: parsedValue, hasExpression: true}
					}
				}
			}
		}
	}
	makeExpression := func(kind, prefix string) *simpleStatement {
		expressionText, ok := prefixFold(source, prefix)
		if !ok || strings.TrimSpace(expressionText) == "" {
			return nil
		}
		expressionText = strings.TrimSpace(expressionText)
		offset := strings.LastIndex(source, expressionText)
		if offset < 0 {
			offset = 0
		}
		parsed, err := parseTellExpressionAt(expressionText, line, column+utf8.RuneCountInString(source[:offset]))
		if err != nil {
			return nil
		}
		return &simpleStatement{kind: kind, source: expressionText, expression: parsed, hasExpression: true}
	}
	for _, prefix := range []string{"say ", "show ", "print "} {
		if statement := makeExpression("print", prefix); statement != nil {
			return statement
		}
	}
	if statement := makeExpression("check", "check that "); statement != nil {
		return statement
	}
	for _, prefix := range []string{"give back ", "return "} {
		if statement := makeExpression("return", prefix); statement != nil {
			return statement
		}
	}
	if strings.EqualFold(source, "give back") || strings.EqualFold(source, "return") {
		return &simpleStatement{kind: "return"}
	}
	if statement := makeExpression("raise", "raise an error saying "); statement != nil {
		return statement
	}
	if rest, ok := prefixFold(source, "tell "); ok {
		target, methodText, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(target) {
			method, tail := firstWord(methodText)
			if identifier(method) {
				arguments := []callArgument{}
				valid := true
				if tail != "" {
					argumentText, hasWith := prefixFold(tail, "with ")
					if !hasWith {
						valid = false
					} else {
						for _, argument := range splitAnd(argumentText) {
							parsed, ok := parseExpression(argument)
							if !ok {
								valid = false
								break
							}
							arguments = append(arguments, callArgument{value: parsed})
						}
					}
				}
				if valid {
					callee := memberExpression{value: nameExpression{name: target}, member: method}
					return &simpleStatement{kind: "expression", source: source, expression: callExpression{callee: callee, args: arguments}, hasExpression: true}
				}
			}
		}
	}
	if strings.EqualFold(source, "stop this loop") || strings.EqualFold(source, "break out of this loop") {
		return &simpleStatement{kind: "break"}
	}
	if strings.EqualFold(source, "skip to the next repetition") || strings.EqualFold(source, "continue to the next repetition") {
		return &simpleStatement{kind: "continue"}
	}
	if source != "" {
		if parsed, err := parseTellExpressionAt(source, line, column); err == nil {
			return &simpleStatement{kind: "expression", source: source, expression: parsed, hasExpression: true}
		}
	}
	return nil
}

func bindLoopValues(target *env, names []string, value any) error {
	if len(names) == 1 {
		target.set(names[0], value)
		return nil
	}
	values, ok := sequenceValues(value)
	if !ok {
		return fail("for each expects %d values, got a non-list item", len(names))
	}
	if len(values) != len(names) {
		return fail("for each expects %d values, got %d", len(names), len(values))
	}
	for index, name := range names {
		target.set(name, values[index])
	}
	return nil
}

func sequenceValues(value any) ([]any, bool) {
	switch values := value.(type) {
	case []any:
		return values, true
	case tupleValue:
		return []any(values), true
	default:
		return nil, false
	}
}

func parseConstructorHeader(header string) (name string, params []string, ok bool) {
	rest, ok := prefixFold(header, "when creating ")
	if !ok {
		return "", nil, false
	}
	articleRest := rest
	rest, hasArticle := prefixFold(articleRest, "an ")
	if !hasArticle {
		rest, hasArticle = prefixFold(articleRest, "a ")
	}
	if !hasArticle {
		return "", nil, false
	}
	name, tail := firstWord(rest)
	if !identifier(name) {
		return "", nil, false
	}
	if tail != "" {
		tail, ok = prefixFold(tail, "with ")
		if !ok {
			return "", nil, false
		}
		params = splitAnd(tail)
		for index, param := range params {
			parsed, valid := parseParameterText(param)
			if !valid {
				return "", nil, false
			}
			params[index] = parsed
		}
		if !validParameters(params) {
			return "", nil, false
		}
	}
	return name, params, true
}

func parseFunctionHeader(header string) (name string, params []string, ok bool) {
	rest, ok := prefixFold(header, "to ")
	if !ok {
		rest, ok = prefixFold(header, "define a function called ")
	}
	if !ok {
		return "", nil, false
	}
	name, tail := firstWord(rest)
	if !identifier(name) {
		return "", nil, false
	}
	if strings.HasPrefix(tail, "(") && strings.HasSuffix(tail, ")") {
		inner := strings.TrimSpace(tail[1 : len(tail)-1])
		if inner != "" {
			params = splitTop(inner, ",")
		}
	} else if tail != "" {
		tail, ok = prefixFold(tail, "with ")
		if !ok {
			return "", nil, false
		}
		params = splitAnd(tail)
	}
	for index, param := range params {
		parsed, valid := parseParameterText(param)
		if !valid {
			return "", nil, false
		}
		params[index] = parsed
	}
	if !validParameters(params) {
		return "", nil, false
	}
	return name, params, true
}

func (p *parser) ifBlock(condition string, lineNo int) (node, error) {
	body, stop, err := p.block(map[string]bool{
		"otherwise:": true, "otherwise-if": true, "end if.": true,
	})
	if err != nil {
		return node{}, err
	}
	result := node{kind: "if", condition: strings.TrimSpace(condition), body: body, line: lineNo, column: p.columnAt(lineNo)}
	if stop == "end if." {
		return result, nil
	}
	if stop == "otherwise:" {
		other, otherStop, err := p.block(map[string]bool{"otherwise-if": true, "end if.": true})
		if err != nil {
			return node{}, err
		}
		if strings.HasPrefix(strings.ToLower(otherStop), "otherwise if ") {
			nestedHeader, _ := blockHeader(otherStop)
			nestedCondition, ok := prefixFold(nestedHeader, "otherwise if ")
			if !ok {
				return node{}, fail("invalid Otherwise if block on line %d", p.index)
			}
			nested, err := p.ifBlock(nestedCondition, p.index)
			if err != nil {
				return node{}, err
			}
			result.otherwise = []node{nested}
			return result, nil
		}
		if otherStop != "end if." {
			return node{}, fail("if block on line %d needs 'End if.'", lineNo)
		}
		result.otherwise = other
		return result, nil
	}
	if strings.HasPrefix(strings.ToLower(stop), "otherwise if ") {
		nestedHeader, _ := blockHeader(stop)
		nestedCondition, ok := prefixFold(nestedHeader, "otherwise if ")
		if !ok {
			return node{}, fail("invalid Otherwise if block on line %d", p.index)
		}
		nested, err := p.ifBlock(nestedCondition, p.index)
		if err != nil {
			return node{}, err
		}
		result.otherwise = []node{nested}
		return result, nil
	}
	return node{}, fail("if block on line %d needs 'End if.'", lineNo)
}

func (p *parser) columnAt(lineNo int) int {
	if lineNo < 1 || lineNo > len(p.lines) {
		return 1
	}
	line := p.lines[lineNo-1]
	trimmed := strings.TrimLeft(line, " \t")
	return utf8.RuneCountInString(line[:len(line)-len(trimmed)]) + 1
}

func (p *parser) block(stops map[string]bool) ([]node, string, error) {
	var out []node
	for p.index < len(p.lines) {
		raw := p.lines[p.index]
		lineNo := p.index + 1
		p.index++
		line := cleanLine(raw)
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		if stops["otherwise-if"] && strings.HasPrefix(low, "otherwise if ") && strings.HasSuffix(line, ":") {
			return out, line, nil
		}
		if stops[low] {
			return out, low, nil
		}
		header, hasHeader := blockHeader(line)
		if low == "end if." || low == "end while." || low == "end repeat." || low == "end loop." || low == "end function." || low == "end try." || low == "end constructor." || low == "end class." || low == "otherwise:" || low == "if something goes wrong:" {
			return nil, "", fail("unexpected block ending %q on line %d", line, lineNo)
		}
		if name, base, ok := parseClassHeader(header); hasHeader && ok {
			body, stop, err := p.block(map[string]bool{"end class.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end class." {
				return nil, "", fail("class %q on line %d needs 'End class.'", name, lineNo)
			}
			for _, member := range body {
				if member.kind != "function" && member.kind != "constructor" {
					return nil, "", fail("class %q can contain only methods and a constructor", name)
				}
			}
			out = append(out, node{kind: "class", name: name, base: base, body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		if name, params, ok := parseConstructorHeader(header); hasHeader && ok {
			body, stop, err := p.block(map[string]bool{"end constructor.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end constructor." {
				return nil, "", fail("constructor on line %d needs 'End constructor.'", lineNo)
			}
			out = append(out, node{kind: "constructor", name: name, params: params, body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		if condition, ok := prefixFold(header, "if "); hasHeader && ok && condition != "" {
			conditional, err := p.ifBlock(condition, lineNo)
			if err != nil {
				return nil, "", err
			}
			out = append(out, conditional)
			continue
		}
		if condition, ok := prefixFold(header, "while "); hasHeader && ok && condition != "" {
			body, stop, err := p.block(map[string]bool{"end while.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end while." {
				return nil, "", fail("while block on line %d needs 'End while.'", lineNo)
			}
			out = append(out, node{kind: "while", condition: strings.TrimSpace(condition), body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		repeatHeader, repeatOK := prefixFold(header, "repeat ")
		repeatEnd := strings.LastIndex(strings.ToLower(repeatHeader), " times")
		if hasHeader && repeatOK && repeatEnd > 0 && strings.EqualFold(strings.TrimSpace(repeatHeader[repeatEnd:]), "times") {
			body, stop, err := p.block(map[string]bool{"end repeat.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end repeat." {
				return nil, "", fail("repeat block on line %d needs 'End repeat.'", lineNo)
			}
			out = append(out, node{kind: "repeat", condition: strings.TrimSpace(repeatHeader[:repeatEnd]), body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		forHeader, forOK := prefixFold(header, "for each ")
		loopName, items, hasIn := splitFoldOnce(forHeader, " in ")
		loopNames, validNames := parseLoopNames(loopName)
		if hasHeader && forOK && hasIn && validNames && items != "" {
			body, stop, err := p.block(map[string]bool{"end loop.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end loop." {
				return nil, "", fail("for-each block on line %d needs 'End loop.'", lineNo)
			}
			out = append(out, node{kind: "for", name: strings.Join(loopNames, ","), condition: items, body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		if name, params, ok := parseFunctionHeader(header); hasHeader && ok {
			body, stop, err := p.block(map[string]bool{"end function.": true})
			if err != nil {
				return nil, "", err
			}
			if stop != "end function." {
				return nil, "", fail("function %q on line %d needs 'End function.'", name, lineNo)
			}
			out = append(out, node{kind: "function", name: name, params: params, body: body, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		if low == "try:" {
			body, stop, err := p.block(map[string]bool{"if something goes wrong:": true, "end try.": true})
			if err != nil {
				return nil, "", err
			}
			var handler []node
			if stop == "if something goes wrong:" {
				handler, stop, err = p.block(map[string]bool{"end try.": true})
				if err != nil {
					return nil, "", err
				}
			}
			if stop != "end try." {
				return nil, "", fail("try block on line %d needs 'End try.'", lineNo)
			}
			out = append(out, node{kind: "try", body: body, handler: handler, line: lineNo, column: p.columnAt(lineNo)})
			continue
		}
		text := stripSentenceEnd(line)
		column := p.columnAt(lineNo)
		out = append(out, node{kind: "statement", text: text, simple: parseSimpleStatement(text, lineNo, column), line: lineNo, column: column})
	}
	return out, "", nil
}
func parse(source, filename string) ([]node, error) {
	p := &parser{lines: strings.Split(source, "\n"), filename: filename}
	n, stop, err := p.block(map[string]bool{})
	if err != nil {
		return nil, err
	}
	if stop != "" {
		return nil, fail("unexpected %q", stop)
	}
	return n, nil
}

var (
	reIdentifier = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*$`)
)

func identifier(s string) bool   { return reIdentifier.MatchString(s) }
func splitAnd(s string) []string { return splitTop(s, " and ") }
func splitTop(s, sep string) []string {
	var out []string
	quote := byte(0)
	escaped := false
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
		}
		if c == ')' || c == ']' || c == '}' {
			depth--
		}
		if depth == 0 && i+len(sep) <= len(s) && strings.EqualFold(s[i:i+len(sep)], sep) {
			out = append(out, strings.TrimSpace(s[start:i]))
			i += len(sep) - 1
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

type env struct {
	values    map[string]any
	parent    *env
	constants map[string]bool
	types     map[string]string
}

func newEnv(parent *env) *env {
	return &env{values: map[string]any{}, parent: parent, constants: map[string]bool{}, types: map[string]string{}}
}
func (e *env) get(name string) (any, error) {
	if v, ok := e.values[name]; ok {
		return v, nil
	}
	if e.parent != nil {
		return e.parent.get(name)
	}
	return nil, fail("I don't know what %q means. Did you define it first?", name)
}
func (e *env) set(name string, v any) { e.values[name] = v }

type callable func([]any) (any, error)
type namedCallable func([]any, map[string]any) (any, error)
type userFunction struct {
	name    string
	params  []string
	body    []node
	closure *env
	runtime *runtime
}
type userClass struct {
	name        string
	methods     map[string]*userFunction
	constructor *userFunction
	base        *userClass
}
type userInstance struct {
	class  *userClass
	fields map[string]any
}
type signal struct {
	kind  string
	value any
}

func (s signal) Error() string { return s.kind }

type runtime struct {
	filename        string
	global          *env
	reader          *bufio.Reader
	output          io.Writer
	echoResults     bool
	callDepth       int
	limits          ExecutionLimits
	modules         map[string]map[string]any
	bytecodeModules map[string]BytecodeProgram
	loading         map[string]bool
}

func typeMatches(typeName string, value any, e *env) (bool, error) {
	switch strings.ToLower(typeName) {
	case "any":
		return true, nil
	case "nothing":
		return value == nil, nil
	case "boolean":
		_, ok := value.(bool)
		return ok, nil
	case "number":
		switch value.(type) {
		case float64, int, int64:
			return true, nil
		}
		return false, nil
	case "text":
		_, ok := value.(string)
		return ok, nil
	case "list":
		_, ok := value.([]any)
		return ok, nil
	case "tuple":
		_, ok := value.(tupleValue)
		return ok, nil
	case "dictionary":
		_, ok := value.(map[string]any)
		return ok, nil
	case "function":
		switch value.(type) {
		case callable, namedCallable, *userFunction:
			return true, nil
		}
		return false, nil
	case "class":
		switch value.(type) {
		case *userClass, *bytecodeClassValue:
			return true, nil
		default:
			return false, nil
		}
	default:
		candidate, err := e.get(typeName)
		if err != nil {
			return false, fail("unknown type annotation %q", typeName)
		}
		switch class := candidate.(type) {
		case *userClass:
			instance, ok := value.(*userInstance)
			if !ok {
				return false, nil
			}
			for current := instance.class; current != nil; current = current.base {
				if current == class {
					return true, nil
				}
			}
			return false, nil
		case *bytecodeClassValue:
			instance, ok := value.(*bytecodeInstance)
			if !ok {
				return false, nil
			}
			for current := instance.class; current != nil; current = current.base {
				if current == class {
					return true, nil
				}
			}
			return false, nil
		default:
			return false, fail("%q is not a type", typeName)
		}
	}
}

func (r *runtime) checkType(annotation string, value any, e *env, name string) error {
	match, err := typeMatches(annotation, value, e)
	if err != nil {
		return err
	}
	if !match {
		return fail("type mismatch for %q: expected %s, got %s", name, annotation, typeName(value))
	}
	return nil
}

func (r *runtime) declare(e *env, name string, value any, typeName string, constant bool) error {
	if e.constants[name] {
		return fail("constant %q cannot be redeclared", name)
	}
	if typeName != "" {
		if previous, exists := e.types[name]; exists && !strings.EqualFold(previous, typeName) {
			return fail("%q is already annotated as %s", name, previous)
		}
		if err := r.checkType(typeName, value, e, name); err != nil {
			return err
		}
		e.types[name] = typeName
	}
	if constant {
		e.constants[name] = true
	}
	e.set(name, value)
	return nil
}

func (r *runtime) assign(e *env, name string, value any) error {
	if e.constants[name] {
		return fail("constant %q cannot be reassigned", name)
	}
	if typeName, ok := e.types[name]; ok {
		if err := r.checkType(typeName, value, e, name); err != nil {
			return err
		}
	}
	e.set(name, value)
	return nil
}

func (r *runtime) assignMany(e *env, names []string, values []any, constant bool) error {
	if len(names) != len(values) {
		return fail("multiple assignment expected %d values, got %d", len(names), len(values))
	}
	for index, name := range names {
		if constant {
			if e.constants[name] {
				return fail("constant %q cannot be redeclared", name)
			}
			if annotation, ok := e.types[name]; ok {
				if err := r.checkType(annotation, values[index], e, name); err != nil {
					return err
				}
			}
			continue
		}
		if e.constants[name] {
			return fail("constant %q cannot be reassigned", name)
		}
		if annotation, ok := e.types[name]; ok {
			if err := r.checkType(annotation, values[index], e, name); err != nil {
				return err
			}
		}
	}
	for index, name := range names {
		if constant {
			if err := r.declare(e, name, values[index], "", true); err != nil {
				return err
			}
		} else {
			e.set(name, values[index])
		}
	}
	return nil
}

func newRuntime(filename string, input io.Reader, output io.Writer, limits ExecutionLimits) *runtime {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	reader, ok := input.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(input)
	}
	defaults := DefaultExecutionLimits()
	if limits.MaxFunctionCallDepth <= 0 {
		limits.MaxFunctionCallDepth = defaults.MaxFunctionCallDepth
	}
	if limits.MaxLoopIterations <= 0 {
		limits.MaxLoopIterations = defaults.MaxLoopIterations
	}
	r := &runtime{filename: filename, reader: reader, output: output, limits: limits, modules: make(map[string]map[string]any), loading: make(map[string]bool)}
	r.global = newEnv(nil)
	r.installBuiltins(r.global)
	return r
}
func Check(source, filename string) error { _, err := parse(source, filename); return err }

// Session executes multiple source units in one persistent global environment.
type Session struct{ runtime *runtime }

// SetEchoExpressions makes bare expression statements display their result.
// Interactive shells enable this; file execution leaves it disabled.
func (s *Session) SetEchoExpressions(enabled bool) { s.runtime.echoResults = enabled }

func NewSession(input io.Reader, output io.Writer) *Session {
	return NewSessionWithLimits(input, output, DefaultExecutionLimits())
}

func NewSessionWithLimits(input io.Reader, output io.Writer, limits ExecutionLimits) *Session {
	return &Session{runtime: newRuntime("<memory>", input, output, limits)}
}

func (s *Session) Exec(source, filename string) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	r := s.runtime
	r.filename = filename
	err = r.execute(nodes, r.global)
	if err != nil {
		var signalErr signal
		if errors.As(err, &signalErr) {
			switch signalErr.kind {
			case "return":
				return fail("give back can only be used inside a function")
			case "break", "continue":
				return fail("loop control can only be used inside a loop")
			}
		}
	}
	return err
}

func Run(source, filename string) error {
	return RunWithIO(source, filename, os.Stdin, os.Stdout)
}
func RunWithIO(source, filename string, input io.Reader, output io.Writer) error {
	return NewSession(input, output).Exec(source, filename)
}
func (r *runtime) installBuiltins(e *env) {
	wrap := func(f func([]any) (any, error)) any { return callable(f) }
	e.set("len", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("len expects one argument")
		}
		switch v := a[0].(type) {
		case string:
			return float64(len([]rune(v))), nil
		case []any:
			return float64(len(v)), nil
		case tupleValue:
			return float64(len(v)), nil
		case map[string]any:
			return float64(len(v)), nil
		}
		return nil, fail("this value has no length")
	}))
	e.set("sum", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("sum expects one list")
		}
		xs, ok := a[0].([]any)
		if !ok {
			return nil, fail("sum expects a list")
		}
		var n float64
		for _, x := range xs {
			v, err := numberValue(x)
			if err != nil {
				return nil, err
			}
			n += v
		}
		return n, nil
	}))
	e.set("tuple", wrap(func(a []any) (any, error) {
		if len(a) == 0 {
			return tupleValue{}, nil
		}
		if len(a) != 1 {
			return nil, fail("tuple expects zero or one iterable")
		}
		switch values := a[0].(type) {
		case []any:
			return tupleValue(append([]any(nil), values...)), nil
		case tupleValue:
			return tupleValue(append([]any(nil), values...)), nil
		case string:
			items := make(tupleValue, 0, utf8.RuneCountInString(values))
			for _, character := range values {
				items = append(items, string(character))
			}
			return items, nil
		default:
			return nil, fail("tuple expects a list, tuple, or text")
		}
	}))
	e.set("min", wrap(func(a []any) (any, error) { return numericExtreme(a, false) }))
	e.set("max", wrap(func(a []any) (any, error) { return numericExtreme(a, true) }))
	e.set("abs", wrap(unaryMath(math.Abs)))
	e.set("sqrt", wrap(unaryMath(math.Sqrt)))
	e.set("floor", wrap(unaryMath(math.Floor)))
	e.set("ceiling", wrap(unaryMath(math.Ceil)))
	e.set("round", wrap(func(a []any) (any, error) {
		if len(a) < 1 || len(a) > 2 {
			return nil, fail("round expects one or two arguments")
		}
		n, err := numberValue(a[0])
		if err != nil {
			return nil, err
		}
		digits := 0.0
		if len(a) == 2 {
			digits, _ = numberValue(a[1])
		}
		p := math.Pow10(int(digits))
		return math.Round(n*p) / p, nil
	}))
	e.set("number", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("number expects one argument")
		}
		return numberValue(a[0])
	}))
	e.set("int", e.values["number"])
	e.set("float", e.values["number"])
	e.set("str", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("str expects one argument")
		}
		return display(a[0]), nil
	}))
	e.set("bool", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("bool expects one argument")
		}
		return truthy(a[0]), nil
	}))
	e.set("first", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "first")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("first cannot be used on an empty list")
		}
		return xs[0], nil
	}))
	e.set("last", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "last")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("last cannot be used on an empty list")
		}
		return xs[len(xs)-1], nil
	}))
	e.set("average", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "average")
		if err != nil {
			return nil, err
		}
		if len(xs) == 0 {
			return nil, fail("average needs at least one value")
		}
		var s float64
		for _, x := range xs {
			n, e := numberValue(x)
			if e != nil {
				return nil, e
			}
			s += n
		}
		return s / float64(len(xs)), nil
	}))
	e.set("sorted", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "sorted")
		if err != nil {
			return nil, err
		}
		out := append([]any(nil), xs...)
		sort.SliceStable(out, func(i, j int) bool { return compare(out[i], out[j]) < 0 })
		return out, nil
	}))
	e.set("reversed", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "reversed")
		if err != nil {
			return nil, err
		}
		out := append([]any(nil), xs...)
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
		return out, nil
	}))
	e.set("enumerate", wrap(func(a []any) (any, error) {
		xs, err := oneList(a, "enumerate")
		if err != nil {
			return nil, err
		}
		out := make([]any, len(xs))
		for index, value := range xs {
			out[index] = []any{float64(index), value}
		}
		return out, nil
	}))
	e.set("map", wrap(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("map expects a function and a list")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("map expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("map expects a list as its second argument")
		}
		out := make([]any, len(values))
		for index, value := range values {
			mapped, err := invokeNamed(a[0], []any{value}, nil)
			if err != nil {
				return nil, err
			}
			out[index] = mapped
		}
		return out, nil
	}))
	e.set("filter", wrap(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("filter expects a function and a list")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("filter expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("filter expects a list as its second argument")
		}
		out := make([]any, 0, len(values))
		for _, value := range values {
			keep, err := invokeNamed(a[0], []any{value}, nil)
			if err != nil {
				return nil, err
			}
			if truthy(keep) {
				out = append(out, value)
			}
		}
		return out, nil
	}))
	e.set("reduce", wrap(func(a []any) (any, error) {
		if len(a) != 3 {
			return nil, fail("reduce expects a function, a list, and an initial value")
		}
		if !isCallableValue(a[0]) {
			return nil, fail("reduce expects a function as its first argument")
		}
		values, ok := a[1].([]any)
		if !ok {
			return nil, fail("reduce expects a list as its second argument")
		}
		result := a[2]
		for _, value := range values {
			next, err := invokeNamed(a[0], []any{result, value}, nil)
			if err != nil {
				return nil, err
			}
			result = next
		}
		return result, nil
	}))
	e.set("range", wrap(func(a []any) (any, error) {
		if len(a) < 1 || len(a) > 3 {
			return nil, fail("range expects one to three arguments")
		}
		nums := make([]int, len(a))
		for i, x := range a {
			n, err := numberValue(x)
			if err != nil {
				return nil, err
			}
			nums[i] = int(n)
		}
		start, end, step := 0, nums[0], 1
		if len(nums) >= 2 {
			start, end = nums[0], nums[1]
		}
		if len(nums) == 3 {
			step = nums[2]
		}
		if step == 0 {
			return nil, fail("range step cannot be zero")
		}
		out := []any{}
		for i := start; (step > 0 && i < end) || (step < 0 && i > end); i += step {
			if len(out) >= r.limits.MaxLoopIterations {
				return nil, fail("range is too large")
			}
			out = append(out, float64(i))
		}
		return out, nil
	}))
	e.set("random_number", wrap(func(a []any) (any, error) {
		lo, hi := 0, 100
		if len(a) > 2 {
			return nil, fail("random_number expects zero, one, or two arguments")
		}
		if len(a) > 0 {
			n, er := numberValue(a[0])
			if er != nil {
				return nil, er
			}
			lo = int(n)
		}
		if len(a) > 1 {
			n, er := numberValue(a[1])
			if er != nil {
				return nil, er
			}
			hi = int(n)
		}
		if hi < lo {
			return nil, fail("random_number upper bound must not be smaller than lower bound")
		}
		return float64(lo + rand.Intn(hi-lo+1)), nil
	}))
	e.set("type_of", wrap(func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("type_of expects one argument")
		}
		return typeName(a[0]), nil
	}))
}
func unaryMath(f func(float64) float64) func([]any) (any, error) {
	return func(a []any) (any, error) {
		if len(a) != 1 {
			return nil, fail("this function expects one argument")
		}
		n, e := numberValue(a[0])
		if e != nil {
			return nil, e
		}
		return f(n), nil
	}
}
func oneList(a []any, name string) ([]any, error) {
	if len(a) != 1 {
		return nil, fail("%s expects one list", name)
	}
	xs, ok := a[0].([]any)
	if !ok {
		return nil, fail("%s expects a list", name)
	}
	return xs, nil
}
func numericExtreme(a []any, wantMax bool) (any, error) {
	if len(a) == 1 {
		if xs, ok := a[0].([]any); ok {
			a = xs
		}
	}
	if len(a) == 0 {
		return nil, fail("min/max needs at least one value")
	}
	best, err := numberValue(a[0])
	if err != nil {
		return nil, err
	}
	for _, x := range a[1:] {
		n, e := numberValue(x)
		if e != nil {
			return nil, e
		}
		if (wantMax && n > best) || (!wantMax && n < best) {
			best = n
		}
	}
	return best, nil
}
func numberValue(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case string:
		f, e := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if e != nil {
			return 0, fail("%q is not a number", n)
		}
		return f, nil
	}
	return 0, fail("%s cannot be used as a number", typeName(v))
}
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case tupleValue:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "nothing"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "text"
	case []any:
		return "list"
	case tupleValue:
		return "tuple"
	case map[string]any:
		return "dictionary"
	case callable, namedCallable, *userFunction:
		return "function"
	case *userClass:
		return "class"
	case *userInstance:
		return v.(*userInstance).class.name
	case *bytecodeClassValue:
		return "class"
	case *bytecodeInstance:
		return v.(*bytecodeInstance).class.name
	}
	return "value"
}
func display(v any) string {
	switch x := v.(type) {
	case nil:
		return "nothing"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if math.IsNaN(x) {
			return "NaN"
		}
		if math.IsInf(x, 1) {
			return "infinity"
		}
		if math.IsInf(x, -1) {
			return "-infinity"
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i, v := range x {
			parts[i] = display(v)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case tupleValue:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = display(item)
		}
		if len(parts) == 1 {
			return "(" + parts[0] + ",)"
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case map[string]any:
		b, e := json.Marshal(x)
		if e == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	default:
		return fmt.Sprint(v)
	}
}
func compare(a, b any) int {
	if an, e := numberValue(a); e == nil {
		if bn, e2 := numberValue(b); e2 == nil {
			if an < bn {
				return -1
			}
			if an > bn {
				return 1
			}
			return 0
		}
	}
	as, bs := display(a), display(b)
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

func (r *runtime) execute(nodes []node, e *env) error {
	for _, n := range nodes {
		if err := r.execNode(n, e); err != nil {
			var s signal
			if errors.As(err, &s) {
				return err
			}
			return fail("%s:%d:%d: %v", r.filename, n.line, n.column, err)
		}
	}
	return nil
}
func (r *runtime) execNode(n node, e *env) error {
	switch n.kind {
	case "if":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		if truthy(v) {
			return r.execute(n.body, e)
		}
		return r.execute(n.otherwise, e)
	case "while":
		iterations := 0
		for {
			v, err := r.eval(n.condition, e)
			if err != nil {
				return err
			}
			if !truthy(v) {
				return nil
			}
			if iterations >= r.limits.MaxLoopIterations {
				return fail("this loop exceeded the configured limit of %d iterations", r.limits.MaxLoopIterations)
			}
			iterations++
			err = r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					return nil
				}
				return err
			}
		}
	case "repeat":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		f, err := numberValue(v)
		if err != nil {
			return err
		}
		count := int(f)
		if count < 0 || count > r.limits.MaxLoopIterations {
			return fail("repeat count must be between 0 and %d", r.limits.MaxLoopIterations)
		}
		for i := 1; i <= count; i++ {
			e.set("_repeat_index", float64(i))
			err := r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					break
				}
				return err
			}
		}
		return nil
	case "for":
		v, err := r.eval(n.condition, e)
		if err != nil {
			return err
		}
		items, ok := v.([]any)
		if tuple, isTuple := v.(tupleValue); isTuple {
			items, ok = []any(tuple), true
		}
		if !ok {
			if s, yes := v.(string); yes {
				items = make([]any, 0, r.limits.MaxLoopIterations+1)
				for _, c := range s {
					items = append(items, string(c))
					if len(items) > r.limits.MaxLoopIterations {
						break
					}
				}
			} else {
				return fail("for each needs a list, tuple, or text value")
			}
		}
		loopNames, _ := parseLoopNames(n.name)
		for index, item := range items {
			if index >= r.limits.MaxLoopIterations {
				return fail("for-each loop exceeded the configured limit of %d iterations", r.limits.MaxLoopIterations)
			}
			if err := bindLoopValues(e, loopNames, item); err != nil {
				return err
			}
			e.set("_current_item", item)
			err := r.execute(n.body, e)
			if err != nil {
				var s signal
				if errors.As(err, &s) && s.kind == "continue" {
					continue
				}
				if errors.As(err, &s) && s.kind == "break" {
					break
				}
				return err
			}
		}
		return nil
	case "function":
		e.set(n.name, &userFunction{name: n.name, params: n.params, body: n.body, closure: e, runtime: r})
		return nil
	case "class":
		class := &userClass{name: n.name, methods: make(map[string]*userFunction)}
		if n.base != "" {
			value, err := e.get(n.base)
			if err != nil {
				return err
			}
			base, ok := value.(*userClass)
			if !ok {
				return fail("base type %q is not a class", n.base)
			}
			class.base = base
			class.constructor = base.constructor
		}
		hasConstructor := false
		for _, member := range n.body {
			function := &userFunction{name: member.name, params: member.params, body: member.body, closure: e, runtime: r}
			if member.kind == "constructor" {
				if strings.ToLower(member.name) != strings.ToLower(n.name) {
					return fail("constructor name %q does not match class %q", member.name, n.name)
				}
				if hasConstructor {
					return fail("class %q has more than one constructor", n.name)
				}
				hasConstructor = true
				class.constructor = function
				continue
			}
			if _, exists := class.methods[member.name]; exists {
				return fail("class %q has duplicate method %q", n.name, member.name)
			}
			class.methods[member.name] = function
		}
		e.set(n.name, class)
		return nil
	case "constructor":
		return fail("a constructor can only be declared inside a class")
	case "try":
		err := r.execute(n.body, e)
		if err == nil {
			return nil
		}
		var s signal
		if errors.As(err, &s) {
			return err
		}
		e.set("error_message", err.Error())
		return r.execute(n.handler, e)
	case "statement":
		if n.simple != nil {
			return r.executeSimpleStatement(n.simple, e)
		}
		return r.statement(n.text, e)
	}
	return r.statement(n.text, e)
}

func (r *runtime) executeSimpleStatement(statement *simpleStatement, e *env) error {
	var value any
	var err error
	if statement.hasExpression {
		value, err = r.evalExpression(statement.expression, e)
		if err != nil {
			return err
		}
	}
	switch statement.kind {
	case "let":
		if len(statement.names) > 1 {
			values, ok := sequenceValues(value)
			if !ok {
				return fail("multiple assignment expects a list or tuple")
			}
			return r.assignMany(e, statement.names, values, statement.constant)
		}
		if statement.constant || statement.annotation != "" {
			return r.declare(e, statement.names[0], value, statement.annotation, statement.constant)
		}
		return r.assign(e, statement.names[0], value)
	case "set":
		if len(statement.names) > 1 {
			values, ok := sequenceValues(value)
			if !ok {
				return fail("multiple assignment expects a list or tuple")
			}
			return r.assignMany(e, statement.names, values, false)
		}
		return r.assign(e, statement.names[0], value)
	case "set_member":
		memberTarget := statement.target.(memberExpression)
		target, targetErr := r.evalExpression(memberTarget.value, e)
		if targetErr != nil {
			return targetErr
		}
		instance, ok := target.(*userInstance)
		if !ok {
			return fail("property assignment requires a Tell instance")
		}
		instance.fields[memberTarget.member] = value
	case "print":
		_, err = fmt.Fprintln(r.output, display(value))
		return err
	case "expression":
		if r.echoResults {
			e.set("_", value)
			_, err = fmt.Fprintln(r.output, display(value))
		}
		return err
	case "check":
		if !truthy(value) {
			return fail("check failed: %s", statement.source)
		}
	case "return":
		return signal{kind: "return", value: value}
	case "raise":
		return fail("%s", display(value))
	case "break":
		return signal{kind: "break"}
	case "continue":
		return signal{kind: "continue"}
	default:
		return fail("unknown parsed statement %q", statement.kind)
	}
	return nil
}

var (
	reRemember   = regexp.MustCompile(`(?i)^remember\s+(.+?)\s+as\s+this\.([\p{L}_][\p{L}\p{N}_]*)$`)
	reTell       = regexp.MustCompile(`(?i)^tell\s+([\p{L}_][\p{L}\p{N}_]*)\s+to\s+([\p{L}_][\p{L}\p{N}_]*)(?:\s+with\s+(.+))?$`)
	reSetMember  = regexp.MustCompile(`(?i)^set\s+([\p{L}_][\p{L}\p{N}_]*)\.([\p{L}_][\p{L}\p{N}_]*)\s+to\s+(.+)$`)
	reImportTell = regexp.MustCompile(`(?i)^use the module\s+(.+?)\s+as\s+([\p{L}_][\p{L}\p{N}_]*)$`)
	reAsk        = regexp.MustCompile(`(?i)^ask\s+(.+?)\s+and\s+(?:save the answer as|put the answer in)\s+([\p{L}_][\p{L}\p{N}_]*)$`)
	reRead       = regexp.MustCompile(`(?i)^read the file\s+(.+?)\s+into\s+([\p{L}_][\p{L}\p{N}_]*)$`)
	reWrite      = regexp.MustCompile(`(?i)^write\s+(.+?)\s+to the file\s+(.+)$`)
	reAppend     = regexp.MustCompile(`(?i)^append\s+(.+?)\s+to the file\s+(.+)$`)
	reUse        = regexp.MustCompile(`(?i)^use the\s+([A-Za-z_]+)\s+library(?:\s+as\s+([\p{L}_][\p{L}\p{N}_]*))?$`)
	reSetItem    = regexp.MustCompile(`(?i)^set (?:the )?item at position (.+?) in ([\p{L}_][\p{L}\p{N}_]*) to (.+)$`)
	reSetKey     = regexp.MustCompile(`(?i)^set (?:the )?key (.+?) in ([\p{L}_][\p{L}\p{N}_]*) to (.+)$`)
	reAdd        = regexp.MustCompile(`(?i)^add\s+(.+?)\s+to\s+([\p{L}_][\p{L}\p{N}_]*)$`)
	reRemove     = regexp.MustCompile(`(?i)^remove\s+(.+?)\s+from\s+([\p{L}_][\p{L}\p{N}_]*)$`)
	reRaise      = regexp.MustCompile(`(?i)^raise an error saying\s+(.+)$`)
)

func (r *runtime) statement(s string, e *env) error {
	if expression, ok := prefixFold(s, "check that "); ok {
		if expression == "" {
			return fail("Check that needs a condition")
		}
		value, err := r.eval(expression, e)
		if err != nil {
			return err
		}
		if !truthy(value) {
			return fail("check failed: %s", expression)
		}
		return nil
	}
	if rest, ok := prefixFold(s, "let "); ok {
		constant := false
		if constantRest, isConstant := prefixFold(rest, "constant "); isConstant {
			rest = constantRest
			constant = true
		}
		binding, expression, separated := splitFoldOnce(rest, " be ")
		name, annotation, annotated := splitFoldOnce(binding, " as ")
		if !annotated {
			name = binding
		}
		if separated && strings.Contains(binding, ",") {
			targets := strings.Split(binding, ",")
			names := make([]string, len(targets))
			valid := len(targets) > 1 && !annotated
			seen := make(map[string]bool, len(targets))
			for index, target := range targets {
				names[index] = strings.TrimSpace(target)
				if !identifier(names[index]) || seen[names[index]] {
					valid = false
				}
				seen[names[index]] = true
			}
			if valid && expression != "" {
				value, err := r.eval(expression, e)
				if err != nil {
					return err
				}
				values, ok := sequenceValues(value)
				if !ok {
					return fail("multiple assignment expects a list")
				}
				return r.assignMany(e, names, values, constant)
			}
		}
		if separated && identifier(name) && expression != "" {
			value, err := r.eval(expression, e)
			if err != nil {
				return err
			}
			if constant || annotated {
				return r.declare(e, name, value, annotation, constant)
			}
			return r.assign(e, name, value)
		}
	}
	if rest, ok := prefixFold(s, "set "); ok {
		name, expression, separated := splitFoldOnce(rest, " to ")
		if separated && strings.Contains(name, ",") {
			targets := strings.Split(name, ",")
			names := make([]string, len(targets))
			valid := len(targets) > 1
			seen := make(map[string]bool, len(targets))
			for index, target := range targets {
				names[index] = strings.TrimSpace(target)
				if !identifier(names[index]) || seen[names[index]] {
					valid = false
				}
				seen[names[index]] = true
			}
			if valid && expression != "" {
				value, err := r.eval(expression, e)
				if err != nil {
					return err
				}
				values, ok := sequenceValues(value)
				if !ok {
					return fail("multiple assignment expects a list or tuple")
				}
				return r.assignMany(e, names, values, false)
			}
		}
		if separated && identifier(name) && expression != "" {
			value, err := r.eval(expression, e)
			if err != nil {
				return err
			}
			return r.assign(e, name, value)
		}
	}
	for _, prefix := range []string{"say ", "show ", "print "} {
		if expression, ok := prefixFold(s, prefix); ok && expression != "" {
			value, err := r.eval(expression, e)
			if err != nil {
				return err
			}
			fmt.Fprintln(r.output, display(value))
			return nil
		}
	}
	for _, prefix := range []string{"give back ", "return "} {
		if expression, ok := prefixFold(s, prefix); ok {
			var value any
			if expression != "" {
				var err error
				value, err = r.eval(expression, e)
				if err != nil {
					return err
				}
			}
			return signal{kind: "return", value: value}
		}
	}
	if strings.EqualFold(s, "give back") || strings.EqualFold(s, "return") {
		return signal{kind: "return", value: nil}
	}
	if strings.EqualFold(s, "stop this loop") || strings.EqualFold(s, "break out of this loop") {
		return signal{kind: "break"}
	}
	if strings.EqualFold(s, "skip to the next repetition") || strings.EqualFold(s, "continue to the next repetition") {
		return signal{kind: "continue"}
	}
	for _, prefix := range []string{"call ", "run "} {
		if rest, ok := prefixFold(s, prefix); ok {
			name, tail := firstWord(rest)
			if args, valid := prefixFold(tail, "with "); valid && identifier(name) && args != "" {
				target, err := e.get(name)
				if err != nil {
					return err
				}
				values := make([]any, 0)
				for _, argument := range splitAnd(args) {
					value, err := r.eval(argument, e)
					if err != nil {
						return err
					}
					values = append(values, value)
				}
				_, err = invoke(target, values)
				return err
			}
		}
	}
	if rest, ok := prefixFold(s, "use the module "); ok {
		pathExpr, alias, separated := splitFoldOnce(rest, " as ")
		if separated && identifier(alias) {
			pathValue, err := r.eval(pathExpr, e)
			if err != nil {
				return err
			}
			module, err := r.loadTellModule(fmt.Sprint(pathValue))
			if err != nil {
				return err
			}
			e.set(alias, module)
			return nil
		}
	}
	if rest, ok := prefixFold(s, "set "); ok {
		left, expression, separated := splitFoldOnce(rest, " to ")
		if separated {
			objectName, field, member := splitFoldOnce(left, ".")
			if member && identifier(objectName) && identifier(field) {
				value, err := r.eval(expression, e)
				if err != nil {
					return err
				}
				target, err := e.get(objectName)
				if err != nil {
					return err
				}
				instance, ok := target.(*userInstance)
				if !ok {
					return fail("%q is not a Tell instance", objectName)
				}
				instance.fields[field] = value
				return nil
			}
		}
	}
	if rest, ok := prefixFold(s, "remember "); ok {
		expression, field, separated := splitFoldOnce(rest, " as this.")
		if separated && identifier(field) {
			value, err := r.eval(expression, e)
			if err != nil {
				return err
			}
			instance, err := e.get("this")
			if err != nil {
				return fail("Remember ... as this.field can only be used in a method or constructor")
			}
			object, ok := instance.(*userInstance)
			if !ok {
				return fail("this is not an instance")
			}
			object.fields[field] = value
			return nil
		}
	}
	if rest, ok := prefixFold(s, "tell "); ok {
		objectName, action, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(objectName) {
			method, argsText := firstWord(action)
			if identifier(method) {
				args := []string{}
				if argsText != "" {
					argsText, separated = prefixFold(argsText, "with ")
					if !separated || argsText == "" {
						method = ""
					} else {
						args = splitAnd(argsText)
					}
				}
				if method != "" {
					_, err := r.eval(objectName+"."+method+"("+strings.Join(args, ",")+")", e)
					return err
				}
			}
		}
	}
	if rest, ok := prefixFold(s, "ask "); ok {
		for _, delimiter := range []string{" and save the answer as ", " and put the answer in "} {
			prompt, name, separated := splitFoldOnce(rest, delimiter)
			if separated && identifier(name) {
				value, err := r.eval(prompt, e)
				if err != nil {
					return err
				}
				fmt.Fprint(r.output, display(value))
				line, err := r.reader.ReadString('\n')
				if err != nil && len(line) == 0 {
					return err
				}
				line = strings.TrimRight(line, "\r\n")
				e.set(name, line)
				e.set("_last_answer", line)
				return nil
			}
		}
	}
	if rest, ok := prefixFold(s, "read the file "); ok {
		pathExpr, name, separated := splitFoldOnce(rest, " into ")
		if separated && identifier(name) {
			path, err := r.eval(pathExpr, e)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(fmt.Sprint(path))
			if err != nil {
				return err
			}
			e.set(name, string(data))
			return nil
		}
	}
	for _, operation := range []struct{ prefix, delimiter string }{{"write ", " to the file "}, {"append ", " to the file "}} {
		if rest, ok := prefixFold(s, operation.prefix); ok {
			valueExpr, pathExpr, separated := splitFoldOnce(rest, operation.delimiter)
			if separated {
				value, err := r.eval(valueExpr, e)
				if err != nil {
					return err
				}
				path, err := r.eval(pathExpr, e)
				if err != nil {
					return err
				}
				if operation.prefix == "write " {
					return os.WriteFile(fmt.Sprint(path), []byte(display(value)), 0644)
				}
				file, err := os.OpenFile(fmt.Sprint(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = file.WriteString(display(value))
				return err
			}
		}
	}
	if rest, ok := prefixFold(s, "add "); ok {
		valueExpr, name, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(name) {
			value, err := r.eval(valueExpr, e)
			if err != nil {
				return err
			}
			collection, err := e.get(name)
			if err != nil {
				return err
			}
			items, ok := collection.([]any)
			if !ok {
				return fail("%q does not support adding items", name)
			}
			e.values[name] = append(items, value)
			return nil
		}
	}
	if rest, ok := prefixFold(s, "remove "); ok {
		valueExpr, name, separated := splitFoldOnce(rest, " from ")
		if separated && identifier(name) {
			value, err := r.eval(valueExpr, e)
			if err != nil {
				return err
			}
			collection, err := e.get(name)
			if err != nil {
				return err
			}
			items, ok := collection.([]any)
			if !ok {
				return fail("%q does not support removing items", name)
			}
			for i, item := range items {
				if reflect.DeepEqual(item, value) {
					e.values[name] = append(items[:i], items[i+1:]...)
					return nil
				}
			}
			return fail("item %q was not found", display(value))
		}
	}
	if rest, ok := prefixFold(s, "raise an error saying "); ok && rest != "" {
		value, err := r.eval(rest, e)
		if err != nil {
			return err
		}
		return fail("%s", display(value))
	}
	if rest, ok := prefixFold(s, "use the "); ok {
		if index := strings.Index(strings.ToLower(rest), " library"); index >= 0 {
			requested := strings.TrimSpace(rest[:index])
			tail := strings.TrimSpace(rest[index+len(" library"):])
			alias := strings.ToLower(strings.ReplaceAll(requested, " ", ""))
			aliasOK := true
			if tail != "" {
				alias, aliasOK = prefixFold(tail, "as ")
				aliasOK = aliasOK && identifier(alias)
			}
			if requested != "" && aliasOK && identifier(strings.ReplaceAll(requested, " ", "")) {
				library := strings.ToLower(strings.ReplaceAll(requested, " ", ""))
				switch library {
				case "dateandtime", "dates":
					library = "datetime"
				}
				module, err := moduleValue(library)
				if err != nil {
					return err
				}
				e.set(alias, module)
				return nil
			}
		}
	}
	if rest, ok := prefixFold(s, "set "); ok {
		location, valueExpr, separated := splitFoldOnce(rest, " to ")
		if separated {
			itemText, isItem := prefixFold(location, "the item at position ")
			if !isItem {
				itemText, isItem = prefixFold(location, "item at position ")
			}
			if isItem {
				indexExpr, name, hasIn := splitFoldOnce(itemText, " in ")
				if hasIn && identifier(name) {
					collection, err := e.get(name)
					if err != nil {
						return err
					}
					items, ok := collection.([]any)
					if !ok {
						return fail("%q is not a list", name)
					}
					indexValue, err := r.eval(indexExpr, e)
					if err != nil {
						return err
					}
					number, err := numberValue(indexValue)
					if err != nil {
						return err
					}
					index := int(number)
					if index < 0 {
						index += len(items)
					}
					if index < 0 || index >= len(items) {
						return fail("list index out of range")
					}
					items[index], err = r.eval(valueExpr, e)
					return err
				}
			}
			keyText, isKey := prefixFold(location, "the key ")
			if !isKey {
				keyText, isKey = prefixFold(location, "key ")
			}
			if isKey {
				keyExpr, name, hasIn := splitFoldOnce(keyText, " in ")
				if hasIn && identifier(name) {
					collection, err := e.get(name)
					if err != nil {
						return err
					}
					dictionary, ok := collection.(map[string]any)
					if !ok {
						return fail("%q is not a dictionary", name)
					}
					key, err := r.eval(keyExpr, e)
					if err != nil {
						return err
					}
					value, err := r.eval(valueExpr, e)
					if err != nil {
						return err
					}
					dictionary[fmt.Sprint(key)] = value
					return nil
				}
			}
		}
	}
	if m := reImportTell.FindStringSubmatch(s); m != nil {
		pathValue, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		module, err := r.loadTellModule(fmt.Sprint(pathValue))
		if err != nil {
			return err
		}
		e.set(m[2], module)
		return nil
	}
	if m := reSetMember.FindStringSubmatch(s); m != nil {
		value, err := r.eval(m[3], e)
		if err != nil {
			return err
		}
		target, err := e.get(m[1])
		if err != nil {
			return err
		}
		instance, ok := target.(*userInstance)
		if !ok {
			return fail("%q is not a Tell instance", m[1])
		}
		instance.fields[m[2]] = value
		return nil
	}
	if m := reRemember.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		instance, err := e.get("this")
		if err != nil {
			return fail("Remember ... as this.field can only be used in a method or constructor")
		}
		object, ok := instance.(*userInstance)
		if !ok {
			return fail("this is not an instance")
		}
		object.fields[m[2]] = v
		return nil
	}
	if m := reTell.FindStringSubmatch(s); m != nil {
		args := []string{}
		if strings.TrimSpace(m[3]) != "" {
			args = splitAnd(m[3])
		}
		_, err := r.eval(m[1]+"."+m[2]+"("+strings.Join(args, ",")+")", e)
		return err
	}
	if m := reAsk.FindStringSubmatch(s); m != nil {
		p, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		fmt.Fprint(r.output, display(p))
		line, err := r.reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		e.set(m[2], line)
		e.set("_last_answer", line)
		return nil
	}
	if m := reRead.FindStringSubmatch(s); m != nil {
		path, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(fmt.Sprint(path))
		if err != nil {
			return err
		}
		e.set(m[2], string(data))
		return nil
	}
	if m := reWrite.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		path, err := r.eval(m[2], e)
		if err != nil {
			return err
		}
		return os.WriteFile(fmt.Sprint(path), []byte(display(v)), 0644)
	}
	if m := reAppend.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		path, err := r.eval(m[2], e)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(fmt.Sprint(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(display(v))
		return err
	}
	if m := reUse.FindStringSubmatch(s); m != nil {
		requested := strings.ToLower(m[1])
		alias := m[2]
		if alias == "" {
			alias = requested
		}
		module, err := moduleValue(requested)
		if err != nil {
			return err
		}
		e.set(alias, module)
		return nil
	}
	if m := reSetItem.FindStringSubmatch(s); m != nil {
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q is not a list", m[2])
		}
		ix, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		i, err := numberValue(ix)
		if err != nil {
			return err
		}
		val, err := r.eval(m[3], e)
		if err != nil {
			return err
		}
		idx := int(i)
		if idx < 0 {
			idx += len(xs)
		}
		if idx < 0 || idx >= len(xs) {
			return fail("list index out of range")
		}
		xs[idx] = val
		return nil
	}
	if m := reSetKey.FindStringSubmatch(s); m != nil {
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		obj, ok := v.(map[string]any)
		if !ok {
			return fail("%q is not a dictionary", m[2])
		}
		key, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		val, err := r.eval(m[3], e)
		if err != nil {
			return err
		}
		obj[fmt.Sprint(key)] = val
		return nil
	}
	if m := reAdd.FindStringSubmatch(s); m != nil {
		val, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q does not support adding items", m[2])
		}
		e.values[m[2]] = append(xs, val)
		return nil
	}
	if m := reRemove.FindStringSubmatch(s); m != nil {
		val, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		v, err := e.get(m[2])
		if err != nil {
			return err
		}
		xs, ok := v.([]any)
		if !ok {
			return fail("%q does not support removing items", m[2])
		}
		for i, x := range xs {
			if reflect.DeepEqual(x, val) {
				e.values[m[2]] = append(xs[:i], xs[i+1:]...)
				return nil
			}
		}
		return fail("item %q was not found", display(val))
	}
	if m := reRaise.FindStringSubmatch(s); m != nil {
		v, err := r.eval(m[1], e)
		if err != nil {
			return err
		}
		return fail("%s", display(v))
	}
	value, err := r.eval(s, e)
	if err != nil {
		return fail("I don't recognise this sentence: %q (%v)", s, err)
	}
	if r.echoResults {
		e.set("_", value)
		fmt.Fprintln(r.output, display(value))
	}
	return nil
}

func (r *runtime) loadTellModule(path string) (map[string]any, error) {
	if !strings.EqualFold(filepath.Ext(path), ".tell") {
		return nil, fail("Tell modules must use the .tell extension")
	}
	absolute, err := filepath.Abs(filepath.Join(filepath.Dir(r.filename), path))
	if err != nil {
		return nil, err
	}
	absolute = filepath.Clean(absolute)
	if cached, ok := r.modules[absolute]; ok {
		return cached, nil
	}
	if r.loading[absolute] {
		return nil, fail("circular module import detected for %q", absolute)
	}
	source, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fail("cannot load Tell module %q: %v", path, err)
	}
	nodes, err := parse(string(source), absolute)
	if err != nil {
		return nil, err
	}
	r.loading[absolute] = true
	defer delete(r.loading, absolute)
	moduleEnv := newEnv(r.global)
	previousFilename := r.filename
	r.filename = absolute
	err = r.execute(nodes, moduleEnv)
	r.filename = previousFilename
	if err != nil {
		return nil, err
	}
	exports := make(map[string]any)
	for name, value := range moduleEnv.values {
		if !strings.HasPrefix(name, "_") {
			exports[name] = value
		}
	}
	r.modules[absolute] = exports
	return exports, nil
}

func (f *userFunction) call(args []any, receiver *userInstance) (any, error) {
	return f.callNamed(args, nil, receiver)
}

func (f *userFunction) callNamed(args []any, named map[string]any, receiver *userInstance) (any, error) {
	variadicIndex := -1
	for index, parameter := range f.params {
		_, _, _, variadic, ok := parameterParts(parameter)
		if !ok {
			return nil, fail("invalid parameter definition %q in %s", parameter, f.name)
		}
		if variadic {
			variadicIndex = index
		}
	}
	if len(args) > len(f.params) && variadicIndex < 0 {
		return nil, fail("%q accepts %d argument(s), but got %d positional arguments", f.name, len(f.params), len(args))
	}
	parameterIndex := make(map[string]int, len(f.params))
	bindings := make(map[string]any, len(f.params))
	parameterNames := make([]string, len(f.params))
	defaults := make([]string, len(f.params))
	for index, parameter := range f.params {
		name, defaultValue, hasDefault, variadic, ok := parameterParts(parameter)
		if !ok {
			return nil, fail("invalid parameter definition %q in %s", parameter, f.name)
		}
		parameterNames[index] = name
		parameterIndex[name] = index
		if hasDefault {
			defaults[index] = defaultValue
		}
		if variadic {
			if index < len(args) {
				bindings[name] = append([]any(nil), args[index:]...)
			} else {
				bindings[name] = []any{}
			}
		} else if index < len(args) {
			bindings[name] = args[index]
		}
	}
	for name, value := range named {
		index, exists := parameterIndex[name]
		if !exists {
			return nil, fail("%q has no parameter named %q", f.name, name)
		}
		_, _, _, variadic, _ := parameterParts(f.params[index])
		if variadic {
			return nil, fail("%q variadic parameter %q cannot be supplied by name", f.name, name)
		}
		if index < len(args) {
			return nil, fail("%q got multiple values for %q", f.name, name)
		}
		bindings[name] = value
	}
	for index, parameter := range parameterNames {
		if _, exists := bindings[parameter]; !exists && defaults[index] == "" {
			return nil, fail("%q is missing argument %q", f.name, parameter)
		}
	}
	if f.runtime.callDepth >= f.runtime.limits.MaxFunctionCallDepth {
		return nil, fail("maximum function call depth (%d) exceeded", f.runtime.limits.MaxFunctionCallDepth)
	}
	f.runtime.callDepth++
	defer func() { f.runtime.callDepth-- }()
	local := newEnv(f.closure)
	for name, value := range bindings {
		local.set(name, value)
	}
	if receiver != nil {
		local.set("this", receiver)
	}
	for index, parameter := range parameterNames {
		if _, exists := bindings[parameter]; exists {
			continue
		}
		value, err := f.runtime.eval(defaults[index], local)
		if err != nil {
			return nil, err
		}
		local.set(parameter, value)
	}
	err := f.runtime.execute(f.body, local)
	if err != nil {
		var s signal
		if errors.As(err, &s) && s.kind == "return" {
			return s.value, nil
		}
		return nil, err
	}
	return nil, nil
}

func invoke(v any, args []any) (any, error) {
	return invokeNamed(v, args, nil)
}

func isCallableValue(value any) bool {
	switch value.(type) {
	case callable, namedCallable, *userFunction, *userClass, *bytecodeClassValue:
		return true
	default:
		return false
	}
}

func invokeNamed(v any, args []any, named map[string]any) (any, error) {
	switch f := v.(type) {
	case namedCallable:
		return f(args, named)
	case callable:
		if len(named) != 0 {
			return nil, fail("named arguments are not supported by this function")
		}
		return f(args)
	case *userFunction:
		return f.callNamed(args, named, nil)
	case *userClass:
		instance := &userInstance{class: f, fields: make(map[string]any)}
		if f.constructor == nil {
			if len(args) > 0 || len(named) > 0 {
				return nil, fail("class %q has no constructor and accepts no arguments", f.name)
			}
			return instance, nil
		}
		if _, err := f.constructor.callNamed(args, named, instance); err != nil {
			return nil, err
		}
		return instance, nil
	}
	return nil, fail("this value cannot be called")
}

func (c *userClass) method(name string) (*userFunction, bool) {
	if method, ok := c.methods[name]; ok {
		return method, true
	}
	if c.base != nil {
		return c.base.method(name)
	}
	return nil, false
}

func moduleValue(name string) (any, error) {
	mathModule := map[string]any{"pi": math.Pi, "e": math.E, "sqrt": callable(unaryMath(math.Sqrt)), "floor": callable(unaryMath(math.Floor)), "ceil": callable(unaryMath(math.Ceil)), "sin": callable(unaryMath(math.Sin)), "cos": callable(unaryMath(math.Cos)), "tan": callable(unaryMath(math.Tan)), "abs": callable(unaryMath(math.Abs)), "log": callable(unaryMath(math.Log)), "pow": callable(func(a []any) (any, error) {
		if len(a) != 2 {
			return nil, fail("pow expects two arguments")
		}
		x, e := numberValue(a[0])
		if e != nil {
			return nil, e
		}
		y, e := numberValue(a[1])
		return math.Pow(x, y), e
	})}
	switch name {
	case "math", "mathematics":
		return mathModule, nil
	case "random":
		return map[string]any{"number": callable(func(a []any) (any, error) {
			lo, hi := 0.0, 1.0
			if len(a) > 0 {
				lo, _ = numberValue(a[0])
			}
			if len(a) > 1 {
				hi, _ = numberValue(a[1])
			}
			return lo + rand.Float64()*(hi-lo), nil
		}), "integer": callable(func(a []any) (any, error) {
			return newRuntime("", nil, nil, DefaultExecutionLimits()).global.values["random_number"].(callable)(a)
		})}, nil
	case "json":
		return map[string]any{"loads": callable(func(a []any) (any, error) {
			if len(a) != 1 {
				return nil, fail("json.loads expects one text argument")
			}
			var v any
			err := json.Unmarshal([]byte(fmt.Sprint(a[0])), &v)
			return normalizeJSON(v), err
		}), "dumps": callable(func(a []any) (any, error) {
			if len(a) != 1 {
				return nil, fail("json.dumps expects one argument")
			}
			b, e := json.Marshal(a[0])
			return string(b), e
		})}, nil
	case "csv":
		return map[string]any{
			"parse": callable(func(a []any) (any, error) {
				if len(a) != 1 {
					return nil, fail("csv.parse expects one text argument")
				}
				text, ok := a[0].(string)
				if !ok {
					return nil, fail("csv.parse expects one text argument")
				}
				reader := csv.NewReader(strings.NewReader(text))
				reader.FieldsPerRecord = -1
				rows := make([]any, 0)
				for {
					record, err := reader.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						return nil, fail("csv.parse: %v", err)
					}
					if len(record) == 0 {
						continue
					}
					row := make([]any, len(record))
					for i, cell := range record {
						row[i] = cell
					}
					rows = append(rows, row)
				}
				return rows, nil
			}),
			"stringify": callable(func(a []any) (any, error) {
				if len(a) != 1 {
					return nil, fail("csv.stringify expects one list of rows")
				}
				rows, ok := a[0].([]any)
				if !ok {
					return nil, fail("csv.stringify expects one list of rows")
				}
				var output bytes.Buffer
				writer := csv.NewWriter(&output)
				for rowIndex, value := range rows {
					row, ok := value.([]any)
					if !ok {
						return nil, fail("csv.stringify row %d must be a list", rowIndex+1)
					}
					record := make([]string, len(row))
					for column, cell := range row {
						text, ok := cell.(string)
						if !ok {
							return nil, fail("csv.stringify row %d must contain only text", rowIndex+1)
						}
						record[column] = text
					}
					if err := writer.Write(record); err != nil {
						return nil, err
					}
				}
				writer.Flush()
				if err := writer.Error(); err != nil {
					return nil, err
				}
				return output.String(), nil
			}),
		}, nil
	case "statistics":
		return map[string]any{"mean": callable(func(a []any) (any, error) {
			return newRuntime("", nil, nil, DefaultExecutionLimits()).global.values["average"].(callable)(a)
		})}, nil
	case "datetime", "dates":
		return map[string]any{"now": callable(func(a []any) (any, error) { return time.Now().Format(time.RFC3339), nil }), "today": callable(func(a []any) (any, error) { return time.Now().Format("2006-01-02"), nil })}, nil
	default:
		return nil, fail("the %q library is not available in this version of the native runtime", name)
	}
}
func normalizeJSON(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = normalizeJSON(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = normalizeJSON(x[k])
		}
	}
	return v
}

// token carries source offsets and one-based diagnostic coordinates.
type token struct {
	kind, text   string
	start, end   int
	line, column int
}

func translateExpr(s string) string {
	newObject := regexp.MustCompile(`(?i)^(?:a\s+)?new\s+([\p{L}_][\p{L}\p{N}_]*)(?:\s+with\s+(.+))?$`)
	if match := newObject.FindStringSubmatch(strings.TrimSpace(s)); match != nil {
		if strings.TrimSpace(match[2]) == "" {
			return match[1] + "()"
		}
		return match[1] + "(" + strings.Join(splitAnd(match[2]), ",") + ")"
	}
	result := regexp.MustCompile(`(?i)^the result of\s+([\p{L}_][\p{L}\p{N}_]*)\s+with\s+(.+)$`)
	if match := result.FindStringSubmatch(strings.TrimSpace(s)); match != nil {
		return match[1] + "(" + strings.Join(splitAnd(match[2]), ",") + ")"
	}
	for _, phrase := range []struct{ pattern, replacement string }{
		{`(?i)^the length of\s+(.+)$`, `len($1)`},
		{`(?i)^the square root of\s+(.+)$`, `sqrt($1)`},
		{`(?i)^the absolute value of\s+(.+)$`, `abs($1)`},
		{`(?i)^the type of\s+(.+)$`, `type_of($1)`},
	} {
		if pattern := regexp.MustCompile(phrase.pattern); pattern.MatchString(strings.TrimSpace(s)) {
			return pattern.ReplaceAllString(strings.TrimSpace(s), phrase.replacement)
		}
	}
	return s
}

type languagePhrase struct{ phrase, kind, text string }

var languagePhrases = []languagePhrase{
	{"the current repetition number", "ident", "_repeat_index"},
	{"the current item", "ident", "_current_item"},
	{"the answer", "ident", "_last_answer"},
	{"is greater than or equal to", ">=", ">="},
	{"is less than or equal to", "<=", "<="},
	{"is not equal to", "!=", "!="},
	{"isn't equal to", "!=", "!="},
	{"is greater than", ">", ">"},
	{"is more than", ">", ">"},
	{"is less than", "<", "<"},
	{"is at least", ">=", ">="},
	{"is at most", "<=", "<="},
	{"is not in", "notin", "notin"},
	{"is equal to", "==", "=="},
	{"isn't", "!=", "!="},
	{"is not", "!=", "!="},
	{"is below", "<", "<"},
	{"to the power of", "**", "**"},
	{"multiplied by", "*", "*"},
	{"divided by", "/", "/"},
	{"modulo", "%", "%"},
	{"plus", "+", "+"},
	{"minus", "-", "-"},
	{"times", "*", "*"},
	{"is in", "in", "in"},
	{"is", "==", "=="},
}

func identifierRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func matchLanguagePhrase(source string, offset int) (kind, text string, end int, ok bool) {
	for _, phrase := range languagePhrases {
		end = offset + len(phrase.phrase)
		if end > len(source) || !strings.EqualFold(source[offset:end], phrase.phrase) {
			continue
		}
		if offset > 0 {
			before, _ := utf8.DecodeLastRuneInString(source[:offset])
			if identifierRune(before) {
				continue
			}
		}
		if end < len(source) {
			after, _ := utf8.DecodeRuneInString(source[end:])
			if identifierRune(after) {
				continue
			}
		}
		return phrase.kind, phrase.text, end, true
	}
	return "", "", offset, false
}
func lex(s string) ([]token, error) {
	s = translateExpr(s)
	var out []token
	position := func(offset int) (int, int) {
		prefix := s[:offset]
		line := strings.Count(prefix, "\n") + 1
		lastNewline := strings.LastIndex(prefix, "\n")
		return line, utf8.RuneCountInString(prefix[lastNewline+1:]) + 1
	}
	appendToken := func(kind, text string, start, end int) {
		line, column := position(start)
		out = append(out, token{kind: kind, text: text, start: start, end: end, line: line, column: column})
	}
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(c) {
			i += size
			continue
		}
		start := i
		if kind, text, end, ok := matchLanguagePhrase(s, i); ok {
			appendToken(kind, text, start, end)
			i = end
			continue
		}
		if c == '"' || c == '\'' {
			q := byte(c)
			i += size
			var b strings.Builder
			esc := false
			closed := false
			for i < len(s) {
				x, width := utf8.DecodeRuneInString(s[i:])
				i += width
				if esc {
					switch x {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
						b.WriteRune(x)
					}
					esc = false
					continue
				}
				if x == '\\' {
					esc = true
					continue
				}
				if x == rune(q) {
					closed = true
					break
				}
				b.WriteRune(x)
			}
			if !closed {
				line, column := position(start)
				return nil, fail("unterminated text at line %d, column %d", line, column)
			}
			appendToken("string", b.String(), start, i)
			continue
		}
		if (c >= '0' && c <= '9') || (c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9') {
			i += size
			for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
				i++
			}
			if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
				i++
				if i < len(s) && (s[i] == '+' || s[i] == '-') {
					i++
				}
				exponentStart := i
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
				if exponentStart == i {
					line, column := position(start)
					return nil, fail("invalid numeric exponent at line %d, column %d", line, column)
				}
			}
			if _, err := strconv.ParseFloat(s[start:i], 64); err != nil {
				line, column := position(start)
				return nil, fail("invalid number %q at line %d, column %d", s[start:i], line, column)
			}
			appendToken("number", s[start:i], start, i)
			continue
		}
		if unicode.IsLetter(c) || c == '_' {
			i += size
			for i < len(s) {
				next, width := utf8.DecodeRuneInString(s[i:])
				if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' {
					break
				}
				i += width
			}
			word := s[start:i]
			low := strings.ToLower(word)
			switch low {
			case "true", "false", "nothing", "and", "or", "not", "in", "notin":
				appendToken(low, low, start, i)
			default:
				appendToken("ident", word, start, i)
			}
			continue
		}
		if i+1 < len(s) {
			two := s[i : i+2]
			if two == ">=" || two == "<=" || two == "==" || two == "!=" || two == "**" || two == "//" {
				appendToken(two, two, start, i+2)
				i += 2
				continue
			}
		}
		if strings.ContainsRune("+-*/%<>()[]{}.,:", c) {
			appendToken(string(c), string(c), start, start+size)
			i += size
			continue
		}
		if c == '=' {
			appendToken("=", "=", start, start+size)
			i += size
			continue
		}
		line, column := position(start)
		return nil, fail("unexpected character %q at line %d, column %d", c, line, column)
	}
	line, column := position(len(s))
	out = append(out, token{kind: "eof", line: line, column: column, start: len(s), end: len(s)})
	return out, nil
}
func precedence(op string) int {
	switch op {
	case "or":
		return 1
	case "and":
		return 2
	case "==", "!=", "<", ">", "<=", ">=", "in", "notin":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "//", "%":
		return 5
	case "**":
		return 6
	}
	return 0
}
func sliceIndices(length int, start, end *any, step int) ([]int, error) {
	bound := func(value *any, fallback int) (int, error) {
		if value == nil {
			return fallback, nil
		}
		n, err := numberValue(*value)
		if err != nil {
			return 0, err
		}
		i := int(n)
		if i < 0 {
			i += length
		}
		return i, nil
	}
	startDefault := 0
	if step < 0 {
		startDefault = length - 1
	}
	first, err := bound(start, startDefault)
	if err != nil {
		return nil, err
	}
	last, err := bound(end, func() int {
		if step < 0 {
			return -1
		}
		return length
	}())
	if err != nil {
		return nil, err
	}
	if step > 0 {
		if first < 0 {
			first = 0
		} else if first > length {
			first = length
		}
		if last < 0 {
			last = 0
		} else if last > length {
			last = length
		}
	} else {
		if first < -1 {
			first = -1
		} else if first >= length {
			first = length - 1
		}
		if last < -1 {
			last = -1
		} else if last >= length {
			last = length - 1
		}
	}
	indices := make([]int, 0)
	for i := first; (step > 0 && i < last) || (step < 0 && i > last); i += step {
		indices = append(indices, i)
	}
	return indices, nil
}

func (r *runtime) eval(s string, e *env) (any, error) {
	expr, err := parseTellExpressionAt(s, 1, 1)
	if err != nil {
		return nil, err
	}
	return r.evalExpression(expr, e)
}

var interpolation = regexp.MustCompile(`\{([^{}]+)\}`)

func (r *runtime) interpolate(text string, e *env) string {
	return interpolation.ReplaceAllStringFunc(text, func(match string) string {
		inner := strings.TrimSpace(match[1 : len(match)-1])
		v, err := r.eval(inner, e)
		if err != nil {
			return match
		}
		return display(v)
	})
}

// Keep path helpers platform-neutral and prevent an empty filename from silently targeting the cwd.
func cleanPath(path string) string { return filepath.Clean(path) }
