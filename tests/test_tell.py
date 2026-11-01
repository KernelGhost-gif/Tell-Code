import os
import tempfile
import unittest
from pathlib import Path

from tell import Interpreter, Parser, TellError


class TellLanguageTests(unittest.TestCase):
    def run_tell(self, source, answers=None):
        output = []
        inputs = iter(answers or [])
        interpreter = Interpreter(
            input_fn=lambda prompt: (output.append(prompt), next(inputs))[1],
            output_fn=output.append,
        )
        interpreter.run(source, "test.tell")
        return output, interpreter.global_env

    def test_variables_math_and_interpolation(self):
        output, env = self.run_tell('''
Let name be "Ada".
Let score be 20.
Set score to score plus 2.
Say "{name} has {score} points.".
''')
        self.assertEqual(output, ["Ada has 22 points."])
        self.assertEqual(env.get("score"), 22)

    def test_expression_interpolation(self):
        output, _ = self.run_tell('''
Let items be [1, 2, 3].
Say "There are {the length of items} items.".
''')
        self.assertEqual(output, ["There are 3 items."])

    def test_if_else(self):
        output, _ = self.run_tell('''
Let score be 8.
If score is greater than 5:
    Say "yes".
Otherwise:
    Say "no".
End if.
''')
        self.assertEqual(output, ["yes"])

    def test_repeat_and_for(self):
        output, _ = self.run_tell('''
Repeat 2 times:
    Say the current repetition number.
End repeat.
For each item in ["a", "b"]:
    Say item.
End loop.
''')
        self.assertEqual(output, ["1", "2", "a", "b"])

    def test_functions_and_result_phrase(self):
        output, env = self.run_tell('''
To multiply(first, second):
    Give back first times second.
End function.
Let answer be the result of multiply with 6 and 7.
Say answer.
''')
        self.assertEqual(output, ["42"])
        self.assertEqual(env.get("answer"), 42)

    def test_input_and_number_conversion(self):
        output, _ = self.run_tell('''
Ask "Age? " and save the answer as raw_age.
Let age be number(raw_age).
Say age plus 1.
''', ["12"])
        self.assertEqual(output, ["Age? ", "13"])

    def test_collections_can_be_changed(self):
        source = """
Let numbers be [1, 2].
Set the item at position 0 in numbers to 9.
Add 3 to numbers.
Let profile be {"name": "Ada"}.
Set the key "age" in profile to 36.
Say numbers[0].
Say profile["age"].
"""
        output, env = self.run_tell(source)
        self.assertEqual(output, ["9", "36"])
        self.assertEqual(env.get("numbers"), [9, 2, 3])

    def test_while_loop(self):
        source = """
Let count be 0.
While count is less than 3:
    Set count to count plus 1.
    Say count.
End while.
"""
        output, env = self.run_tell(source)
        self.assertEqual(output, ["1", "2", "3"])
        self.assertEqual(env.get("count"), 3)

    def test_try_handler(self):
        output, _ = self.run_tell('''
Try:
    Let answer be 10 divided by 0.
If something goes wrong:
    Say "caught".
End try.
''')
        self.assertEqual(output, ["caught"])

    def test_recursion_and_standard_library(self):
        output, _ = self.run_tell("""
To factorial with number:
    If number is at most 1:
        Give back 1.
    End if.
    Give back number times factorial(number minus 1).
End function.
Use the math library as maths.
Say the result of factorial with 5.
Say maths.sqrt(81).
""")
        self.assertEqual(output, ["120", "9.0"])

    def test_file_read_and_write(self):
        original = Path.cwd()
        with tempfile.TemporaryDirectory() as folder:
            os.chdir(folder)
            try:
                output, _ = self.run_tell("""
Write "Hello from Tell" to the file "note.txt".
Read the file "note.txt" into contents.
Say contents.
""")
                self.assertEqual(output, ["Hello from Tell"])
                self.assertEqual(Path("note.txt").read_text(encoding="utf-8"), "Hello from Tell")
            finally:
                os.chdir(original)

    def test_inline_comment_is_ignored(self):
        output, _ = self.run_tell('Say "hello # still text". # this is a comment\n')
        self.assertEqual(output, ["hello # still text"])

    def test_parser_requires_end_marker(self):
        with self.assertRaises(TellError):
            Parser("If true:\n  Say 1.\n", "bad.tell").parse()

    def test_example_programs_parse(self):
        root = Path(__file__).resolve().parents[1]
        for file in (root / "examples").glob("*.tell"):
            Parser(file.read_text(encoding="utf-8"), str(file)).parse()


if __name__ == "__main__":
    unittest.main()
