"""A small, dependency-free Language Server Protocol server for Tell."""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from typing import BinaryIO

# TellC's typed language and object declarations.
COMPLETIONS = (
    "let", "let number", "let decimal", "let text", "let bool",
    "object", "own", "others", "end object", "to", "out", "end function",
    "self", "if", "otherwise", "end if", "while", "end while", "return",
    "give back", "say", "true", "false", "extern cpp function", "extern c function",
)


def _utf16_length(text: str) -> int:
    return len(text.encode("utf-16-le")) // 2


def diagnostics(source: str, uri: str) -> list[dict]:
    compiler = os.environ.get("TELL_LSP_COMPILER", "tellc")
    try:
        result = subprocess.run([compiler, "--check-stdin"], input=source.encode("utf-8"),
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
    except (OSError, subprocess.TimeoutExpired) as exc:
        message = f"Could not run TellC checker ({compiler}): {exc}. Set TELL_LSP_COMPILER to a compatible tellc."
        result = None
    else:
        if result.returncode == 0:
            return []
        message = result.stderr.decode("utf-8", errors="replace").strip() or "TellC checker failed without a diagnostic"
    line = column = 0
    match = re.match(r"^<stdin>:(\d+):(\d+):\s*(.*)$", message, re.DOTALL)
    lines = source.split("\n")
    if match:
        line = min(max(int(match.group(1)) - 1, 0), len(lines) - 1)
        # TellC locations count UTF-8 bytes; LSP positions default to UTF-16.
        text = lines[line].rstrip("\r")
        byte_offset = max(int(match.group(2)) - 1, 0)
        prefix = text.encode("utf-8")[:byte_offset].decode("utf-8", errors="ignore")
        column = _utf16_length(prefix)
        length = _utf16_length(text[len(prefix):len(prefix) + 1])
        message = match.group(3)
    else:
        length = 0
    return [{"range": {"start": {"line": line, "character": column},
                       "end": {"line": line, "character": column + length}},
             "severity": 1, "source": "tellc", "message": message}]


class Server:
    def __init__(self, reader: BinaryIO, writer: BinaryIO):
        self.reader, self.writer = reader, writer
        self.documents: dict[str, tuple[str, int | None]] = {}
        self.shutdown_requested = False

    def send(self, message: dict) -> None:
        data = json.dumps(message, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        self.writer.write(f"Content-Length: {len(data)}\r\n\r\n".encode("ascii") + data)
        self.writer.flush()

    def publish(self, uri: str, source: str, version: int | None) -> None:
        params = {"uri": uri, "diagnostics": diagnostics(source, uri)}
        if version is not None:
            params["version"] = version
        self.send({"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": params})

    def handle(self, message: dict) -> bool:
        method = message.get("method")
        params = message.get("params") or {}
        request = "id" in message
        result = None
        error = None
        if method == "exit":
            return False
        if method == "initialize":
            result = {"capabilities": {"textDocumentSync": {"openClose": True, "change": 1},
                                       "completionProvider": {}},
                      "serverInfo": {"name": "tell-lsp", "version": "0.1.0"}}
        elif method == "shutdown":
            self.shutdown_requested = True
        elif method == "textDocument/didOpen":
            doc = params["textDocument"]
            self.documents[doc["uri"]] = (doc["text"], doc.get("version"))
            self.publish(doc["uri"], doc["text"], doc.get("version"))
        elif method == "textDocument/didChange":
            doc = params["textDocument"]
            changes = params.get("contentChanges", [])
            if doc["uri"] in self.documents and changes:
                source = changes[-1]["text"]
                self.documents[doc["uri"]] = (source, doc.get("version"))
                self.publish(doc["uri"], source, doc.get("version"))
        elif method == "textDocument/didClose":
            uri = params["textDocument"]["uri"]
            self.documents.pop(uri, None)
            self.send({"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
                       "params": {"uri": uri, "diagnostics": []}})
        elif method == "textDocument/completion":
            result = [{"label": word, "kind": 14} for word in COMPLETIONS]
        elif request:
            error = {"code": -32601, "message": "Method not found"}
        if request:
            response = {"jsonrpc": "2.0", "id": message["id"]}
            response["error" if error else "result"] = error if error else result
            self.send(response)
        return True

    def run(self) -> int:
        while True:
            headers = {}
            while True:
                line = self.reader.readline()
                if not line:
                    return 0 if self.shutdown_requested else 1
                if line in (b"\r\n", b"\n"):
                    break
                name, sep, value = line.partition(b":")
                if not sep:
                    return 1
                headers[name.strip().lower()] = value.strip()
            try:
                length = int(headers[b"content-length"])
                if length < 0:
                    return 1
                data = self.reader.read(length)
                if len(data) != length:
                    return 1
                message = json.loads(data.decode("utf-8"))
            except (KeyError, ValueError, UnicodeError):
                return 1
            if not self.handle(message):
                return 0 if self.shutdown_requested else 1


def main() -> int:
    return Server(sys.stdin.buffer, sys.stdout.buffer).run()


if __name__ == "__main__":
    raise SystemExit(main())
