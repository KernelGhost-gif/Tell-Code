package tell

import (
	"strings"
	"testing"
)

func TestIfBlocksAcceptOptionalThenInBothBranches(t *testing.T) {
	source := "If true, then:\n    Say \"primary\".\nOtherwise if false, then:\n    Say \"secondary\".\nOtherwise:\n    Say \"fallback\".\nEnd if.\n"
	nodes, err := parse(source, "optional-then.tell")
	if err != nil {
		t.Fatalf("optional then suffix should parse: %v", err)
	}
	if len(nodes) != 1 || nodes[0].condition != "true" {
		t.Fatalf("unexpected top-level if node: %#v", nodes)
	}
	if len(nodes[0].otherwise) != 1 || nodes[0].otherwise[0].condition != "false" {
		t.Fatalf("otherwise-if condition was not normalized: %#v", nodes[0].otherwise)
	}
}

func TestParserRejectsMalformedDeclarationAndAssignmentExpressions(t *testing.T) {
	cases := []string{
		"Let value be 1 plus ).\n",
		"Set value to (2 plus ).\n",
	}
	for _, source := range cases {
		if _, err := parse(source, "malformed.tell"); err == nil {
			t.Errorf("expected parse-time syntax error for %q", source)
		} else if !strings.Contains(err.Error(), "expected a value") {
			t.Errorf("expected a value diagnostic for %q, got %v", source, err)
		}
	}
}
