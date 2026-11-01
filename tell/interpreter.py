from __future__ import annotations

import ast
import datetime
import json
import math
import operator
import random
import re
import statistics
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable


class TellError(Exception):
    """An error in a Tell program."""


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


@dataclass
class UserFunction:
    interpreter: "Interpreter"
    name: str
    parameters: list[str]
    body: list[Node]
    closure: Environment

    def __call__(self, *args: Any) -> Any:
        if len(args) != len(self.parameters):
            raise TellError(
                f"'{self.name}' needs {len(self.parameters)} argument(s), but got {len(args)}."
            )
        local = Environment(dict(zip(self.parameters, args)), self.closure)
        try:
            self.interpreter.execute(self.body, local)
        except ReturnSignal as signal:
            return signal.value
        return None


class Parser:
    def __init__(self, source: str, filename: str = "<memory>"):
        self.filename = filename
        self.lines = source.splitlines()
        self.index = 0

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
            if low in stops:
                return nodes, low
            if low in {"end if.", "end while.", "end repeat.", "end loop.", "end function.", "end try.", "otherwise:", "if something goes wrong:"}:
                raise TellError(f"Unexpected block ending '{line}' on line {line_no}.")

            if re.match(r"^if\s+.+:\s*$", line, re.I):
                condition = re.sub(r"^if\s+", "", line, flags=re.I).strip()[:-1].strip()
                condition = re.sub(r",\s*then$", "", condition, flags=re.I).strip()
                body, stop = self.parse_block({"otherwise:", "end if."})
                otherwise: list[Node] = []
                if stop == "otherwise:":
                    otherwise, stop = self.parse_block({"end if."})
                if stop != "end if.":
                    raise TellError(f"If block on line {line_no} needs 'End if.'.")
                nodes.append(Node("if", {"condition": condition, "body": body, "otherwise": otherwise}, line_no))
                continue

            if re.match(r"^while\s+.+:\s*$", line, re.I):
                condition = line[6:-1].strip()
                body, stop = self.parse_block({"end while."})
                if stop != "end while.":
                    raise TellError(f"While block on line {line_no} needs 'End while.'.")
                nodes.append(Node("while", {"condition": condition, "body": body}, line_no))
                continue

            repeat_match = re.match(r"^repeat\s+(.+?)\s+times\s*:\s*$", line, re.I)
            if repeat_match:
                body, stop = self.parse_block({"end repeat."})
                if stop != "end repeat.":
                    raise TellError(f"Repeat block on line {line_no} needs 'End repeat.'.")
                nodes.append(Node("repeat", {"count": repeat_match.group(1), "body": body}, line_no))
                continue

            for_match = re.match(r"^for each\s+([A-Za-z_]\w*)\s+in\s+(.+?)\s*:\s*$", line, re.I)
            if for_match:
                body, stop = self.parse_block({"end loop."})
                if stop != "end loop.":
                    raise TellError(f"For-each block on line {line_no} needs 'End loop.'.")
                nodes.append(Node("for", {"name": for_match.group(1), "items": for_match.group(2), "body": body}, line_no))
                continue

            function_match = re.match(r"^(?:to|define a function called)\s+([A-Za-z_]\w*)\s*(.*?)\s*:\s*$", line, re.I)
            if function_match:
                name, tail = function_match.group(1), function_match.group(2).strip()
                if tail.startswith("(") and tail.endswith(")"):
                    params_text = tail[1:-1].strip()
                    params = [p.strip() for p in params_text.split(",") if p.strip()]
                elif tail.lower().startswith("with "):
                    params = [p.strip() for p in re.split(r"\s+and\s+", tail[5:], flags=re.I) if p.strip()]
                elif tail:
                    params = [p.strip() for p in re.split(r"\s+and\s+", tail) if p.strip()]
                else:
                    params = []
                if any(not re.fullmatch(r"[A-Za-z_]\w*", p) for p in params):
                    raise TellError(f"Function parameters on line {line_no} must be simple names.")
                body, stop = self.parse_block({"end function."})
                if stop != "end function.":
                    raise TellError(f"Function '{name}' on line {line_no} needs 'End function.'.")
                nodes.append(Node("function", {"name": name, "parameters": params, "body": body}, line_no))
                continue

            if low == "try:":
                body, stop = self.parse_block({"if something goes wrong:", "end try."})
                handler: list[Node] = []
                if stop == "if something goes wrong:":
                    handler, stop = self.parse_block({"end try."})
                if stop != "end try.":
                    raise TellError(f"Try block on line {line_no} needs 'End try.'.")
                nodes.append(Node("try", {"body": body, "handler": handler}, line_no))
                continue

            nodes.append(Node("statement", {"text": self._remove_sentence_end(line)}, line_no))

        return nodes, None

    @staticmethod
    def _remove_sentence_end(line: str) -> str:
        line = line.strip()
        if line.endswith("."):
            return line[:-1].rstrip()
        return line


class Interpreter:
    SAFE_MODULES = {
        "math": math,
        "random": random,
        "statistics": statistics,
        "json": json,
        "datetime": datetime,
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

    def __init__(self, input_fn: Callable[[str], str] = input, output_fn: Callable[[str], None] = print):
        self.input_fn = input_fn
        self.output_fn = output_fn
        self.global_env = Environment()
        self._install_builtins(self.global_env)
        self.filename = "<memory>"
        self.current_line = 0

    def _install_builtins(self, env: Environment) -> None:
        def average(values: Any) -> float:
            return statistics.mean(values)

        def first(values: Any) -> Any:
            return values[0]

        def last(values: Any) -> Any:
            return values[-1]

        def type_of(value: Any) -> str:
            return type(value).__name__

        def random_number(start: int = 0, end: int = 100) -> int:
            return random.randint(start, end)

        def number(value: Any) -> int | float:
            converted = float(value)
            return int(converted) if converted.is_integer() else converted

        builtins = {
            "len": len, "range": range, "sum": sum, "min": min, "max": max,
            "sorted": sorted, "reversed": reversed, "enumerate": enumerate,
            "zip": zip, "abs": abs, "round": round, "int": int, "float": float,
            "str": str, "bool": bool, "list": list, "tuple": tuple, "dict": dict,
            "set": set, "any": any, "all": all, "average": average,
            "first": first, "last": last, "type_of": type_of,
            "random_number": random_number, "number": number, "sqrt": math.sqrt,
            "floor": math.floor, "ceiling": math.ceil,
        }
        for name, value in builtins.items():
            env.set(name, value)

    @staticmethod
    def _split_top_level(text: str, separator: str) -> list[str]:
        parts: list[str] = []
        quote: str | None = None
        escaped = False
        depth = 0
        start = 0
        i = 0
        while i < len(text):
            ch = text[i]
            if quote:
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == quote:
                    quote = None
            else:
                if ch in "'\"":
                    quote = ch
                elif ch in "([{":
                    depth += 1
                elif ch in ")]}":
                    depth -= 1
                elif depth == 0 and text[i:i + len(separator)].lower() == separator.lower():
                    parts.append(text[start:i].strip())
                    i += len(separator) - 1
                    start = i + 1
            i += 1
        parts.append(text[start:].strip())
        return parts

    @staticmethod
    def _replace_outside_quotes(text: str, replacements: list[tuple[str, str]]) -> str:
        out: list[str] = []
        i = 0
        quote: str | None = None
        escaped = False
        while i < len(text):
            ch = text[i]
            if quote:
                out.append(ch)
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == quote:
                    quote = None
                i += 1
                continue
            if ch in "'\"":
                quote = ch
                out.append(ch)
                i += 1
                continue
            matched = False
            for old, new in replacements:
                if text[i:i + len(old)].lower() == old.lower():
                    before = text[i - 1] if i else " "
                    after = text[i + len(old)] if i + len(old) < len(text) else " "
                    if (not old[0].isalnum() or not before.isalnum()) and (not old[-1].isalnum() or not after.isalnum()):
                        out.append(new)
                        i += len(old)
                        matched = True
                        break
            if not matched:
                out.append(ch)
                i += 1
        return "".join(out)

    def translate_expression(self, expression: str) -> str:
        text = expression.strip()
        # Natural-language function result: "the result of add with 2 and 3".
        result_match = re.fullmatch(r"the result of\s+([A-Za-z_]\w*)\s+with\s+(.+)", text, re.I)
        if result_match:
            args = self._split_top_level(result_match.group(2), " and ")
            text = f"{result_match.group(1)}({', '.join(args)})"

        # Common phrases that read naturally but have direct expression equivalents.
        prefix_patterns = [
            (r"^the length of\s+(.+)$", r"len(\1)"),
            (r"^the square root of\s+(.+)$", r"sqrt(\1)"),
            (r"^the absolute value of\s+(.+)$", r"abs(\1)"),
            (r"^the type of\s+(.+)$", r"type_of(\1)"),
        ]
        for pattern, replacement in prefix_patterns:
            text = re.sub(pattern, replacement, text, flags=re.I)

        replacements = [
            ("the current repetition number", "_repeat_index"),
            ("the current item", "_current_item"),
            ("the answer", "_last_answer"),
            ("is greater than or equal to", " >= "),
            ("is less than or equal to", " <= "),
            ("is not equal to", " != "),
            ("isn't equal to", " != "),
            ("is equal to", " == "),
            ("isn't", " != "), ("is not", " != "),
            ("is at least", " >= "), ("is at most", " <= "),
            ("is greater than", " > "), ("is more than", " > "),
            ("is less than", " < "), ("is below", " < "),
            ("is not in", " not in "), ("is in", " in "),
            ("to the power of", " ** "), ("divided by", " / "),
            ("multiplied by", " * "), ("plus", " + "),
            ("minus", " - "), ("times", " * "), ("modulo", " % "),
            (" and ", " and "), (" or ", " or "),
        ]
        text = self._replace_outside_quotes(text, replacements)
        # "x is 4" is a useful, natural equality shorthand.
        text = self._replace_outside_quotes(text, [(" is ", " == ")])
        text = self._replace_outside_quotes(text, [("nothing", "None"), ("true", "True"), ("false", "False")])
        return text

    def evaluate(self, expression: str, env: Environment) -> Any:
        translated = self.translate_expression(expression)
        try:
            tree = ast.parse(translated, mode="eval")
        except SyntaxError as exc:
            raise TellError(f"I couldn't understand the expression '{expression}'.") from exc
        try:
            return self._eval(tree.body, env)
        except TellError:
            raise
        except Exception as exc:
            raise TellError(f"This expression failed: {exc}") from exc

    def _eval(self, node: ast.AST, env: Environment) -> Any:
        if isinstance(node, ast.Constant):
            value = node.value
            if isinstance(value, str):
                def replace(match: re.Match[str]) -> str:
                    # Curly-brace interpolation accepts either a variable name
                    # or a complete Tell expression, e.g. {the length of names}.
                    try:
                        return str(self.evaluate(match.group(1).strip(), env))
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
                result: Any = True
                for value in node.values:
                    result = self._eval(value, env)
                    if not result:
                        return result
                return result
            result = False
            for value in node.values:
                result = self._eval(value, env)
                if result:
                    return result
            return result
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
                    raise TellError(f"{self.filename}:{node.line}: {exc}") from exc
                raise TellError(f"{self.filename}:{node.line}: {exc}") from exc

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
                if iterations > 1_000_000:
                    raise TellError("This loop ran over one million times. I stopped it to prevent a runaway program.")
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
            if count > 1_000_000:
                raise TellError("That repeat count is too large (maximum: 1,000,000).")
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
            for item in items:
                env.set(data["name"], item)
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
        if node.kind == "try":
            try:
                self.execute(data["body"], env)
            except (ReturnSignal, BreakSignal, ContinueSignal):
                raise
            except Exception as exc:
                env.set("error_message", str(exc))
                self.execute(data["handler"], env)
            return
        self.execute_statement(data["text"], env)

    def execute_statement(self, statement: str, env: Environment) -> None:
        match = re.match(r"^let\s+([A-Za-z_]\w*)\s+be\s+(.+)$", statement, re.I)
        if match:
            env.set(match.group(1), self.evaluate(match.group(2), env))
            return
        match = re.match(r"^set\s+([A-Za-z_]\w*)\s+to\s+(.+)$", statement, re.I)
        if match:
            env.set(match.group(1), self.evaluate(match.group(2), env))
            return
        match = re.match(r"^(?:say|show|print)\s+(.+)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            if isinstance(value, (dict, list, tuple, set)):
                rendered = repr(value)
            else:
                rendered = str(value)
            self.output_fn(rendered)
            return
        match = re.match(r"^ask\s+(.+?)\s+and\s+(?:save the answer as|put the answer in)\s+([A-Za-z_]\w*)$", statement, re.I)
        if match:
            prompt = self.evaluate(match.group(1), env)
            answer = self.input_fn(str(prompt))
            env.set(match.group(2), answer)
            env.set("_last_answer", answer)
            return
        match = re.match(r"^read the file\s+(.+?)\s+into\s+([A-Za-z_]\w*)$", statement, re.I)
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
        match = re.match(r"^use the\s+([A-Za-z_]+)\s+library(?:\s+as\s+([A-Za-z_]\w*))?$", statement, re.I)
        if match:
            requested = match.group(1).lower()
            aliases = {"mathematics": "math", "dates": "datetime", "date and time": "datetime"}
            module_name = aliases.get(requested, requested)
            if module_name not in self.SAFE_MODULES:
                raise TellError(f"The '{requested}' library is not available in this version of Tell.")
            env.set(match.group(2) or module_name, self.SAFE_MODULES[module_name])
            return
        match = re.match(r"^(?:call|run)\s+([A-Za-z_]\w*)\s+with\s+(.+)$", statement, re.I)
        if match:
            args = self._split_top_level(match.group(2), " and ")
            function = env.get(match.group(1))
            function(*(self.evaluate(arg, env) for arg in args))
            return
        match = re.match(r"^(?:give back|return)(?:\s+(.+))?$", statement, re.I)
        if match:
            raise ReturnSignal(self.evaluate(match.group(1), env) if match.group(1) else None)
        if re.fullmatch(r"(?:stop this loop|break out of this loop)", statement, re.I):
            raise BreakSignal()
        if re.fullmatch(r"(?:skip to the next repetition|continue to the next repetition)", statement, re.I):
            raise ContinueSignal()
        match = re.match(r"^set (?:the )?item at position (.+?) in ([A-Za-z_]\w*) to (.+)$", statement, re.I)
        if match:
            collection = env.get(match.group(2))
            index = self.evaluate(match.group(1), env)
            value = self.evaluate(match.group(3), env)
            collection[index] = value
            return
        match = re.match(r"^set (?:the )?key (.+?) in ([A-Za-z_]\w*) to (.+)$", statement, re.I)
        if match:
            collection = env.get(match.group(2))
            if not isinstance(collection, dict):
                raise TellError(f"'{match.group(2)}' is not a dictionary.")
            key = self.evaluate(match.group(1), env)
            value = self.evaluate(match.group(3), env)
            collection[key] = value
            return
        match = re.match(r"^add\s+(.+?)\s+to\s+([A-Za-z_]\w*)$", statement, re.I)
        if match:
            value = self.evaluate(match.group(1), env)
            collection = env.get(match.group(2))
            if not hasattr(collection, "append"):
                raise TellError(f"'{match.group(2)}' does not support adding items.")
            collection.append(value)
            return
        match = re.match(r"^remove\s+(.+?)\s+from\s+([A-Za-z_]\w*)$", statement, re.I)
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
            self.evaluate(statement, env)
        except TellError as exc:
            if "I don't know what" in str(exc) or "couldn't understand" in str(exc):
                raise TellError(f"I don't recognise this sentence: '{statement}'.") from exc
            raise
