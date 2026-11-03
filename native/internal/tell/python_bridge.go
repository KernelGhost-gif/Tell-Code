package tell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var pythonIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var pythonModuleName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

const pythonBridge = `import contextlib, importlib, json, sys
try:
    request = json.load(sys.stdin)
    module = importlib.import_module(request["module"])
    function = getattr(module, request["function"])
    if not callable(function):
        raise TypeError(request["function"] + " is not callable")
    with contextlib.redirect_stdout(sys.stderr):
        result = function(*request.get("args", []), **request.get("kwargs", {}))
    response = json.dumps({"result": result}, ensure_ascii=False, allow_nan=False)
except Exception as error:
    response = json.dumps({"error": type(error).__name__ + ": " + str(error)}, ensure_ascii=False)
sys.stdout.write(response + "\n")
`

type pythonCallRequest struct {
	Module   string         `json:"module"`
	Function string         `json:"function"`
	Args     []any          `json:"args"`
	Kwargs   map[string]any `json:"kwargs"`
}

type pythonCallResponse struct {
	Result any    `json:"result"`
	Error  string `json:"error"`
}

func callPython(moduleName, functionName string, args []any, named map[string]any) (any, error) {
	if !pythonModuleName.MatchString(moduleName) || !pythonIdentifier.MatchString(functionName) || strings.HasPrefix(functionName, "_") {
		return nil, fail("invalid Python module or function name")
	}
	request := pythonCallRequest{Module: moduleName, Function: functionName, Args: args, Kwargs: named}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, fail("Python bridge arguments must be JSON-compatible: %v", err)
	}
	executable := strings.TrimSpace(os.Getenv("TELL_PYTHON_EXECUTABLE"))
	if executable == "" {
		for _, candidate := range []string{"python3", "python"} {
			if path, lookErr := exec.LookPath(candidate); lookErr == nil {
				executable = path
				break
			}
		}
	}
	if executable == "" {
		return nil, fail("Python interoperability needs Python on PATH or TELL_PYTHON_EXECUTABLE set")
	}
	commandArgs := []string{"-c", pythonBridge}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, commandArgs...)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fail("Python call %s.%s exceeded the 30 second limit", moduleName, functionName)
		}
		return nil, fail("could not start Python bridge: %s", strings.TrimSpace(stderr.String()))
	}
	var response pythonCallResponse
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		return nil, fail("Python bridge returned invalid JSON: %v", err)
	}
	if response.Error != "" {
		return nil, fail("Python call %s.%s failed: %s", moduleName, functionName, response.Error)
	}
	return response.Result, nil
}

func newPythonModule(name string) (pythonModule, error) {
	if !pythonModuleName.MatchString(name) {
		return pythonModule{}, fmt.Errorf("invalid Python module name %q", name)
	}
	return pythonModule{name: name}, nil
}
