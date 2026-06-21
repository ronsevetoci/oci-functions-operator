# OCI Functions Operator Design

## Problem Statement

OKE users often want Kubernetes-native workflows around OCI Functions: declare a function target, submit invocation work, track progress, and inspect failures through `kubectl`. Without an operator, teams must wire ad hoc scripts, CI jobs, or application-specific controllers that duplicate OCI authentication, invocation retry behavior, and status reporting.

The OCI Functions Operator provides a small Kubernetes API for invoking OCI Functions while keeping the OCI SDK details behind a narrow internal boundary.

## Why CRDs And An Operator

CRDs let users describe function invocation work with normal Kubernetes resources, RBAC, audit trails, and status. An operator can reconcile those resources idempotently, aggregate per-payload state, emit Kubernetes events, and make progress visible through `kubectl get` and `kubectl describe`.

This is intentionally not modeled as Pods or Kubernetes Jobs. OCI Functions are external serverless resources, so the operator tracks invocation intent and observed outcomes instead of pretending each invocation is a pod lifecycle.

## MVP Scope

The MVP introduces two namespaced resources in `functions.oci.oracle.com/v1alpha1`:

- `Function`: references an existing OCI Function by `spec.functionId`, with `spec.existingFunctionOcid` retained as a compatibility alias. It can also hold desired config for future lifecycle management.
- `FunctionJob`: references a `Function`, carries inline JSON payloads, controls per-reconcile parallelism, applies retry limits, and records status aggregation.

Implemented invoker modes:

- `fake`: deterministic local success path for development and demos.
- `oci`: OCI Go SDK-backed invocation of an existing OCI Function.

## Non-Goals

- Creating, updating, or deleting OCI Functions.
- Cron scheduling.
- Event source integration.
- Native Kubernetes Job compatibility.
- Pod templates, volumes, sidecars, init containers, GPUs, or privileged execution.
- Managing application networking, image publishing, or function deployment.
- Long-running durable queue semantics for large batches.

## Architecture

The controller manager runs two reconcilers:

- `FunctionReconciler` resolves a `Function` into a ready or pending status. Existing function references become ready immediately.
- `FunctionJobReconciler` resolves the referenced `Function`, initializes per-payload status, invokes runnable payloads through `invoker.Interface`, aggregates status, and emits events.

OCI SDK usage is isolated under `internal/invoker`. Controllers depend only on:

```go
type Interface interface {
    Invoke(ctx context.Context, request Request) (Response, error)
}
```

An optional `FunctionIDRequirement` capability lets OCI mode tell the controller that the referenced `Function` must use `spec.functionId`. This keeps controllers independent of OCI SDK types while still validating OCI-mode requirements early.

## Function Lifecycle

For the MVP, `Function` is a lightweight reference object.

When `spec.functionId` or `spec.existingFunctionOcid` is set:

- `status.phase` becomes `Ready`.
- `status.functionOcid` is populated.
- the `Ready` condition is set to true.

When `spec.config` is set:

- the desired config is accepted by the API.
- `status.phase` remains `Pending`.
- the `Ready` condition explains that lifecycle management is not implemented yet.

## FunctionJob Lifecycle

A `FunctionJob` starts by resolving `spec.functionRef.name` in the same namespace.

If the `Function` is missing or not ready:

- `status.phase` is `Pending`.
- `Pending` is true.
- `Running`, `Complete`, and `Failed` explain why no invocation has started.

When the `Function` is ready:

- inline payloads are normalized into ordered per-payload status entries.
- succeeded payloads are never invoked again.
- pending or retryable failed payloads are invoked up to `spec.parallelism` per reconcile.
- each payload gets attempts, phase, status code, invocation ID, OCI request ID, error, and completion time.

When all payloads succeed:

- `status.phase` becomes `Succeeded`.
- `Complete` is true.
- a normal completion event is emitted.

When one or more payloads exhaust retries:

- `status.phase` becomes `Failed`.
- `Failed` is true.
- `status.lastError` and the per-payload error carry the most useful bounded failure details.
- warning events are emitted.

## Fake Mode Vs OCI Mode

`INVOKER_MODE=fake` is the default. It returns deterministic successful responses and is intended for local demos, controller development, and CI-friendly tests. It does not contact OCI.

`INVOKER_MODE=oci` constructs an OCI Go SDK Functions invoke client. It requires:

- `OCI_AUTH_MODE=workload` for OKE Workload Identity, which is the default when `OCI_AUTH_MODE` is unset.
- `OCI_AUTH_MODE=config` only for local development with an OCI config file/profile.
- `OCI_FUNCTIONS_INVOKE_ENDPOINT` set to the existing function's invoke endpoint.
- referenced `Function` resources to use `spec.functionId`.

OCI mode records `Fn-Call-Id` when available, otherwise `opc-request-id`, and stores the OCI request ID separately as `lastOciRequestId` and per-payload `ociRequestId`.

## Known Limitations

- OCI mode uses one configured invoke endpoint per manager process.
- The MVP invokes existing functions only; lifecycle management is intentionally deferred.
- Inline payloads are intended for small demo and operational jobs, not large queues.
- Retry behavior is local to reconciliation and status, not a durable external work queue.
- There is no admission webhook yet for cross-field validation beyond CRD CEL rules.
- Function response bodies are captured internally by the invoker response but are not surfaced in `FunctionJob` status.
- OCI mode currently supports OKE Workload Identity and local OCI config-file auth only.
