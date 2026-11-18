# LLVM compiler workflow

TellC's full LLVM/LLD jobs are resource-intensive. They run on a weekly
schedule and through manual workflow dispatch, not on ordinary pull requests.
Fast Go/runtime checks remain part of normal PR validation.

To run the full matrix manually, open **Actions → TellC LLVM compiler →
Run workflow**. The workflow validates Linux with the external LLVM toolchain,
Apple silicon with embedded LLVM + LLD, and Intel macOS with the external
toolchain.
