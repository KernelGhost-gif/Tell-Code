package tell

import "strings"

type semanticBinding struct {
	annotation string
	inferred   string
}

type semanticSignature struct {
	parameters []string
	types []string
	returnType string
}

type semanticAnalyzer struct {
	filename string
	classes map[string]string
	functions map[string]semanticSignature
	currentReturn string
}

var semanticBuiltins = map[string]string{
	"len": "number", "range": "list", "sum": "number", "min": "number",
	"max": "number", "sorted": "list", "reversed": "list", "enumerate": "list",
	"zip": "list", "abs": "number", "round": "number", "int": "number",
	"float": "number", "number": "number", "str": "text", "bool": "boolean",
	"list": "list", "tuple": "tuple", "dict": "dictionary", "set": "set",
	"any": "boolean", "all": "boolean", "average": "number", "sqrt": "number",
	"floor": "number", "ceiling": "number", "type_of": "text",
}

var semanticBuiltinTypes = map[string]bool{
	"any": true, "nothing": true, "boolean": true, "bool": true, "void": true,
	"number": true, "decimal": true,
	"text": true, "list": true, "tuple": true, "set": true,
	"dictionary": true, "function": true, "class": true,
}

// Analyze performs conservative compile-time checks for statically knowable
// expression types. Dynamic values remain valid and are checked at runtime.
func Analyze(source, filename string) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	return analyzeBytecodeSemantics(nodes, filename)
}

func analyzeBytecodeSemantics(nodes []node, filename string) error {
	analyzer := semanticAnalyzer{filename: filename, classes: make(map[string]string), functions: make(map[string]semanticSignature)}
	analyzer.collectClasses(nodes)
	analyzer.collectFunctions(nodes)
	return analyzer.statements(nodes, map[string]semanticBinding{}, false)
}

func (a *semanticAnalyzer) collectClasses(nodes []node) {
	for _, statement := range nodes {
		if statement.kind == "class" {
			a.classes[statement.name] = statement.base
		}
		a.collectClasses(statement.body)
		a.collectClasses(statement.otherwise)
		a.collectClasses(statement.handler)
	}
}

func (a *semanticAnalyzer) collectFunctions(nodes []node) {
	for _, statement := range nodes {
		if statement.kind == "function" {
			a.functions[statement.name] = semanticSignature{
				parameters: append([]string(nil), statement.params...),
				types: append([]string(nil), statement.paramTypes...),
				returnType: statement.returnType,
			}
		}
		a.collectFunctions(statement.body)
		a.collectFunctions(statement.otherwise)
		a.collectFunctions(statement.handler)
	}
}

func normalizeSemanticType(typeName string) string {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "bool":
		return "boolean"
	case "void":
		return "nothing"
	case "decimal":
		return "number"
	default:
		return strings.ToLower(strings.TrimSpace(typeName))
	}
}

func (a *semanticAnalyzer) statements(nodes []node, bindings map[string]semanticBinding, inFunction bool) error {
	for _, statement := range nodes {
		switch statement.kind {
		case "statement":
			if err := a.statement(statement, bindings); err != nil {
				return err
			}
		case "class":
			bindings[statement.name] = semanticBinding{inferred: "class"}
			for _, member := range statement.body {
				local := map[string]semanticBinding{
					"this": {inferred: "class:" + statement.name},
				}
				for index, parameter := range member.params {
					typeName := ""
					if index < len(member.paramTypes) {
						typeName = member.paramTypes[index]
					}
					if typeName == "" {
						local[parameter] = semanticBinding{inferred: "any"}
					} else {
						if err := a.checkAnnotation(parameter, typeName, "any"); err != nil {
							return a.at(member, err)
						}
						local[parameter] = semanticBinding{annotation: typeName}
					}
				}
				previousReturn := a.currentReturn
				a.currentReturn = member.returnType
				if err := a.statements(member.body, local, true); err != nil {
					a.currentReturn = previousReturn
					return err
				}
				a.currentReturn = previousReturn
			}
		case "function":
			bindings[statement.name] = semanticBinding{inferred: "function"}
			local := map[string]semanticBinding{statement.name: {inferred: "function"}}
			for index, parameter := range statement.params {
				typeName := ""
				if index < len(statement.paramTypes) {
					typeName = statement.paramTypes[index]
				}
				if typeName == "" {
					local[parameter] = semanticBinding{inferred: "any"}
				} else {
					if err := a.checkAnnotation(parameter, typeName, "any"); err != nil {
						return a.at(statement, err)
					}
					local[parameter] = semanticBinding{annotation: typeName}
				}
			}
			previousReturn := a.currentReturn
			a.currentReturn = statement.returnType
			if err := a.statements(statement.body, local, true); err != nil {
				a.currentReturn = previousReturn
				return err
			}
			a.currentReturn = previousReturn
		case "if":
			if statement.conditionExpr != nil {
				if _, err := a.infer(statement.conditionExpr, bindings); err != nil {
					return a.at(statement, err)
				}
			}
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
			if statement.conditionExpr != nil {
				if _, err := a.infer(statement.conditionExpr, bindings); err != nil {
					return a.at(statement, err)
				}
			}
			local := cloneSemanticBindings(bindings)
			if statement.kind == "for" {
				names, valid := parseLoopNames(statement.name)
				if valid {
					for _, name := range names {
						local[name] = semanticBinding{inferred: "any"}
					}
				}
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

func (a *semanticAnalyzer) statement(statement node, bindings map[string]semanticBinding) error {
	simple := statement.simple
	if simple == nil {
		return nil
	}
	inferExpression := func(expression expression) (string, error) {
		if expression == nil {
			return "nothing", nil
		}
		return a.infer(expression, bindings)
	}

	switch simple.kind {
	case "return":
		inferred, err := inferExpression(simple.expression)
		if err != nil {
			return a.at(statement, err)
		}
		if a.currentReturn != "" {
			if strings.EqualFold(a.currentReturn, "void") {
				if simple.hasExpression && inferred != "nothing" {
					return a.at(statement, fail("void function cannot return a value"))
				}
			} else if !simple.hasExpression {
				return a.at(statement, fail("%q must return a value of type %s", "function", a.currentReturn))
			} else if err := a.checkAnnotation("return value", a.currentReturn, inferred); err != nil {
				return a.at(statement, err)
			}
		}
	case "let":
		inferred, err := inferExpression(simple.expression)
		if err != nil {
			return a.at(statement, err)
		}
		if len(simple.names) > 1 {
			for _, name := range simple.names {
				bindings[name] = semanticBinding{inferred: "any"}
			}
			return nil
		}
		if len(simple.names) == 0 {
			return nil
		}
		name := simple.names[0]
		if simple.annotation != "" {
			if err := a.checkAnnotation(name, simple.annotation, inferred); err != nil {
				return a.at(statement, err)
			}
		}
		fact := semanticBinding{inferred: inferred}
		if simple.annotation != "" {
			fact.annotation = strings.TrimSpace(simple.annotation)
		}
		bindings[name] = fact
	case "set":
		inferred, err := inferExpression(simple.expression)
		if err != nil {
			return a.at(statement, err)
		}
		for _, name := range simple.names {
			fact, exists := bindings[name]
			if !exists {
				continue
			}
			if fact.annotation != "" {
				if err := a.checkAnnotation(name, fact.annotation, inferred); err != nil {
					return a.at(statement, err)
				}
			} else {
				fact.inferred = inferred
				bindings[name] = fact
			}
		}
	default:
		if simple.hasExpression {
			if _, err := inferExpression(simple.expression); err != nil {
				return a.at(statement, err)
			}
		}
		if simple.hasTarget {
			if _, err := inferExpression(simple.target); err != nil {
				return a.at(statement, err)
			}
		}
	}
	return nil
}

func (a *semanticAnalyzer) at(statement node, err error) error {
	if err == nil {
		return nil
	}
	return fail("%s:%d:%d: %v", a.filename, statement.line, statement.column, err)
}

func (a *semanticAnalyzer) checkAnnotation(name, annotation, inferred string) error {
	typeName := strings.TrimSpace(annotation)
	normalized := strings.ToLower(typeName)
	if !semanticBuiltinTypes[normalized] {
		if _, exists := a.classes[typeName]; !exists {
			return fail("unknown type annotation %q", typeName)
		}
	}
	if inferred == "" || inferred == "any" || normalized == "any" {
		return nil
	}
	normalized = normalizeSemanticType(normalized)
	if !strings.HasPrefix(inferred, "class:") {
		inferred = normalizeSemanticType(inferred)
	}
	if strings.HasPrefix(inferred, "class:") {
		actual := strings.TrimPrefix(inferred, "class:")
		if _, expectedIsClass := a.classes[typeName]; expectedIsClass && a.isSubtype(actual, typeName) {
			return nil
		}
		return fail("type mismatch for %q: annotation is %s, but the expression has the type %s", name, typeName, actual)
	}
	if inferred != normalized {
		return fail("type mismatch for %q: annotation is %s, but the expression has the type %s", name, typeName, inferred)
	}
	return nil
}

func (a *semanticAnalyzer) isSubtype(actual, expected string) bool {
	seen := make(map[string]bool)
	for current := actual; current != "" && !seen[current]; current = a.classes[current] {
		if current == expected {
			return true
		}
		seen[current] = true
	}
	return false
}

func (a *semanticAnalyzer) infer(expr expression, bindings map[string]semanticBinding) (string, error) {
	switch value := expr.(type) {
	case literalExpression:
		switch value.token.kind {
		case "number":
			return "number", nil
		case "string":
			return "text", nil
		case "true", "false":
			return "boolean", nil
		case "nothing":
			return "nothing", nil
		}
	case bytecodeConstantExpression:
		switch constant := value.value.(type) {
		case float64, int, int64:
			return "number", nil
		case string:
			return "text", nil
		case bool:
			return "boolean", nil
		default:
			if constant == nil {
				return "nothing", nil
			}
		}
	case nameExpression:
		if binding, exists := bindings[value.name]; exists {
			if binding.annotation != "" {
				if _, class := a.classes[binding.annotation]; class {
					return "class:" + binding.annotation, nil
				}
				return strings.ToLower(binding.annotation), nil
			}
			if binding.inferred != "" {
				return binding.inferred, nil
			}
		}
		if _, exists := semanticBuiltins[value.name]; exists {
			return "function", nil
		}
		if _, exists := a.classes[value.name]; exists {
			return "class", nil
		}
	case unaryExpression:
		operand, err := a.infer(value.operand, bindings)
		if err != nil {
			return "", err
		}
		if value.operator == "not" {
			return "boolean", nil
		}
		if operand == "any" || operand == "" {
			return "any", nil
		}
		if operand == "number" {
			return "number", nil
		}
		return "", fail("operator %s requires number, got %s", value.operator, operand)
	case binaryExpression:
		left, err := a.infer(value.left, bindings)
		if err != nil {
			return "", err
		}
		right, err := a.infer(value.right, bindings)
		if err != nil {
			return "", err
		}
		switch value.operator {
		case "==", "!=", "<", "<=", ">", ">=", "in", "notin", "and", "or":
			return "boolean", nil
		case "+":
			if left == "text" {
				return "text", nil
			}
			if left == "list" && right == "list" {
				return "list", nil
			}
			if left == "tuple" && right == "tuple" {
				return "tuple", nil
			}
			if left == "number" && (right == "number" || right == "text") {
				return "number", nil
			}
			if left == "any" || right == "any" || left == "" || right == "" {
				return "any", nil
			}
			return "", fail("operator + requires compatible operands, got %s and %s", left, right)
		case "-", "*", "/", "//", "%", "**", "&", "|", "^", "<<", ">>":
			if left == "number" && right == "number" {
				return "number", nil
			}
			if left == "any" || right == "any" || left == "" || right == "" {
				return "any", nil
			}
			return "", fail("operator %s requires numeric operands, got %s and %s", value.operator, left, right)
		}
	case conditionalExpression:
		if _, err := a.infer(value.condition, bindings); err != nil {
			return "", err
		}
		consequent, err := a.infer(value.consequent, bindings)
		if err != nil {
			return "", err
		}
		alternative, err := a.infer(value.alternative, bindings)
		if err != nil {
			return "", err
		}
		if consequent == alternative {
			return consequent, nil
		}
		return "any", nil
	case listExpression:
		for _, child := range value.items {
			if _, err := a.infer(child, bindings); err != nil {
				return "", err
			}
		}
		return "list", nil
	case tupleExpression:
		for _, child := range value.items {
			if _, err := a.infer(child, bindings); err != nil {
				return "", err
			}
		}
		return "tuple", nil
	case setExpression:
		for _, child := range value.items {
			if _, err := a.infer(child, bindings); err != nil {
				return "", err
			}
		}
		return "set", nil
	case dictionaryExpression:
		for i, key := range value.keys {
			if _, err := a.infer(key, bindings); err != nil {
				return "", err
			}
			if i < len(value.values) {
				if _, err := a.infer(value.values[i], bindings); err != nil {
					return "", err
				}
			}
		}
		return "dictionary", nil
	case indexExpression:
		parent, err := a.infer(value.value, bindings)
		if err != nil {
			return "", err
		}
		if value.isSlice {
			for _, bound := range []expression{value.start, value.end, value.step} {
				if bound != nil {
					if _, err := a.infer(bound, bindings); err != nil {
						return "", err
					}
				}
			}
			if parent == "text" || parent == "list" || parent == "tuple" {
				return parent, nil
			}
		} else {
			if _, err := a.infer(value.index, bindings); err != nil {
				return "", err
			}
			if parent == "text" {
				return "text", nil
			}
		}
	case memberExpression:
		if _, err := a.infer(value.value, bindings); err != nil {
			return "", err
		}
		return "any", nil
	case callExpression:
		argumentTypes := make([]string, len(value.args))
		for index, argument := range value.args {
			inferred, err := a.infer(argument.value, bindings)
			if err != nil {
				return "", err
			}
			argumentTypes[index] = inferred
		}
		if callee, ok := value.callee.(nameExpression); ok {
			if _, isClass := a.classes[callee.name]; isClass {
				return "class:" + callee.name, nil
			}
			if result, isBuiltin := semanticBuiltins[callee.name]; isBuiltin {
				return result, nil
			}
			if signature, exists := a.functions[callee.name]; exists {
				positionalIndex := 0
				used := make(map[string]bool)
				for index, argument := range value.args {
					parameterIndex := -1
					if argument.name != "" {
						for candidate, parameter := range signature.parameters {
							if parameter == argument.name {
								parameterIndex = candidate
								break
							}
						}
					} else {
						parameterIndex = positionalIndex
						positionalIndex++
					}
					if parameterIndex < 0 || parameterIndex >= len(signature.parameters) {
						continue
					}
					parameterName := signature.parameters[parameterIndex]
					if used[parameterName] {
						return "", fail("%q got multiple values for %q", callee.name, parameterName)
					}
					used[parameterName] = true
					if parameterIndex < len(signature.types) && signature.types[parameterIndex] != "" {
						if err := a.checkAnnotation(parameterName, signature.types[parameterIndex], argumentTypes[index]); err != nil {
							return "", err
						}
					}
				}
				if signature.returnType != "" {
					if _, class := a.classes[signature.returnType]; class {
						return "class:" + signature.returnType, nil
					}
					return normalizeSemanticType(signature.returnType), nil
				}
			}
		}
		return "any", nil
	}
	return "any", nil
}

func cloneSemanticBindings(source map[string]semanticBinding) map[string]semanticBinding {
	clone := make(map[string]semanticBinding, len(source))
	for name, binding := range source {
		clone[name] = binding
	}
	return clone
}

func mergeSemanticBindings(destination, left, right map[string]semanticBinding) {
	for name := range destination {
		leftBinding, leftOK := left[name]
		rightBinding, rightOK := right[name]
		if !leftOK || !rightOK || leftBinding != rightBinding {
			delete(destination, name)
		}
	}
	for name, leftBinding := range left {
		if rightBinding, ok := right[name]; ok && leftBinding == rightBinding {
			destination[name] = leftBinding
		}
	}
}
