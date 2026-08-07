# ADGO security model

ADGO is a durable orchestration engine. It provides workflow-level safety controls, but it does not replace application authentication, authorization, secret management, transport security or provider-side idempotency.

## 1. Trust boundaries

A typical deployment contains separate trust zones:

```text
operator / webhook / API clients
              |
              v
       application control API
              |
              v
      ADGO coordinator / Host
              |
       durable worker protocol
              |
      local or remote workers
              |
       external providers/tools
```

The durable store is trusted infrastructure. Anyone able to mutate it outside ADGO APIs can bypass runtime invariants.

## 2. Secrets

Do not persist raw credentials in:

- `Execution.Data`;
- event payloads;
- human patches;
- durable history metadata;
- schedule initial data;
- result cache entries;
- artifact metadata unless intentionally encrypted by the application.

Use environment injection, workload identity or an external secret manager in the worker process. Persist only non-secret references.

Operator patch APIs reject reserved `__adgo:` keys, but they cannot determine whether an arbitrary application field contains a secret.

## 3. External effects

ADGO deliberately guarantees **at-least-once**, not exactly-once, execution of external effects.

Every external-effect handler must use `ActivityRequest.IdempotencyKey` or an equivalent provider transaction identity.

If the provider may have accepted a request but the worker cannot prove the outcome, return `FailureAmbiguousSideEffect`. The workflow must reconcile before retrying.

Never make irreversible side effects speculative/hedged/ensemble work.

## 4. Risk and permissions

Use plan-level controls:

- `Node.Risk`;
- `RequiredPermissions`;
- provider permissions;
- human approval threshold;
- resource keys;
- compensation.

These are workflow safety constraints, not user authentication.

A provider that scores better is never allowed to bypass a hard risk/privacy/permission constraint.

## 5. Human-in-the-loop

`ResolveHuman` records actor/reason supplied by the caller, but ADGO does not authenticate that identity itself.

Your control API must authenticate and authorize the caller before invoking:

- approve/edit/reject;
- pause/resume/cancel;
- budget changes;
- data patches;
- rewind;
- migration;
- retention/deletion.

Treat these methods as privileged operations.

## 6. Remote worker HTTP transport

`HTTPWorkerServer` supports bearer authentication or a custom authorization callback.

Production requirements:

- run behind TLS;
- use short-lived workload credentials where possible;
- rotate bearer tokens;
- restrict network reachability;
- do not expose the worker protocol directly to browsers/public internet;
- give worker identities only the activity set/risk level they require through `WorkerSpec`;
- use an authenticating reverse proxy/service mesh when stronger identity is required.

The built-in bearer token comparison is constant-time, but static bearer tokens are intentionally a minimal transport primitive, not a full identity system.

## 7. Store permissions

### FileStore

Protect execution and control directories with OS-level permissions. Shared-filesystem deployments rely on filesystem atomic create/rename/fsync semantics.

### PebbleStore

Protect the database directory. Pebble is a single-owner database path; do not attempt to open the same path from several hosts through network storage as a substitute for a distributed database.

### Custom stores

A distributed Store implementation must preserve:

- atomic compare-and-swap semantics;
- durable inbox writes;
- read-after-commit expectations used by the engine;
- unique execution IDs;
- immutable version integrity when implementing `VersionedStore`.

## 8. Result cache

Result cache entries may contain model/tool outputs and therefore sensitive application data.

Use a namespace that changes when semantics/model/prompt/schema changes.

Do not cache:

- secrets;
- user data beyond retention policy;
- irreversible side effects;
- results whose authorization depends on the caller unless the authorization context is part of the cache key.

## 9. Schedules

Schedule initial payload is durable. Do not embed credentials.

Deterministic schedule execution IDs prevent duplicate workflow starts, but downstream external activities still need their own idempotency contract.

## 10. Time travel and forks

Historical versions and forks may contain old sensitive data even after current execution data changes.

Retention/version pruning is not a substitute for a full security incident response because backups, archives and external artifact stores may still contain the data.

## 11. Plan migration

Migration is privileged because it changes the immutable execution contract.

Default validation rejects silent semantic reinterpretation of completed nodes. Avoid `AllowSemanticChange` unless the migration was explicitly reviewed and its effect on already-completed side effects is understood.

## 12. Adaptive planning / LLM agents

Probabilistic components may propose facts, artifacts or a validated plan proposal. They must not directly mutate a live execution graph.

The safe boundary is:

```text
probabilistic proposal
       |
       v
 deterministic validation
       |
       v
 new immutable Plan / explicit migration
```

Never grant a model direct durable-store write access.

## 13. Admission control

Admission limits protect shared providers from overload but are not a security quota system. If quotas are tenant-sensitive, include tenant identity in the admission key and enforce authorization outside ADGO.

## 14. Retention and deletion

Deletion is explicit. When audit retention matters, configure an archive hook and verify it before enabling GC.

A malicious or overly privileged caller can request deletion through `ExecutionDeletionStore`; protect that API at the application boundary.

## 15. Logging and diagnostics

History/diagnostics may expose:

- provider names;
- failure messages;
- idempotency keys;
- operator reasons;
- node/task identifiers.

Treat logs/diagnostics as sensitive operational data. Avoid placing provider secrets or raw authorization headers in error strings.

## 16. Security checklist

Before production:

- [ ] secrets are external to durable workflow state;
- [ ] control API has authentication + authorization;
- [ ] remote workers use TLS + workload authentication;
- [ ] external effects use stable idempotency keys;
- [ ] ambiguous effects use reconciliation;
- [ ] high-risk effects require approval/compensation where appropriate;
- [ ] provider permissions/risk/privacy constraints are configured;
- [ ] filesystem/database directories are restricted;
- [ ] cache namespaces include semantic versioning;
- [ ] speculation is restricted to pure work;
- [ ] retention/archive requirements are explicit;
- [ ] operator actions are monitored/audited;
- [ ] failure messages do not leak credentials.
