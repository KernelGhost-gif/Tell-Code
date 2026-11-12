#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
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


def write_case_files(workdir: Path, case: dict[str, Any]) -> str:
    filename = case.get("filename") or f"{case['name'].replace(' ', '-')}.tell"
    def write_relative(name: str, content: str) -> None:
        path = Path(name)
        if path.is_absolute() or ".." in path.parts:
            raise ValueError(f"fixture path escapes temporary directory: {name}")
        target = workdir / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8")
    write_relative(filename, case["source_code"])
    for name, content in case.get("files", {}).items():
        write_relative(name, content)
    return filename


def execute(command: list[str], case: dict[str, Any]) -> Result:
    with tempfile.TemporaryDirectory(prefix="tell-conformance-") as temp:
        workdir = Path(temp)
        filename = write_case_files(workdir, case)
        args = list(command)
        for key, flag in (("max_call_depth", "--max-call-depth"),
                          ("max_loop_iterations", "--max-loop-iterations")):
            if key in case:
                args.extend([flag, str(case[key])])
        args.append(filename)
        env = os.environ.copy()
        env["PYTHONPATH"] = str(ROOT) + os.pathsep + env.get("PYTHONPATH", "")
        try:
            process = subprocess.run(
                args, cwd=workdir, input=case.get("stdin", ""), text=True, encoding="utf-8",
                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                timeout=float(case.get("timeout_seconds", 10)), env=env, check=False,
            )
        except subprocess.TimeoutExpired:
            return Result("", "conformance timeout", 124)
        return Result(process.stdout, process.stderr, process.returncode)


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
    if suite.get("schema_version") != 1 or not isinstance(suite.get("tests"), list):
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
    required = ("name", "source_code", "expected_stdout", "expected_stderr", "expected_exit_code")
    for case in suite["tests"]:
        if not all(field in case for field in required):
            print(f"FAIL malformed case: {case.get('name', '<unnamed>')}", file=sys.stderr)
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
