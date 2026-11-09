import unittest

from tell.diagnostics import make_diagnostic, exit_code
from tell.interpreter import Parser, TellSyntaxError, TellError


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
        self.assertEqual(diagnostic.column, 17)
        self.assertEqual(diagnostic.kind, "SyntaxError")
        self.assertEqual(exit_code(diagnostic.kind), 64)

    def test_static_type_and_name_classification(self):
        self.assertEqual(make_diagnostic("type mismatch for age").kind, "TypeError")
        self.assertEqual(make_diagnostic("I don't know what 'missing' means").kind, "NameError")

    def test_io_and_usage_status_codes(self):
        self.assertEqual(exit_code("IOError"), 74)
        self.assertEqual(exit_code("UsageError"), 64)

    def test_invalid_expressions_are_rejected_by_parser_not_runtime(self):
        with self.assertRaises(TellSyntaxError):
            Parser("Say 1 plus ).", "invalid.tell").parse()


if __name__ == "__main__":
    unittest.main()
