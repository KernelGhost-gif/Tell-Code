#!/usr/bin/env python3
"""Check a cross-build offline, or smoke-test it on native Linux."""
import argparse
import os
from pathlib import Path
import platform
import struct
import subprocess
import tempfile

MACHINES = {"x86_64": 62, "aarch64": 183}


def check_static_elf(binary, arch):
    with binary.open("rb") as file:
        header = file.read(64)
        if header[:6] != b"\x7fELF\x02\x01" or len(header) != 64:
            raise ValueError(f"{binary}: expected a little-endian 64-bit ELF")
        kind, machine = struct.unpack_from("<HH", header, 16)
        if kind != 2 or machine != MACHINES[arch]:
            raise ValueError(f"{binary}: expected a Linux {arch} executable")
        offset, = struct.unpack_from("<Q", header, 32)
        entry_size, count = struct.unpack_from("<HH", header, 54)
        if entry_size != 56 or count == 0:
            raise ValueError(f"{binary}: expected ELF program headers")
        for index in range(count):
            file.seek(offset + entry_size * index)
            segment, = struct.unpack("<I", file.read(4))
            if segment in (2, 3):  # PT_DYNAMIC and PT_INTERP
                raise ValueError(f"{binary}: found a dynamic segment or interpreter")
    print(f"Verified static {arch} ELF: {binary}")


def smoke_test(compiler, arch):
    if platform.system() != "Linux" or platform.machine() != arch:
        raise ValueError(f"Run this smoke test on native {arch} Linux")
    source = Path(__file__).resolve().parents[1] / "examples" / "factorial.tell"
    env = dict(os.environ, PATH="/nonexistent", TMPDIR="/nonexistent")
    with tempfile.TemporaryDirectory(prefix="tellc-linux-") as directory:
        native = Path(directory) / "factorial"
        subprocess.run([compiler, source, "-o", native], env=env, check=True)
        check_static_elf(native, arch)
        result = subprocess.run([native], env=env, check=True, capture_output=True)
        if result.stdout != b"120\n":
            raise ValueError(f"Unexpected factorial result: {result.stdout!r}")
        other = next(candidate for candidate in MACHINES if candidate != arch)
        cross = Path(directory) / "factorial-cross"
        subprocess.run([compiler, source, "--target", other, "-o", cross], env=env, check=True)
        check_static_elf(cross, other)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("compiler", type=Path)
    parser.add_argument("arch", choices=MACHINES)
    parser.add_argument("--elf-only", action="store_true")
    args = parser.parse_args()
    try:
        compiler = args.compiler.resolve()
        check_static_elf(compiler, args.arch)
        if not args.elf_only:
            smoke_test(compiler, args.arch)
    except (OSError, ValueError, struct.error, subprocess.CalledProcessError) as error:
        parser.exit(1, f"{error}\n")
