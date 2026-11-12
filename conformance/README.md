# Cross-runtime conformance contract

The schema is versioned by the top-level schema_version field. Each test case provides name, filename, source_code, expected_stdout, expected_stderr, and expected_exit_code. Optional max_call_depth, max_loop_iterations, and timeout_seconds configure that test invocation.

The harness compares stdout, stderr, and exit status byte-for-byte. Cases are run in fresh processes with UTF-8 source passed on stdin.

runtime-v1.json is the target behavioral contract, not a claim that the current 0.3 runtimes already pass every case. It includes numeric boundaries, closure capture, recursion limits, division by zero, and null member access. Until diagnostic normalization and semantic parity are complete, treat failures as migration work rather than weakening expected values.

Run both engines after building the native binary:

    python scripts/conformance.py --fixtures conformance/runtime-v1.json --native-bin releases/tell-linux-amd64

Run one engine while migrating:

    python scripts/conformance.py --runtime python --fixtures conformance/runtime-v1.json
    python scripts/conformance.py --runtime native --fixtures conformance/runtime-v1.json --native-bin ./releases/tell-linux-amd64

Diagnostic wording and status codes are normative. A change requires a versioned fixture/schema update.
