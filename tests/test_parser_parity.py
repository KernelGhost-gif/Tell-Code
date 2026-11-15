import unittest

from tell.interpreter import Parser, TellSyntaxError


class ParserParityTests(unittest.TestCase):
    def test_optional_then_is_accepted_for_if_and_otherwise_if(self):
        source = (
            'If true, then:\n'
            '    Say "primary".\n'
            'Otherwise if false, then:\n'
            '    Say "secondary".\n'
            'Otherwise:\n'
            '    Say "fallback".\n'
            'End if.\n'
        )
        nodes = Parser(source, "optional-then.tell").parse()
        self.assertEqual(nodes[0].data["condition"], "true")
        self.assertEqual(nodes[0].data["otherwise"][0].data["condition"], "false")

    def test_malformed_let_and_set_expressions_are_parse_errors(self):
        for source in ("Let value be 1 plus ).\n", "Set value to (2 plus ).\n"):
            with self.subTest(source=source):
                with self.assertRaises(TellSyntaxError):
                    Parser(source, "malformed.tell").parse()


if __name__ == "__main__":
    unittest.main()
