package tell

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

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
			if _, err := parseFloatPortable(s[start:i]); err != nil {
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
			case "true", "false", "nothing", "and", "or", "not", "in", "notin", "if", "else":
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
	return r.evalAt(s, e, 1, 1)
}

func (r *runtime) evalAt(s string, e *env, line, column int) (any, error) {
	expr, err := parseTellExpressionAt(s, line, column)
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

