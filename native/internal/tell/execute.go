package tell

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

func (r *runtime) execute(nodes []node, e *env) error {
	for _, n := range nodes {
		if err := r.execNode(n, e); err != nil {
			var s signal
			if errors.As(err, &s) {
				return err
			}
			return withSourceLocation(err, r.filename, n.line, n.column)
		}
	}
	return nil
}
func (r *runtime) execNode(n node, e *env) error {
	switch n.kind {
	case "if":
		v, err := r.evalAt(n.condition, e, n.line, n.column+len("If "))
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
			v, err := r.evalAt(n.condition, e, n.line, n.column+len("While "))
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
		v, err := r.evalAt(n.condition, e, n.line, n.column+len("Repeat "))
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
		v, err := r.evalAt(n.condition, e, n.line, n.column+len("For each ")+len(n.name)+len(" in "))
		if err != nil {
			return err
		}
		items, ok := v.([]any)
		if tuple, isTuple := v.(tupleValue); isTuple {
			items, ok = []any(tuple), true
		}
		if set, isSet := v.(setValue); isSet {
			items, ok = []any(set), true
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
				return fail("for each needs a list, tuple, set, or text value")
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

