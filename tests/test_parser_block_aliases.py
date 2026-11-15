from __future__ import annotations

import unittest

from tell.interpreter import Interpreter


class ParserBlockAliasTests(unittest.TestCase):
    def test_else_if_else_and_undotted_block_terminators(self):
        source = """\
Let score be 15.
If score is less than 10:
    Say "low".
Else if score is less than 20:
    Say "middle".
Else:
    Say "high".
End if
Let count be 0.
While count is less than 1:
    Set count to count plus 1.
End while
Repeat 2 times:
    Say count.
End repeat
For each item in [1, 2]:
    Say item.
End loop
To identity with value:
    Give back value.
End function
Say identity(count).
Try:
    Say "try".
End try
Create a class called Box:
    When creating a Box with value:
        Remember value as this.value.
    End constructor
End class
Let box be new Box with 7.
Say box.value.
"""
        output = []
        Interpreter(output_fn=output.append).run(source, "block-aliases.tell")
        self.assertEqual(
            output,
            ["middle", "1", "1", "1", "2", "1", "try", "7"],
        )


if __name__ == "__main__":
    unittest.main()
