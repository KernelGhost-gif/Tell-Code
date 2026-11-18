package tell

import "strings"

// ValidateTypedProgram enforces the opt-in statically declared dialect used by
// the native Go runtime. It preserves the dynamic language as the default:
// callers must explicitly select typed mode.
func ValidateTypedProgram(source, filename string) error {
	nodes, err := parse(source, filename)
	if err != nil {
		return err
	}
	analyzer := semanticAnalyzer{
		filename: filename,
		classes: make(map[string]string),
		functions: make(map[string]semanticSignature),
	}
	analyzer.collectClasses(nodes)
	analyzer.collectFunctions(nodes)

	var validate func([]node) error
	validate = func(statements []node) error {
		for _, statement := range statements {
			if statement.kind == "function" || statement.kind == "constructor" {
				for index, parameter := range statement.params {
					if index >= len(statement.paramTypes) || strings.TrimSpace(statement.paramTypes[index]) == "" {
						return fail("%s:%d:%d: parameter %q in %q needs an explicit type (use type: name)", filename, statement.line, statement.column, parameter, statement.name)
					}
					if err := analyzer.checkAnnotation(parameter, statement.paramTypes[index], "any"); err != nil {
						return fail("%s:%d:%d: %v", filename, statement.line, statement.column, err)
					}
				}
				if statement.kind == "function" {
					if strings.TrimSpace(statement.returnType) == "" {
						return fail("%s:%d:%d: function %q needs an explicit return type (use out type or out void)", filename, statement.line, statement.column, statement.name)
					}
					if err := analyzer.checkAnnotation("return type", statement.returnType, "any"); err != nil {
						return fail("%s:%d:%d: %v", filename, statement.line, statement.column, err)
					}
				}
			}
			if statement.kind == "statement" && statement.simple != nil && statement.simple.kind == "let" {
				if strings.TrimSpace(statement.simple.annotation) == "" {
					return fail("%s:%d:%d: variable %q needs an explicit type (use 'let type name be value')", filename, statement.line, statement.column, strings.Join(statement.simple.names, ", "))
				}
				if err := analyzer.checkAnnotation(strings.Join(statement.simple.names, ", "), statement.simple.annotation, "any"); err != nil {
					return fail("%s:%d:%d: %v", filename, statement.line, statement.column, err)
				}
			}
			if err := validate(statement.body); err != nil {
				return err
			}
			if err := validate(statement.otherwise); err != nil {
				return err
			}
			if err := validate(statement.handler); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(nodes); err != nil {
		return err
	}
	return analyzer.statements(nodes, map[string]semanticBinding{}, false)
}
