"""Runtime discovery and command construction for the Tell desktop IDE."""

from __future__ import annotations

import os
import platform
import shutil
import sys
from pathlib import Path


def repository_root() -> Path | None:
    """Return the source checkout containing this package, when installed from it."""
    for parent in Path(__file__).resolve().parents:
        if (parent / "native" / "go.mod").is_file():
            return parent
    return None


def _target_name() -> str:
    os_name = platform.system().lower()
    machine = platform.machine().lower()
    arch = {
        "amd64": "amd64",
        "x86_64": "amd64",
        "x64": "amd64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }.get(machine)
    if not arch:
        return ""
    target_os = {"windows": "windows", "linux": "linux", "darwin": "darwin"}.get(os_name)
    if not target_os:
        return ""
    suffix = ".exe" if target_os == "windows" else ""
    return f"tell-{target_os}-{arch}{suffix}"


def native_executable(root: Path | None = None) -> str | None:
    """Find a native Tell runtime, preferring the current checkout's release binary."""
    override = os.environ.get("TELL_NATIVE_EXECUTABLE")
    if override:
        path = Path(override).expanduser()
        if path.is_file():
            return str(path.resolve())

    root = root or repository_root()
    target = _target_name()
    if root and target:
        candidates = (
            root / "releases" / target,
            root / "native" / "build" / target,
            root / "build" / target,
        )
        for candidate in candidates:
            if candidate.is_file():
                return str(candidate.resolve())

    return shutil.which("tell")


def command_for(
    operation: str,
    path: str | Path | None = None,
    *,
    output: str | Path | None = None,
    root: Path | None = None,
) -> tuple[list[str] | None, str]:
    """Build a safe argv vector for a Tell command and identify its runtime."""
    executable = native_executable(root)
    if executable:
        base = [executable]
        backend = f"Native Go · {Path(executable).name}"
    else:
        base = [sys.executable, "-m", "tell"]
        backend = "Python runtime"

    if operation == "repl":
        return base + ["repl", "--quiet-repl"], backend
    if operation in {"run", "check", "test", "format"}:
        if path is None:
            return None, backend
        return base + [operation, str(path)], backend
    if operation == "build":
        if path is None or not executable:
            return None, backend
        destination = Path(output) if output is not None else Path(path).with_suffix(".tbc")
        return base + ["build", str(path), "--output", str(destination)], backend
    return None, backend


def child_environment(root: Path | None = None) -> dict[str, str]:
    """Prepare a UTF-8 child environment and make the checkout importable as fallback."""
    environment = os.environ.copy()
    environment.setdefault("PYTHONUTF8", "1")
    root = root or repository_root()
    if root:
        current = environment.get("PYTHONPATH", "")
        paths = [part for part in current.split(os.pathsep) if part]
        if str(root) not in paths:
            environment["PYTHONPATH"] = os.pathsep.join([str(root), *paths])
    return environment
