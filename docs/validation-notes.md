# OCI Mode Validation Notes

Use this template to record results from a real OCI-mode demo run.

## Environment

- Date/time:
- Operator commit:
- Kubernetes context:
- Kubernetes version:
- Namespace:
- `INVOKER_MODE`:
- `OCI_AUTH_MODE`:
- `OCI_CONFIG_FILE`:
- `OCI_CONFIG_PROFILE`:
- `OCI_FUNCTIONS_INVOKE_ENDPOINT`:
- `OCI_RESOURCE_PRINCIPAL_VERSION`:
- `OCI_RESOURCE_PRINCIPAL_REGION`:
- OCI region:
- OCI tenancy/profile notes:

## Function Details

- OCI Function display name:
- Function OCID:
- Application OCID:
- Invoke endpoint:
- Function runtime/image:
- Expected payload shape:
- Expected response:
- IAM/policy notes:

## FunctionJob Spec

```yaml
# Paste the FunctionJob manifest used for validation.
```

- Referenced `Function` name:
- Payload count:
- `parallelism`:
- `retryLimit`:

## Observed Status

```yaml
# Paste: kubectl get functionjob <name> -o yaml
```

- Final phase:
- `status.succeeded`:
- `status.failed`:
- `status.lastError`:
- `status.lastOciRequestId`:
- Per-payload invocation IDs:
- Per-payload OCI request IDs:

## Errors

- Did any invocation fail?
- Error classification observed:
- Was the error actionable from `kubectl get` or `kubectl describe`?
- Was any response body truncated appropriately?
- Related manager log excerpt:

## Latency

- Time from `kubectl apply` to first status update:
- Time from `kubectl apply` to terminal phase:
- Approximate OCI invocation latency:
- Any retries observed:
- Any timeout behavior observed:

## UX Notes

- Was setup clear?
- Were required environment variables obvious?
- Was `Function.spec.functionId` validation clear?
- Were status fields easy to find?
- Were events useful?
- What should change before the next demo?
