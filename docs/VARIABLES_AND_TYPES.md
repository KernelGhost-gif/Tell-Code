# Variables and Types

Tell values are dynamically typed. The current runtime can optionally enforce a type annotation when a value is declared and whenever an annotated binding is reassigned.

## Variables

Use `Let` to create or update a binding and `Set` to update one:

```tell
Let visits be 2.
Set visits to visits plus 1.
Say visits.
```

Function calls create a local environment whose parent is the environment where the function was declared. A function assignment writes to its current environment. Loop bindings use the current environment. This is the behavior of the current interpreter and is not a static scope analysis.

## Constants

Declare an immutable binding with `Let constant`:

```tell
Let constant app_name be "Tell".
Say app_name.
```

Constants cannot be reassigned with `Set` or redeclared as constants in the same environment. A constant can also carry a runtime type annotation:

```tell
Let constant maximum_attempts as number be 5.
```

## Runtime annotations

Use `as TYPE` before `be`:

```tell
Let username as text be "Ada".
Let active as boolean be true.
Let scores as list be [10, 20, 30].
Set username to "Grace".
```

Supported built-in annotations are `any`, `nothing`, `boolean`, `number`, `text`, `list`, `tuple`, `set`, `dictionary`, `function`, and `class`. A Tell class name can be used as an annotation; instances of subclasses satisfy the base-class annotation.

At runtime, incompatible values and unknown annotation names produce Tell errors with the source statement location. The native compiler also performs conservative static checks for declared primitive annotations when an initializer or reassignment has a known type. Run `tell check --types program.tell` to request these checks without execution; `tell build` applies them automatically. Unknown or dynamic expressions remain subject to runtime checks. This is not whole-program static typing or general type inference. Tell has no generics, unions, interfaces, or parameter/return annotations yet.

Booleans are a distinct type from numbers for annotations, even though numeric conversion accepts boolean values in the current runtime. Use `type_of(value)` to inspect a value's Tell type name.

## Current limits

`Let` and `Set` operate in the current environment. `Set` does not search parent environments to mutate an outer binding; a function assignment can therefore shadow a parent name. Type annotations are attached to the current environment's binding. Tell execution is not sandboxed, and runtime annotations do not restrict a program's filesystem or other effects.
