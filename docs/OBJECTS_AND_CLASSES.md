# Objects and Classes

Tell currently implements a small, shared object-oriented core in the Go and Python runtimes: class declarations, constructors, dynamically created instance fields, bound instance methods, single inheritance, and method overriding.

## Define and instantiate

```tell
Create a class called Animal:
    When creating an Animal with name:
        Remember name as this.name.
    End constructor.

    To speak:
        Say "{this.name} makes a sound.".
    End function.
End class.

Let animal be a new Animal with "Milo".
Tell animal to speak.
```

`When creating a/an ClassName with first and second:` declares a positional constructor. A class without a constructor accepts no arguments. `Remember value as this.field.` creates or changes an instance field in a constructor or method. Outside the class, `Set animal.name to "Milo the cat".` updates a public field.

Methods use the existing function declaration syntax. Methods receive their instance through `this` and can be called as `Tell animal to speak.`, `Tell animal to greet with "Hello".`, or `animal.greet("Hello")`.

## Inheritance

```tell
Create a class called Dog based on Animal:
    To speak:
        Say "Woof, {this.name}!".
    End function.
End class.
```

The base class must already be declared. The derived class inherits methods it does not override. A constructor is inherited when the derived class does not declare one. If a derived class declares a constructor, it replaces the base constructor; there is not yet a `super` call to initialize the base part.

Tell classes and instances are runtime objects, separate from dictionaries. The Python implementation restricts member access on these values to declared fields and methods; implementation storage is not exposed as Tell attributes.

The native bytecode compiler also supports the class subset in this guide: constructors, dynamically created fields, instance methods, one base class, inherited constructors, and overrides. Static/class methods and `super` remain unavailable in compiled programs.

## Current limits

Static and class methods, access modifiers, abstract/sealed classes, multiple inheritance, interfaces/traits, properties, destructors, operator overloading, reflection, dependency injection, generic classes, and `super` are not implemented. Do not rely on undocumented Python behavior for object semantics; use this page and the shared programs in `conformance/core.json` as the supported contract.
