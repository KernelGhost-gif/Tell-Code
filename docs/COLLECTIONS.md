# Collections

Tell's built-in collection values include mutable lists and dictionaries plus immutable tuples. Lists and tuples preserve order and support indexing, slicing, and iteration; slicing a tuple returns a tuple. Lists support mutation. Dictionaries map keys to values and retain string, number, boolean, and `nothing` keys supported by the selected runtime. These collections can be nested.

List- or tuple-valued expressions can be unpacked into multiple names with `Let first, second be [value1, value2].` or `Let first, second be (value1, value2).`, and reassigned with `Set first, second to ...`. The number of values must match the number of distinct target names. The operation validates every target before changing any target. Type annotations on individual targets are not supported in this syntax.

Tuple literals use comma-separated values in parentheses. `()` is empty, `(value,)` is a one-item tuple, `(first, second)` has two items, and `(value)` is ordinary grouping. Tuples support `len`, indexing, slicing, iteration, destructuring, membership, concatenation with another tuple, and `type_of` returning `tuple`. `tuple()` creates an empty tuple; `tuple(list_or_tuple_or_text)` converts the supplied sequence. Tuple display uses parentheses and a trailing comma for a one-item tuple.

```tell
Let scores be [8, 6, 10].
Add 9 to scores.
Set the item at position 1 in scores to 7.
Say sorted(scores).
Say scores[1:3].

Let player be {"name": "Ada", "score": 10}.
Set the key "score" in player to 11.
Say player["score"].
```

## Functional transformations

`map`, `filter`, and `reduce` accept Tell functions and lists. They eagerly return values; `map` and `filter` return lists. `reduce` requires an explicit initial value, which is returned unchanged for an empty input list. `enumerate(list)` eagerly returns `[[index, value], ...]`, with zero-based integer indexes.

```tell
To double with value:
    Give back value times 2.
End function.

To is_even with value:
    Give back value modulo 2 is equal to 0.
End function.

To add with total and value:
    Give back total plus value.
End function.

Say map(double, [1, 2, 3]).
Say filter(is_even, [1, 2, 3, 4]).
Say reduce(add, [1, 2, 3, 4], 0).
Say enumerate(["Ada", "Grace"]).
```

`For each` can destructure each pair for direct indexed iteration:

```tell
For each index, name in enumerate(["Ada", "Grace"]):
    Say "{index}: {name}".
End loop.
```

Each destructured item must be a list or tuple whose length matches the number of loop bindings. A single loop binding continues to receive the complete item unchanged.

These operations work with named Tell functions, returned functions, and closures. Each callback is invoked once per element (or once per accumulated element for `reduce`) and obeys the runtime's function-call depth limit. `filter` keeps elements whose callback result is truthy.

Built-ins also include `len`, `sum`, `min`, `max`, `sorted`, `reversed`, `first`, `last`, `any`, and `all`. The `tuple` constructor and tuple semantics are available in both runtimes. See [Variables and types](VARIABLES_AND_TYPES.md) for the runtime type model and [the language specification](LANGUAGE_SPECIFICATION.md) for indexing and slicing.

## Current limits

Set literals, collection comprehensions, generators, lazy streams, and immutable dictionary/set types are not implemented. Collection size is not bounded by the execution loop limits. Programs that process untrusted or very large data should apply their own input limits.
