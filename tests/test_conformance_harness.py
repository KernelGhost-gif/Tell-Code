from __future__ import annotations

import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.conformance import execute, validate_case, write_case_files
from tell.__main__ import _configure_standard_streams


def make_case(**overrides):
    case = {
        "name": "byte-preservation",
        "filename": "nested/main.tell",
        "source_code": "Say \"hello\".\r\n",
        "expected_stdout": "",
        "expected_stderr": "",
        "expected_exit_code": 0,
    }
    case.update(overrides)
    return case


class ConformanceHarnessTests(unittest.TestCase):
    def test_fixture_files_preserve_exact_utf8_and_newline_bytes(self):
        case = make_case(files={"nested/helper.tell": "Say \"π\".\r\n"})
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            filename = write_case_files(root, case)
            self.assertEqual((root / filename).read_bytes(), case["source_code"].encode("utf-8"))
            self.assertEqual(
                (root / "nested/helper.tell").read_bytes(),
                case["files"]["nested/helper.tell"].encode("utf-8"),
            )

    def test_subprocess_output_preserves_crlf_and_lf_distinctions(self):
        case = make_case()
        command = [
            sys.executable,
            "-c",
            "import sys; sys.stdout.buffer.write(b'first\\r\\nsecond\\n')",
        ]
        result = execute(command, case)
        self.assertEqual(result.stdout, "first\r\nsecond\n")
        self.assertEqual(result.stderr, "")
        self.assertEqual(result.exit_code, 0)

    def test_stdin_is_passed_as_exact_utf8_bytes(self):
        case = make_case(stdin="Ada\r\nπ")
        command = [
            sys.executable,
            "-c",
            "import sys; sys.stdout.buffer.write(sys.stdin.buffer.read())",
        ]
        result = execute(command, case)
        self.assertEqual(result.stdout, "Ada\r\nπ")
        self.assertEqual(result.exit_code, 0)

    def test_types_option_selects_check_subcommand_before_source_filename(self):
        case = make_case(type_check=True)
        command = [
            sys.executable,
            "-c",
            "import sys; print(' '.join(sys.argv[1:]))",
            "run",
        ]
        result = execute(command, case)
        self.assertEqual(result.stdout, f"check --types nested/main.tell{os.linesep}")
        self.assertEqual(result.exit_code, 0)

    def test_rejects_windows_style_and_parent_traversal_paths(self):
        for filename in ("..\\escape.tell", "C:/outside.tell", "//server/share/file.tell", "../escape.tell"):
            with self.subTest(filename=filename):
                self.assertIsNotNone(validate_case(make_case(filename=filename)))

    def test_rejects_malformed_case_types(self):
        self.assertEqual(validate_case(make_case(expected_exit_code=True)), "expected_exit_code must be an integer")
        self.assertEqual(validate_case(make_case(files=[])), "files must be an object mapping paths to text")
        self.assertEqual(validate_case(make_case(timeout_seconds=float("inf"))), "timeout_seconds must be a positive finite number")
        self.assertEqual(validate_case(make_case(type_check="yes")), "type_check must be a boolean")

    def test_standard_streams_are_utf8_with_stable_newlines(self):
        class FakeStream:
            def __init__(self):
                self.settings = None

            def reconfigure(self, **settings):
                self.settings = settings

        stdin, stdout, stderr = FakeStream(), FakeStream(), FakeStream()
        with (
            patch("tell.__main__.sys.stdin", stdin),
            patch("tell.__main__.sys.stdout", stdout),
            patch("tell.__main__.sys.stderr", stderr),
        ):
            _configure_standard_streams()
        self.assertEqual(stdin.settings, {"encoding": "utf-8", "errors": "strict", "newline": ""})
        self.assertEqual(stdout.settings, {"encoding": "utf-8", "errors": "strict", "newline": "\n"})
        self.assertEqual(stderr.settings, {"encoding": "utf-8", "errors": "strict", "newline": "\n"})


if __name__ == "__main__":
    unittest.main()
