# Testing TellC

Run the full TellC test suite after configuring and building:

~~~sh
ctest --test-dir build/tellc --output-on-failure
~~~

The suite is split by responsibility:

| Test | What it protects |
| --- | --- |
| recursion_ast | Recursive function syntax and AST construction |
| foreign_ast | Foreign function declaration parsing |
| object_ast | Object declarations, access sections, and members |
| cli_smoke | Help/version output, argument validation, and factorial compilation |
| compiler_integration | Executable output, expected compile-time diagnostics, and Valgrind checks for hashmap cleanup when Valgrind is installed |
| jit_integration | In-memory execution, source diagnostics, and exit status |
| standalone_macos | Embedded linker, executable permissions, no external tools, and no temp artifacts |

The standalone_macos test is registered only when
TELL_STANDALONE_MACOS=ON. It intentionally removes common tools from PATH and
points TMPDIR at a nonexistent directory to catch accidental runtime
dependencies.

## Adding a compiler regression test

For a new backend feature, add a small source program under examples/ or
tests/fixtures/. Add a positive case to tests/compiler_integration.cmake and
assert exact stdout. For invalid programs, use expect_compile_failure with a
specific diagnostic substring. Keep parser-only behavior in the AST tests;
do not imply that parser support means the executable backend supports the
same construct.

The compiler integration script includes a hashmap lifecycle stress fixture.
On Linux CI, Valgrind checks both normal exit and an uncaught runtime-error
exit, covering table headers, bucket arrays, and remaining entry nodes.

For standalone-only behavior, extend tests/standalone_macos.py and make the
test fail if a temporary file, external process, or unexpected output appears.
