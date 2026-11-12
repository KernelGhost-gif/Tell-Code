import unittest

from tell.expression_parser import ExpressionSyntaxError, parse


class ExpressionParserTests(unittest.TestCase):
    def test_tuple_literals_distinguish_grouping_and_empty_tuple(self):
        self.assertEqual(parse("(1)").kind, "literal")
        self.assertEqual(parse("()").kind, "tuple")
        self.assertEqual(parse("(1,)").kind, "tuple")
        self.assertEqual(parse("(1, 2)").kind, "tuple")

    def test_set_literals_remain_distinct_from_dictionary_literals(self):
        self.assertEqual(parse("{}").kind, "dict")
        self.assertEqual(parse('{"a": 1, "b": 2}').kind, "dict")
        self.assertEqual(parse("{1, 2, 1}").kind, "set")

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


    def test_conditional_expression_ast(self):
        tree = parse('"yes" if 2 is greater than 1 else "no"')
        self.assertEqual(tree.kind, "conditional")
        self.assertEqual(tree.children[0].value, "yes")
        self.assertEqual(tree.children[1].kind, "binary")
        self.assertEqual(tree.children[2].value, "no")

    def test_conditional_expression_is_right_associative(self):
        tree = parse('"a" if false else "b" if true else "c"')
        self.assertEqual(tree.kind, "conditional")
        self.assertEqual(tree.children[2].kind, "conditional")


if __name__ == "__main__":
    unittest.main()
