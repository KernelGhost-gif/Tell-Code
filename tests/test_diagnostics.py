import unittest

from tell.diagnostics import make_diagnostic, exit_code
from tell.interpreter import Interpreter, Parser, TellSyntaxError, TellError, TellLocatedError


class DiagnosticContractTests(unittest.TestCase):
    def test_runtime_location_format_and_status(self):
        diagnostic = make_diagnostic(
            TellError("sample.tell:3:7: division by zero"),
            "fallback.tell",
            "RuntimeError",
        )
        self.assertEqual(
            str(diagnostic),
            '[RuntimeError] File "sample.tell", line 3, col 7: division by zero',
        )
        self.assertEqual(exit_code(diagnostic.kind), 70)

    def test_syntax_errors_have_global_source_coordinates(self):
        with self.assertRaises(TellSyntaxError) as caught:
            Parser("If true:\n    Say 1 plus ).\nEnd if.", "syntax.tell").parse()
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "SyntaxError")
        self.assertEqual(diagnostic.filename, "syntax.tell")
        self.assertEqual(diagnostic.line, 2)
        self.assertEqual(diagnostic.column, 16)
        self.assertEqual(diagnostic.kind, "SyntaxError")
        self.assertEqual(exit_code(diagnostic.kind), 64)


    def test_runtime_expression_diagnostics_point_to_operator(self):
        with self.assertRaises(TellLocatedError) as caught:
            Interpreter().run("Say 10 divided by 0.", "division-zero.tell")
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(diagnostic.filename, "division-zero.tell")
        self.assertEqual(diagnostic.line, 1)
        self.assertEqual(diagnostic.column, 8)
        self.assertEqual(diagnostic.message, "division by zero")

    def test_nothing_member_diagnostic_points_to_access(self):
        with self.assertRaises(TellLocatedError) as caught:
            Interpreter().run("Let value be nothing.\nSay value.name.", "null-member.tell")
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(diagnostic.filename, "null-member.tell")
        self.assertEqual(diagnostic.line, 2)
        self.assertEqual(diagnostic.column, 10)
        self.assertEqual(diagnostic.message, "cannot access member on nothing")

    def test_recursion_limit_diagnostic_points_to_call_site(self):
        source = (
            "To recurse with n:\n"
            "    Give back recurse(n plus 1).\n"
            "End function.\n"
            "Say recurse(0)."
        )
        interpreter = Interpreter(max_call_depth=8)
        with self.assertRaises(TellLocatedError) as caught:
            interpreter.run(source, "recursion-limit.tell")
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(diagnostic.filename, "recursion-limit.tell")
        self.assertEqual(diagnostic.line, 2)
        self.assertEqual(diagnostic.column, 22)
        self.assertEqual(diagnostic.message, "maximum function call depth exceeded")


    def test_control_flow_expressions_are_cached_and_source_located(self):
        source = "If 1 divided by 0:\n    Say \"unreachable\".\nEnd if."
        parsed = Parser(source, "control-flow.tell").parse()
        expression = parsed[0].data["condition_ast"]
        self.assertEqual(expression.kind, "binary")
        self.assertEqual(expression.column, 3)
        with self.assertRaises(TellLocatedError) as caught:
            Interpreter().run(source, "control-flow.tell")
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(diagnostic.line, 1)
        self.assertEqual(diagnostic.column, 6)
        self.assertEqual(diagnostic.message, "division by zero")

    def test_static_type_and_name_classification(self):
        self.assertEqual(make_diagnostic("type mismatch for age").kind, "TypeError")
        self.assertEqual(make_diagnostic("I don't know what 'missing' means").kind, "NameError")

    def test_invalid_expression_statement_has_matching_syntax_diagnostic(self):
        with self.assertRaises(TellSyntaxError) as caught:
            Parser("Say 1 plus ).", "syntax-error.tell").parse()
        diagnostic = make_diagnostic(caught.exception, "fallback.tell", "SyntaxError")
        self.assertEqual(diagnostic.filename, "syntax-error.tell")
        self.assertEqual(diagnostic.line, 1)
        self.assertEqual(diagnostic.column, 12)
        self.assertEqual(diagnostic.message, 'expected a value, found ")"')
        self.assertEqual(diagnostic.kind, "SyntaxError")
        self.assertEqual(exit_code(diagnostic.kind), 64)

    def test_name_and_type_messages_match_cross_runtime_contract(self):
        with self.assertRaises(TellError) as missing:
            Interpreter().run("Say missing_name.", "name-error.tell")
        name = make_diagnostic(missing.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(name.kind, "NameError")
        self.assertEqual(name.message, 'I don\'t know what "missing_name" means. Did you define it first?')

        with self.assertRaises(TellError) as mismatch:
            Interpreter().run('Let age as number be "old".', "type-error.tell")
        type_error = make_diagnostic(mismatch.exception, "fallback.tell", "RuntimeError")
        self.assertEqual(type_error.kind, "TypeError")
        self.assertEqual(type_error.message, 'type mismatch for "age": expected number, got text')

    def test_io_and_usage_status_codes(self):
        self.assertEqual(exit_code("IOError"), 74)
        self.assertEqual(exit_code("UsageError"), 64)

    def test_invalid_expressions_are_rejected_by_parser_not_runtime(self):
        with self.assertRaises(TellSyntaxError):
            Parser("Say 1 plus ).", "invalid.tell").parse()


if __name__ == "__main__":
    unittest.main()
