package tell

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// expression is an immutable syntax tree node. Parsing an expression never
// reads variables or performs an operation; evaluation is a separate pass.
type expression interface{ isExpression() }

type literalExpression struct{ token token }
type nameExpression struct{ name string }
type unaryExpression struct {
	operator string
	operand  expression
}
type binaryExpression struct {
	operator    string
	left, right expression
}
type listExpression struct{ items []expression }
type tupleExpression struct{ items []expression }
type setExpression struct{ items []expression }
type dictionaryExpression struct{ keys, values []expression }
type callArgument struct {
	name  string
	value expression
}
type callExpression struct {
	callee expression
	args   []callArgument
}
type memberExpression struct {
	value  expression
	member string
}
type indexExpression struct {
	value      expression
	index      expression
	start, end expression
	step       expression
	isSlice    bool
}

func (literalExpression) isExpression()    {}
func (nameExpression) isExpression()       {}
func (unaryExpression) isExpression()      {}
func (binaryExpression) isExpression()     {}
func (listExpression) isExpression()       {}
func (tupleExpression) isExpression()      {}
func (setExpression) isExpression()        {}
func (dictionaryExpression) isExpression() {}
func (callExpression) isExpression()       {}
func (memberExpression) isExpression()     {}
func (indexExpression) isExpression()      {}

type expressionParser struct {
	tokens []token
	pos    int
}

func parseTellExpressionAt(source string, sourceLine, sourceColumn int) (expression, error) {
	tokens, err := lex(source)
	if err != nil {
		return nil, err
	}
	for index := range tokens {
		originalLine := tokens[index].line
		tokens[index].line += sourceLine - 1
		if originalLine == 1 {
			tokens[index].column += sourceColumn - 1
		}
	}
	parser := &expressionParser{tokens: tokens}
	result, err := parser.parse()
	if err != nil {
		return nil, err
	}
	if parser.peek() != "eof" {
		unexpected := parser.tokens[parser.pos]
		return nil, fail("unexpected %q at line %d, column %d in expression", unexpected.text, unexpected.line, unexpected.column)
	}
	return result, nil
}

func (p *expressionParser) syntaxError(message string) error {
	current := p.tokens[p.pos]
	return fail("%s at line %d, column %d", message, current.line, current.column)
}

func (p *expressionParser) peek() string { return p.tokens[p.pos].kind }

func (p *expressionParser) peekNext() string {
	if p.pos+1 >= len(p.tokens) {
		return "eof"
	}
	return p.tokens[p.pos+1].kind
}

func (p *expressionParser) take() token {
	t := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return t
}

func (p *expressionParser) parse() (expression, error) { return p.parsePrecedence(1) }

func (p *expressionParser) parsePrecedence(min int) (expression, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		op := p.peek()
		prec := precedence(op)
		if prec == 0 || prec < min {
			return left, nil
		}
		p.take()
		next := prec + 1
		if op == "**" {
			next = prec
		}
		right, err := p.parsePrecedence(next)
		if err != nil {
			return nil, err
		}
		left = binaryExpression{operator: op, left: left, right: right}
	}
}

func (p *expressionParser) parsePrefix() (expression, error) {
	t := p.take()
	var value expression
	switch t.kind {
	case "number", "string", "true", "false", "nothing":
		value = literalExpression{token: t}
	case "-", "+", "not":
		operand, err := p.parsePrecedence(7)
		if err != nil {
			return nil, err
		}
		value = unaryExpression{operator: t.kind, operand: operand}
	case "(":
		if p.peek() == ")" {
			p.take()
			value = tupleExpression{items: []expression{}}
			break
		}
		first, err := p.parsePrecedence(1)
		if err != nil {
			return nil, err
		}
		if p.peek() == "," {
			items := []expression{first}
			for p.peek() == "," {
				p.take()
				if p.peek() == ")" {
					break
				}
				item, itemErr := p.parsePrecedence(1)
				if itemErr != nil {
					return nil, itemErr
				}
				items = append(items, item)
			}
			if p.peek() != ")" {
				return nil, p.syntaxError("expected ) after tuple")
			}
			p.take()
			value = tupleExpression{items: items}
		} else {
			if p.peek() != ")" {
				return nil, p.syntaxError("expected )")
			}
			p.take()
			value = first
		}
	case "[":
		items := []expression{}
		if p.peek() != "]" {
			for {
				item, err := p.parsePrecedence(1)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
				if p.peek() != "," {
					break
				}
				p.take()
				if p.peek() == "]" {
					break
				}
			}
		}
		if p.peek() != "]" {
			return nil, p.syntaxError("expected ]")
		}
		p.take()
		value = listExpression{items: items}
	case "{":
		keys, values := []expression{}, []expression{}
		if p.peek() == "}" {
			p.take()
			value = dictionaryExpression{keys: keys, values: values}
			break
		}
		first, err := p.parsePrecedence(1)
		if err != nil {
			return nil, err
		}
		if p.peek() == ":" {
			key := first
			for {
				p.take()
				item, itemErr := p.parsePrecedence(1)
				if itemErr != nil {
					return nil, itemErr
				}
				keys, values = append(keys, key), append(values, item)
				if p.peek() != "," {
					break
				}
				p.take()
				if p.peek() == "}" {
					break
				}
				key, err = p.parsePrecedence(1)
				if err != nil {
					return nil, err
				}
				if p.peek() != ":" {
					return nil, p.syntaxError("expected : in dictionary")
				}
			}
			if p.peek() != "}" {
				return nil, p.syntaxError("expected }")
			}
			p.take()
			value = dictionaryExpression{keys: keys, values: values}
		} else {
			items := []expression{first}
			for p.peek() == "," {
				p.take()
				if p.peek() == "}" {
					break
				}
				item, itemErr := p.parsePrecedence(1)
				if itemErr != nil {
					return nil, itemErr
				}
				items = append(items, item)
			}
			if p.peek() != "}" {
				return nil, p.syntaxError("expected } after set")
			}
			p.take()
			value = setExpression{items: items}
		}
	case "ident":
		value = nameExpression{name: t.text}
	default:
		return nil, fail("expected a value, found %q at line %d, column %d", t.text, t.line, t.column)
	}
	return p.parsePostfix(value)
}

func (p *expressionParser) parsePostfix(value expression) (expression, error) {
	for {
		switch p.peek() {
		case "(":
			p.take()
			args := []callArgument{}
			hasNamed := false
			if p.peek() != ")" {
				for {
					argument := callArgument{}
					if p.peek() == "ident" && p.peekNext() == "=" {
						argument.name = p.take().text
						p.take()
						hasNamed = true
					} else if hasNamed {
						return nil, p.syntaxError("positional arguments cannot follow named arguments")
					}
					arg, err := p.parsePrecedence(1)
					if err != nil {
						return nil, err
					}
					argument.value = arg
					args = append(args, argument)
					if p.peek() != "," {
						break
					}
					p.take()
					if p.peek() == ")" {
						break
					}
				}
			}
			if p.peek() != ")" {
				return nil, p.syntaxError("expected ) after arguments")
			}
			p.take()
			value = callExpression{callee: value, args: args}
		case ".":
			p.take()
			if p.peek() != "ident" {
				return nil, p.syntaxError("expected a member name after .")
			}
			value = memberExpression{value: value, member: p.take().text}
		case "[":
			p.take()
			part := indexExpression{value: value}
			if p.peek() != ":" && p.peek() != "]" {
				index, err := p.parsePrecedence(1)
				if err != nil {
					return nil, err
				}
				part.index = index
			}
			if p.peek() == ":" {
				part.isSlice = true
				p.take()
				if p.peek() != ":" && p.peek() != "]" {
					part.start = part.index
					part.index = nil
					var err error
					part.end, err = p.parsePrecedence(1)
					if err != nil {
						return nil, err
					}
				} else {
					part.start = part.index
					part.index = nil
				}
				if p.peek() == ":" {
					p.take()
					if p.peek() != "]" {
						var err error
						part.step, err = p.parsePrecedence(1)
						if err != nil {
							return nil, err
						}
					}
				}
			} else if part.index == nil {
				return nil, fail("expected an index or slice")
			}
			if p.peek() != "]" {
				return nil, p.syntaxError("expected ] after index")
			}
			p.take()
			value = part
		default:
			return value, nil
		}
	}
}

func (r *runtime) evalExpression(expr expression, e *env) (any, error) {
	switch x := expr.(type) {
	case literalExpression:
		switch x.token.kind {
		case "number":
			return strconv.ParseFloat(x.token.text, 64)
		case "string":
			return r.interpolate(x.token.text, e), nil
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, nil
		}
	case nameExpression:
		return e.get(x.name)
	case unaryExpression:
		value, err := r.evalExpression(x.operand, e)
		if err != nil {
			return nil, err
		}
		if x.operator == "not" {
			return !truthy(value), nil
		}
		n, err := numberValue(value)
		if err != nil {
			return nil, err
		}
		if x.operator == "-" {
			return -n, nil
		}
		return n, nil
	case binaryExpression:
		left, err := r.evalExpression(x.left, e)
		if err != nil {
			return nil, err
		}
		if x.operator == "and" && !truthy(left) {
			return false, nil
		}
		if x.operator == "or" && truthy(left) {
			return true, nil
		}
		right, err := r.evalExpression(x.right, e)
		if err != nil {
			return nil, err
		}
		if x.operator == "and" || x.operator == "or" {
			return truthy(right), nil
		}
		return applyBinary(x.operator, left, right)
	case listExpression:
		items := make([]any, 0, len(x.items))
		for _, item := range x.items {
			value, err := r.evalExpression(item, e)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, nil
	case tupleExpression:
		items := make(tupleValue, 0, len(x.items))
		for _, item := range x.items {
			value, err := r.evalExpression(item, e)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, nil
	case setExpression:
		items := make([]any, 0, len(x.items))
		for _, item := range x.items {
			value, err := r.evalExpression(item, e)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return newSetValue(items), nil
	case dictionaryExpression:
		items := make(map[string]any, len(x.keys))
		for i, keyExpr := range x.keys {
			key, err := r.evalExpression(keyExpr, e)
			if err != nil {
				return nil, err
			}
			value, err := r.evalExpression(x.values[i], e)
			if err != nil {
				return nil, err
			}
			items[fmt.Sprint(key)] = value
		}
		return items, nil
	case callExpression:
		callee, err := r.evalExpression(x.callee, e)
		if err != nil {
			return nil, err
		}
		args := make([]any, 0, len(x.args))
		named := make(map[string]any)
		for _, argument := range x.args {
			arg, err := r.evalExpression(argument.value, e)
			if err != nil {
				return nil, err
			}
			if argument.name == "" {
				args = append(args, arg)
				continue
			}
			if _, exists := named[argument.name]; exists {
				return nil, fail("duplicate named argument %q", argument.name)
			}
			named[argument.name] = arg
		}
		return invokeNamed(callee, args, named)
	case memberExpression:
		value, err := r.evalExpression(x.value, e)
		if err != nil {
			return nil, err
		}
		switch object := value.(type) {
		case pythonModule:
			if strings.HasPrefix(x.member, "_") || !pythonIdentifier.MatchString(x.member) {
				return nil, fail("invalid or private Python member %q", x.member)
			}
			return pythonFunction{module: object.name, name: x.member}, nil
		case javascriptModule:
			if strings.HasPrefix(x.member, "_") || !javaScriptIdentifier.MatchString(x.member) {
				return nil, fail("invalid or private JavaScript member %q", x.member)
			}
			return javascriptFunction{module: object.name, name: x.member}, nil
		case map[string]any:
			member, ok := object[x.member]
			if !ok {
				return nil, fail("unknown member %q", x.member)
			}
			return member, nil
		case *userInstance:
			if member, ok := object.fields[x.member]; ok {
				return member, nil
			}
			method, ok := object.class.method(x.member)
			if !ok {
				return nil, fail("%s has no member %q", object.class.name, x.member)
			}
			receiver := object
			return namedCallable(func(args []any, named map[string]any) (any, error) { return method.callNamed(args, named, receiver) }), nil
		case *bytecodeInstance:
			return bytecodeMember(r, object, x.member, object.class.functions)
		default:
			return nil, fail("this value has no member %q", x.member)
		}
	case indexExpression:
		value, err := r.evalExpression(x.value, e)
		if err != nil {
			return nil, err
		}
		if !x.isSlice {
			index, err := r.evalExpression(x.index, e)
			if err != nil {
				return nil, err
			}
			return indexValue(value, index)
		}
		var start, end *any
		if x.start != nil {
			part, err := r.evalExpression(x.start, e)
			if err != nil {
				return nil, err
			}
			start = &part
		}
		if x.end != nil {
			part, err := r.evalExpression(x.end, e)
			if err != nil {
				return nil, err
			}
			end = &part
		}
		stride := 1
		if x.step != nil {
			part, err := r.evalExpression(x.step, e)
			if err != nil {
				return nil, err
			}
			n, err := numberValue(part)
			if err != nil {
				return nil, err
			}
			stride = int(n)
		}
		if stride == 0 {
			return nil, fail("slice step cannot be zero")
		}
		switch value := value.(type) {
		case []any:
			indices, err := sliceIndices(len(value), start, end, stride)
			if err != nil {
				return nil, err
			}
			result := make([]any, 0, len(indices))
			for _, index := range indices {
				result = append(result, value[index])
			}
			return result, nil
		case tupleValue:
			indices, err := sliceIndices(len(value), start, end, stride)
			if err != nil {
				return nil, err
			}
			result := make(tupleValue, 0, len(indices))
			for _, index := range indices {
				result = append(result, value[index])
			}
			return result, nil
		case string:
			runes := []rune(value)
			indices, err := sliceIndices(len(runes), start, end, stride)
			if err != nil {
				return nil, err
			}
			var result strings.Builder
			for _, index := range indices {
				result.WriteRune(runes[index])
			}
			return result.String(), nil
		default:
			return nil, fail("this value cannot be sliced")
		}
	}
	return nil, fail("unsupported expression node %T", expr)
}

func indexValue(value, index any) (any, error) {
	switch x := value.(type) {
	case []any:
		n, err := numberValue(index)
		if err != nil {
			return nil, err
		}
		i := int(n)
		if i < 0 {
			i += len(x)
		}
		if i < 0 || i >= len(x) {
			return nil, fail("list index out of range")
		}
		return x[i], nil
	case tupleValue:
		n, err := numberValue(index)
		if err != nil {
			return nil, err
		}
		i := int(n)
		if i < 0 {
			i += len(x)
		}
		if i < 0 || i >= len(x) {
			return nil, fail("tuple index out of range")
		}
		return x[i], nil
	case string:
		n, err := numberValue(index)
		if err != nil {
			return nil, err
		}
		runes := []rune(x)
		i := int(n)
		if i < 0 {
			i += len(runes)
		}
		if i < 0 || i >= len(runes) {
			return nil, fail("text index out of range")
		}
		return string(runes[i]), nil
	case map[string]any:
		found, ok := x[fmt.Sprint(index)]
		if !ok {
			return nil, fail("dictionary key %q was not found", display(index))
		}
		return found, nil
	default:
		return nil, fail("this value cannot be indexed")
	}
}

func applyBinary(op string, a, b any) (any, error) {
	if op == "and" || op == "or" {
		return truthy(b), nil
	}
	if op == "==" {
		return valuesEqual(a, b), nil
	}
	if op == "!=" {
		return !valuesEqual(a, b), nil
	}
	if op == "<" {
		return compare(a, b) < 0, nil
	}
	if op == ">" {
		return compare(a, b) > 0, nil
	}
	if op == "<=" {
		return compare(a, b) <= 0, nil
	}
	if op == ">=" {
		return compare(a, b) >= 0, nil
	}
	if op == "in" || op == "notin" {
		found := false
		switch container := b.(type) {
		case []any:
			for _, item := range container {
				if reflect.DeepEqual(a, item) {
					found = true
					break
				}
			}
		case tupleValue:
			for _, item := range container {
				if reflect.DeepEqual(a, item) {
					found = true
					break
				}
			}
		case setValue:
			found = setContains(container, a)
		case string:
			found = strings.Contains(container, fmt.Sprint(a))
		case map[string]any:
			_, found = container[fmt.Sprint(a)]
		}
		if op == "notin" {
			found = !found
		}
		return found, nil
	}
	if op == "+" {
		if text, ok := a.(string); ok {
			return text + display(b), nil
		}
		if left, ok := a.([]any); ok {
			right, ok := b.([]any)
			if !ok {
				return nil, fail("cannot add a list to this value")
			}
			result := append([]any(nil), left...)
			return append(result, right...), nil
		}
		if left, ok := a.(tupleValue); ok {
			right, ok := b.(tupleValue)
			if !ok {
				return nil, fail("cannot add a tuple to this value")
			}
			result := append(tupleValue(nil), left...)
			return append(result, right...), nil
		}
		left, err := numberValue(a)
		if err != nil {
			return nil, err
		}
		right, err := numberValue(b)
		return left + right, err
	}
	if op == "-" || op == "*" || op == "/" || op == "//" || op == "%" || op == "**" {
		left, err := numberValue(a)
		if err != nil {
			return nil, err
		}
		right, err := numberValue(b)
		if err != nil {
			return nil, err
		}
		switch op {
		case "-":
			return left - right, nil
		case "*":
			return left * right, nil
		case "/":
			if right == 0 {
				return nil, fail("division by zero")
			}
			return left / right, nil
		case "//":
			if right == 0 {
				return nil, fail("division by zero")
			}
			return math.Floor(left / right), nil
		case "%":
			if right == 0 {
				return nil, fail("division by zero")
			}
			return math.Mod(left, right), nil
		default:
			return math.Pow(left, right), nil
		}
	}
	return nil, fail("unsupported operator %q", op)
}
