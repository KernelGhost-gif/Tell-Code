import unittest

from tell.expression_parser import ExpressionSyntaxError, parse


class ExpressionParserTests(unittest.TestCase):
    def test_pratt_precedence_and_right_associative_power(self):
        tree = parse("2 plus 3 times 4 to the power of 2")
        self.assertEqual(tree.kind, "binary")
        self.assertEqual(tree.value, "+")
        self.assertEqual(tree.children[1].kind, "binary")
        self.assertEqual(tree.children[1].value, "*")
        power = parse("2 ** 3 ** 2")
        self.assertEqual(power.children[1].kind, "binary")
        self.assertEqual(power.children[1].value, "**")

    def test_operator_phrases_inside_quoted_text_stay_literal(self):
        tree = parse('"plus is equal to remain words" plus "x"')
        self.assertEqual(tree.children[0].value, "plus is equal to remain words")

    def test_nested_calls_named_arguments_and_slices_parse(self):
        tree = parse('choose(values[1:4:2], label="a and b")')
        self.assertEqual(tree.kind, "call")
        self.assertEqual(tree.children[1].kind, "slice")
        self.assertEqual(tree.names, (None, "label"))

    def test_malformed_expression_reports_position(self):
        with self.assertRaisesRegex(ExpressionSyntaxError, "line 1, column 8"):
            parse("1 plus )")


if __name__ == "__main__":
    unittest.main()
