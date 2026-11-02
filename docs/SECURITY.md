# Security

Tell is an interpreter, not a sandbox. Only run programs you trust.

## Execution effects

Tell programs can read, write, and append files relative to the process working directory. The runtimes expose allowlisted standard-library modules, but ordinary execution has no filesystem isolation, network isolation, privilege separation, or operating-system sandbox. Python attribute access is restricted for underscore-prefixed names and Tell class instances hide their host storage, but this is not a security boundary against malicious source.

Local `.tell` imports execute source files with the same process privileges as the importing program. Module paths are resolved relative to the importing file, and imports are cached for the interpreter session; this does not provide dependency verification or isolation.

## Resource controls

Both runtimes default to a maximum active Tell function-call depth of 64 and a maximum of 1,000,000 iterations per loop/range. Use `--max-call-depth` and `--max-loop-iterations` to set positive per-run values, or the corresponding Python/Go runtime API options. These controls do not cap memory, source size, time spent in a single operation, or work performed inside a native/library call. Raising the limits increases resource exposure.

## Package installation

Tell does not yet have a package manager. Do not install package-like content through ad hoc scripts or assume dependency code is isolated.

## Reporting issues

Until a security contact and response process are established, report suspected vulnerabilities privately to the repository owner rather than publishing exploit details in an issue.
