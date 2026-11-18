#!/usr/bin/env python3
"""Link actual C++ libraries and call their exported C ABI functions."""
import os
import pathlib
import subprocess
import sys
import tempfile

compiler = pathlib.Path(sys.argv[1]).resolve()
libraries = [pathlib.Path(path).resolve() for path in sys.argv[2:]]
env = os.environ.copy()
env.update(PATH="/nonexistent", TMPDIR="/nonexistent")
source = '''extern cpp function value(int64 n) returns int64 as "cpp_value"
extern c function twice(int32 n) returns int32 as "cpp_twice"
extern cpp function half(float32 n) returns float32 as "cpp_half"
extern cpp function add(float64 n) returns float64 as "cpp_add"
extern cpp function noop() returns void as "cpp_noop"
noop()
say value(5)
say twice(6)
say half(5.0)
say add(2.0)
'''
with tempfile.TemporaryDirectory(prefix="tell-cpp-") as directory:
    directory = pathlib.Path(directory)
    program = directory / "source.tell"
    program.write_text(source)
    for i, library in enumerate(libraries):
        output = directory / f"program{i}"
        subprocess.run([compiler, program, "--link", library, "-o", output], env=env, check=True)
        run = subprocess.run([output], env=env, text=True, capture_output=True, check=True)
        assert run.stdout == "12\n12\n2.5\n3.25\n", run.stdout
    original = libraries[0].read_bytes()
    result = subprocess.run([compiler, program, "--link", libraries[0], "-o", libraries[0]], env=env, capture_output=True)
    assert result.returncode != 0 and libraries[0].read_bytes() == original
    program.write_text(source.replace('twice(6)', 'twice("bad")'))
    result = subprocess.run([compiler, program, "--link", libraries[0], "-o", directory / "bad"], env=env, text=True, capture_output=True)
    assert result.returncode != 0 and "expected 'number', got 'text'" in result.stderr, result.stderr
    program.write_text(source.replace('"cpp_value"', '"missing_symbol"'))
    result = subprocess.run([compiler, program, "--link", libraries[0], "-o", directory / "bad"], env=env, text=True, capture_output=True)
    assert result.returncode != 0 and "missing_symbol" in result.stderr, result.stderr
    program.write_text(source.replace('twice(6)', 'twice(2147483648)'))
    output = directory / "overflow"
    subprocess.run([compiler, program, "--link", libraries[0], "-o", output], env=env, check=True)
    result = subprocess.run([output], env=env, capture_output=True)
    assert result.returncode != 0
