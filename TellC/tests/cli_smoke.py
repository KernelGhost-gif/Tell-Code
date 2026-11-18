#!/usr/bin/env python3
"""Portable smoke tests for TellC's command-line interface."""
import pathlib
import subprocess
import sys
import tempfile


def run(compiler: pathlib.Path, *arguments: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(compiler), *map(str, arguments)],
        text=True,
        capture_output=True,
        check=False,
    )


def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def main() -> int:
    if len(sys.argv) != 3:
        raise SystemExit("usage: cli_smoke.py TELL_COMPILER EXAMPLE_SOURCE")

    compiler = pathlib.Path(sys.argv[1]).resolve()
    source = pathlib.Path(sys.argv[2]).resolve()

    help_result = run(compiler, "--help")
    expect(help_result.returncode == 0, f"--help failed: {help_result.stderr}")
    expect("Usage:" in help_result.stdout, "--help did not print usage")
    expect("--llvm-version" in help_result.stdout, "--help omitted --llvm-version")

    version_result = run(compiler, "--version")
    expect(version_result.returncode == 0, f"--version failed: {version_result.stderr}")
    expect(version_result.stdout.startswith("TellC "), "unexpected --version output")

    llvm_result = run(compiler, "--llvm-version")
    expect(llvm_result.returncode == 0, f"--llvm-version failed: {llvm_result.stderr}")
    expect(llvm_result.stdout.startswith("LLVM "), "unexpected --llvm-version output")

    no_args = run(compiler)
    expect(no_args.returncode == 2, "calling tellc without arguments must return 2")
    expect("Usage:" in no_args.stderr, "missing-argument diagnostic omitted usage")

    unknown = run(compiler, "--not-a-real-option")
    expect(unknown.returncode == 2, "unknown option must return 2")
    expect("unknown option" in unknown.stderr, "unknown option diagnostic was not clear")

    missing_output = run(compiler, source, "-o")
    expect(missing_output.returncode == 2, "-o without a value must return 2")
    expect("requires a non-empty filename" in missing_output.stderr, "missing -o diagnostic was unclear")

    duplicate_output = run(compiler, source, "-o", "first", "-o", "second")
    expect(duplicate_output.returncode == 2, "duplicate -o must return 2")
    expect("only be specified once" in duplicate_output.stderr, "duplicate -o diagnostic was unclear")

    same_path = run(compiler, source, "-o", source)
    expect(same_path.returncode == 2, "output must not overwrite its source")
    expect("output must differ from input" in same_path.stderr, "same-path diagnostic was unclear")

    with tempfile.TemporaryDirectory(prefix="tellc-cli-") as directory:
        output = pathlib.Path(directory) / "factorial"
        compile_result = run(compiler, source, "-o", output)
        expect(
            compile_result.returncode == 0,
            f"valid source failed to compile: {compile_result.stdout}\n{compile_result.stderr}",
        )
        execution = subprocess.run(
            [str(output)], text=True, capture_output=True, check=False
        )
        expect(execution.returncode == 0, f"compiled program failed: {execution.stderr}")
        expect(execution.stdout == "120\n", f"unexpected output: {execution.stdout!r}")

    print("TellC CLI smoke tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
