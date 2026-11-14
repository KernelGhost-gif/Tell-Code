#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import math
import os
from pathlib import Path, PurePosixPath, PureWindowsPath
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from typing import Any

ROOT = Path(__file__).resolve().parents[1]


@dataclass
class Result:
    stdout: str
    stderr: str
    exit_code: int


def command_for(runtime: str, native_bin: str | None) -> list[str]:
    if runtime == "python":
        return [sys.executable, "-m", "tell", "run"]
    if not native_bin:
        raise ValueError("--native-bin is required for the native runtime")
    path = Path(native_bin)
    if not path.is_absolute():
        path = (ROOT / path).resolve()
    if not path.is_file():
        raise FileNotFoundError(f"native Tell executable not found: {path}")
    return [str(path), "run"]


def _relative_fixture_parts(name: str) -> tuple[str, ...]:
    if not isinstance(name, str) or not name or "\x00" in name or "\\" in name:
        raise ValueError(f"fixture path must be a non-empty relative path using '/': {name!r}")
    path = PurePosixPath(name)
    windows_path = PureWindowsPath(name)
    if (
        path.is_absolute()
        or windows_path.is_absolute()
        or windows_path.drive
        or ".." in path.parts
        or not path.parts
    ):
        raise ValueError(f"fixture path escapes temporary directory: {name}")
    return path.parts


def validate_case(case: Any) -> str | None:
    if not isinstance(case, dict):
        return "case must be a JSON object"
    for field in ("name", "source_code", "expected_stdout", "expected_stderr"):
        if not isinstance(case.get(field), str):
            return f"{field} must be a string"
    if not case["name"].strip():
        return "name must not be empty"
    if type(case.get("expected_exit_code")) is not int:
        return "expected_exit_code must be an integer"
    if "filename" in case:
        try:
            _relative_fixture_parts(case["filename"])
        except ValueError as exc:
            return str(exc)
    else:
        try:
            _relative_fixture_parts(f"{case['name'].replace(' ', '-')}.tell")
        except ValueError as exc:
            return str(exc)
    if "stdin" in case and not isinstance(case["stdin"], str):
        return "stdin must be a string"
    if "type_check" in case and not isinstance(case["type_check"], bool):
        return "type_check must be a boolean"
    fixture_files = case.get("files", {})
    if not isinstance(fixture_files, dict):
        return "files must be an object mapping paths to text"
    for name, content in fixture_files.items():
        try:
            _relative_fixture_parts(name)
        except ValueError as exc:
            return str(exc)
        if not isinstance(content, str):
            return f"fixture file {name!r} must contain a string"
    for field in ("max_call_depth", "max_loop_iterations"):
        if field in case and (type(case[field]) is not int or case[field] < 1):
            return f"{field} must be a positive integer"
    timeout = case.get("timeout_seconds", 10)
    if isinstance(timeout, bool) or not isinstance(timeout, (int, float)):
        return "timeout_seconds must be a positive number"
    if not math.isfinite(timeout) or timeout <= 0:
        return "timeout_seconds must be a positive finite number"
    return None


def write_case_files(workdir: Path, case: dict[str, Any]) -> str:
    filename = case.get("filename") or f"{case['name'].replace(' ', '-')}.tell"

    def write_relative(name: str, content: str) -> None:
        parts = _relative_fixture_parts(name)
        if not isinstance(content, str):
            raise ValueError(f"fixture contents for {name!r} must be a string")
        target = workdir.joinpath(*parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content.encode("utf-8"))

    write_relative(filename, case["source_code"])
    for name, content in case.get("files", {}).items():
        write_relative(name, content)
    return filename


def execute(command: list[str], case: dict[str, Any]) -> Result:
    validation_error = validate_case(case)
    if validation_error:
        raise ValueError(validation_error)
    with tempfile.TemporaryDirectory(prefix="tell-conformance-") as temp:
        workdir = Path(temp)
        filename = write_case_files(workdir, case)
        args = list(command)
        for key, flag in (("max_call_depth", "--max-call-depth"),
                          ("max_loop_iterations", "--max-loop-iterations")):
            if key in case:
                args.extend([flag, str(case[key])])
        if case.get("type_check", False):
            args.append("--types")
        args.append(filename)
        env = os.environ.copy()
        env["PYTHONPATH"] = str(ROOT) + os.pathsep + env.get("PYTHONPATH", "")
        env["PYTHONIOENCODING"] = "utf-8"
        stdin = case.get("stdin", "").encode("utf-8")
        try:
            process = subprocess.run(
                args, cwd=workdir, input=stdin,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                timeout=float(case.get("timeout_seconds", 10)), env=env, check=False,
            )
        except subprocess.TimeoutExpired:
            return Result("", "conformance timeout", 124)
        return Result(
            process.stdout.decode("utf-8"),
            process.stderr.decode("utf-8"),
            process.returncode,
        )


def compare(case: dict[str, Any], result: Result) -> list[str]:
    failures: list[str] = []
    for field, expected, actual in (
        ("stdout", case["expected_stdout"], result.stdout),
        ("stderr", case["expected_stderr"], result.stderr),
        ("exit_code", case["expected_exit_code"], result.exit_code),
    ):
        if expected != actual:
            failures.append(f"{field}: expected {expected!r}, got {actual!r}")
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(description="Run Tell conformance cases against Python and/or native runtimes.")
    parser.add_argument("--fixtures", default="conformance/runtime-v1.json")
    parser.add_argument("--runtime", choices=("both", "python", "native"), default="both")
    parser.add_argument("--native-bin", default=os.environ.get("TELL_NATIVE_BIN"))
    args = parser.parse_args()

    fixture_path = Path(args.fixtures)
    if not fixture_path.is_absolute():
        fixture_path = ROOT / fixture_path
    try:
        suite = json.loads(fixture_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"conformance: cannot load fixture: {exc}", file=sys.stderr)
        return 2
    if (
        not isinstance(suite, dict)
        or type(suite.get("schema_version")) is not int
        or suite.get("schema_version") != 1
        or not isinstance(suite.get("tests"), list)
    ):
        print("conformance: unsupported or malformed fixture schema", file=sys.stderr)
        return 2

    runtimes = ("python", "native") if args.runtime == "both" else (args.runtime,)
    commands: dict[str, list[str]] = {}
    try:
        for runtime in runtimes:
            commands[runtime] = command_for(runtime, args.native_bin)
    except (FileNotFoundError, ValueError) as exc:
        print(f"conformance: {exc}", file=sys.stderr)
        return 2

    passed = failed = 0
    for case in suite["tests"]:
        validation_error = validate_case(case)
        if validation_error:
            case_name = case.get("name", "<unnamed>") if isinstance(case, dict) else "<unnamed>"
            print(f"FAIL malformed case: {case_name}: {validation_error}", file=sys.stderr)
            failed += 1
            continue
        for runtime in runtimes:
            try:
                result = execute(commands[runtime], case)
                errors = compare(case, result)
            except (OSError, ValueError, KeyError) as exc:
                errors = [str(exc)]
            if errors:
                failed += 1
                print(f"FAIL [{runtime}] {case['name']}")
                for error in errors:
                    print(f"  {error}")
            else:
                passed += 1
                print(f"PASS [{runtime}] {case['name']}")
    print(f"Conformance: {passed} passed, {failed} failed.")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
