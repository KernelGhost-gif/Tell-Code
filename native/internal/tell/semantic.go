package tell

import "strings"

type semanticBinding struct {
	annotation string
	inferred   string
}

type semanticAnalyzer struct {
	filename string
	classes  map[string]bool
}

// Analyze performs conservative compile-time checks for known Tell value types.
// Unknown or dynamic expressions remain valid and keep their runtime checks.
func Analyze(source, filename string) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	return analyzeBytecodeSemantics(nodes, filename)
}

func analyzeBytecodeSemantics(nodes []node, filename string) error {
	analyzer := semanticAnalyzer{filename: filename, classes: map[string]bool{}}
	analyzer.collectClasses(nodes)
	return analyzer.statements(nodes, map[string]semanticBinding{}, false)
}

func (a *semanticAnalyzer) collectClasses(nodes []node) {
	for _, statement := range nodes {
		if statement.kind == "class" {
			a.classes[statement.name] = true
		}
		a.collectClasses(statement.body)
		a.collectClasses(statement.otherwise)
		a.collectClasses(statement.handler)
	}
}

func (a *semanticAnalyzer) statements(nodes []node, bindings map[string]semanticBinding, inFunction bool) error {
	for _, statement := range nodes {
		switch statement.kind {
		case "statement":
			if err := a.statement(statement, bindings, inFunction); err != nil {
				return err
			}
		case "class":
			bindings[statement.name] = semanticBinding{inferred: "class"}
			for _, member := range statement.body {
				local := map[string]semanticBinding{"this": {inferred: statement.name}}
				for _, parameter := range member.params {
					local[parameter] = semanticBinding{}
				}
				if err := a.statements(member.body, local, true); err != nil {
					return err
				}
			}
		case "function":
			bindings[statement.name] = semanticBinding{inferred: "function"}
			local := map[string]semanticBinding{statement.name: {inferred: "function"}}
			for _, parameter := range statement.params {
				local[parameter] = semanticBinding{}
			}
			if err := a.statements(statement.body, local, true); err != nil {
				return err
			}
		case "if":
			whenTrue := cloneSemanticBindings(bindings)
			whenFalse := cloneSemanticBindings(bindings)
			if err := a.statements(statement.body, whenTrue, inFunction); err != nil {
				return err
			}
			if err := a.statements(statement.otherwise, whenFalse, inFunction); err != nil {
				return err
			}
			mergeSemanticBindings(bindings, whenTrue, whenFalse)
		case "while", "repeat", "for", "try":
			local := cloneSemanticBindings(bindings)
			if statement.kind == "for" {
				local[statement.name] = semanticBinding{}
			}
			if err := a.statements(statement.body, local, inFunction); err != nil {
				return err
			}
			if err := a.statements(statement.handler, cloneSemanticBindings(bindings), inFunction); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *semanticAnalyzer) statement(statement node, bindings map[string]semanticBinding, inFunction bool) error {
	source := statement.text
	if rest, ok := prefixFold(source, "let "); ok {
		if tail, isConstant := prefixFold(rest, "constant "); isConstant {
			rest = tail
		}
		bindingText, expressionText, separated := splitFoldOnce(rest, " be ")
		name, annotation, annotated := splitFoldOnce(bindingText, " as ")
		if !annotated {
			name = bindingText
		}
		if separated && identifier(name) && strings.TrimSpace(expressionText) != "" {
			expressionType, err := a.expressionType(expressionText, bindings)
			if err != nil {
				return a.at(statement, err)
			}
			if annotated {
				if err := a.checkAnnotation(name, annotation, expressionType); err != nil {
					return a.at(statement, err)
				}
			}
			fact := semanticBinding{inferred: expressionType}
			if annotated {
				fact.annotation = strings.ToLower(strings.TrimSpace(annotation))
			}
			bindings[name] = fact
		}
		return nil
	}
	if rest, ok := prefixFold(source, "set "); ok {
		name, expressionText, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(name) && strings.TrimSpace(expressionText) != "" {
			expressionType, err := a.expressionType(expressionText, bindings)
			if err != nil {
				return a.at(statement, err)
			}
			key := name
			fact, exists := bindings[key]
			if exists && fact.annotation != "" {
				if err := a.checkAnnotation(name, fact.annotation, expressionType); err != nil {
					return a.at(statement, err)
				}
			} else if exists {
				fact.inferred = expressionType
				bindings[key] = fact
			}
		}
		return nil
	}
	if rest, ok := prefixFold(source, "give back"); ok && strings.TrimSpace(rest) != "" && inFunction {
		_, err := a.expressionType(rest, bindings)
		if err != nil {
			return a.at(statement, err)
		}
	}
	return nil
}

func (a *semanticAnalyzer) at(statement node, err error) error {
	return fail("%s:%d:%d: semantic error: %v", a.filename, statement.line, statement.column, err)
}

func (a *semanticAnalyzer) checkAnnotation(name, annotation, inferred string) error {
	typeName := strings.ToLower(strings.TrimSpace(annotation))
	known := map[string]bool{
		"any": true, "nothing": true, "boolean": true, "number": true,
		"text": true, "list": true, "tuple": true, "dictionary": true, "function": true, "class": true,
	}
	if !known[typeName] && !a.classes[strings.TrimSpace(annotation)] {
		return fail("unknown type annotation %q", annotation)
	}
	if typeName != "any" && inferred != "" && inferred != typeName {
		return fail("type mismatch for %q: annotation is %s, but the expression has type %s", name, annotation, inferred)
	}
	return nil
}

func (a *semanticAnalyzer) expressionType(source string, bindings map[string]semanticBinding) (string, error) {
	expr, err := parseBytecodeExpression(source)
	if err != nil {
		return "", err
	}
	return inferSemanticType(expr, bindings), nil
}

func inferSemanticType(expr expression, bindings map[string]semanticBinding) string {
	switch value := expr.(type) {
	case literalExpression:
		switch value.token.kind {
		case "number":
			return "number"
		case "string":
			return "text"
		case "true", "false":
			return "boolean"
		case "nothing":
			return "nothing"
		}
	case bytecodeConstantExpression:
		switch value.value.(type) {
		case float64, int, int64:
			return "number"
		case string:
			return "text"
		case bool:
			return "boolean"
		default:
			if value.value == nil {
				return "nothing"
			}
		}
	case nameExpression:
		if fact, ok := bindings[value.name]; ok {
			if fact.annotation != "" {
				return fact.annotation
			}
			return fact.inferred
		}
	case unaryExpression:
		if value.operator == "not" {
			return "boolean"
		}
		if inferSemanticType(value.operand, bindings) == "number" {
			return "number"
		}
	case binaryExpression:
		left := inferSemanticType(value.left, bindings)
		right := inferSemanticType(value.right, bindings)
		switch value.operator {
		case "==", "!=", "<", "<=", ">", ">=", "in", "notin", "and", "or":
			return "boolean"
		case "+":
			if left == "text" {
				return "text"
			}
			if left == "number" && (right == "number" || right == "text") {
				return "number"
			}
			if left == "list" && right == "list" {
				return "list"
			}
			if left == "tuple" && right == "tuple" {
				return "tuple"
			}
		case "-", "*", "/", "//", "%", "**":
			if left == "number" && right == "number" {
				return "number"
			}
		}
	case listExpression:
		return "list"
	case tupleExpression:
		return "tuple"
	case dictionaryExpression:
		return "dictionary"
	case indexExpression:
		valueType := inferSemanticType(value.value, bindings)
		if value.isSlice {
			if valueType == "text" || valueType == "list" || valueType == "tuple" {
				return valueType
			}
		} else if valueType == "text" {
			return "text"
		}
	}
	return ""
}

func cloneSemanticBindings(source map[string]semanticBinding) map[string]semanticBinding {
	clone := make(map[string]semanticBinding, len(source))
	for name, binding := range source {
		clone[name] = binding
	}
	return clone
}

func mergeSemanticBindings(destination, left, right map[string]semanticBinding) {
	for name, binding := range destination {
		leftBinding, leftOK := left[name]
		rightBinding, rightOK := right[name]
		if !leftOK || !rightOK || leftBinding != rightBinding {
			delete(destination, name)
			continue
		}
		destination[name] = binding
	}
	for name, leftBinding := range left {
		if _, exists := destination[name]; exists {
			continue
		}
		if rightBinding, ok := right[name]; ok && leftBinding == rightBinding {
			destination[name] = leftBinding
		}
	}
}
