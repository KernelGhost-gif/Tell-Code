"""Deterministic lexer and Pratt parser for Tell expressions."""

from __future__ import annotations

from dataclasses import dataclass
import re


@dataclass(frozen=True)
class Token:
    kind: str
    text: str
    offset: int
    line: int
    column: int


@dataclass(frozen=True)
class Expr:
    kind: str
    value: object = None
    children: tuple["Expr", ...] = ()
    names: tuple[str | None, ...] = ()
    start: int = 0


class ExpressionSyntaxError(ValueError):
    def __init__(self, message: str, token: Token):
        super().__init__(f"{message} at line {token.line}, column {token.column}")


_PHRASES = {
    "the current repetition number": ("ident", "_repeat_index"),
    "the current item": ("ident", "_current_item"),
    "the answer": ("ident", "_last_answer"),
    "is greater than or equal to": (">=", ">="),
    "is less than or equal to": ("<=", "<="),
    "is not equal to": ("!=", "!="),
    "isn't equal to": ("!=", "!="),
    "is greater than": (">", ">"),
    "is more than": (">", ">"),
    "is less than": ("<", "<"),
    "is below": ("<", "<"),
    "is at least": (">=", ">="),
    "is at most": ("<=", "<="),
    "is not in": ("notin", "notin"),
    "is equal to": ("==", "=="),
    "isn't": ("!=", "!="),
    "is not": ("!=", "!="),
    "to the power of": ("**", "**"),
    "multiplied by": ("*", "*"),
    "divided by": ("/", "/"),
    "modulo": ("%", "%"),
    "plus": ("+", "+"),
    "minus": ("-", "-"),
    "times": ("*", "*"),
    "is in": ("in", "in"),
    "is": ("==", "=="),
}
_PHRASE_KEYS = sorted(_PHRASES, key=len, reverse=True)
_MULTI_OPERATORS = ("**", "//", ">=", "<=", "==", "!=")
_SINGLE = set("+-*/%<>()[]{}.,:=")


def lex(source: str) -> list[Token]:
    result: list[Token] = []
    i = 0

    def emit(kind: str, text: str, start: int) -> None:
        line = source.count("\n", 0, start) + 1
        last = source.rfind("\n", 0, start)
        result.append(Token(kind, text, start, line, start - last))

    while i < len(source):
        char = source[i]
        if char.isspace():
            i += 1
            continue
        start = i
        if char in "'\"":
            quote = char
            i += 1
            parts: list[str] = []
            while i < len(source) and source[i] != quote:
                if source[i] == "\\":
                    i += 1
                    if i >= len(source):
                        break
                    parts.append({"n": "\n", "t": "\t", "r": "\r"}.get(source[i], source[i]))
                    i += 1
                else:
                    parts.append(source[i])
                    i += 1
            if i >= len(source):
                emit("error", "unterminated text", start)
                raise ExpressionSyntaxError("unterminated text", result[-1])
            i += 1
            emit("string", "".join(parts), start)
            continue
        if char.isdigit() or (char == "." and i + 1 < len(source) and source[i + 1].isdigit()):
            match = re.match(r"(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?", source[i:])
            assert match is not None
            text = match.group(0)
            i += len(text)
            emit("number", text, start)
            continue

        phrase_found = False
        for phrase in _PHRASE_KEYS:
            end = i + len(phrase)
            if source[i:end].casefold() != phrase:
                continue
            before = source[i - 1] if i else " "
            after = source[end] if end < len(source) else " "
            if (before.isalnum() or before == "_") or (after.isalnum() or after == "_"):
                continue
            kind, text = _PHRASES[phrase]
            emit(kind, text, start)
            i = end
            phrase_found = True
            break
        if phrase_found:
            continue

        if char.isalpha() or char == "_":
            i += 1
            while i < len(source) and (source[i].isalnum() or source[i] == "_"):
                i += 1
            word = source[start:i]
            low = word.casefold()
            kind = {"true": "true", "false": "false", "nothing": "nothing"}.get(low, low if low in {"and", "or", "not", "in"} else "ident")
            emit(kind, word, start)
            continue

        operator = next((op for op in _MULTI_OPERATORS if source.startswith(op, i)), None)
        if operator is not None:
            emit(operator, operator, start)
            i += len(operator)
            continue
        if char in _SINGLE:
            emit(char, char, start)
            i += 1
            continue
        emit("error", char, start)
        raise ExpressionSyntaxError(f"unexpected character {char!r}", result[-1])

    line = source.count("\n") + 1
    last = source.rfind("\n")
    result.append(Token("eof", "", len(source), line, len(source) - last))
    return result


_PRECEDENCE = {"or": 1, "and": 2, "==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
               "in": 3, "notin": 3, "+": 4, "-": 4, "*": 5, "/": 5, "//": 5, "%": 5, "**": 6}


class Parser:
    def __init__(self, source: str):
        self.tokens = lex(source)
        self.pos = 0

    def peek(self) -> Token:
        return self.tokens[self.pos]

    def take(self) -> Token:
        token = self.peek()
        if token.kind != "eof":
            self.pos += 1
        return token

    def require(self, kind: str, message: str) -> Token:
        if self.peek().kind != kind:
            raise ExpressionSyntaxError(message, self.peek())
        return self.take()

    def parse(self) -> Expr:
        expression = self.parse_precedence(1)
        if self.peek().kind != "eof":
            raise ExpressionSyntaxError(f"unexpected {self.peek().text!r} in expression", self.peek())
        return expression

    def parse_precedence(self, minimum: int) -> Expr:
        left = self.parse_prefix()
        while (precedence := _PRECEDENCE.get(self.peek().kind, 0)) >= minimum and precedence:
            operator = self.take()
            next_minimum = precedence if operator.kind == "**" else precedence + 1
            right = self.parse_precedence(next_minimum)
            left = Expr("binary", operator.kind, (left, right), start=operator.offset)
        return left

    def parse_prefix(self) -> Expr:
        token = self.take()
        if token.kind in {"number", "string", "true", "false", "nothing"}:
            if token.kind == "number":
                value: object = float(token.text)
                if not any(c in token.text for c in ".eE"):
                    value = int(token.text)
            elif token.kind == "true":
                value = True
            elif token.kind == "false":
                value = False
            elif token.kind == "nothing":
                value = None
            else:
                value = token.text
            node = Expr("literal", value, start=token.offset)
        elif token.kind in {"+", "-", "not"}:
            node = Expr("unary", token.kind, (self.parse_precedence(7),), start=token.offset)
        elif token.kind == "ident":
            node = Expr("name", token.text, start=token.offset)
        elif token.kind == "(":
            if self.peek().kind == ")":
                self.take()
                node = Expr("tuple", children=(), start=token.offset)
            else:
                first = self.parse_precedence(1)
                if self.peek().kind == ",":
                    items = [first]
                    while self.peek().kind == ",":
                        self.take()
                        if self.peek().kind == ")":
                            break
                        items.append(self.parse_precedence(1))
                    self.require(")", "expected ) after tuple")
                    node = Expr("tuple", children=tuple(items), start=token.offset)
                else:
                    self.require(")", "expected )")
                    node = first
        elif token.kind == "[":
            items: list[Expr] = []
            if self.peek().kind != "]":
                while True:
                    items.append(self.parse_precedence(1))
                    if self.peek().kind != ",":
                        break
                    self.take()
                    if self.peek().kind == "]":
                        break
            self.require("]", "expected ]")
            node = Expr("list", children=tuple(items), start=token.offset)
        elif token.kind == "{":
            pairs: list[Expr] = []
            if self.peek().kind != "}":
                while True:
                    pairs.append(self.parse_precedence(1))
                    self.require(":", "expected : in dictionary")
                    pairs.append(self.parse_precedence(1))
                    if self.peek().kind != ",":
                        break
                    self.take()
                    if self.peek().kind == "}":
                        break
            self.require("}", "expected }")
            node = Expr("dict", children=tuple(pairs), start=token.offset)
        else:
            raise ExpressionSyntaxError(f"expected a value, found {token.text!r}", token)
        return self.parse_postfix(node)

    def parse_postfix(self, value: Expr) -> Expr:
        while True:
            token = self.peek()
            if token.kind == ".":
                self.take()
                member = self.require("ident", "expected a member name after .")
                value = Expr("member", member.text, (value,), start=token.offset)
            elif token.kind == "(":
                self.take()
                args: list[Expr] = []
                names: list[str | None] = []
                named = False
                if self.peek().kind != ")":
                    while True:
                        arg_name = None
                        if self.peek().kind == "ident" and self.tokens[self.pos + 1].kind == "=":
                            arg_name = self.take().text
                            self.take()
                            if arg_name in names:
                                raise ExpressionSyntaxError(f"duplicate named argument {arg_name!r}", self.peek())
                            named = True
                        elif named:
                            raise ExpressionSyntaxError("positional arguments cannot follow named arguments", self.peek())
                        args.append(self.parse_precedence(1))
                        names.append(arg_name)
                        if self.peek().kind != ",":
                            break
                        self.take()
                        if self.peek().kind == ")":
                            break
                self.require(")", "expected ) after arguments")
                value = Expr("call", children=(value, *args), names=tuple(names), start=token.offset)
            elif token.kind == "[":
                self.take()
                first = None if self.peek().kind in {":", "]"} else self.parse_precedence(1)
                if self.peek().kind == ":":
                    self.take()
                    second = None if self.peek().kind in {":", "]"} else self.parse_precedence(1)
                    third = None
                    if self.peek().kind == ":":
                        self.take()
                        third = None if self.peek().kind == "]" else self.parse_precedence(1)
                    self.require("]", "expected ] after slice")
                    value = Expr("slice", children=(value, first, second, third), start=token.offset)
                else:
                    if first is None:
                        raise ExpressionSyntaxError("expected an index or slice", self.peek())
                    self.require("]", "expected ] after index")
                    value = Expr("index", children=(value, first), start=token.offset)
            else:
                return value


def parse(source: str) -> Expr:
    return Parser(source).parse()
