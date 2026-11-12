package tell

import (
	"testing"
)

func TestUserFunctionCallFramesTrackAndUnwindLexicalActivations(t *testing.T) {
	r := &runtime{
		filename: "frames.tell",
		global: newEnv(nil),
		limits: ExecutionLimits{MaxFunctionCallDepth: 4, MaxLoopIterations: 100},
		callSiteLine: 7,
		callSiteColumn: 12,
	}
	observed := false
	r.global.set("inspect_frame", namedCallable(func(_ []any, _ map[string]any) (any, error) {
		observed = true
		if len(r.callFrames) != 1 {
			t.Fatalf("active call frames = %d, want 1", len(r.callFrames))
		}
		frame := r.callFrames[0]
		if frame.Function != "outer" {
			t.Fatalf("frame function = %q, want outer", frame.Function)
		}
		if frame.Caller != r.global || frame.Locals == nil {
			t.Fatal("frame must retain caller and local lexical environments")
		}
		if frame.CallSiteLine != 7 || frame.CallSiteColumn != 12 {
			t.Fatalf("call-site = %d:%d, want 7:12", frame.CallSiteLine, frame.CallSiteColumn)
		}
		return nil, nil
	}))
	body := []node{{
		kind: "statement",
		line: 1,
		column: 1,
		simple: &simpleStatement{
			kind: "expression",
			expression: callExpression{
				callee: nameExpression{name: "inspect_frame", line: 1, column: 1},
				line: 1,
				column: 1,
			},
			hasExpression: true,
		},
	}}
	function := &userFunction{
		name: "outer",
		body: body,
		closure: r.global,
		runtime: r,
	}
	if _, err := function.callNamed(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("function body did not execute")
	}
	if len(r.callFrames) != 0 || r.callDepth != 0 {
		t.Fatalf("call-frame stack leaked after return: frames=%d depth=%d", len(r.callFrames), r.callDepth)
	}
}
