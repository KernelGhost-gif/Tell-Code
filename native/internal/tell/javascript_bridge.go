package tell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var javaScriptModuleName = regexp.MustCompile("^(?:node:[A-Za-z0-9_./-]+|(?:@[A-Za-z0-9_.-]+/)?[A-Za-z0-9][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_.-]+)*|\\.{1,2}/[A-Za-z0-9_./-]+)$")
var javaScriptIdentifier = regexp.MustCompile("^[A-Za-z_$][A-Za-z0-9_$]*$")
const javaScriptReplyMarker = "\x1eTELL_JSON\x1e"

const javaScriptBridge = `import fs from "node:fs";
import { createRequire } from "node:module";
const require = createRequire(process.cwd() + "/.tell-polyglot.cjs");
const marker = "\u001eTELL_JSON\u001e";
function reply(value) { process.stdout.write(marker + JSON.stringify(value) + "\n"); }
try {
  const request = JSON.parse(fs.readFileSync(0, "utf8"));
  let imported;
  try { imported = await import(request.module); }
  catch (importError) { imported = require(request.module); }
  let owner = null;
  let fn = null;
  for (const candidate of [imported, imported && imported.default]) {
    if (candidate && typeof candidate[request.function] === "function") {
      owner = candidate;
      fn = candidate[request.function];
      break;
    }
  }
  if (!fn) throw new TypeError(request.function + " is not an exported function");
  const args = Array.isArray(request.args) ? [...request.args] : [];
  if (request.kwargs && Object.keys(request.kwargs).length) args.push(request.kwargs);
  const result = await fn.apply(owner, args);
  if (typeof result === "undefined") throw new TypeError("function returned undefined; return a JSON-compatible value");
  const encoded = JSON.stringify({ result });
  if (encoded === undefined) throw new TypeError("function returned a non-JSON value");
  reply(JSON.parse(encoded));
} catch (error) {
  reply({ error: (error && error.name ? error.name : "Error") + ": " + (error && error.message ? error.message : String(error)) });
}
`

type javascriptModule struct{ name string }
type javascriptFunction struct{ module, name string }
type javaScriptCallRequest struct {
	Module string `json:"module"`
	Function string `json:"function"`
	Args []any `json:"args"`
	Kwargs map[string]any `json:"kwargs"`
}
type javaScriptCallResponse struct {
	Result any `json:"result"`
	Error string `json:"error"`
}

func newJavaScriptModule(name string) (javascriptModule, error) {
	name = strings.TrimSpace(name)
	if !javaScriptModuleName.MatchString(name) || strings.ContainsRune(name, 0) {
		return javascriptModule{}, fmt.Errorf("invalid JavaScript module specifier %q", name)
	}
	return javascriptModule{name: name}, nil
}

func defaultJavaScriptAlias(name string) string {
	if index := strings.LastIndex(name, ":"); index >= 0 { name = name[index+1:] }
	name = strings.TrimRight(name, "/")
	base := name[strings.LastIndex(name, "/")+1:]
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func callJavaScript(moduleName, functionName string, args []any, named map[string]any) (any, error) {
	if _, err := newJavaScriptModule(moduleName); err != nil { return nil, fail("%v", err) }
	if !javaScriptIdentifier.MatchString(functionName) || strings.HasPrefix(functionName, "_") {
		return nil, fail("invalid or private JavaScript export %q", functionName)
	}
	input, err := json.Marshal(javaScriptCallRequest{Module: moduleName, Function: functionName, Args: args, Kwargs: named})
	if err != nil { return nil, fail("JavaScript bridge arguments must be JSON-compatible: %v", err) }
	executable := strings.TrimSpace(os.Getenv("TELL_NODE_EXECUTABLE"))
	if executable == "" {
		executable, err = exec.LookPath("node")
		if err != nil { return nil, fail("JavaScript interoperability needs Node.js on PATH or TELL_NODE_EXECUTABLE set") }
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "--input-type=module", "-e", javaScriptBridge)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded { return nil, fail("JavaScript call %s.%s exceeded the 30 second limit", moduleName, functionName) }
		return nil, fail("could not start JavaScript bridge: %s", strings.TrimSpace(stderr.String()))
	}
	output := stdout.String()
	index := strings.LastIndex(output, javaScriptReplyMarker)
	if index < 0 { return nil, fail("JavaScript bridge returned no response (stdout: %s; stderr: %s)", strings.TrimSpace(output), strings.TrimSpace(stderr.String())) }
	var response javaScriptCallResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(output[index+len(javaScriptReplyMarker):])), &response); err != nil {
		return nil, fail("JavaScript bridge returned invalid JSON: %v", err)
	}
	if response.Error != "" { return nil, fail("JavaScript call %s.%s failed: %s", moduleName, functionName, response.Error) }
	return response.Result, nil
}
