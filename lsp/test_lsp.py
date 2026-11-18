import json
import os
from pathlib import Path
import subprocess
import sys
import unittest

from tell_lsp.__main__ import diagnostics

ROOT = Path(__file__).resolve().parent.parent
COMPILER = Path(os.environ.get("TELL_LSP_COMPILER", ROOT / "build/tellc-macos-arm64/tellc/tellc"))
os.environ["TELL_LSP_COMPILER"] = str(COMPILER)


def frame(message):
    body = json.dumps(message, ensure_ascii=False).encode("utf-8")
    return b"Content-Length: %d\r\n\r\n" % len(body) + body


def messages(data):
    result = []
    while data:
        header, data = data.split(b"\r\n\r\n", 1)
        length = int(header.split(b":", 1)[1])
        result.append(json.loads(data[:length]))
        data = data[length:]
    return result


@unittest.skipUnless(COMPILER.is_file(), "set TELL_LSP_COMPILER to a built tellc")
class LanguageServerTests(unittest.TestCase):
    def test_tellc_objects_and_type_errors(self):
        self.assertEqual(diagnostics((ROOT / "TellC/examples/objects.tell").read_text(), "file:///test.tell"), [])
        for fixture, expected in [("private_field", "field 'secret' is private"),
                                  ("argument_type", "expected 'number', got 'text'"),
                                  ("return_type", "expected 'number', got 'text'"),
                                  ("missing_return", "function must return a value on every path")]:
            with self.subTest(fixture=fixture):
                source = (ROOT / "TellC/tests/fixtures" / (fixture + ".tell")).read_text()
                self.assertIn(expected, diagnostics(source, "file:///test.tell")[0]["message"])

    def test_stdio_lifecycle_and_live_diagnostics(self):
        uri = "file:///test.tell"
        requests = [
            {"id": 1, "method": "initialize", "params": {}},
            {"method": "initialized", "params": {}},
            {"method": "textDocument/didOpen", "params": {"textDocument": {"uri": uri, "text": 'let number age be "old"', "version": 1}}},
            {"method": "textDocument/didChange", "params": {"textDocument": {"uri": uri, "version": 2}, "contentChanges": [{"text": "let number age be 3"}]}},
            {"id": 2, "method": "textDocument/completion", "params": {}},
            {"method": "textDocument/didClose", "params": {"textDocument": {"uri": uri}}},
            {"id": 3, "method": "shutdown"}, {"method": "exit"},
        ]
        process = subprocess.run([sys.executable, "-m", "tell_lsp"], input=b"".join(frame({"jsonrpc": "2.0", **r}) for r in requests), capture_output=True, check=True, cwd=ROOT / "lsp")
        responses = messages(process.stdout)
        self.assertEqual(responses[0]["result"]["capabilities"]["textDocumentSync"]["change"], 1)
        self.assertIn("expected 'number'", responses[1]["params"]["diagnostics"][0]["message"])
        self.assertEqual(responses[2]["params"]["diagnostics"], [])
        self.assertEqual(responses[2]["params"]["version"], 2)
        self.assertIn("object", [item["label"] for item in responses[3]["result"]])
        self.assertEqual(responses[4]["params"]["diagnostics"], [])
        self.assertIsNone(responses[5]["result"])
        self.assertEqual(process.stderr, b"")

    def test_utf8_compiler_column_converts_to_utf16(self):
        result = diagnostics('say "😀" +\n', "file:///test.tell")
        self.assertEqual(result[0]["range"]["start"], {"line": 0, "character": 10})

    def test_missing_compiler_is_visible(self):
        original = os.environ["TELL_LSP_COMPILER"]
        try:
            os.environ["TELL_LSP_COMPILER"] = "/nonexistent/tellc"
            self.assertIn("Could not run TellC checker", diagnostics("", "file:///x.tell")[0]["message"])
        finally:
            os.environ["TELL_LSP_COMPILER"] = original


if __name__ == "__main__":
    unittest.main()
