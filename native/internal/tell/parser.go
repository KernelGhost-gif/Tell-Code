package tell

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
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

type Error struct {
	Kind string
	Message string
	Filename string
	Line int
	Column int
}

// tupleValue is an immutable, ordered Tell sequence distinct from mutable lists.
type tupleValue []any
type setValue []any

func (e *Error) Error() string {
	if e.Filename != "" && e.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", e.Filename, e.Line, e.Column, e.Message)
	}
	return e.Message
}

func fail(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

// withSourceLocation attaches the first meaningful source span to an error.
// Nested expression errors keep their more precise location.

func ioFailure(err error) error {
	if err == nil { return nil }
	return &Error{Kind:"IOError", Message:err.Error()}
}

func ioFailureAt(err error, filename string, expression expression) error {
	if err == nil { return nil }
	line, column := expressionPosition(expression)
	if line > 0 && column > 0 {
		return withSourceLocation(ioFailure(err), filename, line, column)
	}
	return ioFailure(err)
}

func withSourceLocation(err error, filename string, line, column int) error {
	if err == nil {
		return nil
	}
	var located *Error
	kind := ""
	message := err.Error()
	if errors.As(err, &located) {
		kind = located.Kind
		message = located.Message
		if located.Filename != "" && located.Line > 0 {
			return err
		}
	}
	return &Error{Kind:kind, Message:message, Filename:filename, Line:line, Column:column}
}

type node struct {
	kind, text               string
	line, column             int
	condition, name, base    string
	conditionExpr            expression
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
	hasTarget     bool
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
			for _, phrase := range []struct{ prefix, delimiter string }{
				{"the item at position ", " in "},
				{"item at position ", " in "},
				{"the key ", " in "},
				{"key ", " in "},
			} {
				argument, isPhrase := prefixFold(name, phrase.prefix)
				if !isPhrase { continue }
				indexText, collectionName, hasIn := splitFoldOnce(argument, phrase.delimiter)
				if !hasIn || !identifier(collectionName) { continue }
				parsedIndex, validIndex := parseExpression(indexText)
				parsedValue, validValue := parseExpression(expressionText)
				if !validIndex || !validValue { continue }
				containerOffset := strings.LastIndex(source, collectionName)
				if containerOffset < 0 { containerOffset = 0 }
				targetColumn := column + utf8.RuneCountInString(source[:containerOffset])
				containerExpr := nameExpression{name:collectionName, line:line, column:targetColumn}
				target := indexExpression{value:containerExpr, index:parsedIndex, line:line, column:targetColumn}
				return &simpleStatement{kind:"set_index", source:name, target:target, expression:parsedValue, hasExpression:true}
			}
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
				if indexTarget, isIndex := parsedTarget.(indexExpression); isIndex && !indexTarget.isSlice {
					if parsedValue, validValue := parseExpression(expressionText); validValue {
						return &simpleStatement{kind: "set_index", source: name, target: indexTarget, expression: parsedValue, hasExpression: true}
					}
				}
			}
		}
	}
	for _, prefix := range []string{"call ", "run "} {
		rest, ok := prefixFold(source, prefix)
		if !ok { continue }
		name, tail := firstWord(rest)
		if !identifier(name) { continue }
		arguments := []callArgument{}
		valid := true
		if strings.TrimSpace(tail) != "" {
			argumentText, hasWith := prefixFold(tail, "with ")
			if !hasWith || strings.TrimSpace(argumentText) == "" {
				valid = false
			} else {
				for _, argument := range splitAnd(argumentText) {
					parsed, parsedOK := parseExpression(argument)
					if !parsedOK { valid = false; break }
					arguments = append(arguments, callArgument{value:parsed})
				}
			}
		}
		if valid {
			nameOffset := strings.Index(source, name)
			if nameOffset < 0 { nameOffset = len(prefix) }
			callee := nameExpression{name:name, line:line, column:column+utf8.RuneCountInString(source[:nameOffset])}
			callOffset := strings.Index(strings.ToLower(source), strings.TrimSpace(prefix))
			if callOffset < 0 { callOffset = 0 }
			call := callExpression{callee:callee, args:arguments, line:line, column:column+utf8.RuneCountInString(source[:callOffset])}
			return &simpleStatement{kind:"call", source:source, expression:call, hasExpression:true}
		}
	}
	if rest, ok := prefixFold(source, "remember "); ok {
		expressionText, field, separated := splitFoldOnce(rest, " as this.")
		if separated && identifier(field) {
			if parsed, valid := parseExpression(expressionText); valid {
				return &simpleStatement{kind:"remember_field", source:expressionText, names:[]string{field}, expression:parsed, hasExpression:true}
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


	for _, operation := range []struct{ prefix, delimiter, kind string }{
		{"add ", " to ", "add"},
		{"remove ", " from ", "remove"},
	} {
		if rest, ok := prefixFold(source, operation.prefix); ok {
			valueText, targetName, separated := splitFoldOnce(rest, operation.delimiter)
			if separated && identifier(targetName) {
				if parsed, valid := parseExpression(valueText); valid {
					return &simpleStatement{kind:operation.kind, names:[]string{targetName}, expression:parsed, hasExpression:true}
				}
			}
		}
	}
	if rest, ok := prefixFold(source, "use the module "); ok {
		pathText, alias, separated := splitFoldOnce(rest, " as ")
		if separated && identifier(alias) {
			if parsed, valid := parseExpression(pathText); valid {
				return &simpleStatement{kind:"import_module", names:[]string{alias}, expression:parsed, hasExpression:true}
			}
		}
	}

	if rest, ok := prefixFold(source, "use the python library called "); ok {
		moduleName, alias, hasAlias := splitFoldOnce(rest, " as ")
		moduleName = strings.TrimSpace(moduleName)
		if _, err := newPythonModule(moduleName); err == nil {
			if !hasAlias { alias = moduleName[strings.LastIndex(moduleName, ".")+1:] }
			if identifier(alias) {
				return &simpleStatement{kind:"import_python", source:moduleName, names:[]string{alias}}
			}
		}
	}
	if rest, ok := prefixFold(source, "use the javascript library called "); ok {
		moduleName, alias, hasAlias := splitFoldOnce(rest, " as ")
		moduleName = strings.TrimSpace(moduleName)
		if module, err := newJavaScriptModule(moduleName); err == nil {
			if !hasAlias { alias = defaultJavaScriptAlias(moduleName) }
			if identifier(alias) {
				return &simpleStatement{kind:"import_javascript", source:module.name, names:[]string{alias}}
			}
		}
	}
	if rest, ok := prefixFold(source, "use the "); ok {
		if index := strings.Index(strings.ToLower(rest), " library"); index >= 0 {
			requested := strings.TrimSpace(rest[:index])
			tail := strings.TrimSpace(rest[index+len(" library"):])
			alias := strings.ToLower(strings.ReplaceAll(requested, " ", ""))
			aliasOK := true
			if tail != "" {
				alias, aliasOK = prefixFold(tail, "as ")
				aliasOK = aliasOK && identifier(alias)
			}
			library := strings.ToLower(strings.ReplaceAll(requested, " ", ""))
			switch library {
			case "mathematics": library = "math"
			case "dateandtime", "dates": library = "datetime"
			}
			if requested != "" && aliasOK && identifier(strings.ReplaceAll(requested, " ", "")) {
				if _, err := moduleValue(library); err == nil {
					return &simpleStatement{kind:"import_library", source:library, names:[]string{alias}}
				}
			}
		}
	}
	if rest, ok := prefixFold(source, "ask "); ok {
		for _, delimiter := range []string{" and save the answer as ", " and put the answer in "} {
			prompt, name, separated := splitFoldOnce(rest, delimiter)
			if separated && identifier(name) {
				if parsed, valid := parseExpression(prompt); valid {
					return &simpleStatement{kind:"ask", names:[]string{name}, expression:parsed, hasExpression:true}
				}
			}
		}
	}
	if rest, ok := prefixFold(source, "read the file "); ok {
		pathText, name, separated := splitFoldOnce(rest, " into ")
		if separated && identifier(name) {
			if parsed, valid := parseExpression(pathText); valid {
				return &simpleStatement{kind:"read_file", names:[]string{name}, expression:parsed, hasExpression:true}
			}
		}
	}
	for _, operation := range []struct{ prefix, delimiter, kind string }{
		{"write ", " to the file ", "write_file"},
		{"append ", " to the file ", "append_file"},
	} {
		if rest, ok := prefixFold(source, operation.prefix); ok {
			valueText, pathText, separated := splitFoldOnce(rest, operation.delimiter)
			if separated {
				valueExpr, validValue := parseExpression(valueText)
				pathExpr, validPath := parseExpression(pathText)
				if validValue && validPath {
					return &simpleStatement{kind:operation.kind, expression:valueExpr, target:pathExpr, hasExpression:true, hasTarget:true}
				}
			}
		}
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
	case setValue:
		return []any(values), true
	default:
		return nil, false
	}
}

func valuesEqual(left, right any) bool {
	if leftSet, ok := left.(setValue); ok {
		rightSet, rightOK := right.(setValue)
		if !rightOK || len(leftSet) != len(rightSet) {
			return false
		}
		for _, value := range leftSet {
			if !setContains(rightSet, value) {
				return false
			}
		}
		return true
	}
	leftNumber, leftErr := numberValue(left)
	rightNumber, rightErr := numberValue(right)
	if leftErr == nil && rightErr == nil {
		_, leftBool := left.(bool)
		_, rightBool := right.(bool)
		if !leftBool && !rightBool {
			return leftNumber == rightNumber
		}
	}
	return reflect.DeepEqual(left, right)
}

func setContains(values setValue, candidate any) bool {
	for _, value := range values {
		if valuesEqual(value, candidate) {
			return true
		}
	}
	return false
}

func newSetValue(values []any) setValue {
	result := make(setValue, 0, len(values))
	for _, value := range values {
		if !setContains(result, value) {
			result = append(result, value)
		}
	}
	return result
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
	if err := validateNodeExpressions(n, p.lines, filename); err != nil {
		return nil, err
	}
	return n, nil
}

func validateNodeExpressions(nodes []node, lines []string, filename string) error {
	for index := range nodes {
		current := &nodes[index]
		expressionText := ""
		column := current.column
		switch current.kind {
		case "if":
			expressionText = current.condition
			column += len("If ")
		case "while":
			expressionText = current.condition
			column += len("While ")
		case "repeat":
			expressionText = current.condition
			column += len("Repeat ")
		case "for":
			expressionText = current.condition
			if current.line > 0 && current.line <= len(lines) {
				raw := strings.TrimLeft(lines[current.line-1], " \t")
				if separator := strings.Index(strings.ToLower(raw), " in "); separator >= 0 {
					column += utf8.RuneCountInString(raw[:separator+len(" in ")])
				}
			}
		}
		if expressionText != "" {
			parsed, err := parseTellExpressionAt(expressionText, current.line, column)
			if err != nil {
				return err
			}
			current.conditionExpr = parsed
		}
		if err := validateNodeExpressions(current.body, lines, filename); err != nil {
			return err
		}
		if err := validateNodeExpressions(current.otherwise, lines, filename); err != nil {
			return err
		}
		if err := validateNodeExpressions(current.handler, lines, filename); err != nil {
			return err
		}
	}
	return nil
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

