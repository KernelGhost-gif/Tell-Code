#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
from pathlib import Path

REPOSITORY = "KernelGhost-gif/Tell-Code"


def main() -> int:
    parser = argparse.ArgumentParser(description="Generate a checksum-pinned universal macOS Homebrew formula.")
    parser.add_argument("--version", required=True)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    version = args.version.removeprefix("v")
    digest = hashlib.sha256(args.archive.read_bytes()).hexdigest()
    filename = f"tell_{version}_darwin_universal.tar.gz"
    url = f"https://github.com/{REPOSITORY}/releases/download/v{version}/{filename}"
    formula = f'''class Tell < Formula
  desc "Sentence-first programming language and native runtime"
  homepage "https://github.com/{REPOSITORY}"
  url "{url}"
  sha256 "{digest}"
  version "{version}"
  license "MIT"

  def install
    bin.install "tell"
  end

  test do
    assert_match "Tell #{version}", shell_output("#{bin}/tell --version")
  end
end
'''
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(formula, encoding="utf-8")
    print(f"Wrote {args.output} (sha256 {digest})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
