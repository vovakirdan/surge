# Surge Modules and `pragma module`

> **Status:** Implemented (multi-file modules require `pragma module` / `pragma binary`)  
> **Audience:** Surge language users and standard-library authors  
> **Purpose:** Describes the concept of modules in Surge, rules for building multi-file modules, the `pragma module` mechanism, automatic module name determination, and when a pragma is actually needed.

---

# 1. What is a Module in Surge

A module is a **unit of compilation and code reuse**, containing:

- declared types, functions, constants, tags, contracts;
- its own internal namespace (names inside the module do not conflict with external ones);
- clear visibility rules (`pub`, `@hidden`, module-internal);
- its own lifecycle during compilation (hashing, caching, reuse).

Every module:

- has a **unique name**;
- exports only `pub` elements;
- may consist of **a single file** (default) or **a set of files in a directory** (only with `pragma module` / `pragma binary`);
- can be a regular module or an executable module (`binary`).

**Default behavior (important):**
- If there is **no** `pragma module` / `pragma binary` in a directory, **each file is its own module**.
- A directory is only treated as a **multi-file module** when *all* `.sg` files in that directory declare `pragma module` or `pragma binary`.

---

# 2. Regular and Binary Modules

Surge distinguishes two kinds of modules:

### **2.1. Regular Module (`module`)**

Used for libraries, parts of the stdlib, and any non-executable units.

- May or may not have an `@entrypoint`.
- If there is an `@entrypoint`, you can run that directory as a binary.
- Imported as a regular module.

### **2.2. Executable Module (`binary`)**

Means “module with an entry point”.

- Must have **exactly one** `@entrypoint`.
- May be imported as a regular module.
- Can be executed directly (`surge run foo/bar`).

In fact, `binary` is just a regular module with an extra contract: “I have a single entry point.”

---

# 3. `pragma module`

`pragma module` is a **pragma entry** that declares the file belongs to a **multi-file module**, defined by the whole directory.

Example:

```sg
pragma module
```

### What does `pragma module` do:

* enables the “one module per directory” mode;
* merges all files in the directory into a single module;
* enables a shared symbol table (all files see each other's declarations);
* requires this pragma to be present and consistent in all files in the directory.

### When is the module name set automatically

If `pragma module` is used **without a name**, then the module name = the directory name, provided that it:

* is a valid Surge identifier (ASCII, no spaces);
* does not conflict with other names.

Example:

Directory tree:

```
scripts/
   foo.sg
   util.sg
```

In `scripts/foo.sg`:

```sg
pragma module
```

→ module name is **scripts**

Imported as:

```sg
import scripts;
```

---

# 4. Explicit Module Name: `pragma module::name`

If you need a different name or the directory name is invalid, you can specify a name explicitly:

```sg
pragma module::bounded
```

Now, the directory is imported not by its folder name but by `bounded`:

```
import bounded;
```

Even if the file is in `core/math/`, the import path will be:

```
import core/bounded;
```

### Consistency rules:

* If one of the files in the directory specifies a name:  
  **all files must specify the same name**.
* If at least one file specifies a name explicitly — the others must do so as well.

---

# 5. When `pragma module` is required

`pragma module` becomes **mandatory** if:

1. **The directory contains more than one .sg file**
   and these files should be part of the same module.
2. **You need to override the module name** (via `::name`).
3. **You need to specify that the module is binary** (see below).
4. The directory name is invalid, so an explicit name is required.

---

# 6. When `pragma module` is not needed

You don't need to write `pragma module` if:

* the file is **the only one** in its directory;
* this file forms the module by itself;
* you do not need special behavior (`binary`, `no_std`, etc.).

Example:

```
math/
   trig.sg
```

`trig.sg` without pragma:

→ the module is automatically named after the file, `trig`.

Import:

```sg
import math/trig;
```

---

# 7. Multi-file Modules

If even a single file in a directory has `pragma module` or `pragma binary`, then:

* **all files must have one of these pragmas**;
* the module name is shared across the directory;
* all top-level declarations are visible between files, except those marked with `@hidden`.

Diagnostics:
- `ProjMissingModulePragma` if some files in the directory lack the pragma.
- `ProjInconsistentModuleName` if files disagree on the explicit `::name`.

### Example structure

```
core/vector/
   add.sg
   mul.sg
   impl.sg
```

In each file:

```sg
pragma module
```

→ A module `core/vector` is created.

All files see:

```sg
fn internal_helper(...) { ... }
```

even without `pub`.

But if:

```sg
@hidden
fn __tmp() { ... }
```

then this function is **visible only in the current file**.

---

# 8. Naming Errors and Fix-suggestions

### Example situation

File:

```
foo/foo.sg
```

Contains:

```sg
pragma module::bar;
```

Now, the module is named:

```
bar
```

If a programmer writes:

```sg
import foo/foo;
```

The compiler must:

* issue an error: *"The module is named `bar`, not `foo/foo`"*
* suggest an autofix: **change the import to `foo/bar`**

Diagnostic: `ProjWrongModuleNameInImport`.

---

# 9. `pragma binary`

Declares that the module is executable:

```sg
pragma binary
```

Rules:

* the module must have exactly one `@entrypoint`;
* the entry point file can have any name (doesn't have to be `main.sg`);
* importing a `binary` works just like any normal module.

### Explicit name:

```sg
pragma binary::run_app
```

The module is now named `run_app`.

---

# 10. Entry Points: `@entrypoint`

The entry point can have any name:

```sg
@entrypoint
fn run() -> int { ... }
```

### Rules:

* there must be **exactly one** `@entrypoint` in a `binary` module;
* overloads are allowed (`@overload`),
  because the attribute marks a specific function;
* a binary module must have such a function;
* a regular module doesn't have to.

**Modes:** `@entrypoint("argv")` and `@entrypoint("stdin")` are supported; see `docs/ATTRIBUTES.md`.

---

# 11. Visibility of Objects Inside a Module

In a multi-file module, these rules apply:

| Declaration | Visibility                       |
| ----------- | -------------------------------- |
| without `pub`  | visible in all files of the module  |
| `pub`          | exported outside                |
| `@hidden`      | visible only in the current file |

`@hidden` takes precedence over `pub`:
`@hidden pub fn foo()` remains file-local.

---

# 12. Imports

Modules are imported by path:

```
import core/vector;
import foo/bar;
import ./local_module;
import ../parent_module;
```

A path consists of:

* directory segments,
* a final segment — the module name (folder or `::name`).

Import syntax also supports aliasing and groups:

```
import core/math as m;
import core/math::{sin, cos};
import core/math::*;
```

See `docs/LANGUAGE.md` for the full grammar.

If a module inside a directory was renamed — use the new name.

**Resolution order (simplified):**
1) `path.sg` (single-file module)  
2) `path/` (directory module with `pragma module`/`binary`)  
3) explicit-name scan for `pragma module::name` / `pragma binary::name`

---

# 13. When Should a Module Be a Library or Binary

Use a **binary module** if:

* you need to run it (`surge run ...`);
* there is a logical entry point;
* the code should "start working" when called.

Use a **regular module** if:

* it's a library or a part of the stdlib;
* no `@entrypoint` is present;
* no logic should be executed automatically.

---

# 14. Summary

| Scenario                         | Need `pragma module`?          | Note                                  |
| --------------------------------- | ------------------------------ | ------------------------------------- |
| One file in a directory           | ❌ No                          | module name = folder name             |
| Several files, one module         | ✅ Yes                         | every file must contain the pragma    |
| Need a different module name      | ✅ Yes (`::name`)              | folder name is ignored                |
| Non-standard folder name          | ✅ Yes (`::name`)              | explicit name required                |
| Executable module                 | ✅ `pragma binary`             | requires @entrypoint                  |
| Entry point in a regular module   | ⚠️ Optional                   | can be run as a binary                |
| Want file-local objects           | Use `@hidden`                  | pragma does not affect                |
