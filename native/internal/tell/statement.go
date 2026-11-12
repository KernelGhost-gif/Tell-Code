package tell

import (
	"fmt"
	"os"
	"strings"
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
			switch items := collection.(type) {
			case []any:
				e.set(name, append(items, value))
			case setValue:
				if !setContains(items, value) {
					e.set(name, append(items, value))
				}
			default:
				return fail("%q does not support adding items", name)
			}
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
			switch items := collection.(type) {
			case []any:
				for i, item := range items {
					if valuesEqual(item, value) {
						e.set(name, append(items[:i], items[i+1:]...))
						return nil
					}
				}
			case setValue:
				for i, item := range items {
					if valuesEqual(item, value) {
						e.set(name, append(items[:i], items[i+1:]...))
						return nil
					}
				}
			default:
				return fail("%q does not support removing items", name)
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
	if rest, ok := prefixFold(s, "use the javascript library called "); ok {
		name, alias, hasAlias := splitFoldOnce(rest, " as ")
		name = strings.TrimSpace(name)
		module, err := newJavaScriptModule(name)
		if err != nil {
			return fail("%v", err)
		}
		if !hasAlias {
			alias = defaultJavaScriptAlias(name)
		}
		if !identifier(alias) {
			return fail("expected a valid alias for JavaScript module %q", name)
		}
		e.set(alias, module)
		return nil
	}
	if rest, ok := prefixFold(s, "use the python library called "); ok {
		name, alias, hasAlias := splitFoldOnce(rest, " as ")
		name = strings.TrimSpace(name)
		if !hasAlias {
			alias = name[strings.LastIndex(name, ".")+1:]
		}
		if !identifier(alias) {
			return fail("expected a valid alias for Python module %q", name)
		}
		module, err := newPythonModule(name)
		if err != nil {
			return fail("%v", err)
		}
		e.set(alias, module)
		return nil
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
		switch xs := v.(type) {
		case []any:
			e.set(m[2], append(xs, val))
		case setValue:
			if !setContains(xs, val) {
				e.set(m[2], append(xs, val))
			}
		default:
			return fail("%q does not support adding items", m[2])
		}
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
		switch xs := v.(type) {
		case []any:
			for i, x := range xs {
				if valuesEqual(x, val) {
					e.set(m[2], append(xs[:i], xs[i+1:]...))
					return nil
				}
			}
		case setValue:
			for i, x := range xs {
				if valuesEqual(x, val) {
					e.set(m[2], append(xs[:i], xs[i+1:]...))
					return nil
				}
			}
		default:
			return fail("%q does not support removing items", m[2])
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

