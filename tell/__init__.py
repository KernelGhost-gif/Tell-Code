"""Tell: a sentence-first programming language."""

from .interpreter import Interpreter, Parser, TellError

__all__ = ["Interpreter", "Parser", "TellError"]
__version__ = "0.3.6"
