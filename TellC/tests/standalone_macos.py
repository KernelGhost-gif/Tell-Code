#!/usr/bin/env python3
"""Verify the bundled macOS compiler needs no toolchain or writable temp path."""
import os
import pathlib
import subprocess
import sys
import tempfile


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: standalone_macos.py TELL_COMPILER OBJECTS_EXAMPLE")

    compiler = pathlib.Path(sys.argv[1]).resolve()
    source = pathlib.Path(sys.argv[2]).resolve()

    with tempfile.TemporaryDirectory(prefix="tellc-standalone-") as directory:
        work = pathlib.Path(directory)
        output = work / "program"

        # A standalone binary must not shell out to clang/llc or use TMPDIR.
        env = os.environ.copy()
        env.update(PATH="/nonexistent", TMPDIR=str(work / "missing-temp-directory"))

        subprocess.run([compiler, source, "-o", output], env=env, check=True)
        if not output.is_file() or not os.access(output, os.X_OK):
            raise AssertionError("compiler did not produce an executable file")

        result = subprocess.run(
            [output], env=env, check=True, text=True, capture_output=True
        )
        if result.stdout != "true\nfalse\n":
            raise AssertionError(f"unexpected object example output: {result.stdout!r}")

        # Successful compilation should create only the requested executable.
        actual_files = sorted(path.name for path in work.iterdir())
        if actual_files != ["program"]:
            raise AssertionError(f"unexpected temporary artifacts: {actual_files}")

        invalid_source = work / "invalid.tell"
        invalid_source.write_text("let\n", encoding="utf-8")
        invalid_output = work / "invalid-program"
        result = subprocess.run(
            [compiler, invalid_source, "-o", invalid_output],
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        if result.returncode == 0:
            raise AssertionError("invalid Tell source unexpectedly compiled")
        if invalid_output.exists():
            raise AssertionError("failed compilation left an executable behind")

    print("TellC standalone macOS smoke tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
