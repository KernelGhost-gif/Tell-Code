from __future__ import annotations

import ast
import csv
import datetime
import io
import json
import math
import operator
import random
import re
import statistics
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

from .expression_parser import Expr as TellExpr, ExpressionSyntaxError, parse as parse_expression


class TellError(Exception):
    """An error in a Tell program."""


def display(value: Any) -> str:
    if value is None:
        return "nothing"
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, float):
        if math.isnan(value):
            return "NaN"
        if math.isinf(value):
            return "infinity" if value > 0 else "-infinity"
        if value.is_integer():
            return str(int(value))
    if isinstance(value, list):
        return "[" + ", ".join(display(item) for item in value) + "]"
    if isinstance(value, tuple):
        parts = ", ".join(display(item) for item in value)
        if len(value) == 1:
            parts += ","
        return "(" + parts + ")"
    if isinstance(value, dict):
        def json_value(item: Any) -> Any:
            if isinstance(item, float) and math.isfinite(item) and item.is_integer():
                return int(item)
            if isinstance(item, list):
                return [json_value(part) for part in item]
            if isinstance(item, dict):
                return {str(key): json_value(part) for key, part in item.items()}
            return item
        return json.dumps(json_value(value), ensure_ascii=False, separators=(",", ":"), sort_keys=True)
    return str(value)


def display_type(value: Any) -> str:
    if value is None:
        return "nothing"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, (int, float)):
        return "number"
    if isinstance(value, str):
        return "text"
    if isinstance(value, list):
        return "list"
    if isinstance(value, tuple):
        return "tuple"
    if isinstance(value, dict):
        return "dictionary"
    if isinstance(value, TellInstance):
        return value.tell_class.name
    if isinstance(value, TellClass):
        return "class"
    if callable(value):
        return "function"
    return type(value).__name__


class ReturnSignal(Exception):
    def __init__(self, value: Any):
        self.value = value


class BreakSignal(Exception):
    pass


class ContinueSignal(Exception):
    pass


class Environment:
    def __init__(self, values: dict[str, Any] | None = None, parent: "Environment | None" = None):
        self.values = values or {}
        self.parent = parent
        self.constants: set[str] = set()
        self.types: dict[str, str] = {}

    def get(self, name: str) -> Any:
        if name in self.values:
            return self.values[name]
        if self.parent is not None:
            return self.parent.get(name)
        raise TellError(f"I don't know what '{name}' means. Did you define it first?")

    def set(self, name: str, value: Any) -> None:
        self.values[name] = value


@dataclass
class Node:
    kind: str
    data: dict[str, Any]
    line: int
    column: int = 1


@dataclass
class UserFunction:
    interpreter: "Interpreter"
    name: str
    parameters: list[str]
    body: list[Node]
    closure: Environment

    def __call__(self, *args: Any, **kwargs: Any) -> Any:
        return self.invoke(args, kwargs=kwargs)

    def invoke(
        self,
        args: tuple[Any, ...] | list[Any],
        receiver: "TellInstance | None" = None,
        kwargs: dict[str, Any] | None = None,
    ) -> Any:
        named = kwargs or {}
        definitions = [_parameter_parts(parameter) for parameter in self.parameters]
        if any(definition is None for definition in definitions):
            raise TellError(f"'{self.name}' has an invalid parameter definition.")
        parsed = [definition for definition in definitions if definition is not None]
        parameter_names = [name for name, _, _ in parsed]
        variadic_index = next((i for i, definition in enumerate(parsed) if definition[2]), None)
        if len(args) > len(self.parameters) and variadic_index is None:
            raise TellError(
                f"'{self.name}' accepts {len(self.parameters)} argument(s), but got {len(args)} positional arguments."
            )
        values: dict[str, Any] = {}
        for index, (name, _, variadic) in enumerate(parsed):
            if variadic:
                values[name] = list(args[index:])
            elif index < len(args):
                values[name] = args[index]
        for name, value in named.items():
            if name not in parameter_names:
                raise TellError(f"'{self.name}' has no parameter named '{name}'.")
            if parsed[parameter_names.index(name)][2]:
                raise TellError(f"'{self.name}' variadic parameter '{name}' cannot be supplied by name.")
            if name in values:
                raise TellError(f"'{self.name}' got multiple values for '{name}'.")
            values[name] = value
        missing = [name for name, default, variadic in parsed if name not in values and default is None and not variadic]
        if missing:
            raise TellError(f"'{self.name}' is missing argument(s): {', '.join(missing)}.")
        if receiver is not None:
            values["this"] = receiver
        local = Environment(values, self.closure)
        if self.interpreter.call_depth >= self.interpreter.max_call_depth:
            raise TellError(
                f"maximum function call depth ({self.interpreter.max_call_depth}) exceeded"
            )
        self.interpreter.call_depth += 1
        try:
            for name, default, variadic in parsed:
                if name not in local.values and default is not None:
                    local.set(name, self.interpreter.evaluate(default, local))
            self.interpreter.execute(self.body, local)
        except ReturnSignal as signal:
            return signal.value
        finally:
            self.interpreter.call_depth -= 1
        return None


@dataclass
class TellInstance:
    tell_class: "TellClass"
    fields: dict[str, Any]

    def get_member(self, name: str) -> Any:
        if name.startswith("_"):
            raise AttributeError("private attributes are not accessible")
        if name in self.fields:
            return self.fields[name]
        method = self.tell_class.get_method(name)
        if method is not None:
            return BoundTellMethod(method, self)
        raise AttributeError(f"{self.tell_class.name} has no member {name!r}")

    def __getattr__(self, name: str) -> Any:
        return self.get_member(name)


@dataclass
class BoundTellMethod:
    function: UserFunction
    receiver: TellInstance

    def __call__(self, *args: Any, **kwargs: Any) -> Any:
        return self.function.invoke(args, self.receiver, kwargs)


@dataclass
class TellClass:
    name: str
    methods: dict[str, UserFunction]
    constructor: UserFunction | None
    base: "TellClass | None" = None

    def get_method(self, name: str) -> UserFunction | None:
        method = self.methods.get(name)
        if method is not None:
            return method
        return self.base.get_method(name) if self.base is not None else None

    def __call__(self, *args: Any, **kwargs: Any) -> TellInstance:
        instance = TellInstance(self, {})
        if self.constructor is not None:
            self.constructor.invoke(args, instance, kwargs)
        elif args or kwargs:
            raise TellError(f"'{self.name}' has no constructor and accepts no arguments")
        return instance


@dataclass
class TellModule:
    exports: dict[str, Any]

    def get_member(self, name: str) -> Any:
        if name.startswith("_") or name not in self.exports:
            raise TellError(f"Tell module has no public member {name!r}.")
        return self.exports[name]


def _csv_parse(*args: Any, **kwargs: Any) -> list[list[str]]:
    if kwargs or len(args) != 1 or not isinstance(args[0], str):
        raise TellError("csv.parse expects one text argument")
    try:
        text = args[0].replace("\r\n", "\n")
        return [row for row in csv.reader(io.StringIO(text, newline="")) if row]
    except csv.Error as exc:
        raise TellError(f"csv.parse: {exc}") from exc


def _csv_stringify(*args: Any, **kwargs: Any) -> str:
    if kwargs or len(args) != 1 or not isinstance(args[0], list):
        raise TellError("csv.stringify expects one list of rows")
    rows: list[list[str]] = []
    for row_number, row in enumerate(args[0], 1):
        if not isinstance(row, list):
            raise TellError(f"csv.stringify row {row_number} must be a list")
        if any(not isinstance(cell, str) for cell in row):
            raise TellError(f"csv.stringify row {row_number} must contain only text")
        rows.append(row)
    output = io.StringIO(newline="")
    csv.writer(output, lineterminator="\n").writerows(rows)
    return output.getvalue()


def _block_header(line: str) -> str | None:
    line = line.strip()
    return line[:-1].strip() if line.endswith(":") else None


def _take_prefix(text: str, prefix: str) -> str | None:
    if text.casefold().startswith(prefix.casefold()):
        return text[len(prefix):].strip()
    return None


def _split_once_casefold(text: str, separator: str) -> tuple[str, str] | None:
    quote: str | None = None
    escaped = False
    depth = 0
    index = 0
    while index + len(separator) <= len(text):
        char = text[index]
        if quote is not None:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
            index += 1
            continue
        if char in "'\"":
            quote = char
            index += 1
            continue
        if char in "([{":
            depth += 1
        elif char in ")]}":
            depth = max(0, depth - 1)
        if depth == 0 and text[index:index + len(separator)].casefold() == separator.casefold():
            return text[:index].strip(), text[index + len(separator):].strip()
        index += 1
    return None


def _split_top_level_text(text: str, separator: str) -> list[str]:
    parts: list[str] = []
    quote: str | None = None
    escaped = False
    depth = 0
    start = 0
    index = 0
    while index < len(text):
        char = text[index]
        if quote is not None:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
        elif char in "'\"":
            quote = char
        elif char in "([{":
            depth += 1
        elif char in ")]}":
            depth -= 1
        elif depth == 0 and text[index:index + len(separator)].casefold() == separator.casefold():
            parts.append(text[start:index].strip())
            index += len(separator) - 1
            start = index + 1
        index += 1
    parts.append(text[start:].strip())
    return parts


def _translate_expression_text(expression: str) -> str:
    text = expression.strip()
    new_object = re.fullmatch(r"(?:a\s+)?new\s+((?:[^\W\d]|_)\w*)(?:\s+with\s+(.+))?", text, re.I)
    if new_object:
        arguments = _split_top_level_text(new_object.group(2), " and ") if new_object.group(2) else []
        text = f"{new_object.group(1)}({', '.join(arguments)})"
    result_match = re.fullmatch(r"the result of\s+((?:[^\W\d]|_)\w*)\s+with\s+(.+)", text, re.I)
    if result_match:
        args = _split_top_level_text(result_match.group(2), " and ")
        text = f"{result_match.group(1)}({', '.join(args)})"
    for pattern, replacement in (
        (r"^the length of\s+(.+)$", r"len(\1)"),
        (r"^the square root of\s+(.+)$", r"sqrt(\1)"),
        (r"^the absolute value of\s+(.+)$", r"abs(\1)"),
        (r"^the type of\s+(.+)$", r"type_of(\1)"),
    ):
        text = re.sub(pattern, replacement, text, flags=re.I)
    return text


def _first_identifier(text: str) -> tuple[str, str]:
    text = text.strip()
    if not text or not (text[0].isalpha() or text[0] == "_"):
        return "", text
    end = 1
    while end < len(text) and (text[end].isalnum() or text[end] == "_"):
        end += 1
    return text[:end], text[end:].strip()


def _parse_parameter_text(text: str) -> str | None:
    remaining = _take_prefix(text, "all remaining values as ")
    if remaining is not None:
        return f"*{remaining}" if re.fullmatch(r"(?:[^\W\d]|_)\w*", remaining) else None
    parts = _split_once_casefold(text, " defaulting to ")
    if parts is None:
        name = text.strip()
        return name if re.fullmatch(r"(?:[^\W\d]|_)\w*", name) else None
    name, default_value = (part.strip() for part in parts)
    if not re.fullmatch(r"(?:[^\W\d]|_)\w*", name) or not default_value:
        return None
    return f"{name}={default_value}"


def _parameter_parts(parameter: str) -> tuple[str, str | None, bool] | None:
    if parameter.startswith("*"):
        name = parameter[1:].strip()
        if re.fullmatch(r"(?:[^\W\d]|_)\w*", name):
            return name, None, True
        return None
    name, separator, default_value = parameter.partition("=")
    name = name.strip()
    if not re.fullmatch(r"(?:[^\W\d]|_)\w*", name):
        return None
    if separator:
        default_value = default_value.strip()
        if not default_value:
            return None
        return name, default_value, False
    return name, None, False


def _valid_parameters(parameters: list[str]) -> bool:
    names: set[str] = set()
    has_default = False
    has_variadic = False
    for index, parameter in enumerate(parameters):
        parts = _parameter_parts(parameter)
        if parts is None:
            return False
        name, default_value, variadic = parts
        optional = default_value is not None
        if name in names or has_variadic or (has_default and not optional and not variadic) or (variadic and index != len(parameters) - 1):
            return False
        names.add(name)
        has_default = has_default or optional
        has_variadic = variadic
    return True


def _parse_function_header(header: str) -> tuple[str, list[str]] | None:
    rest = _take_prefix(header, "to ")
    if rest is None:
        rest = _take_prefix(header, "define a function called ")
    if rest is None:
        return None
    name, tail = _first_identifier(rest)
    if not name:
        return None
    if tail.startswith("(") and tail.endswith(")"):
        inner = tail[1:-1].strip()
        params = [part.strip() for part in inner.split(",") if part.strip()]
    elif tail:
        tail = _take_prefix(tail, "with ")
        if tail is None:
            return None
        params = [part.strip() for part in re.split(r"\s+and\s+", tail, flags=re.I)]
    else:
        params = []
    parsed_params = [_parse_parameter_text(param) for param in params]
    if any(param is None for param in parsed_params):
        return None
    params = [param for param in parsed_params if param is not None]
    if not _valid_parameters(params):
        return None
    return name, params


class Parser:
    def __init__(self, source: str, filename: str = "<memory>"):
        self.filename = filename
        self.lines = source.splitlines()
        self.index = 0

    def _source_column(self, line_no: int) -> int:
        if line_no < 1 or line_no > len(self.lines):
            return 1
        line = self.lines[line_no - 1]
        return len(line) - len(line.lstrip(" \t")) + 1

    @staticmethod
    def _parse_simple_statement(text: str) -> dict[str, Any] | None:
        def parsed_expression(expression: str) -> TellExpr | None:
            if not expression:
                return None
            try:
                return parse_expression(_translate_expression_text(expression))
            except ExpressionSyntaxError:
                return None

        rest = _take_prefix(text, "let ")
        if rest is not None:
            constant = False
            constant_rest = _take_prefix(rest, "constant ")
            if constant_rest is not None:
                rest, constant = constant_rest, True
            parts = _split_once_casefold(rest, " be ")
            if parts:
                binding = _split_once_casefold(parts[0], " as ")
                name, annotation = binding if binding else (parts[0], None)
                if "," in parts[0] and binding is None:
                    names = [target.strip() for target in parts[0].split(",")]
                    valid = len(names) > 1 and all(
                        re.fullmatch(r"(?:[^\W\d]|_)\w*", target) for target in names
                    ) and len(set(names)) == len(names)
                    expression = parsed_expression(parts[1])
                    if valid and expression is not None:
                        return {"kind": "let", "names": names, "constant": constant, "expression": expression}
                elif re.fullmatch(r"(?:[^\W\d]|_)\w*", name):
                    expression = parsed_expression(parts[1])
                    if expression is not None:
                        return {"kind": "let", "names": [name], "annotation": annotation, "constant": constant, "expression": expression}

        rest = _take_prefix(text, "set ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts:
            if "," in parts[0]:
                names = [target.strip() for target in parts[0].split(",")]
                valid = len(names) > 1 and all(
                    re.fullmatch(r"(?:[^\W\d]|_)\w*", target) for target in names
                ) and len(set(names)) == len(names)
            else:
                names = [parts[0]]
                valid = bool(re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[0]))
            expression = parsed_expression(parts[1])
            if valid and expression is not None:
                return {"kind": "set", "names": names, "expression": expression}
            target = parsed_expression(parts[0])
            if target is not None and target.kind == "member" and expression is not None:
                return {"kind": "set_member", "source": parts[0], "target": target, "expression": expression}

        rest = _take_prefix(text, "tell ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[0]):
            method, tail = _first_identifier(parts[1])
            if method:
                arguments: list[TellExpr] = []
                if tail:
                    args_text = _take_prefix(tail, "with ")
                    if args_text is None:
                        method = ""
                    else:
                        try:
                            arguments = [parse_expression(_translate_expression_text(arg)) for arg in _split_top_level_text(args_text, " and ")]
                        except ExpressionSyntaxError:
                            method = ""
                if method:
                    callee = TellExpr("member", method, (TellExpr("name", parts[0]),))
                    return {
                        "kind": "expression",
                        "source": text,
                        "expression": TellExpr("call", children=(callee, *arguments), names=(None,) * len(arguments)),
                    }

        for prefixes, kind in (
            (("say ", "show ", "print "), "print"),
            (("check that ",), "check"),
            (("give back ", "return "), "return"),
            (("raise an error saying ",), "raise"),
        ):
            for prefix in prefixes:
                expression = _take_prefix(text, prefix)
                if expression is None or not expression:
                    continue
                try:
                    tree = parse_expression(expression)
                except ExpressionSyntaxError:
                    continue
                return {"kind": kind, "source": expression, "expression": tree}
        if any(_take_prefix(text, prefix) is not None for prefix in ("say ", "show ", "print ", "check that ", "give back ", "return ", "raise an error saying ")):
            return None
        if text.casefold() in {"give back", "return"}:
            return {"kind": "return", "source": "", "expression": None}
        if text.casefold() in {"stop this loop", "break out of this loop"}:
            return {"kind": "break", "source": "", "expression": None}
        if text.casefold() in {"skip to the next repetition", "continue to the next repetition"}:
            return {"kind": "continue", "source": "", "expression": None}
        if text:
            try:
                expression = parse_expression(_translate_expression_text(text))
            except ExpressionSyntaxError:
                pass
            else:
                return {"kind": "expression", "source": text, "expression": expression}
        return None

    @staticmethod
    def clean(line: str) -> str:
        # A Note: line and a # line are comments. Inline # comments are handled
        # only when outside a quoted string.
        line = line.strip()
        if not line or line.startswith("#") or line.lower().startswith("note:"):
            return ""
        # Remove inline comments, but keep # characters inside quoted text.
        quote: str | None = None
        escaped = False
        for index, char in enumerate(line):
            if quote:
                if escaped:
                    escaped = False
                elif char == "\\":
                    escaped = True
                elif char == quote:
                    quote = None
            elif char in "'\"":
                quote = char
            elif char == "#":
                line = line[:index].rstrip()
                break
        return line

    def parse(self) -> list[Node]:
        nodes, stop = self.parse_block(set())
        if stop is not None:
            raise TellError(f"Unexpected '{stop}' on line {self.index}.")
        return nodes

    def _parse_if_block(self, condition: str, line_no: int) -> Node:
        body, stop = self.parse_block({"otherwise:", "otherwise-if", "end if."})
        node = Node("if", {"condition": condition.strip(), "body": body, "otherwise": []}, line_no, self._source_column(line_no))
        if stop == "end if.":
            return node

        nested_condition = None
        if stop == "otherwise:":
            otherwise, stop = self.parse_block({"otherwise-if", "end if."})
            node.data["otherwise"] = otherwise
            if stop == "end if.":
                return node
            nested_header = _block_header(stop) if stop is not None else None
            nested_condition = _take_prefix(nested_header, "otherwise if ") if nested_header else None
        elif stop is not None:
            nested_header = _block_header(stop)
            nested_condition = _take_prefix(nested_header, "otherwise if ")

        if nested_condition:
            node.data["otherwise"] = [self._parse_if_block(nested_condition, self.index)]
            return node
        raise TellError(f"If block on line {line_no} needs 'End if.'.")

    def parse_block(self, stops: set[str]) -> tuple[list[Node], str | None]:
        nodes: list[Node] = []
        while self.index < len(self.lines):
            raw = self.lines[self.index]
            line_no = self.index + 1
            self.index += 1
            line = self.clean(raw)
            if not line:
                continue
            low = line.lower()
            if "otherwise-if" in stops and low.startswith("otherwise if ") and line.endswith(":"):
                return nodes, line
            if low in stops:
                return nodes, low
            if low in {"end if.", "end while.", "end repeat.", "end loop.", "end function.", "end try.", "end constructor.", "end class.", "otherwise:", "if something goes wrong:"}:
                raise TellError(f"Unexpected block ending '{line}' on line {line_no}.")

            header = _block_header(line)
            class_rest = _take_prefix(header, "create a class called ") if header is not None else None
            class_parts = _split_once_casefold(class_rest, " based on ") if class_rest is not None else None
            if class_rest is not None:
                class_name, base_name = class_parts if class_parts else (class_rest, None)
                if not re.fullmatch(r"(?:[^\W\d]|_)\w*", class_name) or (base_name is not None and not re.fullmatch(r"(?:[^\W\d]|_)\w*", base_name)):
                    class_name = ""
            else:
                class_name, base_name = "", None
            if class_name:
                body, stop = self.parse_block({"end class."})
                if stop != "end class.":
                    raise TellError(f"Class '{class_name}' on line {line_no} needs 'End class.'.")
                if any(member.kind not in {"function", "constructor"} for member in body):
                    raise TellError(f"Class '{class_name}' can contain only methods and a constructor.")
                nodes.append(Node("class", {"name": class_name, "base": base_name, "body": body}, line_no, self._source_column(line_no)))
                continue

            constructor_rest = _take_prefix(header, "when creating ") if header is not None else None
            constructor_rest = _take_prefix(constructor_rest, "an ") if constructor_rest is not None else None
            if constructor_rest is None and header is not None:
                article_rest = _take_prefix(header, "when creating ")
                constructor_rest = _take_prefix(article_rest, "a ") if article_rest is not None else None
            constructor_name, constructor_tail = _first_identifier(constructor_rest or "")
            constructor_params: list[str] = []
            if constructor_name and constructor_tail:
                constructor_tail = _take_prefix(constructor_tail, "with ")
                if constructor_tail is not None:
                    constructor_params = [p.strip() for p in re.split(r"\s+and\s+", constructor_tail, flags=re.I)]
                else:
                    constructor_name = ""
            if constructor_name and not re.fullmatch(r"(?:[^\W\d]|_)\w*", constructor_name):
                constructor_name = ""
            if constructor_name:
                parsed_params = [_parse_parameter_text(parameter) for parameter in constructor_params]
                if any(parameter is None for parameter in parsed_params):
                    raise TellError(f"Constructor parameters on line {line_no} are invalid.")
                params = [parameter for parameter in parsed_params if parameter is not None]
                if not _valid_parameters(params):
                    raise TellError(f"Constructor parameters on line {line_no} are invalid.")
                body, stop = self.parse_block({"end constructor."})
                if stop != "end constructor.":
                    raise TellError(f"Constructor on line {line_no} needs 'End constructor.'.")
                nodes.append(Node("constructor", {"name": constructor_name, "parameters": params, "body": body}, line_no, self._source_column(line_no)))
                continue

            condition = _take_prefix(header, "if ") if header is not None else None
            if condition:
                condition = re.sub(r",\s*then$", "", condition, flags=re.I).strip()
                nodes.append(self._parse_if_block(condition, line_no))
                continue

            condition = _take_prefix(header, "while ") if header is not None else None
            if condition:
                body, stop = self.parse_block({"end while."})
                if stop != "end while.":
                    raise TellError(f"While block on line {line_no} needs 'End while.'.")
                nodes.append(Node("while", {"condition": condition, "body": body}, line_no, self._source_column(line_no)))
                continue

            repeat_rest = _take_prefix(header, "repeat ") if header is not None else None
            repeat_end = repeat_rest.casefold().rfind(" times") if repeat_rest is not None else -1
            if repeat_end > 0:
                body, stop = self.parse_block({"end repeat."})
                if stop != "end repeat.":
                    raise TellError(f"Repeat block on line {line_no} needs 'End repeat.'.")
                nodes.append(Node("repeat", {"count": repeat_rest[:repeat_end].strip(), "body": body}, line_no, self._source_column(line_no)))
                continue

            for_rest = _take_prefix(header, "for each ") if header is not None else None
            for_parts = _split_once_casefold(for_rest, " in ") if for_rest is not None else None
            loop_names = [name.strip() for name in for_parts[0].split(",")] if for_parts else []
            valid_loop_names = bool(loop_names) and all(
                re.fullmatch(r"(?:[^\W\d]|_)\w*", name) for name in loop_names
            ) and len(set(loop_names)) == len(loop_names)
            if for_parts and valid_loop_names and for_parts[1]:
                body, stop = self.parse_block({"end loop."})
                if stop != "end loop.":
                    raise TellError(f"For-each block on line {line_no} needs 'End loop.'.")
                nodes.append(Node("for", {"names": loop_names, "items": for_parts[1], "body": body}, line_no, self._source_column(line_no)))
                continue

            function_header = _parse_function_header(header or "")
            if function_header:
                name, params = function_header
                body, stop = self.parse_block({"end function."})
                if stop != "end function.":
                    raise TellError(f"Function '{name}' on line {line_no} needs 'End function.'.")
                nodes.append(Node("function", {"name": name, "parameters": params, "body": body}, line_no, self._source_column(line_no)))
                continue

            if low == "try:":
                body, stop = self.parse_block({"if something goes wrong:", "end try."})
                handler: list[Node] = []
                if stop == "if something goes wrong:":
                    handler, stop = self.parse_block({"end try."})
                if stop != "end try.":
                    raise TellError(f"Try block on line {line_no} needs 'End try.'.")
                nodes.append(Node("try", {"body": body, "handler": handler}, line_no, self._source_column(line_no)))
                continue

            statement_text = self._remove_sentence_end(line)
            simple = self._parse_simple_statement(statement_text)
            nodes.append(Node("statement", {"text": statement_text, "simple": simple}, line_no, self._source_column(line_no)))

        return nodes, None

    @staticmethod
    def _remove_sentence_end(line: str) -> str:
        line = line.strip()
        if line.endswith("."):
            return line[:-1].rstrip()
        return line


def format_source(source: str, filename: str = "<memory>") -> str:
    nodes = Parser(source, filename).parse()
    block_kinds = {"if", "while", "repeat", "for", "function", "class", "constructor", "try"}
    openers: set[int] = set()

    def collect(items: list[Node]) -> None:
        for node in items:
            if node.kind in block_kinds:
                openers.add(node.line)
            for key in ("body", "otherwise", "handler"):
                children = node.data.get(key, [])
                if isinstance(children, list):
                    collect(children)

    collect(nodes)
    lines = source.split("\n")
    formatted: list[str] = []
    depth = 0
    ends = {"end if.", "end while.", "end repeat.", "end loop.", "end function.", "end try.", "end constructor.", "end class."}
    for line_number, raw in enumerate(lines, start=1):
        cleaned = Parser.clean(raw).casefold()
        trimmed = raw.strip()
        if not trimmed:
            formatted.append("")
            continue
        branch = cleaned in {"otherwise:", "if something goes wrong:"} or (
            cleaned.startswith("otherwise if ") and cleaned.endswith(":")
        )
        if cleaned in ends or branch:
            depth = max(0, depth - 1)
        formatted.append("    " * depth + trimmed)
        if line_number in openers or branch:
            depth += 1
    return "\n".join(formatted)


class Interpreter:
    DEFAULT_MAX_FUNCTION_CALL_DEPTH = 64
    DEFAULT_MAX_LOOP_ITERATIONS = 1_000_000
    SAFE_MODULES = {
        "math": math,
        "random": random,
        "statistics": statistics,
        "json": json,
        "datetime": datetime,
        "csv": TellModule({"parse": _csv_parse, "stringify": _csv_stringify}),
    }

    BINOPS: dict[type, Callable[[Any, Any], Any]] = {
        ast.Add: operator.add, ast.Sub: operator.sub, ast.Mult: operator.mul,
        ast.Div: operator.truediv, ast.FloorDiv: operator.floordiv, ast.Mod: operator.mod,
        ast.Pow: operator.pow, ast.BitAnd: operator.and_, ast.BitOr: operator.or_,
        ast.BitXor: operator.xor, ast.LShift: operator.lshift, ast.RShift: operator.rshift,
    }
    UNARYOPS: dict[type, Callable[[Any], Any]] = {
        ast.UAdd: operator.pos, ast.USub: operator.neg, ast.Not: operator.not_, ast.Invert: operator.invert,
    }
    COMPARES: dict[type, Callable[[Any, Any], bool]] = {
        ast.Eq: operator.eq, ast.NotEq: operator.ne, ast.Lt: operator.lt,
        ast.LtE: operator.le, ast.Gt: operator.gt, ast.GtE: operator.ge,
        ast.In: lambda a, b: a in b, ast.NotIn: lambda a, b: a not in b,
        ast.Is: operator.is_, ast.IsNot: operator.is_not,
    }

    def __init__(
        self,
        input_fn: Callable[[str], str] = input,
        output_fn: Callable[[str], None] = print,
        *,
        max_call_depth: int = DEFAULT_MAX_FUNCTION_CALL_DEPTH,
        max_loop_iterations: int = DEFAULT_MAX_LOOP_ITERATIONS,
    ):
        if max_call_depth < 1 or max_loop_iterations < 1:
            raise ValueError("execution limits must be positive integers")
        self.input_fn = input_fn
        self.output_fn = output_fn
        self.global_env = Environment()
        self._install_builtins(self.global_env)
        self.filename = "<memory>"
        self.current_line = 0
        self.call_depth = 0
        self.max_call_depth = max_call_depth
        self.max_loop_iterations = max_loop_iterations
        self.module_cache: dict[str, TellModule] = {}
        self.loading_modules: set[str] = set()
        self.echo_expressions = False

    def _install_builtins(self, env: Environment) -> None:
        def average(values: Any) -> float:
            return statistics.mean(values)

        def first(values: Any) -> Any:
            return values[0]

        def last(values: Any) -> Any:
            return values[-1]

        def type_of(value: Any) -> str:
            if isinstance(value, TellInstance):
                return value.tell_class.name
            if isinstance(value, TellClass):
                return "class"
            if isinstance(value, (UserFunction, BoundTellMethod)):
                return "function"
            if value is None:
                return "nothing"
            if isinstance(value, bool):
                return "boolean"
            if isinstance(value, (int, float)):
                return "number"
            if isinstance(value, str):
                return "text"
            if isinstance(value, list):
                return "list"
            if isinstance(value, tuple):
                return "tuple"
            if isinstance(value, dict):
                return "dictionary"
            return type(value).__name__

        def random_number(start: int = 0, end: int = 100) -> int:
            return random.randint(start, end)

        def number(value: Any) -> int | float:
            converted = float(value)
            return int(converted) if converted.is_integer() else converted

        def map_values(function: Any, values: Any) -> list[Any]:
            if not callable(function):
                raise TellError("map expects a function as its first argument")
            if not isinstance(values, list):
                raise TellError("map expects a list as its second argument")
            return [function(value) for value in values]

        def filter_values(function: Any, values: Any) -> list[Any]:
            if not callable(function):
                raise TellError("filter expects a function as its first argument")
            if not isinstance(values, list):
                raise TellError("filter expects a list as its second argument")
            return [value for value in values if function(value)]

        def reduce_values(function: Any, values: Any, initial: Any) -> Any:
            if not callable(function):
                raise TellError("reduce expects a function as its first argument")
            if not isinstance(values, list):
                raise TellError("reduce expects a list as its second argument")
            result = initial
            for value in values:
                result = function(result, value)
            return result

        def enumerate_values(values: Any) -> list[list[Any]]:
            if not isinstance(values, list):
                raise TellError("enumerate expects one list")
            return [[index, value] for index, value in enumerate(values)]

        def tuple_values(values: Any = ()) -> tuple[Any, ...]:
            if isinstance(values, (list, tuple, str)):
                return tuple(values)
            raise TellError("tuple expects a list, tuple, or text")

        builtins = {
            "len": len, "range": range, "sum": sum, "min": min, "max": max,
            "sorted": sorted, "reversed": reversed, "enumerate": enumerate_values,
            "zip": zip, "abs": abs, "round": round, "int": int, "float": float,
            "str": str, "bool": bool, "list": list, "tuple": tuple_values, "dict": dict,
            "set": set, "any": any, "all": all, "average": average,
            "map": map_values, "filter": filter_values, "reduce": reduce_values,
            "first": first, "last": last, "type_of": type_of,
            "random_number": random_number, "number": number, "sqrt": math.sqrt,
            "floor": math.floor, "ceiling": math.ceil,
        }
        for name, value in builtins.items():
            env.set(name, value)

    @staticmethod
    def _split_top_level(text: str, separator: str) -> list[str]:
        return _split_top_level_text(text, separator)

    def translate_expression(self, expression: str) -> str:
        return _translate_expression_text(expression)

    @staticmethod
    def _normalize_expression_words(text: str) -> str:
        """Translate Tell operator phrases without rewriting quoted text."""
        phrases = [
            ("the current repetition number", "_repeat_index"),
            ("the current item", "_current_item"),
            ("the answer", "_last_answer"),
            ("is greater than or equal to", " >= "),
            ("is less than or equal to", " <= "),
            ("is not equal to", " != "), ("isn't equal to", " != "),
            ("is greater than", " > "), ("is more than", " > "),
            ("is less than", " < "), ("is below", " < "),
            ("is at least", " >= "), ("is at most", " <= "),
            ("is not in", " not in "), ("is equal to", " == "),
            ("isn't", " != "), ("is not", " != "),
            ("to the power of", " ** "), ("multiplied by", " * "),
            ("divided by", " / "), ("modulo", " % "),
            ("plus", " + "), ("minus", " - "), ("times", " * "),
            ("is in", " in "), ("is", " == "),
        ]
        out: list[str] = []
        index = 0
        quote: str | None = None
        escaped = False
        while index < len(text):
            char = text[index]
            if quote is not None:
                out.append(char)
                if escaped:
                    escaped = False
                elif char == "\\":
                    escaped = True
                elif char == quote:
                    quote = None
                index += 1
                continue
            if char in "'\"":
                quote = char
                out.append(char)
                index += 1
                continue

            matched = False
            for phrase, replacement in phrases:
                end = index + len(phrase)
                if text[index:end].casefold() != phrase:
                    continue
                before = text[index - 1] if index else " "
                after = text[end] if end < len(text) else " "
                if (before.isalnum() or before == "_") or (after.isalnum() or after == "_"):
                    continue
                out.append(replacement)
                index = end
                matched = True
                break
            if matched:
                continue

            if char.isalpha() or char == "_":
                end = index + 1
                while end < len(text) and (text[end].isalnum() or text[end] == "_"):
                    end += 1
                word = text[index:end]
                canonical = {"true": "True", "false": "False", "nothing": "None"}.get(word.casefold(), word)
                out.append(canonical)
                index = end
                continue
            out.append(char)
            index += 1
        return "".join(out)

    def evaluate(self, expression: str, env: Environment) -> Any:
        translated = self.translate_expression(expression)
        try:
            tree = parse_expression(translated)
        except ExpressionSyntaxError as exc:
            # Keep previously accepted Python-compatible forms working while
            # they are added to Tell's shared grammar and native runtime.
            normalized = self._normalize_expression_words(translated)
            try:
                legacy_tree = ast.parse(normalized, mode="eval")
            except SyntaxError:
                raise TellError(f"I couldn't understand the expression '{expression}': {exc}.") from exc
            try:
                return self._eval(legacy_tree.body, env)
            except TellError:
                raise
            except Exception as legacy_error:
                raise TellError(f"This expression failed: {legacy_error}") from legacy_error
        try:
            return self._eval_expression(tree, env)
        except TellError:
            raise
        except Exception as exc:
            raise TellError(f"This expression failed: {exc}") from exc

    def _eval_expression(self, node: TellExpr, env: Environment) -> Any:
        if node.kind == "literal":
            if isinstance(node.value, str):
                def replace(match: re.Match[str]) -> str:
                    try:
                        return display(self.evaluate(match.group(1).strip(), env))
                    except Exception:
                        return match.group(0)
                return re.sub(r"\{([^{}]+)\}", replace, node.value)
            return node.value
        if node.kind == "name":
            return env.get(str(node.value))
        if node.kind == "list":
            return [self._eval_expression(item, env) for item in node.children]
        if node.kind == "tuple":
            return tuple(self._eval_expression(item, env) for item in node.children)
        if node.kind == "dict":
            return {
                display(self._eval_expression(node.children[index], env)):
                self._eval_expression(node.children[index + 1], env)
                for index in range(0, len(node.children), 2)
            }
        if node.kind == "unary":
            value = self._eval_expression(node.children[0], env)
            if node.value == "not":
                return not value
            number = self._as_number(value)
            return number if node.value == "+" else -number
        if node.kind == "binary":
            operator_name = str(node.value)
            left = self._eval_expression(node.children[0], env)
            if operator_name == "and":
                return bool(left) and bool(self._eval_expression(node.children[1], env))
            if operator_name == "or":
                return bool(left) or bool(self._eval_expression(node.children[1], env))
            right = self._eval_expression(node.children[1], env)
            if operator_name == "==":
                return type(left) is type(right) and left == right or (
                    isinstance(left, (int, float)) and not isinstance(left, bool)
                    and isinstance(right, (int, float)) and not isinstance(right, bool) and left == right
                )
            if operator_name == "!=":
                return not (type(left) is type(right) and left == right or (
                    isinstance(left, (int, float)) and not isinstance(left, bool)
                    and isinstance(right, (int, float)) and not isinstance(right, bool) and left == right
                ))
            if operator_name in {"<", ">", "<=", ">="}:
                left_number = self._try_number(left)
                right_number = self._try_number(right)
                left, right = (left_number, right_number) if left_number is not None and right_number is not None else (display(left), display(right))
                return {"<": operator.lt, ">": operator.gt, "<=": operator.le, ">=": operator.ge}[operator_name](left, right)
            if operator_name in {"in", "notin"}:
                if isinstance(right, dict):
                    contains = display(left) in right
                elif isinstance(right, (list, tuple, str)):
                    needle = display(left) if isinstance(right, str) else left
                    contains = needle in right
                else:
                    contains = False
                return not contains if operator_name == "notin" else contains
            if operator_name == "+":
                if isinstance(left, str):
                    return left + display(right)
                if isinstance(left, list) and isinstance(right, list):
                    return left + right
                if isinstance(left, tuple) and isinstance(right, tuple):
                    return left + right
                return self._as_number(left) + self._as_number(right)
            a, b = self._as_number(left), self._as_number(right)
            if operator_name == "-":
                return a - b
            if operator_name == "*":
                return a * b
            if operator_name in {"/", "//", "%"} and b == 0:
                raise TellError("division by zero")
            if operator_name == "/":
                return a / b
            if operator_name == "//":
                return math.floor(a / b)
            if operator_name == "%":
                return math.fmod(a, b)
            if operator_name == "**":
                return math.pow(a, b)
            raise TellError(f"unsupported operator {operator_name!r}")
        if node.kind == "member":
            member = str(node.value)
            if member.startswith("_"):
                raise TellError("Names beginning with an underscore are private in Tell.")
            value = self._eval_expression(node.children[0], env)
            if isinstance(value, TellInstance):
                return value.get_member(member)
            if isinstance(value, TellClass):
                raise TellError("Tell classes do not expose static members in this version.")
            if isinstance(value, TellModule):
                return value.get_member(member)
            if isinstance(value, dict):
                if member not in value:
                    raise TellError(f"unknown member {member!r}")
                return value[member]
            if isinstance(value, BoundTellMethod):
                raise TellError("Bound methods do not expose runtime internals.")
            return getattr(value, member)
        if node.kind == "index":
            value = self._eval_expression(node.children[0], env)
            index = self._eval_expression(node.children[1], env)
            if isinstance(value, (list, tuple, str)):
                index = int(self._as_number(index))
            elif isinstance(value, dict):
                index = display(index)
            return value[index]
        if node.kind == "slice":
            value = self._eval_expression(node.children[0], env)
            bounds = [self._eval_expression(item, env) if item is not None else None for item in node.children[1:]]
            bounds = [int(self._as_number(item)) if item is not None else None for item in bounds]
            if bounds[2] == 0:
                raise TellError("slice step cannot be zero")
            if not isinstance(value, (list, tuple, str)):
                raise TellError("this value cannot be sliced")
            return value[slice(*bounds)]
        if node.kind == "call":
            function = self._eval_expression(node.children[0], env)
            values = [self._eval_expression(arg, env) for arg in node.children[1:]]
            positional = [value for value, name in zip(values, node.names) if name is None]
            named = {name: value for value, name in zip(values, node.names) if name is not None}
            return function(*positional, **named)
        raise TellError(f"That kind of expression is not supported yet: {node.kind}.")

    @staticmethod
    def _try_number(value: Any) -> float | None:
        try:
            if isinstance(value, bool):
                return float(value)
            return float(value)
        except (TypeError, ValueError):
            return None

    @classmethod
    def _as_number(cls, value: Any) -> float:
        number = cls._try_number(value)
        if number is None:
            raise TellError(f"{display(value)} cannot be used as a number")
        return number

    def _eval(self, node: ast.AST, env: Environment) -> Any:
        if isinstance(node, ast.Constant):
            value = node.value
            if isinstance(value, str):
                def replace(match: re.Match[str]) -> str:
                    # Curly-brace interpolation accepts either a variable name
                    # or a complete Tell expression, e.g. {the length of names}.
                    try:
                        return display(self.evaluate(match.group(1).strip(), env))
                    except Exception:
                        # Keep literal braces intact (useful for JSON text, etc.).
                        return match.group(0)
                value = re.sub(r"\{([^{}]+)\}", replace, value)
            return value
        if isinstance(node, ast.Name):
            return env.get(node.id)
        if isinstance(node, ast.List):
            return [self._eval(item, env) for item in node.elts]
        if isinstance(node, ast.Tuple):
            return tuple(self._eval(item, env) for item in node.elts)
        if isinstance(node, ast.Set):
            return {self._eval(item, env) for item in node.elts}
        if isinstance(node, ast.Dict):
            return {self._eval(k, env): self._eval(v, env) for k, v in zip(node.keys, node.values)}
        if isinstance(node, ast.BinOp) and type(node.op) in self.BINOPS:
            return self.BINOPS[type(node.op)](self._eval(node.left, env), self._eval(node.right, env))
        if isinstance(node, ast.UnaryOp) and type(node.op) in self.UNARYOPS:
            return self.UNARYOPS[type(node.op)](self._eval(node.operand, env))
        if isinstance(node, ast.BoolOp):
            if isinstance(node.op, ast.And):
                for value in node.values:
                    if not self._eval(value, env):
                        return False
                return True
            for value in node.values:
                if self._eval(value, env):
                    return True
            return False
        if isinstance(node, ast.Compare):
            left = self._eval(node.left, env)
            for op_node, comparator in zip(node.ops, node.comparators):
                right = self._eval(comparator, env)
                operation = self.COMPARES.get(type(op_node))
                if operation is None or not operation(left, right):
                    return False
                left = right
            return True
        if isinstance(node, ast.IfExp):
            return self._eval(node.body if self._eval(node.test, env) else node.orelse, env)
        if isinstance(node, ast.Subscript):
            return self._eval(node.value, env)[self._eval(node.slice, env)]
        if isinstance(node, ast.Slice):
            return slice(
                self._eval(node.lower, env) if node.lower else None,
                self._eval(node.upper, env) if node.upper else None,
                self._eval(node.step, env) if node.step else None,
            )
        if isinstance(node, ast.Attribute):
            if node.attr.startswith("_"):
                raise TellError("Names beginning with an underscore are private in Tell.")
            value = self._eval(node.value, env)
            if isinstance(value, TellInstance):
                return value.get_member(node.attr)
            if isinstance(value, TellClass):
                raise TellError("Tell classes do not expose static members in this version.")
            if isinstance(value, TellModule):
                return value.get_member(node.attr)
            if isinstance(value, BoundTellMethod):
                raise TellError("Bound methods do not expose runtime internals.")
            return getattr(value, node.attr)
        if isinstance(node, ast.Call):
            function = self._eval(node.func, env)
            args = [self._eval(arg, env) for arg in node.args]
            kwargs = {kw.arg: self._eval(kw.value, env) for kw in node.keywords if kw.arg is not None}
            return function(*args, **kwargs)
        if isinstance(node, ast.JoinedStr):
            return "".join(str(self._eval(value, env)) for value in node.values)
        if isinstance(node, ast.FormattedValue):
            return str(self._eval(node.value, env))
        raise TellError(f"That kind of expression is not supported yet: {type(node).__name__}.")

    def run(self, source: str, filename: str = "<memory>") -> None:
        self.filename = filename
        try:
            program = Parser(source, filename).parse()
            self.execute(program, self.global_env)
        except ReturnSignal as exc:
            raise TellError("Give back can only be used inside a function.") from exc
        except (BreakSignal, ContinueSignal) as exc:
            raise TellError("Loop control can only be used inside a loop.") from exc

    def execute(self, nodes: list[Node], env: Environment) -> None:
        for node in nodes:
            self.current_line = node.line
            try:
                self.execute_node(node, env)
            except (ReturnSignal, BreakSignal, ContinueSignal):
                raise
            except Exception as exc:
                if isinstance(exc, TellError):
                    raise TellError(f"{self.filename}:{node.line}:{node.column}: {exc}") from exc
                raise TellError(f"{self.filename}:{node.line}:{node.column}: {exc}") from exc

    def execute_node(self, node: Node, env: Environment) -> None:
        data = node.data
        if node.kind == "if":
            branch = data["body"] if self.evaluate(data["condition"], env) else data["otherwise"]
            self.execute(branch, env)
            return
        if node.kind == "while":
            iterations = 0
            while self.evaluate(data["condition"], env):
                iterations += 1
                if iterations > self.max_loop_iterations:
                    raise TellError(f"This loop exceeded the configured limit of {self.max_loop_iterations} iterations.")
                try:
                    self.execute(data["body"], env)
                except ContinueSignal:
                    continue
                except BreakSignal:
                    break
            return
        if node.kind == "repeat":
            count = int(self.evaluate(data["count"], env))
            if count < 0:
                raise TellError("A repeat count cannot be negative.")
            if count > self.max_loop_iterations:
                raise TellError(f"That repeat count is too large (maximum: {self.max_loop_iterations}).")
            for index in range(1, count + 1):
                env.set("_repeat_index", index)
                try:
                    self.execute(data["body"], env)
                except ContinueSignal:
                    continue
                except BreakSignal:
                    break
            return
        if node.kind == "for":
            items = self.evaluate(data["items"], env)
            for index, item in enumerate(items, start=1):
                if index > self.max_loop_iterations:
                    raise TellError(f"For-each loop exceeded the configured limit of {self.max_loop_iterations} iterations.")
                names = data["names"]
                if len(names) == 1:
                    env.set(names[0], item)
                else:
                    if not isinstance(item, (list, tuple)):
                        raise TellError(f"for each expects {len(names)} values, got a non-list item")
                    if len(item) != len(names):
                        raise TellError(f"for each expects {len(names)} values, got {len(item)}")
                    for name, value in zip(names, item):
                        env.set(name, value)
                env.set("_current_item", item)
                try:
                    self.execute(data["body"], env)
                except ContinueSignal:
                    continue
                except BreakSignal:
                    break
            return
        if node.kind == "function":
            function = UserFunction(self, data["name"], data["parameters"], data["body"], env)
            env.set(data["name"], function)
            return
        if node.kind == "class":
            methods: dict[str, UserFunction] = {}
            base = None
            if data["base"]:
                candidate = env.get(data["base"])
                if not isinstance(candidate, TellClass):
                    raise TellError(f"Base type '{data['base']}' is not a class.")
                base = candidate
            constructor = base.constructor if base is not None else None
            has_constructor = False
            for member in data["body"]:
                if member.kind == "constructor":
                    if has_constructor:
                        raise TellError(f"Class '{data['name']}' has more than one constructor.")
                    has_constructor = True
                    if member.data["name"].lower() != data["name"].lower():
                        raise TellError(f"Constructor name '{member.data['name']}' does not match class '{data['name']}'.")
                    constructor = UserFunction(self, data["name"], member.data["parameters"], member.data["body"], env)
                else:
                    method_name = member.data["name"]
                    if method_name in methods:
                        raise TellError(f"Class '{data['name']}' has duplicate method '{method_name}'.")
                    methods[method_name] = UserFunction(self, method_name, member.data["parameters"], member.data["body"], env)
            env.set(data["name"], TellClass(data["name"], methods, constructor, base))
            return
        if node.kind == "try":
            try:
                self.execute(data["body"], env)
            except (ReturnSignal, BreakSignal, ContinueSignal):
                raise
            except Exception as exc:
                env.set("error_message", str(exc))
                self.execute(data["handler"], env)
            return
        simple = data.get("simple")
        if simple is not None:
            expression = simple["expression"]
            value = self._eval_expression(expression, env) if expression is not None else None
            if simple["kind"] == "let":
                names = simple["names"]
                if len(names) > 1:
                    self.assign_many(env, names, value, simple["constant"])
                elif simple["constant"] or simple.get("annotation") is not None:
                    self.declare(env, names[0], value, simple.get("annotation"), simple["constant"])
                else:
                    self.assign(env, names[0], value)
            elif simple["kind"] == "set":
                if len(simple["names"]) > 1:
                    self.assign_many(env, simple["names"], value)
                else:
                    self.assign(env, simple["names"][0], value)
            elif simple["kind"] == "set_member":
                target = self._eval_expression(simple["target"].children[0], env)
                if not isinstance(target, TellInstance):
                    raise TellError("Property assignment requires a Tell instance.")
                target.fields[simple["target"].value] = value
            elif simple["kind"] == "print":
                self.output_fn(display(value))
            elif simple["kind"] == "expression":
                if self.echo_expressions:
                    env.set("_", value)
                    self.output_fn(display(value))
            elif simple["kind"] == "check":
                if not value:
                    raise TellError(f"Check failed: {simple['source']}.")
            elif simple["kind"] == "return":
                raise ReturnSignal(value)
            elif simple["kind"] == "raise":
                raise TellError(str(value))
            elif simple["kind"] == "break":
                raise BreakSignal()
            elif simple["kind"] == "continue":
                raise ContinueSignal()
            else:
                raise TellError(f"Unknown parsed statement {simple['kind']!r}.")
            return
        self.execute_statement(data["text"], env)

    def _matches_type(self, type_name: str, value: Any, env: Environment) -> bool:
        normalized = type_name.casefold()
        primitives = {
            "any": lambda item: True,
            "nothing": lambda item: item is None,
            "boolean": lambda item: isinstance(item, bool),
            "number": lambda item: isinstance(item, (int, float)) and not isinstance(item, bool),
            "text": lambda item: isinstance(item, str),
            "list": lambda item: isinstance(item, list),
            "tuple": lambda item: isinstance(item, tuple),
            "dictionary": lambda item: isinstance(item, dict),
            "function": lambda item: callable(item) and not isinstance(item, TellClass),
            "class": lambda item: isinstance(item, TellClass),
        }
        if normalized in primitives:
            return primitives[normalized](value)
        try:
            candidate = env.get(type_name)
        except TellError as exc:
            raise TellError(f"Unknown type annotation {type_name!r}.") from exc
        if not isinstance(candidate, TellClass):
            raise TellError(f"{type_name!r} is not a type.")
        current = value.tell_class if isinstance(value, TellInstance) else None
        while current is not None:
            if current is candidate:
                return True
            current = current.base
        return False

    def _check_type(self, type_name: str, value: Any, env: Environment, name: str) -> None:
        if not self._matches_type(type_name, value, env):
            raise TellError(f"type mismatch for {name!r}: expected {type_name}, got {display_type(value)}")

    def declare(
        self,
        env: Environment,
        name: str,
        value: Any,
        type_name: str | None,
        constant: bool,
    ) -> None:
        if name in env.constants:
            raise TellError(f"Constant {name!r} cannot be redeclared.")
        if type_name is not None:
            type_name = type_name.strip()
            if not type_name:
                raise TellError("A type annotation cannot be empty.")
            previous_type = env.types.get(name)
            if previous_type is not None and previous_type.casefold() != type_name.casefold():
                raise TellError(f"{name!r} is already annotated as {previous_type}.")
            self._check_type(type_name, value, env, name)
            env.types[name] = type_name
        if constant:
            env.constants.add(name)
        env.set(name, value)

    def assign(self, env: Environment, name: str, value: Any) -> None:
        if name in env.constants:
            raise TellError(f"Constant {name!r} cannot be reassigned.")
        annotation = env.types.get(name)
        if annotation is not None:
            self._check_type(annotation, value, env, name)
        env.set(name, value)

    def assign_many(self, env: Environment, names: list[str], values: Any, constant: bool = False) -> None:
        if not isinstance(values, (list, tuple)):
            raise TellError("Multiple assignment expects a list or tuple.")
        if len(values) != len(names):
            raise TellError(f"Multiple assignment expected {len(names)} values, got {len(values)}.")
        for target, value in zip(names, values):
            if constant:
                if target in env.constants:
                    raise TellError(f"Constant {target!r} cannot be redeclared.")
            elif target in env.constants:
                raise TellError(f"Constant {target!r} cannot be reassigned.")
            annotation = env.types.get(target)
            if annotation is not None:
                self._check_type(annotation, value, env, target)
        if constant:
            for target, value in zip(names, values):
                self.declare(env, target, value, None, True)
        else:
            for target, value in zip(names, values):
                env.set(target, value)

    def execute_statement(self, statement: str, env: Environment) -> None:
        expression = _take_prefix(statement, "check that ")
        if expression is not None:
            if not expression:
                raise TellError("Check that needs a condition.")
            if not self.evaluate(expression, env):
                raise TellError(f"Check failed: {expression}.")
            return

        rest = _take_prefix(statement, "let ")
        if rest is not None:
            constant = False
            constant_rest = _take_prefix(rest, "constant ")
            if constant_rest is not None:
                rest = constant_rest
                constant = True
            parts = _split_once_casefold(rest, " be ")
            if parts:
                binding = _split_once_casefold(parts[0], " as ")
                name, type_name = binding if binding else (parts[0], None)
            else:
                name, type_name = "", None
            if parts and "," in parts[0]:
                targets = [target.strip() for target in parts[0].split(",")]
                valid_names = (
                    len(targets) > 1
                    and binding is None
                    and all(re.fullmatch(r"(?:[^\W\d]|_)\w*", target) for target in targets)
                    and len(set(targets)) == len(targets)
                )
                if valid_names and parts[1]:
                    values = self.evaluate(parts[1], env)
                    if not isinstance(values, list):
                        raise TellError("Multiple assignment expects a list.")
                    if len(values) != len(targets):
                        raise TellError(f"Multiple assignment expected {len(targets)} values, got {len(values)}.")
                    for target in targets:
                        if constant and target in env.constants:
                            raise TellError(f"Constant {target!r} cannot be redeclared.")
                        annotation = env.types.get(target)
                        if constant:
                            if annotation is not None:
                                self._check_type(annotation, values[targets.index(target)], env, target)
                        else:
                            if target in env.constants:
                                raise TellError(f"Constant {target!r} cannot be reassigned.")
                            if annotation is not None:
                                self._check_type(annotation, values[targets.index(target)], env, target)
                    if constant:
                        for target, value in zip(targets, values):
                            self.declare(env, target, value, None, True)
                    else:
                        for target, value in zip(targets, values):
                            env.set(target, value)
                    return
            if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", name) and parts[1]:
                value = self.evaluate(parts[1], env)
                if constant or type_name is not None:
                    self.declare(env, name, value, type_name, constant)
                else:
                    self.assign(env, name, value)
                return
        rest = _take_prefix(statement, "set ")
        if rest is not None:
            parts = _split_once_casefold(rest, " to ")
            if parts and "," in parts[0]:
                targets = [target.strip() for target in parts[0].split(",")]
                if (
                    len(targets) > 1
                    and all(re.fullmatch(r"(?:[^\W\d]|_)\w*", target) for target in targets)
                    and len(set(targets)) == len(targets)
                    and parts[1]
                ):
                    values = self.evaluate(parts[1], env)
                    if not isinstance(values, list):
                        raise TellError("Multiple assignment expects a list.")
                    if len(values) != len(targets):
                        raise TellError(f"Multiple assignment expected {len(targets)} values, got {len(values)}.")
                    for index, target in enumerate(targets):
                        if target in env.constants:
                            raise TellError(f"Constant {target!r} cannot be reassigned.")
                        annotation = env.types.get(target)
                        if annotation is not None:
                            self._check_type(annotation, values[index], env, target)
                    for target, value in zip(targets, values):
                        env.set(target, value)
                    return
            if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[0]) and parts[1]:
                self.assign(env, parts[0], self.evaluate(parts[1], env))
                return
        for prefix in ("say ", "show ", "print "):
            expression = _take_prefix(statement, prefix)
            if expression:
                value = self.evaluate(expression, env)
                self.output_fn(display(value))
                return
        for prefix in ("give back ", "return "):
            expression = _take_prefix(statement, prefix)
            if expression is not None:
                raise ReturnSignal(self.evaluate(expression, env) if expression else None)
        if statement.casefold() in {"give back", "return"}:
            raise ReturnSignal(None)
        if statement.casefold() in {"stop this loop", "break out of this loop"}:
            raise BreakSignal()
        if statement.casefold() in {"skip to the next repetition", "continue to the next repetition"}:
            raise ContinueSignal()
        for prefix in ("call ", "run "):
            rest = _take_prefix(statement, prefix)
            if rest is not None:
                name, tail = _first_identifier(rest)
                args_text = _take_prefix(tail, "with ")
                if name and args_text:
                    function = env.get(name)
                    args = self._split_top_level(args_text, " and ")
                    function(*(self.evaluate(arg, env) for arg in args))
                    return

        rest = _take_prefix(statement, "use the module ")
        parts = _split_once_casefold(rest, " as ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
            env.set(parts[1], self._load_module(str(self.evaluate(parts[0], env))))
            return
        rest = _take_prefix(statement, "set ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts:
            member = _split_once_casefold(parts[0], ".")
            if member and re.fullmatch(r"(?:[^\W\d]|_)\w*", member[0]) and re.fullmatch(r"(?:[^\W\d]|_)\w*", member[1]):
                target = env.get(member[0])
                if not isinstance(target, TellInstance):
                    raise TellError(f"'{member[0]}' is not a Tell instance.")
                target.fields[member[1]] = self.evaluate(parts[1], env)
                return
        rest = _take_prefix(statement, "remember ")
        parts = _split_once_casefold(rest, " as this.") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
            instance = env.get("this")
            if not isinstance(instance, TellInstance):
                raise TellError("Remember ... as this.field can only be used in a constructor or method.")
            instance.fields[parts[1]] = self.evaluate(parts[0], env)
            return
        rest = _take_prefix(statement, "tell ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[0]):
            method, tail = _first_identifier(parts[1])
            arguments: list[str] = []
            if tail:
                tail = _take_prefix(tail, "with ")
                if not tail:
                    method = ""
                else:
                    arguments = self._split_top_level(tail, " and ")
            if method:
                target = env.get(parts[0])
                if not isinstance(target, TellInstance):
                    raise TellError(f"'{parts[0]}' is not a Tell instance.")
                getattr(target, method)(*(self.evaluate(arg, env) for arg in arguments))
                return
        rest = _take_prefix(statement, "ask ")
        if rest is not None:
            for delimiter in (" and save the answer as ", " and put the answer in "):
                parts = _split_once_casefold(rest, delimiter)
                if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
                    answer = self.input_fn(str(self.evaluate(parts[0], env)))
                    env.set(parts[1], answer)
                    env.set("_last_answer", answer)
                    return
        rest = _take_prefix(statement, "read the file ")
        parts = _split_once_casefold(rest, " into ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
            env.set(parts[1], Path(str(self.evaluate(parts[0], env))).read_text(encoding="utf-8"))
            return
        for prefix in ("write ", "append "):
            rest = _take_prefix(statement, prefix)
            parts = _split_once_casefold(rest, " to the file ") if rest is not None else None
            if parts:
                value = display(self.evaluate(parts[0], env))
                path = Path(str(self.evaluate(parts[1], env)))
                if prefix == "write ":
                    path.write_text(value, encoding="utf-8")
                else:
                    with path.open("a", encoding="utf-8") as file:
                        file.write(value)
                return
        rest = _take_prefix(statement, "add ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
            collection = env.get(parts[1])
            if not isinstance(collection, list):
                raise TellError(f"'{parts[1]}' does not support adding items.")
            collection.append(self.evaluate(parts[0], env))
            return
        rest = _take_prefix(statement, "remove ")
        parts = _split_once_casefold(rest, " from ") if rest is not None else None
        if parts and re.fullmatch(r"(?:[^\W\d]|_)\w*", parts[1]):
            collection = env.get(parts[1])
            if not isinstance(collection, list):
                raise TellError(f"'{parts[1]}' does not support removing items.")
            value = self.evaluate(parts[0], env)
            try:
                collection.remove(value)
            except ValueError as exc:
                raise TellError(f"item {value!r} was not found") from exc
            return
        rest = _take_prefix(statement, "raise an error saying ")
        if rest:
            raise TellError(str(self.evaluate(rest, env)))
        rest = _take_prefix(statement, "use the ")
        if rest is not None:
            library_text, suffix = _split_once_casefold(rest, " library") or ("", "")
            if library_text:
                requested = library_text.casefold().replace(" ", "")
                alias = requested
                suffix = suffix.strip()
                if suffix:
                    alias = _take_prefix(suffix, "as ") or ""
                if alias and re.fullmatch(r"(?:[^\W\d]|_)\w*", alias):
                    module_name = {"mathematics": "math", "dates": "datetime", "dateandtime": "datetime"}.get(requested, requested)
                    if module_name not in self.SAFE_MODULES:
                        raise TellError(f"The '{requested}' library is not available in this version of Tell.")
                    env.set(alias, self.SAFE_MODULES[module_name])
                    return
        rest = _take_prefix(statement, "set ")
        parts = _split_once_casefold(rest, " to ") if rest is not None else None
        if parts:
            location, value_expr = parts
            item_text = _take_prefix(location, "the item at position ")
            if item_text is None:
                item_text = _take_prefix(location, "item at position ")
            if item_text is not None:
                pair = _split_once_casefold(item_text, " in ")
                if pair and re.fullmatch(r"(?:[^\W\d]|_)\w*", pair[1]):
                    collection = env.get(pair[1])
                    if not isinstance(collection, list):
                        raise TellError(f"'{pair[1]}' is not a list.")
                    collection[self.evaluate(pair[0], env)] = self.evaluate(value_expr, env)
                    return
            key_text = _take_prefix(location, "the key ")
            if key_text is None:
                key_text = _take_prefix(location, "key ")
            if key_text is not None:
                pair = _split_once_casefold(key_text, " in ")
                if pair and re.fullmatch(r"(?:[^\W\d]|_)\w*", pair[1]):
                    collection = env.get(pair[1])
                    if not isinstance(collection, dict):
                        raise TellError(f"'{pair[1]}' is not a dictionary.")
                    collection[self.evaluate(pair[0], env)] = self.evaluate(value_expr, env)
                    return

        module_match = re.match(
            r"^use the module\s+(.+?)\s+as\s+((?:[^\W\d]|_)\w*)$", statement, re.I
        )
        if module_match:
            module_path = self.evaluate(module_match.group(1), env)
            env.set(module_match.group(2), self._load_module(str(module_path)))
            return
        tell_match = re.match(
            r"^tell\s+((?:[^\W\d]|_)\w*)\s+to\s+((?:[^\W\d]|_)\w*)(?:\s+with\s+(.+))?$", statement, re.I
        )
        if tell_match:
            arguments = self._split_top_level(tell_match.group(3), " and ") if tell_match.group(3) else []
            target = env.get(tell_match.group(1))
            method = getattr(target, tell_match.group(2))
            method(*(self.evaluate(argument, env) for argument in arguments))
            return
        match = re.match(r"^set\s+((?:[^\W\d]|_)\w*)\.((?:[^\W\d]|_)\w*)\s+to\s+(.+)$", statement, re.I)
        if match:
            target = env.get(match.group(1))
            if not isinstance(target, TellInstance):
                raise TellError(f"'{match.group(1)}' is not a Tell instance.")
            target.fields[match.group(2)] = self.evaluate(match.group(3), env)
            return
        match = re.match(r"^remember\s+(.+?)\s+as\s+this\.((?:[^\W\d]|_)\w*)$", statement, re.I)
        if match:
            instance = env.get("this")
            if not isinstance(instance, TellInstance):
                raise TellError("Remember ... as this.field can only be used in a constructor or method.")
            instance.fields[match.group(2)] = self.evaluate(match.group(1), env)
            return
        match = re.match(r"^ask\s+(.+?)\s+and\s+(?:save the answer as|put the answer in)\s+((?:[^\W\d]|_)\w*)$", statement, re.I)
        if match:
            prompt = self.evaluate(match.group(1), env)
            answer = self.input_fn(str(prompt))
            env.set(match.group(2), answer)
            env.set("_last_answer", answer)
            return
        match = re.match(r"^read the file\s+(.+?)\s+into\s+((?:[^\W\d]|_)\w*)$", statement, re.I)
        if match:
            path = self.evaluate(match.group(1), env)
            env.set(match.group(2), Path(str(path)).read_text(encoding="utf-8"))
            return
        match = re.match(r"^write\s+(.+?)\s+to the file\s+(.+)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            path = self.evaluate(match.group(2), env)
            Path(str(path)).write_text(str(value), encoding="utf-8")
            return
        match = re.match(r"^append\s+(.+?)\s+to the file\s+(.+)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            path = self.evaluate(match.group(2), env)
            with Path(str(path)).open("a", encoding="utf-8") as file:
                file.write(str(value))
            return
        match = re.match(r"^use the\s+([A-Za-z_]+)\s+library(?:\s+as\s+((?:[^\W\d]|_)\w*))?$", statement, re.I)
        if match:
            requested = match.group(1).lower()
            aliases = {"mathematics": "math", "dates": "datetime", "date and time": "datetime"}
            module_name = aliases.get(requested, requested)
            if module_name not in self.SAFE_MODULES:
                raise TellError(f"The '{requested}' library is not available in this version of Tell.")
            env.set(match.group(2) or module_name, self.SAFE_MODULES[module_name])
            return
        match = re.match(r"^set (?:the )?item at position (.+?) in ((?:[^\W\d]|_)\w*) to (.+)$", statement, re.I)
        if match:
            collection = env.get(match.group(2))
            index = self.evaluate(match.group(1), env)
            value = self.evaluate(match.group(3), env)
            collection[index] = value
            return
        match = re.match(r"^set (?:the )?key (.+?) in ((?:[^\W\d]|_)\w*) to (.+)$", statement, re.I)
        if match:
            collection = env.get(match.group(2))
            if not isinstance(collection, dict):
                raise TellError(f"'{match.group(2)}' is not a dictionary.")
            key = self.evaluate(match.group(1), env)
            value = self.evaluate(match.group(3), env)
            collection[key] = value
            return
        match = re.match(r"^add\s+(.+?)\s+to\s+((?:[^\W\d]|_)\w*)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            collection = env.get(match.group(2))
            if not hasattr(collection, "append"):
                raise TellError(f"'{match.group(2)}' does not support adding items.")
            collection.append(value)
            return
        match = re.match(r"^remove\s+(.+?)\s+from\s+((?:[^\W\d]|_)\w*)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            collection = env.get(match.group(2))
            if hasattr(collection, "remove"):
                collection.remove(value)
            else:
                raise TellError(f"'{match.group(2)}' does not support removing items.")
            return
        match = re.match(r"^raise an error saying\s+(.+)$", statement, re.I)
        if match:
            raise TellError(str(self.evaluate(match.group(1), env)))

        # A bare function call is allowed for concise one-line actions.
        try:
            value = self.evaluate(statement, env)
            if self.echo_expressions:
                env.set("_", value)
                self.output_fn(display(value))
        except TellError as exc:
            if "I don't know what" in str(exc) or "couldn't understand" in str(exc):
                raise TellError(f"I don't recognise this sentence: '{statement}'.") from exc
            raise

    def _load_module(self, requested_path: str) -> TellModule:
        module_path = Path(requested_path)
        if module_path.suffix.lower() != ".tell":
            raise TellError("Tell modules must use the .tell extension.")
        importer = Path(self.filename)
        base = importer.resolve().parent if self.filename != "<memory>" else Path.cwd()
        resolved = (base / module_path).resolve()
        key = str(resolved)
        if key in self.module_cache:
            return self.module_cache[key]
        if key in self.loading_modules:
            raise TellError(f"Circular module import detected for {key!r}.")
        try:
            source = resolved.read_text(encoding="utf-8")
        except OSError as exc:
            raise TellError(f"Cannot load Tell module {requested_path!r}: {exc}") from exc
        nodes = Parser(source, key).parse()
        self.loading_modules.add(key)
        module_env = Environment(parent=self.global_env)
        previous_filename = self.filename
        self.filename = key
        try:
            self.execute(nodes, module_env)
        finally:
            self.filename = previous_filename
            self.loading_modules.remove(key)
        exports = {name: value for name, value in module_env.values.items() if not name.startswith("_")}
        module = TellModule(exports)
        self.module_cache[key] = module
        return module
