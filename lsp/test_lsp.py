import json
import os
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parent.parent
BINARY = os.environ.get("TELL_LSP_BINARY", "tell-lsp")
URI = "file:///test.tell"


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


class LanguageServerTests(unittest.TestCase):
    def exchange(self, requests):
        lifecycle = [
            {"id": 1, "method": "initialize", "params": {}},
            {"method": "initialized", "params": {}},
            *requests,
            {"id": 3, "method": "shutdown"},
            {"method": "exit"},
        ]
        process = subprocess.run(
            [BINARY],
            input=b"".join(frame({"jsonrpc": "2.0", **request}) for request in lifecycle),
            capture_output=True,
            check=True,
            timeout=15,
            env={**os.environ, "TELL_LSP_COMPILER": "/nonexistent/tellc"},
        )
        self.assertEqual(process.stderr, b"")
        return messages(process.stdout)

    def diagnostics(self, source):
        responses = self.exchange([
            {"method": "textDocument/didOpen", "params": {"textDocument": {"uri": URI, "text": source}}},
        ])
        return responses[1]["params"]["diagnostics"]

    def test_tellc_objects_and_type_errors(self):
        self.assertEqual(self.diagnostics((ROOT / "TellC/examples/objects.tell").read_text()), [])
        for fixture, expected in [("private_field", "field 'secret' is private"),
                                  ("argument_type", "expected 'number', got 'text'"),
                                  ("return_type", "expected 'number', got 'text'"),
                                  ("missing_return", "function must return a value on every path")]:
            with self.subTest(fixture=fixture):
                source = (ROOT / "TellC/tests/fixtures" / (fixture + ".tell")).read_text()
                self.assertIn(expected, self.diagnostics(source)[0]["message"])

    def test_stdio_lifecycle_and_live_diagnostics(self):
        responses = self.exchange([
            {"method": "textDocument/didOpen", "params": {"textDocument": {"uri": URI, "text": "let number age be", "version": 1}}},
            {"id": "completion", "method": "textDocument/completion", "params": {}},
            {"method": "textDocument/didChange", "params": {"textDocument": {"uri": URI, "version": 2}, "contentChanges": [{"text": "let number age be 3"}]}},
            {"method": "textDocument/didClose", "params": {"textDocument": {"uri": URI}}},
            {"id": 2, "method": "textDocument/hover", "params": {}},
        ])
        self.assertEqual(responses[0]["result"]["capabilities"]["textDocumentSync"]["change"], 1)
        self.assertIn("expected expression", responses[1]["params"]["diagnostics"][0]["message"])
        self.assertEqual(responses[1]["params"]["version"], 1)
        self.assertEqual(responses[2]["id"], "completion")
        self.assertIn("object", [item["label"] for item in responses[2]["result"]])
        self.assertEqual(responses[3]["params"]["diagnostics"], [])
        self.assertEqual(responses[3]["params"]["version"], 2)
        self.assertEqual(responses[4]["params"]["diagnostics"], [])
        self.assertEqual(responses[5]["error"]["code"], -32601)
        self.assertIsNone(responses[6]["result"])

    def test_utf8_compiler_column_converts_to_utf16(self):
        result = self.diagnostics('say "ok"\r\nsay "😀" +\r\n')
        self.assertEqual(result[0]["range"], {
            "start": {"line": 1, "character": 10},
            "end": {"line": 1, "character": 10},
        })


if __name__ == "__main__":
    unittest.main()
