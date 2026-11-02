package tell

import (
	"strings"
)

// Format validates source and normalizes indentation and trailing whitespace.
// Statement contents, comments, and sentence punctuation are preserved.
func Format(source, filename string) (string, error) {
	nodes, err := parse(source, filename)
	if err != nil {
		return "", err
	}
	openers := make(map[int]bool)
	var collect func([]node)
	collect = func(items []node) {
		for _, item := range items {
			switch item.kind {
			case "if", "while", "repeat", "for", "function", "class", "constructor", "try":
				openers[item.line] = true
			}
			collect(item.body)
			collect(item.otherwise)
			collect(item.handler)
		}
	}
	collect(nodes)

	lines := strings.Split(source, "\n")
	formatted := make([]string, 0, len(lines))
	depth := 0
	for index, raw := range lines {
		cleaned := cleanLine(raw)
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			formatted = append(formatted, "")
			continue
		}
		low := strings.ToLower(cleaned)
		if isBlockEnd(low) || isBlockBranch(low) {
			if depth > 0 {
				depth--
			}
		}
		formatted = append(formatted, strings.Repeat("    ", depth)+trimmed)
		if openers[index+1] || isBlockBranch(low) {
			depth++
		}
	}
	return strings.Join(formatted, "\n"), nil
}

func isBlockEnd(line string) bool {
	switch line {
	case "end if.", "end while.", "end repeat.", "end loop.", "end function.", "end try.", "end constructor.", "end class.":
		return true
	default:
		return false
	}
}

func isBlockBranch(line string) bool {
	return line == "otherwise:" || line == "if something goes wrong:" ||
		(strings.HasPrefix(line, "otherwise if ") && strings.HasSuffix(line, ":"))
}
