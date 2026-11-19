# Tell source standard library

These modules run in the Python interpreter and the Go interpreter. The Go bytecode compiler embeds them in the `.tbc` file. TellC requires import support before it can use these modules.

For a program saved in the repository root, import the modules by their paths in `StdLib`. Tell resolves each path relative to the source file containing the import.

```tell
Use the module "StdLib/lists.tell" as lists.
Use the module "StdLib/text.tell" as texts.

Let batches be lists.chunked(["Ada", "Grace", "Lin"], 2).
Say texts.join(batches[0], ", ").
```

This prints `Ada, Grace`. A program in `examples/` would use `../StdLib/lists.tell` and `../StdLib/text.tell`.

| Module | Function | Behavior |
| --- | --- | --- |
| `math.tell` | `clamp(value, lower, upper)` | Restricts a number to the inclusive bounds. Raises an error if `lower` exceeds `upper`. |
| | `lerp(start, finish, weight)` | Linearly interpolates between two numbers. Weight `0` gives the start, weight `1` gives the finish, and weights outside that interval extrapolate. |
| | `sign(value)` | Returns `-1`, `0`, or `1` for a negative, zero, or positive number. |
| `lists.tell` | `chunked(items, size)` | Splits a list into new lists of up to `size` elements. The last chunk may be shorter. `size` must be a positive whole number. |
| | `flatten(rows)` | Concatenates a list of lists into one list in input order. It flattens one level. |
| | `zip(left, right)` | Pairs items at matching indexes into two-item lists. The result stops at the end of the shorter list. |
| `text.tell` | `starts_with(value, prefix)` | Tests for an exact prefix. Every text value starts with the empty string. |
| | `ends_with(value, suffix)` | Tests for an exact suffix. Every text value ends with the empty string. |
| | `join(parts, separator)` | Joins a list of strings with a text separator. An empty list gives an empty string. |
| `statistics.tell` | `median(values)` | Returns the middle value of a nonempty list of numbers, or the average of the two middle values for an even length. |

Pass finite numbers to the numeric helpers. List transformations leave their inputs unchanged and reuse the element values. Text matching is case-sensitive and compares Unicode code points.

Run the module checks from the repository root with either interpreter or the Go bytecode compiler and VM.

```sh
python3 tell.py StdLib/stdlib_test.tell
tell StdLib/stdlib_test.tell
tell build StdLib/stdlib_test.tell --output stdlib_test.tbc
tell run stdlib_test.tbc
```
