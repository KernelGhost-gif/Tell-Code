# Cross-runtime conformance contract

The schema is versioned by the top-level schema_version field. Each test case provides name, filename, source_code, expected_stdout, expected_stderr, and expected_exit_code. Optional max_call_depth, max_loop_iterations, and timeout_seconds configure that test invocation.

The harness compares stdout, stderr, and exit status byte-for-byte. Each case runs in a fresh process. Source and auxiliary fixture files are written as exact UTF-8 bytes, optional stdin is passed as UTF-8 bytes, and captured output is decoded without newline normalization. Fixture paths must be relative and use `/` separators.

runtime-v1.json is the byte-exact process contract for behavior implemented by both current runtimes. It covers numeric boundaries, closure capture, recursion limits, byte-exact SyntaxError/NameError/TypeError/RuntimeError/IOError diagnostics and exit codes, null member access, stdin prompts, file round trips, relative module imports, collection mutation, indexed assignments, call statements, Tell method sentences, and built-in library imports. Keep expectations identical across runtimes; fix behavior rather than weakening fixtures when a regression appears.

Run both engines after building the native binary:

    python scripts/conformance.py --fixtures conformance/runtime-v1.json --native-bin releases/tell-linux-amd64

Run one engine while migrating:

    python scripts/conformance.py --runtime python --fixtures conformance/runtime-v1.json
    python scripts/conformance.py --runtime native --fixtures conformance/runtime-v1.json --native-bin ./releases/tell-linux-amd64

Diagnostic wording and status codes are normative. A change requires a versioned fixture/schema update.
