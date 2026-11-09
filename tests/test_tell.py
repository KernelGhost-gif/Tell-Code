import os
import json
import io
import shutil
import tempfile
import unittest
from pathlib import Path

from tell import Interpreter, Parser, TellError
from tell.__main__ import repl, run_tests
from tell.interpreter import format_source


class TellLanguageTests(unittest.TestCase):
    def test_parser_builds_expression_asts_for_common_statements(self):
        nodes = Parser('Let answer be 1 plus 2.\nSet answer to answer plus 1.\nSay answer.').parse()
        self.assertEqual([node.data["simple"]["kind"] for node in nodes], ["let", "set", "print"])
        self.assertEqual(nodes[0].data["simple"]["expression"].kind, "binary")
        self.assertEqual(nodes[1].data["simple"]["names"], ["answer"])
        constructed = Parser("Let box be a new Box.").parse()[0]
        self.assertEqual(constructed.data["simple"]["expression"].kind, "call")
        told = Parser('Tell box to open with "wide".').parse()[0]
        self.assertEqual(told.data["simple"]["expression"].kind, "call")
        member_assignment = Parser('Set box.name to "Ada".').parse()[0]
        self.assertEqual(member_assignment.data["simple"]["kind"], "set_member")
        self.assertEqual(member_assignment.data["simple"]["target"].kind, "member")

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

    def test_tell_expression_forms_use_only_the_tell_ast(self):
        output = []
        Interpreter(output_fn=output.append).run(
            'Say (1, 2).\nSay {1, 2}.\nSay "yes" if true else "no".'
        )
        self.assertEqual(output, ["(1, 2)", "{1, 2}", "yes"])

    def test_python_only_comprehensions_do_not_fall_back_to_host_ast(self):
        with self.assertRaises(TellSyntaxError):
            Parser("Say [item for item in [1, 2]].", "no-host-ast.tell").parse()

    def test_python_module_interop_uses_imported_modules_and_named_arguments(self):
        output, _ = self.run_tell('''
Use the Python library called statistics as stats.
Say stats.mean([1, 2, 3]).
Say stats.fmean(data=[4, 8]).
''')
        self.assertEqual(output, ["2", "6"])
 
    @unittest.skipUnless(shutil.which("node"), "Node.js is optional for the Python runtime")
    def test_javascript_module_interop_uses_node_modules(self):
        output, _ = self.run_tell('''
Use the JavaScript library called node:path as paths.
Say paths.basename("/projects/tell/main.js").
Say paths.extname("/projects/tell/main.js").
''')
        self.assertEqual(output, ["main.js", ".js"])

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

    def test_multiple_assignment_validates_before_mutating(self):
        interpreter = Interpreter()
        interpreter.global_env.set("first", 1)
        interpreter.global_env.set("second", "before")
        interpreter.global_env.types["second"] = "text"
        with self.assertRaisesRegex(TellError, "type mismatch"):
            interpreter.run("Set first, second to [2, 3].")
        self.assertEqual(interpreter.global_env.get("first"), 1)
        self.assertEqual(interpreter.global_env.get("second"), "before")

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
        self.assertEqual(output, ["120", "9"])

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

    def test_module_example_executes(self):
        root = Path(__file__).resolve().parents[1]
        filename = root / "examples" / "modules.tell"
        output = io.StringIO()
        interpreter = Interpreter(output_fn=lambda text: output.write(text + "\n"))
        interpreter.run(filename.read_text(encoding="utf-8"), str(filename))
        self.assertEqual(output.getvalue(), "42\n")

    def test_check_that_assertion_passes_and_fails(self):
        Interpreter().run("Check that 2 plus 2 is equal to 4.")
        with self.assertRaisesRegex(TellError, "Check failed"):
            Interpreter().run("Check that 2 plus 2 is equal to 5.", "assertion.tell")

    def test_tell_test_discovers_and_runs_test_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "one_test.tell").write_text("Check that true.\n", encoding="utf-8")
            (root / "ignored.tell").write_text("Check that false.\n", encoding="utf-8")
            output, errors = io.StringIO(), io.StringIO()
            status = run_tests(str(root), output_stream=output, error_stream=errors)
            self.assertEqual(status, 0)
            self.assertIn("1 passed, 0 failed", output.getvalue())
            self.assertEqual(errors.getvalue(), "")

            (root / "failure_test.tell").write_text("Check that false.\n", encoding="utf-8")
            output, errors = io.StringIO(), io.StringIO()
            status = run_tests(str(root), output_stream=output, error_stream=errors)
            self.assertEqual(status, 1)
            self.assertIn("1 failed", output.getvalue())
            self.assertIn("Check failed", errors.getvalue())

    def test_shared_conformance_corpus(self):
        root = Path(__file__).resolve().parents[1]
        cases = json.loads((root / "conformance" / "core.json").read_text(encoding="utf-8"))
        for case in cases:
            with self.subTest(case=case["name"]):
                output = io.StringIO()
                interpreter = Interpreter(output_fn=lambda text: output.write(text + "\n"))
                filename = str(root / "conformance" / case.get("filename", f"{case['name']}.tell"))
                if case.get("error"):
                    with self.assertRaises(TellError) as caught:
                        interpreter.run(case["source"], filename)
                    self.assertIn(case["error"].casefold(), str(caught.exception).casefold())
                    continue
                interpreter.run(case["source"], filename)
                self.assertEqual(output.getvalue(), case["output"])

    def test_named_arguments_reject_duplicates_and_unknown_names(self):
        source = '''To add with left and right:
    Give back left plus right.
End function.
Say add(1, left=2).'''
        with self.assertRaisesRegex(TellError, "multiple values"):
            Interpreter().run(source)

        source = '''To add with left and right:
    Give back left plus right.
End function.
Say add(other=2).'''
        with self.assertRaisesRegex(TellError, "no parameter named"):
            Interpreter().run(source)

    def test_runtime_errors_include_source_columns(self):
        source = "If true:\n    Say 1 divided by 0.\nEnd if."
        with self.assertRaisesRegex(TellError, r"sample\.tell:2:5:"):
            Interpreter().run(source, "sample.tell")

    def test_circular_module_import_is_rejected(self):
        root = Path(__file__).resolve().parents[1]
        source_path = root / "conformance" / "modules" / "entry.tell"
        with self.assertRaisesRegex(TellError, "Circular module import"):
            Interpreter().run('Use the module "cycle_a.tell" as a.', str(source_path))

    def test_repl_persists_values_and_accepts_multiline_functions(self):
        source = '''Let name be "Ada".
To greet with person:
    Give back "Hello, {person}!".
End function.
Say greet(name).
:exit
'''
        output = io.StringIO()
        self.assertEqual(repl(io.StringIO(source), output), 0)
        self.assertIn("Hello, Ada!", output.getvalue())

    def test_repl_echoes_bare_expression_results_like_idle(self):
        output = io.StringIO()
        repl(io.StringIO("2 plus 2\n_\n:exit\n"), output)
        self.assertIn("4\n4\n", output.getvalue())

    def test_formatter_normalizes_indentation_and_preserves_program(self):
        source = '# top\nIf true:\nSay "keep  ".\n# inside\n Otherwise:\n   Say "skip". # trailing\nEnd if.\n'
        expected = '# top\nIf true:\n    Say "keep  ".\n    # inside\nOtherwise:\n    Say "skip". # trailing\nEnd if.\n'
        formatted = format_source(source, "format.tell")
        self.assertEqual(formatted, expected)
        self.assertEqual(format_source(formatted, "format.tell"), formatted)
        before, _ = self.run_tell(source)
        after, _ = self.run_tell(formatted)
        self.assertEqual(before, after)

    def test_recursion_limit_is_a_tell_error(self):
        source = '''To recurse with value:
    Give back recurse(value plus 1).
End function.
Say recurse(0).
'''
        with self.assertRaisesRegex(TellError, r"maximum function call depth \(64\) exceeded"):
            Interpreter().run(source, "recursion.tell")

    def test_execution_limits_can_be_configured(self):
        with self.assertRaisesRegex(TellError, "repeat count is too large"):
            Interpreter(max_loop_iterations=2).run("Repeat 3 times:\n    Say 1.\nEnd repeat.")
        source = "To repeat_call with number:\n    Give back repeat_call(number plus 1).\nEnd function.\nSay repeat_call(0)."
        with self.assertRaisesRegex(TellError, r"maximum function call depth \(3\) exceeded"):
            Interpreter(max_call_depth=3).run(source)
        with self.assertRaisesRegex(TellError, "For-each loop exceeded"):
            Interpreter(max_loop_iterations=2).run("For each item in [1, 2, 3]:\n    Say item.\nEnd loop.")

    def test_class_runtime_storage_is_not_exposed_as_language_members(self):
        source = '''Create a class called Box:
    When creating a Box with value:
        Remember value as this.value.
    End constructor.
End class.
Let box be a new Box with 3.
Say box.fields.
'''
        with self.assertRaisesRegex(TellError, "has no member 'fields'"):
            Interpreter().run(source, "private-fields.tell")


if __name__ == "__main__":
    unittest.main()
