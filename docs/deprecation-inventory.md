# Public API Facade & Deprecation Inventory

Status: **Canonical Specification (T-052 / API-003)**  
Scope: `github.com/Homiakus/axiom` Root Facade + Pre-v1 Deprecation Schedule

---

## 1. Overview and Migration Policy

In accordance with [versioning.md](file:///d:/Programms/axiom/docs/versioning.md), Axiom maintains strict backward compatibility for all public exported symbols in pre-v1 releases (`v0.1.x`). Any planned removals or refactorings follow a formal deprecation schedule before being sunset in minor version updates (`v0.x.0`) or `v1.0.0`.

This document records the complete inventory of deprecated symbols, duplicate constructors, and low-level runtime compiler aliases re-exported at the root facade.

---

## 2. Deprecated Constructors and Functions

| Deprecated Symbol | Status | Preferred Canonical Replacement | Removal Target | Rationale & Migration Guidance |
|---|---|---|---|---|
| `axiom.Register(name, fn)` | **Deprecated** | `axiom.Act(name, fn)` or `axiom.ActTyped(name, fn)` | `v0.2.0` | Replaced by `Act`/`ActTyped` which clearly distinguish dynamic map payloads from statically-checked Go structs. |
| `axiom.CompileModule(source)` | **Deprecated** | `axiom.Compile(source)` | `v0.2.0` | Simplified top-level compile helper returning `(*Module, error)`. |
| `axiom.NewEngineWithStore(module, store, opts...)` | **Deprecated** | `axiom.New(module, WithStore(store), WithActivities(opts...))` | `v0.2.0` | Functional options pattern (`WithStore`, `WithActivities`) replaces specialized positional parameter constructors. |

---

## 3. Root Facade Low-Level Runtime Type Aliases

The root `axiom` package exposes several type aliases originating from `internal/runtime` and `internal/compiler`. Application code should prefer high-level orchestrator abstractions (`Plan`, `Engine`, `Execution`, `Run`).

| Re-exported Alias | Origin Package | Intended Audience | Pre-v1 Status | Recommendation |
|---|---|---|---|---|
| `axiom.FieldID` | `internal/runtime` | Tooling / Low-level AST | **Stable Alias** | Avoid in application code; use `model.Key` instead. |
| `axiom.AtomID` | `internal/runtime` | Compiler / Bytecode | **Stable Alias** | Internal identifier for fast evaluation VM. |
| `axiom.RuleID` | `internal/runtime` | Compiler / Bytecode | **Stable Alias** | Internal rule index. |
| `axiom.SignalID` | `internal/runtime` | Compiler / Bytecode | **Stable Alias** | Internal signal index. |
| `axiom.ActivityID` | `internal/runtime` | Compiler / Bytecode | **Stable Alias** | Internal activity slot. |
| `axiom.ValueKind` | `internal/runtime` | Runtime Value System | **Stable Alias** | Used by low-level value introspection. |
| `axiom.Value` | `internal/runtime` | Runtime Value System | **Stable Alias** | Tagged union for expression evaluation. |
| `axiom.ExecutionState` | `internal/runtime` | Low-level State Snapshot | **Stable Alias** | For serialization tooling; prefer `run.State(ctx, &target)`. |
| `axiom.RetryScheduledError` | `internal/runtime` | Advanced Engine Control | **Stable Alias** | Sentinel error returned by `RunUntilIdle`. |
| `axiom.ErrRetryScheduled` | `internal/runtime` | Error Matching | **Stable Alias** | Target for `errors.Is` when distinguishing deferred retries. |
| `axiom.TRIZNormalization` | `internal/triz` | TRIZ Tooling / Linters | **Stable Alias** | AST normalization source map result. |
| `axiom.SourceMapEntry` | `internal/triz` | TRIZ Tooling / Linters | **Stable Alias** | Source mapping entries between TRIZ and AXM. |

---

## 4. Mechanical Enforcement

The stability of all symbols in this inventory is mechanically guarded by:
- `api_compatibility_test.go` (`TestPublicAPICompatibilityGate`) against [public_api_manifest.txt](file:///d:/Programms/axiom/testdata/compat/public_api_manifest.txt).
- No symbol may be removed from the manifest without prior documentation in this table and a minor version bump.
