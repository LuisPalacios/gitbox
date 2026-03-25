---
paths:
  - "git-toolkit/core/**/*.{cpp,h}"
  - "git-toolkit/cli/**/*.{cpp,h}"
  - "git-toolkit/gui/**/*.{cpp,h,mm,swift}"
---

# C++ Mandatory Rules

Modern C++20. **These rules are mandatory.**

## Core Principles

- **No exceptions** — return status/enum, log failures
- **RAII** — smart pointers, no raw `new`/`delete`
- **Const-correct** — `const std::string&` for heavy types
- **Early returns** — validate at function top, exit fast

## Naming Conventions

| Element | Convention | Example |
| ------- | ---------- | ------- |
| Classes/Structs | PascalCase | `GtkFileManager`, `GtkWidget` |
| Functions | PascalCase | `GetUserName`, `CalculateResult` |
| Variables | camelCase | `bufferSize`, `userName` |
| Members | _camelCase | `_bufferSize`, `_userName` |
| Static vars | s_camelCase | `s_instanceCount` |
| Global vars | g_camelCase | `g_appConfig` |
| Constants | ALL_CAPS | `MAX_BUFFER_SIZE` |
| Enum classes | PascalCase | `GtkStatus::Active` |
| Namespaces | lowercase | `gittoolkit`, `gittoolkit::platform` |

## Locking (MANDATORY)

**Use standard C++ locks. NEVER use `std::lock_guard`.**

| Mutex Type | When to Use |
| ---------- | ----------- |
| `std::mutex` | **Default** — simple exclusive access |
| `std::shared_mutex` | Read-heavy data — multiple concurrent readers, exclusive writers |

| Lock Type | When to Use |
| --------- | ----------- |
| `std::scoped_lock` | **Exclusive access** — single or multiple mutexes (writes) |
| `std::shared_lock` | **Read-only access** on `std::shared_mutex` (concurrent readers OK) |
| `std::unique_lock` | Manual unlock/relock, condition variables, try-lock |
| `std::lock_guard` | **NEVER** — legacy C++11 only |

```cpp
// Exclusive mutex (simple case)
std::mutex _mutex;
std::scoped_lock lock(_mutex);
std::scoped_lock lock(_mutexA, _mutexB);  // Multiple mutexes, deadlock-safe
```

**Shared mutex pattern (read-heavy data):**

```cpp
mutable std::shared_mutex _mutex;

void Write() {
    std::scoped_lock lock(_mutex);      // Exclusive — blocks readers and writers
}

int Read() const {
    std::shared_lock lock(_mutex);      // Shared — multiple readers OK
    return _data;
}
```

## File Organization

- One class per `.h`/`.cpp` pair
- Naming: `module.class.cpp` (e.g., `log.cpp`, `config.cpp`, `platform.cpp`)
- Platform-specific: `src/credentials/` for credential backends
- A header file should be included only when a forward declaration would not do the job
- Headers: `#pragma once`, definitions only (no implementations)
- Format: ClangFormat (Microsoft C++ Style Guide)
- Every new `.h`/`.cpp` file **must** start with this header (3 lines, last is blank):

```cpp
//----------------------------------------------------------------------------------------------------
// 2026 - My Name
//----------------------------------------------------------------------------------------------------

```

## Class Structure

```cpp
class GtkExample {
public:
    GtkExample();
    ~GtkExample();

    // Public methods (PascalCase)
    void DoSomething();

protected:
    // Protected methods

private:
    // Private methods

    // Members last
    std::string _name;
    int _count = 0;
};
```

## Error Handling

- **No exceptions** — ever
- Return status codes or enums
- Log unexpected cases with appropriate level
- Validate inputs at function entry (early returns)

## Comments and Doxygen

- Single-line only (`//`), simple Doxygen with `///`
- Document "why", not "what"
- TODO format: `// TODO: <developer> [<date>] Description`
- Comments attach directly to code — no blank line between comment and declaration
- No separator lines (`// ----`, `// ====`, `// ****`, etc.) — **exception:** copyright header block

Simple Doxygen style for public API:

```cpp
/// Returns the expanded path with ~ resolved to home directory.
std::string ExpandPath(const std::string& path);

/// Credential store result.
enum class GtkCredentialResult {
    Ok,       /// Credential found and valid
    NotFound, /// No credential in store
    Error     /// Store access failed
};
```

## Don'ts

- **No exceptions** — use return codes
- **No `std::function`** — use templates or function pointers
- **No `std::lock_guard`** — use `std::scoped_lock`
- **No macros** — use `constexpr`/templates
- **No shadowing** variable names
- **No extensive parameter lists** — use param structs
- **No `auto`** except for iterators, lambdas, complex templates
- **No non-trivial statics** in headers
