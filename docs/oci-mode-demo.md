# OCI Mode End-to-End Demo

This demo runs the manager locally with `INVOKER_MODE=oci` and invokes an existing OCI Function through a `FunctionJob`.

## Prerequisites

- A Kubernetes cluster reachable by your current `kubectl` context.
- OCI CLI configured for the same tenancy/profile you want the manager to use.
- An existing OCI Function that can be invoked by that profile.
- Permission for the OCI principal to invoke the target function.

## 1. Create Or Identify An Existing OCI Function

This operator currently invokes existing OCI Functions. You can create one through the OCI Console, the Fn Project tooling, or your existing deployment pipeline.

To identify an existing function with OCI CLI:

```sh
export COMPARTMENT_OCID="ocid1.compartment.oc1..exampleuniqueid"

oci fn application list \
  --compartment-id "$COMPARTMENT_OCID" \
  --query 'data[].{name:"display-name",id:id}' \
  --output table
```

Choose the application, then list functions:

```sh
export APPLICATION_OCID="ocid1.fnapp.oc1.iad.exampleuniqueid"

oci fn function list \
  --application-id "$APPLICATION_OCID" \
  --query 'data[].{name:"display-name",id:id}' \
  --output table
```

Set the target function OCID:

```sh
export FUNCTION_OCID="ocid1.fnfunc.oc1.iad.exampleuniqueid"
```

## 2. Find The Invoke Endpoint

The OCI Go SDK Functions invoke client needs the function invoke endpoint. Fetch it from the existing function:

```sh
export OCI_FUNCTIONS_INVOKE_ENDPOINT="$(
  oci fn function get \
    --function-id "$FUNCTION_OCID" \
    --query 'data."invoke-endpoint"' \
    --raw-output
)"

echo "$OCI_FUNCTIONS_INVOKE_ENDPOINT"
```

The value should be an HTTPS base URL and should not include `/20181201` or another API path.

## 3. Set Local Environment

The manager uses OCI Go SDK default config behavior. For local development, `$HOME/.oci/config` and the `DEFAULT` profile are enough when configured correctly.

```sh
export INVOKER_MODE=oci
export OCI_CONFIG_FILE="${OCI_CONFIG_FILE:-$HOME/.oci/config}"
export OCI_CONFIG_PROFILE="${OCI_CONFIG_PROFILE:-DEFAULT}"
export OCI_FUNCTIONS_INVOKE_ENDPOINT="$OCI_FUNCTIONS_INVOKE_ENDPOINT"
```

Validate the OCI CLI can see the function with the same profile:

```sh
oci fn function get --function-id "$FUNCTION_OCID"
```

## 4. Install CRDs And Run The Manager Locally

```sh
make generate
make manifests
kubectl apply -k config/crd
```

Run the manager in a terminal:

```sh
INVOKER_MODE=oci \
OCI_CONFIG_FILE="$OCI_CONFIG_FILE" \
OCI_CONFIG_PROFILE="$OCI_CONFIG_PROFILE" \
OCI_FUNCTIONS_INVOKE_ENDPOINT="$OCI_FUNCTIONS_INVOKE_ENDPOINT" \
go run ./cmd
```

## 5. Submit A Function And FunctionJob

In another terminal, create a `Function` that references the existing OCI Function by `spec.functionId`:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: oci-existing-hello
spec:
  functionId: ${FUNCTION_OCID}
EOF
```

Create a `FunctionJob` with a small inline JSON payload:

```sh
cat <<'EOF' | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: oci-hello-job
spec:
  functionRef:
    name: oci-existing-hello
  payloads:
  - message: hello from oci mode
    requestId: demo-001
  parallelism: 1
  retryLimit: 1
EOF
```

## 6. Inspect Status And Events

Watch the resources:

```sh
kubectl get functions,functionjobs
kubectl get functionjob oci-hello-job -o yaml
```

Useful status fields:

- `status.phase`
- `status.succeeded`
- `status.failed`
- `status.lastError`
- `status.lastOciRequestId`
- `status.invocationStatuses[*].invocationId`
- `status.invocationStatuses[*].ociRequestId`
- `status.invocationStatuses[*].error`

Describe the job and inspect events:

```sh
kubectl describe functionjob oci-hello-job

kubectl get events \
  --field-selector involvedObject.kind=FunctionJob,involvedObject.name=oci-hello-job \
  --sort-by=.lastTimestamp
```

If invocation fails, `status.lastError` and the per-payload `error` field should distinguish common cases such as authentication errors, endpoint/network errors, missing function OCID, timeouts, and non-2xx OCI responses.
