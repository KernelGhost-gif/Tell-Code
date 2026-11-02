package tell

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const bytecodeFormat = "tell-bytecode-v1"

// BytecodeInstruction is a serializable operation in Tell's portable bytecode.
// Its fields are exported so tools can inspect the artifact as JSON.
type BytecodeInstruction struct {
	Op       string   `json:"op"`
	Name     string   `json:"name,omitempty"`
	Type     string   `json:"type,omitempty"`
	Constant bool     `json:"constant,omitempty"`
	Value    any      `json:"value,omitempty"`
	Count    int      `json:"count,omitempty"`
	Jump     int      `json:"jump,omitempty"`
	ArgNames []string `json:"argument_names,omitempty"`
	Line     int      `json:"line"`
}

// BytecodeProgram is the versioned on-disk representation emitted by Compile.
type BytecodeProgram struct {
	Format       string                `json:"format"`
	Instructions []BytecodeInstruction `json:"instructions"`
	Functions    []BytecodeFunction    `json:"functions,omitempty"`
	Classes      []BytecodeClass       `json:"classes,omitempty"`
	Modules      []BytecodeModule      `json:"modules,omitempty"`
}

// BytecodeModule embeds one compiled local module so the artifact is self-contained.
type BytecodeModule struct {
	Path    string          `json:"path"`
	Program BytecodeProgram `json:"program"`
}

// BytecodeFunction stores a separately compiled function body and its signature.
type BytecodeFunction struct {
	Name         string                `json:"name"`
	Parameters   []string              `json:"parameters"`
	Receiver     bool                  `json:"receiver,omitempty"`
	Instructions []BytecodeInstruction `json:"instructions"`
}

// BytecodeClass describes the constructor and methods for a compiled Tell class.
type BytecodeClass struct {
	Name        string         `json:"name"`
	Base        string         `json:"base,omitempty"`
	Constructor *int           `json:"constructor,omitempty"`
	Methods     map[string]int `json:"methods"`
}

type bytecodeCompiler struct {
	code          []BytecodeInstruction
	line          int
	loops         []bytecodeLoop
	functions     *[]BytecodeFunction
	classes       *[]BytecodeClass
	inFunction    bool
	receiver      bool
	allowRemember bool
	tryDepth      int
	filename      string
	rootDirectory string
	moduleCache   map[string]BytecodeProgram
	moduleLoading map[string]bool
}

type bytecodeLoop struct {
	continueJumps []int
	breakJumps    []int
	tryDepth      int
}

func (c *bytecodeCompiler) emit(op string) int {
	c.code = append(c.code, BytecodeInstruction{Op: op, Line: c.line})
	return len(c.code) - 1
}

func (c *bytecodeCompiler) patch(index, target int) { c.code[index].Jump = target }

// Compile translates the supported Tell subset to versioned stack bytecode.
// Programs using unsupported statements fail with a source line diagnostic.
func Compile(source, filename string) ([]byte, error) {
	rootFilename, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	context := &bytecodeCompiler{
		filename:      rootFilename,
		rootDirectory: filepath.Dir(rootFilename),
		moduleCache:   map[string]BytecodeProgram{},
		moduleLoading: map[string]bool{},
	}
	context.moduleLoading[rootFilename] = true
	program, err := context.compileProgram(source, rootFilename)
	delete(context.moduleLoading, rootFilename)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(context.moduleCache))
	for path := range context.moduleCache {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	program.Modules = make([]BytecodeModule, 0, len(paths))
	for _, path := range paths {
		program.Modules = append(program.Modules, BytecodeModule{Path: path, Program: context.moduleCache[path]})
	}
	data, err := json.MarshalIndent(program, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (context *bytecodeCompiler) compileProgram(source, filename string) (BytecodeProgram, error) {
	nodes, err := parse(source, filename)
	if err != nil {
		return BytecodeProgram{}, err
	}
	if err := analyzeBytecodeSemantics(nodes, filename); err != nil {
		return BytecodeProgram{}, err
	}
	functions := []BytecodeFunction{}
	classes := []BytecodeClass{}
	c := &bytecodeCompiler{functions: &functions, classes: &classes, filename: filename, rootDirectory: context.rootDirectory, moduleCache: context.moduleCache, moduleLoading: context.moduleLoading}
	if err := c.statements(nodes); err != nil {
		return BytecodeProgram{}, err
	}
	c.emit("HALT")
	program := BytecodeProgram{Format: bytecodeFormat, Instructions: c.code, Functions: functions, Classes: classes}
	if err := validateBytecode(program.Instructions, "HALT", len(program.Functions), len(program.Classes), false); err != nil {
		return BytecodeProgram{}, err
	}
	if err := verifyBytecodeStack(program.Instructions); err != nil {
		return BytecodeProgram{}, err
	}
	for _, function := range program.Functions {
		if err := validateBytecode(function.Instructions, "RETURN", len(program.Functions), len(program.Classes), true); err != nil {
			return BytecodeProgram{}, fail("invalid compiled function %q: %v", function.Name, err)
		}
		if err := verifyBytecodeStack(function.Instructions); err != nil {
			return BytecodeProgram{}, fail("invalid compiled function %q: %v", function.Name, err)
		}
	}
	return program, nil
}

func (c *bytecodeCompiler) statements(nodes []node) error {
	for _, statement := range nodes {
		c.line = statement.line
		switch statement.kind {
		case "function":
			index, err := c.compileFunction(statement.name, statement.params, statement.body, false)
			if err != nil {
				return err
			}
			instruction := c.emit("DEFINE_FUNC")
			c.code[instruction].Name = statement.name
			c.code[instruction].Count = index
		case "class":
			compiledClass := BytecodeClass{Name: statement.name, Base: statement.base, Methods: map[string]int{}}
			hasConstructor := false
			for _, member := range statement.body {
				index, err := c.compileFunction(member.name, member.params, member.body, true)
				if err != nil {
					return err
				}
				if member.kind == "constructor" {
					if !strings.EqualFold(member.name, statement.name) {
						return fail("constructor name %q does not match class %q", member.name, statement.name)
					}
					if hasConstructor {
						return fail("class %q has more than one constructor", statement.name)
					}
					hasConstructor = true
					compiledClass.Constructor = &index
					continue
				}
				if _, exists := compiledClass.Methods[member.name]; exists {
					return fail("class %q has duplicate method %q", statement.name, member.name)
				}
				compiledClass.Methods[member.name] = index
			}
			classIndex := len(*c.classes)
			*c.classes = append(*c.classes, compiledClass)
			instruction := c.emit("DEFINE_CLASS")
			instructionName := statement.name
			c.code[instruction].Name = instructionName
			c.code[instruction].Count = classIndex
		case "statement":
			if err := c.statement(statement.text); err != nil {
				return fail("cannot compile line %d: %v", statement.line, err)
			}
		case "if":
			if err := c.expression(statement.condition); err != nil {
				return fail("cannot compile line %d: %v", statement.line, err)
			}
			falseJump := c.emit("JUMP_IF_FALSE")
			c.emit("POP")
			if err := c.statements(statement.body); err != nil {
				return err
			}
			endJump := c.emit("JUMP")
			c.patch(falseJump, len(c.code))
			c.emit("POP")
			if err := c.statements(statement.otherwise); err != nil {
				return err
			}
			c.patch(endJump, len(c.code))
		case "while":
			start := len(c.code)
			if err := c.expression(statement.condition); err != nil {
				return fail("cannot compile line %d: %v", statement.line, err)
			}
			endJump := c.emit("JUMP_IF_FALSE")
			c.emit("POP")
			c.loops = append(c.loops, bytecodeLoop{tryDepth: c.tryDepth})
			if err := c.statements(statement.body); err != nil {
				return err
			}
			loop := c.popLoop()
			back := c.emit("JUMP")
			c.patch(back, start)
			for _, jump := range loop.continueJumps {
				c.patch(jump, start)
			}
			c.patch(endJump, len(c.code))
			c.emit("POP")
			for _, jump := range loop.breakJumps {
				c.patch(jump, len(c.code))
			}
		case "repeat", "for":
			if err := c.expression(statement.condition); err != nil {
				return fail("cannot compile line %d: %v", statement.line, err)
			}
			start := len(c.code)
			if statement.kind == "repeat" {
				c.emit("REPEAT_INIT")
			} else {
				c.emit("ITER_INIT")
				c.code[start].Name = statement.name
			}
			c.loops = append(c.loops, bytecodeLoop{tryDepth: c.tryDepth})
			if err := c.statements(statement.body); err != nil {
				return err
			}
			loop := c.popLoop()
			nextOp := "REPEAT_NEXT"
			if statement.kind == "for" {
				nextOp = "ITER_NEXT"
			}
			next := c.emit(nextOp)
			c.code[next].Count = start
			c.patch(next, start+1)
			for _, jump := range loop.continueJumps {
				c.patch(jump, next)
			}
			end := len(c.code)
			c.patch(start, end)
			for _, jump := range loop.breakJumps {
				c.patch(jump, end)
			}
		case "try":
			handlerJump := c.emit("TRY_PUSH")
			c.tryDepth++
			if err := c.statements(statement.body); err != nil {
				return err
			}
			c.tryDepth--
			c.emit("TRY_POP")
			endJump := c.emit("JUMP")
			c.patch(handlerJump, len(c.code))
			if err := c.statements(statement.handler); err != nil {
				return err
			}
			c.patch(endJump, len(c.code))
		default:
			return fail("cannot compile %s blocks yet (line %d)", statement.kind, statement.line)
		}
	}
	return nil
}

func (c *bytecodeCompiler) compileFunction(name string, params []string, body []node, receiver bool) (int, error) {
	functionCompiler := &bytecodeCompiler{functions: c.functions, classes: c.classes, inFunction: true, receiver: receiver, allowRemember: receiver || c.allowRemember, filename: c.filename, rootDirectory: c.rootDirectory, moduleCache: c.moduleCache, moduleLoading: c.moduleLoading}
	if err := functionCompiler.statements(body); err != nil {
		return 0, err
	}
	functionCompiler.emit("PUSH")
	functionCompiler.emit("RETURN")
	function := BytecodeFunction{Name: name, Parameters: append([]string(nil), params...), Receiver: receiver, Instructions: functionCompiler.code}
	index := len(*c.functions)
	*c.functions = append(*c.functions, function)
	return index, nil
}

func (c *bytecodeCompiler) popLoop() bytecodeLoop {
	index := len(c.loops) - 1
	loop := c.loops[index]
	c.loops = c.loops[:index]
	return loop
}

func (c *bytecodeCompiler) statement(source string) error {
	if expression, ok := prefixFold(source, "check that "); ok {
		if expression == "" {
			return fmt.Errorf("Check that needs a condition")
		}
		if err := c.expression(expression); err != nil {
			return err
		}
		instruction := c.emit("ASSERT")
		c.code[instruction].Name = expression
		return nil
	}
	if rest, ok := prefixFold(source, "use the "); ok {
		library, suffix, hasLibrary := splitFoldOnce(rest, " library")
		if hasLibrary && library != "" {
			library = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(library), " ", ""))
			alias := ""
			if suffix != "" {
				if !strings.HasPrefix(strings.ToLower(suffix), "as ") {
					return fmt.Errorf("expected an optional alias after the library name")
				}
				alias = strings.TrimSpace(suffix[3:])
				if !identifier(alias) {
					return fmt.Errorf("expected a valid library alias")
				}
			}
			canonical := map[string]string{"mathematics": "math", "dates": "datetime", "dateandtime": "datetime"}[library]
			if canonical == "" {
				canonical = library
			}
			if _, err := moduleValue(canonical); err != nil {
				return err
			}
			if alias == "" {
				alias = canonical
			}
			instruction := c.emit("USE_LIBRARY")
			c.code[instruction].Name = alias
			c.code[instruction].Value = canonical
			return nil
		}
	}
	if rest, ok := prefixFold(source, "use the module "); ok {
		moduleExpression, alias, separated := splitFoldOnce(rest, " as ")
		if !separated || !identifier(alias) {
			return fmt.Errorf("expected Use the module \"path.tell\" as alias")
		}
		pathExpression, err := parseBytecodeExpression(moduleExpression)
		if err != nil {
			return fmt.Errorf("invalid Tell module path: %v", err)
		}
		literal, ok := pathExpression.(literalExpression)
		if !ok || literal.token.kind != "string" {
			return fmt.Errorf("Tell module path must be a quoted string")
		}
		modulePath, err := c.compileModule(literal.token.text)
		if err != nil {
			return err
		}
		instruction := c.emit("IMPORT_MODULE")
		c.code[instruction].Name = alias
		c.code[instruction].Value = modulePath
		return nil
	}
	if rest, ok := prefixFold(source, "give back"); ok {
		if !c.inFunction {
			return fmt.Errorf("give back can only be used inside a function")
		}
		if strings.TrimSpace(rest) == "" {
			instruction := c.emit("PUSH")
			c.code[instruction].Value = nil
		} else if err := c.expression(rest); err != nil {
			return err
		}
		c.emit("RETURN")
		return nil
	}
	if strings.EqualFold(source, "stop this loop") || strings.EqualFold(source, "break out of this loop") {
		if len(c.loops) == 0 {
			return fmt.Errorf("loop break used outside a loop")
		}
		loop := &c.loops[len(c.loops)-1]
		for depth := loop.tryDepth; depth < c.tryDepth; depth++ {
			c.emit("TRY_POP")
		}
		loop.breakJumps = append(loop.breakJumps, c.emit("JUMP"))
		return nil
	}
	if strings.EqualFold(source, "skip to the next repetition") || strings.EqualFold(source, "continue to the next repetition") {
		if len(c.loops) == 0 {
			return fmt.Errorf("loop continue used outside a loop")
		}
		loop := &c.loops[len(c.loops)-1]
		for depth := loop.tryDepth; depth < c.tryDepth; depth++ {
			c.emit("TRY_POP")
		}
		loop.continueJumps = append(loop.continueJumps, c.emit("JUMP"))
		return nil
	}
	if expression, ok := prefixFold(source, "raise an error saying "); ok && strings.TrimSpace(expression) != "" {
		if err := c.expression(expression); err != nil {
			return err
		}
		c.emit("RAISE")
		return nil
	}
	if rest, ok := prefixFold(source, "remember "); ok {
		if !c.allowRemember {
			return fmt.Errorf("Remember ... as this.field is only supported in a class method or constructor")
		}
		expressionText, field, separated := splitFoldOnce(rest, " as this.")
		if !separated || !identifier(field) {
			return fmt.Errorf("expected Remember expression as this.field")
		}
		if err := c.expression(expressionText); err != nil {
			return err
		}
		loadThis := c.emit("LOAD")
		c.code[loadThis].Name = "this"
		set := c.emit("SET_FIELD")
		c.code[set].Name = field
		return nil
	}
	if rest, ok := prefixFold(source, "let "); ok {
		constant := false
		if tail, isConstant := prefixFold(rest, "constant "); isConstant {
			rest, constant = tail, true
		}
		binding, expression, separated := splitFoldOnce(rest, " be ")
		name, annotation, annotated := splitFoldOnce(binding, " as ")
		if !annotated {
			name = binding
		}
		if separated && identifier(name) && expression != "" {
			if err := c.expression(expression); err != nil {
				return err
			}
			instruction := c.emit("DECLARE")
			c.code[instruction].Name = name
			c.code[instruction].Type = annotation
			c.code[instruction].Constant = constant
			return nil
		}
	}
	if rest, ok := prefixFold(source, "set "); ok {
		name, expression, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(name) && expression != "" {
			if err := c.expression(expression); err != nil {
				return err
			}
			instruction := c.emit("ASSIGN")
			c.code[instruction].Name = name
			return nil
		}
		if separated && expression != "" {
			target, err := parseBytecodeExpression(name)
			if err != nil {
				return err
			}
			member, ok := target.(memberExpression)
			if !ok {
				return fmt.Errorf("compiled property assignment needs an object.field target")
			}
			if err := c.compileExpression(member.value); err != nil {
				return err
			}
			if err := c.expression(expression); err != nil {
				return err
			}
			instruction := c.emit("SET_MEMBER")
			c.code[instruction].Name = member.member
			return nil
		}
	}
	for _, prefix := range []string{"say ", "show ", "print "} {
		if expression, ok := prefixFold(source, prefix); ok && expression != "" {
			if err := c.expression(expression); err != nil {
				return err
			}
			c.emit("PRINT")
			return nil
		}
	}
	if rest, ok := prefixFold(source, "tell "); ok {
		objectName, action, separated := splitFoldOnce(rest, " to ")
		if separated && identifier(objectName) {
			method, argsText := firstWord(action)
			if !identifier(method) {
				return fmt.Errorf("expected a method name after Tell %s to", objectName)
			}
			args := []string{}
			if argsText != "" {
				argsText, separated = prefixFold(argsText, "with ")
				if !separated || strings.TrimSpace(argsText) == "" {
					return fmt.Errorf("expected method arguments after with")
				}
				args = splitAnd(argsText)
			}
			if err := c.expression(objectName + "." + method + "(" + strings.Join(args, ",") + ")"); err != nil {
				return err
			}
			c.emit("POP")
			return nil
		}
	}
	if err := c.expression(source); err == nil {
		c.emit("POP")
		return nil
	}
	return fmt.Errorf("unsupported statement %q", source)
}

func (c *bytecodeCompiler) compileModule(importPath string) (string, error) {
	if !strings.EqualFold(filepath.Ext(importPath), ".tell") {
		return "", fail("Tell modules must use the .tell extension")
	}
	absPath, err := filepath.Abs(filepath.Join(filepath.Dir(c.filename), importPath))
	if err != nil {
		return "", err
	}
	absPath = filepath.Clean(absPath)
	pathKey, err := filepath.Rel(c.rootDirectory, absPath)
	if err != nil {
		return "", err
	}
	pathKey = filepath.ToSlash(filepath.Clean(pathKey))
	if _, ok := c.moduleCache[pathKey]; ok {
		return pathKey, nil
	}
	if c.moduleLoading[absPath] {
		return "", fail("circular module import detected for %q", importPath)
	}
	source, err := os.ReadFile(absPath)
	if err != nil {
		return "", fail("cannot load Tell module %q: %v", importPath, err)
	}
	c.moduleLoading[absPath] = true
	program, compileErr := c.compileProgram(string(source), absPath)
	delete(c.moduleLoading, absPath)
	if compileErr != nil {
		return "", compileErr
	}
	c.moduleCache[pathKey] = program
	return pathKey, nil
}

func parseBytecodeExpression(source string) (expression, error) {
	tokens, err := lex(source)
	if err != nil {
		return nil, err
	}
	parser := &expressionParser{tokens: tokens}
	result, err := parser.parse()
	if err != nil {
		return nil, err
	}
	if parser.peek() != "eof" {
		return nil, fail("unexpected %q in expression", parser.tokens[parser.pos].text)
	}
	return result, nil
}

func (c *bytecodeCompiler) expression(source string) error {
	expr, err := parseBytecodeExpression(source)
	if err != nil {
		return err
	}
	return c.compileExpression(foldBytecodeExpression(expr))
}

type bytecodeConstantExpression struct{ value any }

func (bytecodeConstantExpression) isExpression() {}

func foldBytecodeExpression(expr expression) expression {
	switch value := expr.(type) {
	case unaryExpression:
		operand := foldBytecodeExpression(value.operand)
		if literal, ok := bytecodeLiteralValue(operand); ok {
			var result any
			var err error
			switch value.operator {
			case "not":
				result = !truthy(literal)
			case "+", "-":
				var number float64
				number, err = numberValue(literal)
				if value.operator == "-" {
					number = -number
				}
				result = number
			}
			if err == nil {
				return bytecodeConstantExpression{value: result}
			}
		}
		return unaryExpression{operator: value.operator, operand: operand}
	case binaryExpression:
		left := foldBytecodeExpression(value.left)
		right := foldBytecodeExpression(value.right)
		leftValue, leftConstant := bytecodeLiteralValue(left)
		rightValue, rightConstant := bytecodeLiteralValue(right)
		if value.operator == "and" || value.operator == "or" {
			if leftConstant {
				leftTruth := truthy(leftValue)
				if (value.operator == "and" && !leftTruth) || (value.operator == "or" && leftTruth) {
					return bytecodeConstantExpression{value: leftTruth}
				}
				if rightConstant {
					return bytecodeConstantExpression{value: truthy(rightValue)}
				}
			}
			return binaryExpression{operator: value.operator, left: left, right: right}
		}
		if leftConstant && rightConstant {
			if leftText, ok := leftValue.(string); ok && strings.ContainsAny(leftText, "{}") {
				return binaryExpression{operator: value.operator, left: left, right: right}
			}
			if rightText, ok := rightValue.(string); ok && strings.ContainsAny(rightText, "{}") {
				return binaryExpression{operator: value.operator, left: left, right: right}
			}
			if result, err := applyBinary(value.operator, leftValue, rightValue); err == nil {
				if resultText, ok := result.(string); !ok || !strings.ContainsAny(resultText, "{}") {
					return bytecodeConstantExpression{value: result}
				}
			}
		}
		return binaryExpression{operator: value.operator, left: left, right: right}
	case listExpression:
		items := make([]expression, len(value.items))
		for index, item := range value.items {
			items[index] = foldBytecodeExpression(item)
		}
		return listExpression{items: items}
	case dictionaryExpression:
		keys, values := make([]expression, len(value.keys)), make([]expression, len(value.values))
		for index := range value.keys {
			keys[index] = foldBytecodeExpression(value.keys[index])
			values[index] = foldBytecodeExpression(value.values[index])
		}
		return dictionaryExpression{keys: keys, values: values}
	case memberExpression:
		return memberExpression{value: foldBytecodeExpression(value.value), member: value.member}
	case indexExpression:
		value.value = foldBytecodeExpression(value.value)
		if value.index != nil {
			value.index = foldBytecodeExpression(value.index)
		}
		if value.start != nil {
			value.start = foldBytecodeExpression(value.start)
		}
		if value.end != nil {
			value.end = foldBytecodeExpression(value.end)
		}
		if value.step != nil {
			value.step = foldBytecodeExpression(value.step)
		}
		return value
	case callExpression:
		value.callee = foldBytecodeExpression(value.callee)
		for index := range value.args {
			value.args[index].value = foldBytecodeExpression(value.args[index].value)
		}
		return value
	default:
		return expr
	}
}

func bytecodeLiteralValue(expr expression) (any, bool) {
	if constant, ok := expr.(bytecodeConstantExpression); ok {
		return constant.value, true
	}
	literal, ok := expr.(literalExpression)
	if !ok {
		return nil, false
	}
	switch literal.token.kind {
	case "number":
		value, err := strconv.ParseFloat(literal.token.text, 64)
		return value, err == nil
	case "string":
		return literal.token.text, true
	case "true":
		return true, true
	case "false":
		return false, true
	case "nothing":
		return nil, true
	default:
		return nil, false
	}
}

func (c *bytecodeCompiler) compileExpression(expr expression) error {
	switch value := expr.(type) {
	case literalExpression:
		instruction := c.emit("PUSH")
		instructionValue := value.token
		switch instructionValue.kind {
		case "number":
			parsed, err := strconv.ParseFloat(instructionValue.text, 64)
			if err != nil {
				return err
			}
			c.code[instruction].Value = parsed
		case "string":
			c.code[instruction].Value = instructionValue.text
		case "true":
			c.code[instruction].Value = true
		case "false":
			c.code[instruction].Value = false
		case "nothing":
			c.code[instruction].Value = nil
		}
	case bytecodeConstantExpression:
		instruction := c.emit("PUSH")
		c.code[instruction].Value = value.value
	case nameExpression:
		instruction := c.emit("LOAD")
		c.code[instruction].Name = value.name
	case unaryExpression:
		if err := c.compileExpression(value.operand); err != nil {
			return err
		}
		instruction := c.emit("UNARY")
		c.code[instruction].Name = value.operator
	case binaryExpression:
		if value.operator == "and" || value.operator == "or" {
			if err := c.compileExpression(value.left); err != nil {
				return err
			}
			c.emit("BOOL")
			jump := c.emit("JUMP_IF_FALSE")
			if value.operator == "or" {
				jump = c.emit("JUMP_IF_TRUE")
			}
			c.emit("POP")
			if err := c.compileExpression(value.right); err != nil {
				return err
			}
			c.emit("BOOL")
			c.patch(jump, len(c.code))
			return nil
		}
		if err := c.compileExpression(value.left); err != nil {
			return err
		}
		if err := c.compileExpression(value.right); err != nil {
			return err
		}
		instruction := c.emit("BINARY")
		c.code[instruction].Name = value.operator
	case listExpression:
		for _, item := range value.items {
			if err := c.compileExpression(item); err != nil {
				return err
			}
		}
		instruction := c.emit("LIST")
		c.code[instruction].Count = len(value.items)
	case dictionaryExpression:
		for i, key := range value.keys {
			if err := c.compileExpression(key); err != nil {
				return err
			}
			if err := c.compileExpression(value.values[i]); err != nil {
				return err
			}
		}
		instruction := c.emit("DICT")
		c.code[instruction].Count = len(value.keys)
	case memberExpression:
		if err := c.compileExpression(value.value); err != nil {
			return err
		}
		instruction := c.emit("MEMBER")
		c.code[instruction].Name = value.member
	case indexExpression:
		if err := c.compileExpression(value.value); err != nil {
			return err
		}
		if !value.isSlice {
			if err := c.compileExpression(value.index); err != nil {
				return err
			}
			c.emit("INDEX")
			return nil
		}
		for _, part := range []expression{value.start, value.end, value.step} {
			if part == nil {
				c.emit("MISSING")
			} else if err := c.compileExpression(part); err != nil {
				return err
			}
		}
		c.emit("SLICE")
	case callExpression:
		if err := c.compileExpression(value.callee); err != nil {
			return err
		}
		names := make([]string, 0, len(value.args))
		for _, arg := range value.args {
			if err := c.compileExpression(arg.value); err != nil {
				return err
			}
			names = append(names, arg.name)
		}
		instruction := c.emit("CALL")
		c.code[instruction].Count = len(value.args)
		c.code[instruction].ArgNames = names
	default:
		return fmt.Errorf("unsupported expression node %T", expr)
	}
	return nil
}

type bytecodeMissing struct{}

type bytecodeLoopState struct {
	values []any
	count  int
	index  int
	name   string
}

type bytecodeClassValue struct {
	name        string
	base        *bytecodeClassValue
	constructor *BytecodeFunction
	methods     map[string]BytecodeFunction
	closure     *env
	functions   []BytecodeFunction
	classes     []BytecodeClass
}

type bytecodeInstance struct {
	class  *bytecodeClassValue
	fields map[string]any
}

type bytecodeTryState struct {
	handler     int
	stackHeight int
}

// RunBytecode verifies and executes a compiled Tell artifact using the native VM.
func RunBytecode(data []byte, filename string, input io.Reader, output io.Writer) error {
	return RunBytecodeWithLimits(data, filename, input, output, DefaultExecutionLimits())
}

// RunBytecodeWithLimits executes an artifact under the same resource limits as source execution.
func RunBytecodeWithLimits(data []byte, filename string, input io.Reader, output io.Writer, limits ExecutionLimits) error {
	var program BytecodeProgram
	if err := json.Unmarshal(data, &program); err != nil {
		return fail("invalid bytecode: %v", err)
	}
	if program.Format != bytecodeFormat || len(program.Instructions) == 0 {
		return fail("unsupported or empty Tell bytecode artifact")
	}
	compiledModules := make(map[string]BytecodeProgram, len(program.Modules))
	for _, module := range program.Modules {
		if module.Path == "" || module.Program.Format != bytecodeFormat || len(module.Program.Instructions) == 0 {
			return fail("invalid embedded bytecode module")
		}
		if _, duplicate := compiledModules[module.Path]; duplicate {
			return fail("duplicate embedded bytecode module %q", module.Path)
		}
		if len(module.Program.Modules) != 0 {
			return fail("nested module tables are not valid in embedded bytecode")
		}
		compiledModules[module.Path] = module.Program
	}
	if err := validateModuleReferences(program, compiledModules); err != nil {
		return err
	}
	if err := validateExecutableBytecode(program); err != nil {
		return err
	}
	for path, module := range compiledModules {
		if err := validateExecutableBytecode(module); err != nil {
			return fail("invalid embedded module %q: %v", path, err)
		}
	}
	r := newRuntime(filename, input, output, limits)
	r.bytecodeModules = compiledModules
	_, err := executeBytecodeCode(r, program.Instructions, program.Functions, program.Classes)
	return err
}

func validateExecutableBytecode(program BytecodeProgram) error {
	if program.Format != bytecodeFormat || len(program.Instructions) == 0 {
		return fail("unsupported or empty Tell bytecode artifact")
	}
	if err := validateBytecode(program.Instructions, "HALT", len(program.Functions), len(program.Classes), false); err != nil {
		return err
	}
	for index, function := range program.Functions {
		if function.Name == "" || len(function.Instructions) == 0 {
			return fail("invalid bytecode function at index %d", index)
		}
		if err := validateBytecode(function.Instructions, "RETURN", len(program.Functions), len(program.Classes), true); err != nil {
			return fail("invalid bytecode function %q: %v", function.Name, err)
		}
		if err := verifyBytecodeStack(function.Instructions); err != nil {
			return fail("invalid bytecode function %q: %v", function.Name, err)
		}
	}
	for index, class := range program.Classes {
		if class.Name == "" {
			return fail("invalid bytecode class at index %d", index)
		}
		if class.Constructor != nil && (*class.Constructor < 0 || *class.Constructor >= len(program.Functions) || !program.Functions[*class.Constructor].Receiver) {
			return fail("invalid constructor reference for bytecode class %q", class.Name)
		}
		for method, functionIndex := range class.Methods {
			if method == "" || functionIndex < 0 || functionIndex >= len(program.Functions) || !program.Functions[functionIndex].Receiver {
				return fail("invalid method reference for bytecode class %q", class.Name)
			}
		}
	}
	return verifyBytecodeStack(program.Instructions)
}

func validateModuleReferences(program BytecodeProgram, modules map[string]BytecodeProgram) error {
	check := func(instructions []BytecodeInstruction, where string) error {
		for index, instruction := range instructions {
			if instruction.Op != "IMPORT_MODULE" {
				continue
			}
			path, ok := instruction.Value.(string)
			if !ok || instruction.Name == "" {
				return fail("invalid module import at %s instruction %d", where, index)
			}
			if _, exists := modules[path]; !exists {
				return fail("missing embedded bytecode module %q", path)
			}
		}
		return nil
	}
	checkProgram := func(program BytecodeProgram, name string) error {
		if err := check(program.Instructions, name); err != nil {
			return err
		}
		for index, function := range program.Functions {
			if err := check(function.Instructions, fmt.Sprintf("%s function %d", name, index)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkProgram(program, "main"); err != nil {
		return err
	}
	for path, module := range modules {
		if err := checkProgram(module, "module "+path); err != nil {
			return err
		}
	}
	return nil
}

func validateBytecode(instructions []BytecodeInstruction, terminal string, functionCount, classCount int, inFunction bool) error {
	if instructions[len(instructions)-1].Op != terminal {
		return fail("invalid bytecode: missing terminal %s instruction", terminal)
	}
	for pc, instruction := range instructions {
		if instruction.Count < 0 {
			return fail("invalid bytecode count at instruction %d", pc)
		}
		if (instruction.Op == "LIST" || instruction.Op == "DICT" || instruction.Op == "CALL") && instruction.Count > len(instructions) {
			return fail("invalid bytecode count at instruction %d", pc)
		}
		if instruction.Op == "DICT" && instruction.Count > len(instructions)/2 {
			return fail("invalid bytecode dictionary count at instruction %d", pc)
		}
		if (instruction.Op == "JUMP" || instruction.Op == "JUMP_IF_FALSE" || instruction.Op == "JUMP_IF_TRUE" || instruction.Op == "REPEAT_INIT" || instruction.Op == "ITER_INIT" || instruction.Op == "REPEAT_NEXT" || instruction.Op == "ITER_NEXT" || instruction.Op == "TRY_PUSH") && (instruction.Jump < 0 || instruction.Jump >= len(instructions)) {
			return fail("invalid bytecode jump at instruction %d", pc)
		}
		if (instruction.Op == "REPEAT_NEXT" || instruction.Op == "ITER_NEXT") && instruction.Count >= len(instructions) {
			return fail("invalid bytecode loop reference at instruction %d", pc)
		}
		if instruction.Op == "DEFINE_FUNC" && instruction.Count >= functionCount {
			return fail("invalid bytecode function reference at instruction %d", pc)
		}
		if instruction.Op == "DEFINE_CLASS" && instruction.Count >= classCount {
			return fail("invalid bytecode class reference at instruction %d", pc)
		}
		if !inFunction && instruction.Op == "RETURN" {
			return fail("invalid bytecode return at instruction %d", pc)
		}
	}
	return nil
}

func verifyBytecodeStack(instructions []BytecodeInstruction) error {
	heights := map[int]int{0: 0}
	work := []int{0}
	for len(work) > 0 {
		pc := work[0]
		work = work[1:]
		height := heights[pc]
		instruction := instructions[pc]
		need, delta := 0, 0
		switch instruction.Op {
		case "PUSH", "MISSING", "LOAD":
			delta = 1
		case "DECLARE", "ASSIGN", "PRINT", "POP", "ASSERT":
			need, delta = 1, -1
		case "SET_FIELD", "SET_MEMBER":
			need, delta = 2, -2
		case "BOOL", "UNARY", "MEMBER":
			need = 1
		case "DUP":
			need, delta = 1, 1
		case "BINARY":
			need, delta = 2, -1
		case "LIST":
			need, delta = instruction.Count, 1-instruction.Count
		case "DICT":
			need, delta = 2*instruction.Count, 1-2*instruction.Count
		case "INDEX":
			need, delta = 2, -1
		case "SLICE":
			need, delta = 4, -3
		case "CALL":
			need, delta = instruction.Count+1, -instruction.Count
		case "JUMP", "JUMP_IF_FALSE", "JUMP_IF_TRUE", "REPEAT_NEXT", "ITER_NEXT", "DEFINE_FUNC", "DEFINE_CLASS", "IMPORT_MODULE", "USE_LIBRARY", "TRY_PUSH", "TRY_POP":
			if instruction.Op == "JUMP_IF_FALSE" || instruction.Op == "JUMP_IF_TRUE" {
				need = 1
			}
		case "REPEAT_INIT", "ITER_INIT":
			need, delta = 1, -1
		case "HALT":
			if height != 0 {
				return fail("invalid bytecode stack at instruction %d (line %d): HALT has %d value(s)", pc, instruction.Line, height)
			}
			continue
		case "RETURN":
			if height != 1 {
				return fail("invalid bytecode stack at instruction %d (line %d): RETURN needs exactly one value, has %d", pc, instruction.Line, height)
			}
			continue
		case "RAISE":
			if height < 1 {
				return fail("invalid bytecode stack at instruction %d (line %d): RAISE needs a message", pc, instruction.Line)
			}
			continue
		default:
			return fail("unknown bytecode operation %q at instruction %d", instruction.Op, pc)
		}
		if height < need {
			return fail("invalid bytecode stack at instruction %d (line %d): %s needs %d value(s), has %d", pc, instruction.Line, instruction.Op, need, height)
		}
		nextHeight := height + delta
		successors := []int{pc + 1}
		switch instruction.Op {
		case "JUMP":
			successors = []int{instruction.Jump}
		case "JUMP_IF_FALSE", "JUMP_IF_TRUE", "REPEAT_INIT", "ITER_INIT", "REPEAT_NEXT", "ITER_NEXT", "TRY_PUSH":
			successors = append(successors, instruction.Jump)
		}
		for _, successor := range successors {
			if successor < 0 || successor >= len(instructions) {
				return fail("invalid bytecode control flow from instruction %d", pc)
			}
			if old, exists := heights[successor]; exists {
				if old != nextHeight {
					return fail("invalid bytecode stack join at instruction %d: heights %d and %d", successor, old, nextHeight)
				}
				continue
			}
			heights[successor] = nextHeight
			work = append(work, successor)
		}
	}
	return nil
}

func executeBytecodeCode(r *runtime, instructions []BytecodeInstruction, functions []BytecodeFunction, classes []BytecodeClass) (any, error) {
	stack := make([]any, 0, 32)
	pc := 0
	backEdges := map[int]int{}
	loopStates := map[int]*bytecodeLoopState{}
	tryStates := []bytecodeTryState{}
	pop := func() (any, error) {
		if len(stack) == 0 {
			return nil, fail("bytecode stack underflow")
		}
		value := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return value, nil
	}
	for pc >= 0 && pc < len(instructions) {
		instruction := instructions[pc]
		next := pc + 1
		var err error
		switch instruction.Op {
		case "PUSH":
			value := instruction.Value
			if text, ok := value.(string); ok {
				value = r.interpolate(text, r.global)
			}
			stack = append(stack, value)
		case "MISSING":
			stack = append(stack, bytecodeMissing{})
		case "LOAD":
			var value any
			value, err = r.global.get(instruction.Name)
			if err == nil {
				stack = append(stack, value)
			}
		case "DECLARE", "ASSIGN", "PRINT", "POP", "ASSERT", "BOOL", "UNARY":
			var value any
			value, err = pop()
			if err != nil {
				break
			}
			switch instruction.Op {
			case "DECLARE":
				err = r.declare(r.global, instruction.Name, value, instruction.Type, instruction.Constant)
			case "ASSIGN":
				err = r.assign(r.global, instruction.Name, value)
			case "PRINT":
				_, err = fmt.Fprintln(r.output, display(value))
			case "ASSERT":
				if !truthy(value) {
					if instruction.Name == "" {
						err = fail("check failed")
					} else {
						err = fail("check failed: %s", instruction.Name)
					}
				}
			case "POP":
			case "BOOL":
				stack = append(stack, truthy(value))
			case "UNARY":
				if instruction.Name == "not" {
					stack = append(stack, !truthy(value))
				} else {
					var number float64
					number, err = numberValue(value)
					if err == nil && instruction.Name == "-" {
						number = -number
					}
					if err == nil {
						stack = append(stack, number)
					}
				}
			}
		case "DUP":
			if len(stack) == 0 {
				err = fail("bytecode stack underflow")
			} else {
				stack = append(stack, stack[len(stack)-1])
			}
		case "BINARY":
			var right, left any
			right, err = pop()
			if err == nil {
				left, err = pop()
			}
			if err == nil {
				var value any
				value, err = applyBinary(instruction.Name, left, right)
				if err == nil {
					stack = append(stack, value)
				}
			}
		case "LIST":
			items := make([]any, instruction.Count)
			for i := instruction.Count - 1; i >= 0 && err == nil; i-- {
				items[i], err = pop()
			}
			if err == nil {
				stack = append(stack, items)
			}
		case "DICT":
			items := make(map[string]any, instruction.Count)
			for i := instruction.Count - 1; i >= 0 && err == nil; i-- {
				var value, key any
				value, err = pop()
				if err == nil {
					key, err = pop()
				}
				if err == nil {
					items[fmt.Sprint(key)] = value
				}
			}
			if err == nil {
				stack = append(stack, items)
			}
		case "INDEX":
			var index, value any
			index, err = pop()
			if err == nil {
				value, err = pop()
			}
			if err == nil {
				var indexed any
				indexed, err = indexValue(value, index)
				if err == nil {
					stack = append(stack, indexed)
				}
			}
		case "SLICE":
			var stepValue, endValue, startValue, value any
			stepValue, err = pop()
			if err == nil {
				endValue, err = pop()
			}
			if err == nil {
				startValue, err = pop()
			}
			if err == nil {
				value, err = pop()
			}
			if err == nil {
				start, end := missingBound(startValue), missingBound(endValue)
				stride := 1
				if _, missing := stepValue.(bytecodeMissing); !missing {
					var number float64
					number, err = numberValue(stepValue)
					stride = int(number)
				}
				if err == nil && stride == 0 {
					err = fail("slice step cannot be zero")
				}
				if err == nil {
					var sliced any
					sliced, err = sliceValue(value, start, end, stride)
					if err == nil {
						stack = append(stack, sliced)
					}
				}
			}
		case "MEMBER":
			var target any
			target, err = pop()
			if err == nil {
				var member any
				member, err = bytecodeMember(r, target, instruction.Name, functions)
				if err == nil {
					stack = append(stack, member)
				}
			}
		case "SET_FIELD":
			var target, value any
			target, err = pop()
			if err == nil {
				value, err = pop()
			}
			if err == nil {
				instance, ok := target.(*bytecodeInstance)
				if !ok {
					err = fail("this is not a Tell instance")
				} else {
					instance.fields[instruction.Name] = value
				}
			}
		case "SET_MEMBER":
			var value, target any
			value, err = pop()
			if err == nil {
				target, err = pop()
			}
			if err == nil {
				instance, ok := target.(*bytecodeInstance)
				if !ok {
					err = fail("property assignment requires a Tell instance")
				} else {
					instance.fields[instruction.Name] = value
				}
			}
		case "TRY_PUSH":
			tryStates = append(tryStates, bytecodeTryState{handler: instruction.Jump, stackHeight: len(stack)})
		case "TRY_POP":
			if len(tryStates) == 0 {
				err = fail("invalid try-handler state")
			} else {
				tryStates = tryStates[:len(tryStates)-1]
			}
		case "RAISE":
			var value any
			value, err = pop()
			if err == nil {
				err = fail("%s", display(value))
			}
		case "CALL":
			args := make([]any, instruction.Count)
			for i := instruction.Count - 1; i >= 0 && err == nil; i-- {
				args[i], err = pop()
			}
			var callee any
			if err == nil {
				callee, err = pop()
			}
			if err == nil {
				positional := []any{}
				named := map[string]any{}
				for i, value := range args {
					if i < len(instruction.ArgNames) && instruction.ArgNames[i] != "" {
						named[instruction.ArgNames[i]] = value
					} else {
						positional = append(positional, value)
					}
				}
				var result any
				if class, ok := callee.(*bytecodeClassValue); ok {
					result, err = constructBytecodeInstance(r, class, positional, named)
				} else {
					result, err = invokeNamed(callee, positional, named)
				}
				if err == nil {
					stack = append(stack, result)
				}
			}
		case "IMPORT_MODULE":
			modulePath, ok := instruction.Value.(string)
			if !ok {
				err = fail("invalid embedded module path")
				break
			}
			if cached, exists := r.modules[modulePath]; exists {
				r.global.set(instruction.Name, cached)
				break
			}
			moduleProgram, exists := r.bytecodeModules[modulePath]
			if !exists {
				err = fail("missing embedded bytecode module %q", modulePath)
				break
			}
			previousGlobal, previousFilename := r.global, r.filename
			moduleEnvironment := newEnv(previousGlobal)
			r.global = moduleEnvironment
			r.filename = modulePath
			_, err = executeBytecodeCode(r, moduleProgram.Instructions, moduleProgram.Functions, moduleProgram.Classes)
			r.global, r.filename = previousGlobal, previousFilename
			if err == nil {
				exports := make(map[string]any)
				for name, value := range moduleEnvironment.values {
					if !strings.HasPrefix(name, "_") {
						exports[name] = value
					}
				}
				r.modules[modulePath] = exports
				r.global.set(instruction.Name, exports)
			}
		case "USE_LIBRARY":
			library, ok := instruction.Value.(string)
			if !ok {
				err = fail("invalid standard-library reference")
				break
			}
			var module any
			module, err = moduleValue(library)
			if err == nil {
				r.global.set(instruction.Name, module)
			}
		case "DEFINE_CLASS":
			spec := classes[instruction.Count]
			class := &bytecodeClassValue{name: spec.Name, methods: map[string]BytecodeFunction{}, closure: r.global, functions: functions, classes: classes}
			if spec.Base != "" {
				var base any
				base, err = r.global.get(spec.Base)
				if err == nil {
					var ok bool
					class.base, ok = base.(*bytecodeClassValue)
					if !ok {
						err = fail("base type %q is not a compiled Tell class", spec.Base)
					}
				}
			}
			if err == nil {
				if spec.Constructor != nil {
					constructor := functions[*spec.Constructor]
					class.constructor = &constructor
				}
				for name, functionIndex := range spec.Methods {
					class.methods[name] = functions[functionIndex]
				}
				r.global.set(spec.Name, class)
			}
		case "DEFINE_FUNC":
			function := functions[instruction.Count]
			closure := r.global
			compiled := namedCallable(func(args []any, named map[string]any) (any, error) {
				return callBytecodeFunction(r, function, closure, functions, classes, args, named, nil)
			})
			r.global.set(instruction.Name, compiled)
		case "JUMP":
			if instruction.Jump <= pc {
				backEdges[pc]++
				if backEdges[pc] > r.limits.MaxLoopIterations {
					err = fail("bytecode loop exceeded the configured limit of %d iterations", r.limits.MaxLoopIterations)
				} else {
					next = instruction.Jump
				}
			} else {
				next = instruction.Jump
			}
		case "JUMP_IF_FALSE", "JUMP_IF_TRUE":
			if len(stack) == 0 {
				err = fail("bytecode stack underflow")
				break
			}
			condition := truthy(stack[len(stack)-1])
			if (instruction.Op == "JUMP_IF_FALSE" && !condition) || (instruction.Op == "JUMP_IF_TRUE" && condition) {
				next = instruction.Jump
			}
		case "REPEAT_INIT":
			var value any
			value, err = pop()
			if err == nil {
				var number float64
				number, err = numberValue(value)
				if err == nil {
					count := int(number)
					if count < 0 || count > r.limits.MaxLoopIterations {
						err = fail("repeat count must be between 0 and %d", r.limits.MaxLoopIterations)
					} else if count == 0 {
						next = instruction.Jump
					} else {
						loopStates[pc] = &bytecodeLoopState{count: count, index: 1, name: "_repeat_index"}
						r.global.set("_repeat_index", float64(1))
					}
				}
			}
		case "REPEAT_NEXT":
			state, ok := loopStates[instruction.Count]
			if !ok {
				err = fail("invalid repeat-loop state")
				break
			}
			state.index++
			if state.index <= state.count {
				r.global.set(state.name, float64(state.index))
				next = instruction.Jump
			} else {
				delete(loopStates, instruction.Count)
			}
		case "ITER_INIT":
			var value any
			value, err = pop()
			if err != nil {
				break
			}
			var values []any
			switch iterable := value.(type) {
			case []any:
				values = iterable
			case string:
				for _, character := range iterable {
					values = append(values, string(character))
					if len(values) > r.limits.MaxLoopIterations {
						break
					}
				}
			default:
				err = fail("for each needs a list or text value")
			}
			if err == nil {
				if len(values) > r.limits.MaxLoopIterations {
					err = fail("for-each loop exceeded the configured limit of %d iterations", r.limits.MaxLoopIterations)
				} else if len(values) == 0 {
					next = instruction.Jump
				} else {
					loopStates[pc] = &bytecodeLoopState{values: values, index: 0, name: instruction.Name}
					r.global.set(instruction.Name, values[0])
					r.global.set("_current_item", values[0])
				}
			}
		case "ITER_NEXT":
			state, ok := loopStates[instruction.Count]
			if !ok {
				err = fail("invalid for-each loop state")
				break
			}
			state.index++
			if state.index < len(state.values) {
				value := state.values[state.index]
				r.global.set(state.name, value)
				r.global.set("_current_item", value)
				next = instruction.Jump
			} else {
				delete(loopStates, instruction.Count)
			}
		case "HALT":
			return nil, nil
		case "RETURN":
			value, returnErr := pop()
			if returnErr != nil {
				return nil, fail("%s:%d: %v", r.filename, instruction.Line, returnErr)
			}
			return value, nil
		default:
			err = fail("unknown bytecode operation %q", instruction.Op)
		}
		if err != nil {
			if len(tryStates) > 0 {
				handler := tryStates[len(tryStates)-1]
				tryStates = tryStates[:len(tryStates)-1]
				if handler.stackHeight < 0 || handler.stackHeight > len(stack) {
					return nil, fail("invalid stack state while handling bytecode error")
				}
				stack = stack[:handler.stackHeight]
				r.global.set("error_message", fail("%s:%d: %v", r.filename, instruction.Line, err).Error())
				next = handler.handler
			} else {
				return nil, fail("%s:%d: %v", r.filename, instruction.Line, err)
			}
		}
		pc = next
	}
	return nil, nil
}

func bindBytecodeArguments(function BytecodeFunction, positional []any, named map[string]any) (map[string]any, error) {
	if len(positional) > len(function.Parameters) {
		return nil, fail("%q accepts %d argument(s), but got %d positional arguments", function.Name, len(function.Parameters), len(positional))
	}
	indices := make(map[string]int, len(function.Parameters))
	bindings := make(map[string]any, len(function.Parameters))
	for index, parameter := range function.Parameters {
		indices[parameter] = index
		if index < len(positional) {
			bindings[parameter] = positional[index]
		}
	}
	for name, value := range named {
		index, exists := indices[name]
		if !exists {
			return nil, fail("%q has no parameter named %q", function.Name, name)
		}
		if index < len(positional) {
			return nil, fail("%q got multiple values for %q", function.Name, name)
		}
		bindings[name] = value
	}
	for _, parameter := range function.Parameters {
		if _, exists := bindings[parameter]; !exists {
			return nil, fail("%q is missing argument %q", function.Name, parameter)
		}
	}
	return bindings, nil
}

func callBytecodeFunction(r *runtime, function BytecodeFunction, closure *env, functions []BytecodeFunction, classes []BytecodeClass, args []any, named map[string]any, receiver *bytecodeInstance) (any, error) {
	bindings, err := bindBytecodeArguments(function, args, named)
	if err != nil {
		return nil, err
	}
	if function.Receiver && receiver == nil {
		return nil, fail("method %q needs an instance receiver", function.Name)
	}
	if r.callDepth >= r.limits.MaxFunctionCallDepth {
		return nil, fail("maximum function call depth (%d) exceeded", r.limits.MaxFunctionCallDepth)
	}
	r.callDepth++
	defer func() { r.callDepth-- }()
	local := newEnv(closure)
	for name, value := range bindings {
		local.set(name, value)
	}
	if receiver != nil {
		local.set("this", receiver)
	}
	previous := r.global
	r.global = local
	defer func() { r.global = previous }()
	return executeBytecodeCode(r, function.Instructions, functions, classes)
}

func constructBytecodeInstance(r *runtime, class *bytecodeClassValue, args []any, named map[string]any) (any, error) {
	instance := &bytecodeInstance{class: class, fields: map[string]any{}}
	var constructor *BytecodeFunction
	var constructorClosure *env
	for current := class; current != nil; current = current.base {
		if current.constructor != nil {
			constructor = current.constructor
			constructorClosure = current.closure
			break
		}
	}
	if constructor == nil {
		if len(args) != 0 || len(named) != 0 {
			return nil, fail("class %q has no constructor and accepts no arguments", class.name)
		}
		return instance, nil
	}
	if _, err := callBytecodeFunction(r, *constructor, constructorClosure, class.functions, class.classes, args, named, instance); err != nil {
		return nil, err
	}
	return instance, nil
}

func missingBound(value any) *any {
	if _, missing := value.(bytecodeMissing); missing {
		return nil
	}
	return &value
}

func sliceValue(value any, startValue, endValue *any, stride int) (any, error) {
	switch item := value.(type) {
	case []any:
		indices, err := sliceIndices(len(item), startValue, endValue, stride)
		if err != nil {
			return nil, err
		}
		result := make([]any, 0, len(indices))
		for _, index := range indices {
			result = append(result, item[index])
		}
		return result, nil
	case string:
		runes := []rune(item)
		indices, err := sliceIndices(len(runes), startValue, endValue, stride)
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

func bytecodeMember(r *runtime, value any, name string, functions []BytecodeFunction) (any, error) {
	switch object := value.(type) {
	case map[string]any:
		member, ok := object[name]
		if !ok {
			return nil, fail("unknown member %q", name)
		}
		return member, nil
	case *userInstance:
		if member, ok := object.fields[name]; ok {
			return member, nil
		}
		method, ok := object.class.method(name)
		if !ok {
			return nil, fail("%s has no member %q", object.class.name, name)
		}
		receiver := object
		return namedCallable(func(args []any, named map[string]any) (any, error) { return method.callNamed(args, named, receiver) }), nil
	case *bytecodeInstance:
		if member, ok := object.fields[name]; ok {
			return member, nil
		}
		for class := object.class; class != nil; class = class.base {
			method, ok := class.methods[name]
			if !ok {
				continue
			}
			receiver := object
			closure := class.closure
			classFunctions, classTable := class.functions, class.classes
			return namedCallable(func(args []any, named map[string]any) (any, error) {
				return callBytecodeFunction(r, method, closure, classFunctions, classTable, args, named, receiver)
			}), nil
		}
		return nil, fail("%s has no member %q", object.class.name, name)
	default:
		return nil, fail("this value has no member %q", name)
	}
}

// WriteBytecode writes a previously compiled artifact to disk.
func WriteBytecode(path string, data []byte) error {
	if len(data) == 0 {
		return fail("refusing to write empty bytecode")
	}
	return os.WriteFile(path, data, 0644)
}
