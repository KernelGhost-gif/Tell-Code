# Standard library

Tell exposes a small explicit set of standard-library modules. Each runtime owns its own adapters; only the documented names below are intended to be portable. Library objects expose public operations only and are not a route to the host runtime's other APIs.

## CSV

Import CSV with `Use the csv library.` or an alias:

```tell
Use the csv library as tables.
Let rows be tables.parse("name,score\nAda,42\n").
Say rows[1][0].
Say tables.stringify(rows).
```

`csv.parse(text)` takes exactly one text value and returns a list of rows, each a list of text cells. Quoting, escaped quotes, commas, and newlines inside quoted cells follow the runtimes' CSV readers. Blank records are ignored. Empty input returns an empty list. Inconsistent row widths are permitted. Invalid CSV raises a runtime error.

`csv.stringify(rows)` takes exactly one list of rows. Every row must be a list, and every cell must be text. It returns CSV with quotes escaped according to standard CSV rules and LF (`\n`) record endings on every platform. Empty rows produce blank records; an empty outer list produces empty text.

## Other available modules

The common module names are `math`, `random`, `statistics`, `json`, `datetime`, and `csv`. `mathematics` aliases `math`; `dates` and `dateandtime` alias `datetime`. Individual exports are documented as they stabilize; supported names can vary between the current native and Python runtimes, so consult the runtime source and the shared conformance corpus for the exact current contract.

Standard modules use `Use the <name> library [as alias].` Local `.tell` module imports are a separate feature documented in [Modules](MODULES.md). Tell has no package registry or remote dependency installer.
