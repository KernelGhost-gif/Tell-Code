import unittest

from tell.semantic import analyze, SemanticError


class StaticAnalysisTests(unittest.TestCase):
    def test_rejects_known_type_mismatch(self):
        source = 'Let age as number be "old".'
        with self.assertRaisesRegex(SemanticError, "type mismatch"):
            analyze(source, "types.tell")

    def test_rejects_unknown_annotation(self):
        with self.assertRaisesRegex(SemanticError, "unknown type annotation"):
            analyze("Let score as imaginary_type be 10.", "types.tell")

    def test_accepts_known_compatible_annotations(self):
        analyze(
            "Let count as number be 2.\nSet count to count plus 3.\n"
            "Let label as text be \"ready\".\nSay label.",
            "types.tell",
        )

    def test_class_subtyping_is_permitted(self):
        analyze(
            "Create a class called Parent:\nEnd class.\n"
            "Create a class called Child based on Parent:\nEnd class.\n"
            "Let value as Parent be new Child.",
            "classes.tell",
        )

    def test_rejects_statistically_known_class_mismatch(self):
        source = (
            "Create a class called Cat:\nEnd class.\n"
            "Create a class called Dog:\nEnd class.\n"
            "Let pet as Cat be new Dog."
        )
        with self.assertRaisesRegex(SemanticError, "type mismatch"):
            analyze(source, "classes.tell")

    def test_infers_builtin_return_types(self):
        analyze("Let count as number be len([1, 2]).", "builtins.tell")
        with self.assertRaisesRegex(SemanticError, "type mismatch"):
            analyze("Let count as text be len([1, 2]).", "builtins.tell")

    def test_rejects_known_incompatible_operator_types(self):
        with self.assertRaisesRegex(SemanticError, "requires numeric operands"):
            analyze('Let value as number be "not a number" minus 1.', "operators.tell")

    def test_dynamic_values_remain_permitted(self):
        analyze("Let value be external_value.\nSet value to another_value.", "dynamic.tell")


if __name__ == "__main__":
    unittest.main()
