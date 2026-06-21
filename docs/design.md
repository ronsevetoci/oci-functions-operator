# OCI Functions Operator Design

## Problem Statement

OKE users often want Kubernetes-native workflows around OCI Functions: declare a function target, submit invocation work, track progress, and inspect failures through `kubectl`. Without an operator, teams must wire ad hoc scripts, CI jobs, or application-specific controllers that duplicate OCI authentication, invocation retry behavior, and status reporting.

The OCI Functions Operator provides a small Kubernetes API for managing and invoking OCI Functions while keeping the OCI SDK details behind narrow internal boundaries.

## Why CRDs And An Operator

CRDs let users describe function invocation work with normal Kubernetes resources, RBAC, audit trails, and status. An operator can reconcile those resources idempotently, aggregate per-payload state, emit Kubernetes events, and make progress visible through `kubectl get` and `kubectl describe`.

This is intentionally not modeled as Pods or Kubernetes Jobs. OCI Functions are external serverless resources, so the operator tracks invocation intent and observed outcomes instead of pretending each invocation is a pod lifecycle.

## MVP Scope

The MVP introduces two namespaced resources in `functions.oci.oracle.com/v1alpha1`:

- `Function`: either references an existing OCI Function by `spec.functionId` and `spec.invokeEndpoint`, or manages an OCI Functions application/function from desired config.
- `FunctionJob`: references a `Function`, carries inline JSON payloads, controls per-reconcile parallelism, applies retry limits, and records status aggregation.

Implemented invoker modes:

- `fake`: deterministic local success path for development and demos.
- `oci`: OCI Go SDK-backed lifecycle and invocation.

## Non-Goals

- Cron scheduling.
- Event source integration.
- Native Kubernetes Job compatibility.
- Pod templates, volumes, sidecars, init containers, GPUs, or privileged execution.
- Image publishing, function source builds, or deployment packaging.
- Deleting OCI Functions or applications.
- Long-running durable queue semantics for large batches.

## Architecture

The controller manager runs two reconcilers:

- `FunctionReconciler` resolves a `Function` into a ready, pending, or error status. Existing function references validate required fields; managed functions ensure an OCI Functions application and function.
- `FunctionJobReconciler` resolves the referenced `Function`, initializes per-payload status, invokes runnable payloads through `invoker.Interface`, aggregates status, and emits events.

OCI SDK usage is isolated under `internal/invoker` and `internal/lifecycle`. Controllers depend only on small internal interfaces:

```go
type Interface interface {
    Invoke(ctx context.Context, request Request) (Response, error)
}

type Manager interface {
    EnsureFunction(ctx context.Context, desired DesiredFunction) (FunctionState, error)
}
```

An optional `FunctionIDRequirement` capability lets OCI mode tell the job controller that the referenced `Function` must resolve an OCI Function OCID. This keeps controllers independent of OCI SDK concrete types while still validating OCI-mode requirements early.

## Function Lifecycle

`Function.spec.mode` can be `Existing` or `Managed`. When omitted, the controller infers mode from existing references or `spec.config`.

When existing mode is used:

- `status.phase` becomes `Ready`.
- `status.functionId`, `status.functionOcid`, and `status.invokeEndpoint` are populated from spec.
- the `Ready` condition is set to true.
- missing `spec.functionId` or missing `spec.invokeEndpoint` makes the Function `Error`.

When managed mode is used:

- the controller ensures the OCI Functions application exists.
- the controller ensures the OCI Function exists.
- image, memory, timeout, and config are updated when they drift.
- `status.applicationId`, `status.functionId`, and `status.invokeEndpoint` are populated from OCI responses.
- `status.phase` remains `Pending` until the function is active and an invoke endpoint is available.

Managed mode config includes region, compartment OCID, application name, subnet OCIDs, image, memory, timeout, and function config. Jeddah is represented with the OCI region identifier `me-jeddah-1`.

## FunctionJob Lifecycle

A `FunctionJob` starts by resolving `spec.functionRef.name` in the same namespace.

If the `Function` is missing or not ready:

- a missing `Function` leaves the job `Pending`.
- a non-Ready `Function` fails the job clearly without invoking.
- conditions explain why no invocation has started.

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
- existing-mode `Function` resources to use `spec.functionId` and `spec.invokeEndpoint`.
- managed-mode `Function` resources to produce `status.functionId` and `status.invokeEndpoint` before jobs invoke.

OCI mode records `Fn-Call-Id` when available, otherwise `opc-request-id`, and stores the OCI request ID separately as `lastOciRequestId` and per-payload `ociRequestId`.

## Known Limitations

- Managed lifecycle currently reconciles application/function create and function update only; deletion and finalizers are not implemented.
- Existing mode requires the user to provide the invoke endpoint in `spec.invokeEndpoint`.
- Inline payloads are intended for small demo and operational jobs, not large queues.
- Retry behavior is local to reconciliation and status, not a durable external work queue.
- There is no admission webhook yet for cross-field validation beyond CRD CEL rules.
- Function response bodies are captured internally by the invoker response but are not surfaced in `FunctionJob` status.
- OCI mode currently supports OKE Workload Identity and local OCI config-file auth only.
