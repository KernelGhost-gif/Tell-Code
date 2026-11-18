# TellC architecture

TellC is intentionally separate from the Go and Python Tell runtimes. It is a
strongly typed compiler with a narrower executable subset; syntax accepted by
the parser is not automatically guaranteed to have backend support.

## Compilation pipeline

1. The source file is read as UTF-8 bytes by the command-line driver.
2. tell::parse in src/parser.cpp produces the Program and expression/statement
   AST declared in include/tell/parser.hpp.
3. The LLVM generator in src/codegen.cpp validates supported operations and
   declared types while building an LLVM module.
4. The selected platform path turns that module into a native executable:
   - Apple silicon standalone mode emits an AArch64 Mach-O object in memory
     and invokes the embedded LLD Mach-O driver with in-memory inputs.
   - Linux and Intel macOS write temporary LLVM IR and object files, invoke
     the matching llc, and use Clang for the final link.

## Platform boundaries

The standalone linker is currently limited to Apple silicon and macOS 11+.
Its libSystem text stub is deliberately minimal and mirrors the symbols the
current generated code imports. New runtime calls must update that stub and
the no-toolchain integration test together.

The external path asks Clang for its effective target triple before invoking
llc. This keeps the generated object aligned with the active compiler target
and macOS deployment settings. Temporary artifacts are created beneath the
platform's configured temporary directory and are removed by RAII objects.

## Language/backend boundary

The parser recognizes more syntax than the executable backend implements.
The backend must reject unsupported statements and expressions with a
source-located diagnostic instead of generating partial or unsafe code.
Function and method parameters, initializers, and return values are checked
against their declared types. Value-returning functions must return on every
reachable path.

When implementing a feature, update all relevant layers: parser and AST tests
when syntax changes, code generation and integration fixtures when semantics
change, and both the external and standalone compiler lanes when platform
behavior is affected.
