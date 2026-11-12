package tell

import "testing"

// FuzzTellSourceParserNoPanic checks that arbitrary UTF-8 or arbitrary-byte
// source is rejected with an error rather than crashing the production parser.
func FuzzTellSourceParserNoPanic(f *testing.F) {
	seeds := []string{
		"",
		"Say 1.",
		"Let answer be 2 plus 3.\nSay answer.",
		"If true:\n    Say \"ok\".\nEnd if.",
		"To f with x:\n    Give back f(x).\nEnd function.",
		"Create a class called Box:\n    When creating a Box:\n        Remember 1 as this.value.\n    End constructor.\nEnd class.",
		"Tell object to open with \"wide\".",
		"Call add with 1 and 2.",
		"Use the module \"./helper.tell\" as helper.",
		"Set items[-1] to 9.",
		"Set the key \"name\" in data to \"Ada\".",
		"Say \"unterminated",
		"Say \xff\xfe\x00.",
		"\r\n\t# comments\r\n",
		"Otherwise if (",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = parse(source, "fuzz.tell")
	})
}

// FuzzTellExpressionParserNoPanic exercises the production expression lexer,
// Pratt parser, and AST construction independently of statement parsing.
func FuzzTellExpressionParserNoPanic(f *testing.F) {
	seeds := []string{
		"",
		"1 + 2 * 3",
		"not true or false",
		"2 ** 3 ** 2",
		"values[1:3].length",
		"mapping[\"name\"]",
		"f(1, right=2)",
		"1 if condition else 0",
		"\"escaped\\ntext\"",
		"{1, 2, 2}",
		"{\"name\": \"Ada\"}",
		"1 / 0",
		"(((((((((",
		"\"unterminated",
		"\xff\xfe\x00",
		"λ + 1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, expression string) {
		_, _ = parseTellExpressionAt(expression, 1, 1)
	})
}
