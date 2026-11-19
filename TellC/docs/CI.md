# TellC compiler workflow

The PR workflow runs the external LLVM toolchain on Linux, Apple silicon, and
Intel macOS. The macOS jobs use the Homebrew LLVM packages and explicitly turn
off the source-built embedded LLD mode, so they validate the actual compiler
sources on both Apple CPU architectures without rebuilding LLVM from source.

The embedded Apple-silicon LLVM + LLD build remains a weekly/manual workflow
because building that dependency stack is much slower than installing the
external toolchain. It is not required for every pull request.

To run the complete matrix manually, open **Actions → TellC LLVM compiler →
Run workflow**. The scheduled/manual matrix includes the standalone embedded
Apple-silicon build as well as the external-toolchain validation.

The Linux job also installs Valgrind and runs the hashmap lifecycle fixture
through Memcheck to catch leaked hashmap tables, bucket arrays, and entries.
