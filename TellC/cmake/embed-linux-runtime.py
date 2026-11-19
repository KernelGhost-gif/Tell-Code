"""Embed the startup object and static libraries from Zig's Linux link."""
import os
from pathlib import Path
import re
import subprocess
import sys

zig, cache, directory = sys.argv[1:]
directory = Path(directory)
directory.mkdir(parents=True, exist_ok=True)
env = dict(os.environ, ZIG_VERBOSE_LINK="1", ZIG_GLOBAL_CACHE_DIR=cache,
           ZIG_LOCAL_CACHE_DIR=str(directory / "cache"))
probe = directory / "probe.cpp"
probe.write_text("int main() { return 0; }\n")
cpp_libraries = ("libc++abi.a", "libc++.a", "libunwind.a")
runtime_files = ("crt1.o", *cpp_libraries, "libc.a", "libzigc.a", "libcompiler_rt.a")
assembly = ['.section .rodata']
header = ['#pragma once']

for arch in ("x86_64", "aarch64"):
    link = subprocess.run(
        [zig, "c++", "-target", f"{arch}-linux-musl", "-mcpu=baseline",
         "-O2", "-static", "-no-pie", str(probe), "-o", str(directory / f"probe-{arch}")],
        env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    if link.returncode:
        sys.exit(link.stdout)
    trace = next((line for line in reversed(link.stdout.splitlines())
                  if line.startswith("ld.lld ") and " --entry _start " in line), "")
    entries = []
    for name in runtime_files:
        pattern = r"/o/([0-9a-f]+)/" + re.escape(name) + r'(?=[\s"]|$)'
        match = re.search(pattern, trace)
        if not match:
            sys.exit(f"Unable to find {arch} {name} in Zig's link trace:\n{trace}")
        source = Path(cache) / "o" / match[1] / name
        file_id = name.replace("+", "x").replace(".", "_")
        symbol = f"tell_{arch}_{file_id}"
        path = str(source).replace("\\", "\\\\").replace('"', '\\"')
        assembly += ['.balign 16', f'.globl {symbol}', f'.hidden {symbol}',
                     f'{symbol}:', f'.incbin "{path}"']
        header.append(f'extern "C" const char {symbol}[{source.stat().st_size}];')
        optional = "true" if name in cpp_libraries else "false"
        entries.append(f'    {{"{name}", {symbol}, sizeof({symbol}), {optional}}},')
    header.append(f'constexpr EmbeddedFile {arch}_runtime[] = {{')
    header.extend(entries)
    header.append('};')

assembly.append('.section .note.GNU-stack,""')
(directory / "runtime.S").write_text("\n".join(assembly) + "\n")
(directory / "linux_runtime.hpp").write_text("\n".join(header) + "\n")
