from __future__ import annotations

from dataclasses import dataclass
import re

@dataclass(frozen=True)
class Diagnostic:
    kind: str
    filename: str
    line: int
    column: int
    message: str

    def __str__(self) -> str:
        return f'[{self.kind}] File "{self.filename}", line {self.line}, col {self.column}: {self.message}'

_LOCATION = re.compile(r"^(.*):(\d+):(\d+):\s*(.*)$", re.DOTALL)
_TOKEN_LOCATION = re.compile(r"\b(?:at )?line\s+(\d+),\s*column\s+(\d+)\b", re.IGNORECASE)
_LINE_LOCATION = re.compile(r"\bline\s+(\d+)\b", re.IGNORECASE)

EXIT_CODES = {
    "SyntaxError": 64,
    "UsageError": 64,
    "TypeError": 70,
    "NameError": 70,
    "RuntimeError": 70,
    "IOError": 74,
}

def classify_message(message: str, default_kind: str = "RuntimeError") -> str:
    folded = message.casefold()
    if "type mismatch" in folded or "type annotation" in folded or "cannot be used as a number" in folded:
        return "TypeError"
    if "i don't know what" in folded or "undefined name" in folded:
        return "NameError"
    return default_kind

def make_diagnostic(
    error: BaseException | str,
    filename: str = "<input>",
    default_kind: str = "RuntimeError",
    source: str | None = None,
) -> Diagnostic:
    message = str(error)
    line = column = 1
    explicit_position = (
        hasattr(error, "filename") and hasattr(error, "line") and hasattr(error, "column")
    )
    if explicit_position:
        filename = str(getattr(error, "filename")) or filename
        line = int(getattr(error, "line"))
        column = int(getattr(error, "column"))

    match = _LOCATION.match(message)
    if match:
        embedded_file, embedded_line, embedded_column, embedded_message = match.groups()
        if embedded_file:
            filename = embedded_file
        line, column = int(embedded_line), int(embedded_column)
        message = embedded_message
    else:
        match = _TOKEN_LOCATION.search(message)
        if match:
            if not explicit_position:
                line, column = int(match.group(1)), int(match.group(2))
            message = _TOKEN_LOCATION.sub("", message).strip()
            message = re.sub(r"\s+at$", "", message).strip()
        else:
            match = _LINE_LOCATION.search(message)
            if match:
                if not explicit_position:
                    line = int(match.group(1))
                message = _LINE_LOCATION.sub("", message, count=1).strip()
                message = re.sub(r"\s+(on|at)$", "", message).strip()
                if source and not explicit_position:
                    lines = source.splitlines()
                    if 1 <= line <= len(lines):
                        raw = lines[line - 1]
                        column = len(raw) - len(raw.lstrip(" \t")) + 1

    kind = classify_message(message, default_kind)
    return Diagnostic(kind, filename or "<input>", max(1, line), max(1, column), message)

def exit_code(kind: str) -> int:
    return EXIT_CODES.get(kind, 70)
