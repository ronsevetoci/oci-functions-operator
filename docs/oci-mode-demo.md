# OCI Mode End-to-End Demo

This demo runs the manager locally with `INVOKER_MODE=oci` and `OCI_AUTH_MODE=config`. You can either reference an existing OCI Function or let the operator manage an OCI Functions application/function.

## Prerequisites

- A Kubernetes cluster reachable by your current `kubectl` context.
- OCI CLI configured for the tenancy/profile you want the local manager to use.
- Permission for that OCI principal to manage OCI Functions applications/functions and invoke functions in the target compartment.
- For existing mode: an existing OCI Function OCID and its invoke endpoint.
- For managed mode: a compartment OCID, subnet OCIDs, and a function image in OCIR or another registry OCI Functions can pull.

## 1. Choose A Function Mode

### Existing Function Mode

Identify an existing function:

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

Set the existing function OCID and endpoint:

```sh
export FUNCTION_OCID="ocid1.fnfunc.oc1.iad.exampleuniqueid"
export FUNCTION_INVOKE_ENDPOINT="$(
  oci fn function get \
    --function-id "$FUNCTION_OCID" \
    --query 'data."invoke-endpoint"' \
    --raw-output
)"
```

The endpoint must be an HTTPS base URL and must not include `/20181201` or another API path.

### Managed Function Mode

Set the desired managed function inputs. This example uses Jeddah with the OCI region identifier `me-jeddah-1`:

```sh
export REGION="me-jeddah-1"
export COMPARTMENT_OCID="ocid1.compartment.oc1..exampleuniqueid"
export SUBNET_OCID="ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid"
export FUNCTION_IMAGE="me-jeddah-1.ocir.io/<namespace>/functions/hello:latest"
```

The operator will ensure the OCI Functions application exists, ensure the function exists, update image/memory/timeout/config when they drift, and populate `Function.status.functionId` plus `Function.status.invokeEndpoint`.

## 2. Set Local Config Auth Environment

For local development outside OKE, use config-file auth:

```sh
export INVOKER_MODE=oci
export OCI_AUTH_MODE=config
export OCI_CONFIG_FILE="${OCI_CONFIG_FILE:-$HOME/.oci/config}"
export OCI_CONFIG_PROFILE="${OCI_CONFIG_PROFILE:-DEFAULT}"
```

`OCI_AUTH_MODE=config` is the local development path. In OKE, use `OCI_AUTH_MODE=workload`; see [oke-deployment.md](oke-deployment.md).

Validate the OCI CLI can reach the target compartment/profile:

```sh
oci iam compartment get --compartment-id "$COMPARTMENT_OCID"
```

## 3. Install CRDs And Run The Manager Locally

```sh
make generate
make manifests
kubectl apply -k config/crd
```

Run the manager in a terminal:

```sh
INVOKER_MODE=oci \
OCI_AUTH_MODE=config \
OCI_CONFIG_FILE="$OCI_CONFIG_FILE" \
OCI_CONFIG_PROFILE="$OCI_CONFIG_PROFILE" \
go run ./cmd
```

## 4. Submit A Function

### Existing Function

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: oci-existing-hello
spec:
  mode: Existing
  functionId: ${FUNCTION_OCID}
  invokeEndpoint: ${FUNCTION_INVOKE_ENDPOINT}
EOF
```

### Managed Function

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: oci-managed-hello
spec:
  mode: Managed
  config:
    region: ${REGION}
    compartmentId: ${COMPARTMENT_OCID}
    applicationName: oci-functions-operator-demo
    subnetIds:
    - ${SUBNET_OCID}
    displayName: oci-managed-hello
    image: ${FUNCTION_IMAGE}
    memoryInMBs: 128
    timeoutInSeconds: 30
    config:
      LOG_LEVEL: info
EOF
```

Wait for the `Function` to become Ready:

```sh
kubectl get functions
kubectl get function <function-resource-name> -o yaml
```

For managed mode, look for:

- `status.applicationId`
- `status.functionId`
- `status.invokeEndpoint`
- `status.conditions[?(@.type=="Ready")]`

## 5. Submit A FunctionJob

Set the function name for the mode you chose:

```sh
export FUNCTION_RESOURCE_NAME="oci-existing-hello"
# or:
# export FUNCTION_RESOURCE_NAME="oci-managed-hello"
```

Create a job:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: oci-hello-job
spec:
  functionRef:
    name: ${FUNCTION_RESOURCE_NAME}
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

If invocation fails, `status.lastError` and the per-payload `error` field should distinguish common cases such as authentication errors, endpoint/network errors, missing function OCID, missing invoke endpoint, timeouts, and non-2xx OCI responses.

## 7. Validate Operator UX

After the run, capture notes in [validation-notes.md](validation-notes.md).

Checklist:

- `kubectl get functions,functionjobs` shows useful phase and counts without requiring YAML output.
- Existing mode reports Ready only when `spec.functionId` and `spec.invokeEndpoint` are set.
- Managed mode reports `status.applicationId`, `status.functionId`, and `status.invokeEndpoint` before jobs invoke.
- A `FunctionJob` that references a non-Ready `Function` fails clearly without invoking.
- `kubectl describe functionjob oci-hello-job` includes readable conditions and events.
- `status.lastOciRequestId` or per-payload `ociRequestId` is populated after an OCI call.
- Successful payloads include an `invocationId`.
- Failures include a concise, actionable `status.lastError` and per-payload `error`.
- Auth, timeout, endpoint, function OCID, and non-2xx failures are distinguishable.
- The demo steps felt repeatable without hidden local assumptions.
