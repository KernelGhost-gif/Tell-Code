#!/usr/bin/env python3
"""Exercise the distributed compiler with no toolchain or writable temp directory."""
import os
import pathlib
import subprocess
import sys
import tempfile

compiler = pathlib.Path(sys.argv[1]).resolve()
source = pathlib.Path(sys.argv[2]).resolve()
with tempfile.TemporaryDirectory(prefix="tellc-check-") as directory:
    directory = pathlib.Path(directory)
    output = directory / "program"
    env = os.environ.copy()
    env.update(PATH="/nonexistent", TMPDIR=str(directory / "missing"))
    subprocess.run([compiler, source, "-o", output], env=env, check=True)
    result = subprocess.run([output], env=env, check=True, text=True, capture_output=True)
    assert result.stdout == "true\nfalse\n", result.stdout
    assert sorted(p.name for p in directory.iterdir()) == ["program"]
    bad = directory / "invalid.tell"
    bad.write_text("let\n")
    result = subprocess.run([compiler, bad, "-o", directory / "bad"], env=env, capture_output=True)
    assert result.returncode != 0
    assert not (directory / "bad").exists()
