# Deploy The OCI Functions Operator On OKE

This guide deploys the current MVP to Oracle Kubernetes Engine (OKE). The MVP invokes existing OCI Functions only. It does not create, update, or delete OCI Functions.

## What Gets Installed

- CRDs for `Function` and `FunctionJob`.
- A controller manager Deployment in `oci-functions-operator-system`.
- RBAC for watching `Function` and `FunctionJob` resources and writing status/events.
- An invoker mode selected by `INVOKER_MODE`.

## Prerequisites

- `kubectl` points at the target OKE cluster.
- The operator image is built and pushed to a registry reachable by OKE.
- An existing OCI Function is already deployed.
- The OCI principal used by the manager can invoke that function.
- For OCI mode, you know the function `invokeEndpoint` and function OCID.

## Install CRDs

From the repository root:

```sh
make manifests
kubectl apply -k config/crd
kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com
```

## Deploy The Manager With Fake Mode

Fake mode is useful for checking the Kubernetes installation path before wiring OCI credentials.

Set your image:

```sh
export OPERATOR_IMAGE="<region>.ocir.io/<namespace>/oci-functions-operator:<tag>"
```

Deploy:

```sh
kubectl apply -k config/default
kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
```

The base manifest sets `INVOKER_MODE=fake`.

## Deploy The Manager With OCI Mode

OCI mode invokes existing OCI Functions through the OCI Go SDK.

Required environment variables:

- `INVOKER_MODE=oci`
- `OCI_FUNCTIONS_INVOKE_ENDPOINT`: the target function invoke endpoint, without an API path.

Optional environment variables:

- `OCI_CONFIG_FILE`: config file path inside the manager container. The sample overlay uses `/oci/config/config`.
- `OCI_CONFIG_PROFILE`: OCI config profile. The sample overlay defaults to `DEFAULT`.

### Supported Auth Assumptions

The current MVP uses OCI Go SDK config/profile authentication. For OKE deployment, the documented path is mounting an OCI config file and API signing key into the manager pod as a Kubernetes Secret.

The current MVP does not yet provide first-class configuration for instance principals, workload identity, dynamic groups, or Resource Principal auth. Those may work only after code and deployment support are added in a future iteration.

### Prepare OCI Config Secret

The OCI mode overlay expects this Secret to exist. It intentionally does not generate credentials from the example config file.

Create a temporary directory outside the repo with your real OCI config and key:

```sh
mkdir -p /tmp/oci-functions-operator-oci
cp "$HOME/.oci/config" /tmp/oci-functions-operator-oci/config
cp "$HOME/.oci/oci_api_key.pem" /tmp/oci-functions-operator-oci/oci_api_key.pem
```

Make sure the `key_file` in `/tmp/oci-functions-operator-oci/config` matches the mounted path:

```ini
key_file=/oci/config/oci_api_key.pem
```

Create or update the secret:

```sh
kubectl create namespace oci-functions-operator-system --dry-run=client -o yaml | kubectl apply -f -

kubectl -n oci-functions-operator-system create secret generic oci-functions-operator-oci-config \
  --from-file=config=/tmp/oci-functions-operator-oci/config \
  --from-file=oci_api_key.pem=/tmp/oci-functions-operator-oci/oci_api_key.pem \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Apply The OCI Mode Overlay

Copy the sample overlay or edit it with your endpoint/profile:

```sh
export OCI_FUNCTIONS_INVOKE_ENDPOINT="https://<function-invoke-endpoint>"
export OPERATOR_IMAGE="<region>.ocir.io/<namespace>/oci-functions-operator:<tag>"
```

Patch the generated config map after applying the overlay, or update `config/overlays/oci-mode/kustomization.yaml` before deployment:

```sh
kubectl apply -k config/overlays/oci-mode

kubectl -n oci-functions-operator-system create configmap oci-functions-operator-oci-mode \
  --from-literal=INVOKER_MODE=oci \
  --from-literal=OCI_CONFIG_PROFILE="${OCI_CONFIG_PROFILE:-DEFAULT}" \
  --from-literal=OCI_FUNCTIONS_INVOKE_ENDPOINT="$OCI_FUNCTIONS_INVOKE_ENDPOINT" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
kubectl -n oci-functions-operator-system rollout restart deployment/oci-functions-operator-controller-manager
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
```

## Invoke An Existing Function

Create a `Function` that references the existing OCI Function:

```sh
export FUNCTION_OCID="ocid1.fnfunc.oc1.iad.exampleuniqueid"

cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: oci-existing-hello
spec:
  functionId: ${FUNCTION_OCID}
EOF
```

Create a `FunctionJob`:

```sh
cat <<'EOF' | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: oci-hello-job
spec:
  functionRef:
    name: oci-existing-hello
  payload:
    message: hello from OKE
    requestId: oke-demo-001
  parallelism: 1
  retryLimit: 1
EOF
```

Inspect status and events:

```sh
kubectl get functions,functionjobs
kubectl get functionjob oci-hello-job -o yaml
kubectl describe functionjob oci-hello-job
kubectl get events --field-selector involvedObject.kind=FunctionJob,involvedObject.name=oci-hello-job --sort-by=.lastTimestamp
```

## Troubleshooting

### Auth Errors

Symptoms:

- `status.lastError` contains `oci auth error`.
- Manager logs mention `401`, `403`, signing, key, tenancy, user, or policy failures.

Checks:

- Confirm the mounted config exists: `kubectl -n oci-functions-operator-system exec deploy/oci-functions-operator-controller-manager -- ls -l /oci/config`.
- Confirm `OCI_CONFIG_FILE=/oci/config/config`.
- Confirm `key_file=/oci/config/oci_api_key.pem` in the mounted config.
- Confirm the OCI user or principal has permission to invoke the target function.
- Confirm the config profile matches `OCI_CONFIG_PROFILE`.

### Invoke Endpoint Errors

Symptoms:

- Manager fails startup if `OCI_FUNCTIONS_INVOKE_ENDPOINT` is empty.
- `status.lastError` contains `oci endpoint error`.

Checks:

- Confirm the endpoint comes from the function `invokeEndpoint`.
- Do not include `/20181201` or another API path.
- Confirm the OKE cluster can reach the endpoint.
- Confirm DNS and egress rules for worker nodes.

### Bad Function OCID

Symptoms:

- `FunctionJob` fails before invoking if the referenced `Function` omits `spec.functionId`.
- `status.lastError` contains `oci function OCID error` or a 404 from OCI.

Checks:

- Use `spec.functionId`, not only the deprecated `spec.existingFunctionOcid`, for OCI mode.
- Confirm the OCID starts with `ocid1.fnfunc`.
- Confirm the function exists in the same region as the invoke endpoint.
- Confirm the principal has permission to read/invoke that function.

### Missing Kubernetes RBAC

Symptoms:

- Manager logs include `forbidden`.
- `FunctionJob` status does not update.
- Events are missing.

Checks:

- Confirm RBAC was applied: `kubectl apply -k config/rbac`.
- Confirm the deployment uses service account `oci-functions-operator-controller-manager`.
- Confirm generated `ClusterRole` includes `functionjobs/status`, `functions`, and core `events`.
- Reapply the full default or OCI overlay if RBAC drifted.

## Current MVP Boundary

This deployment supports invoking existing OCI Functions. Function lifecycle management, application creation, image deployment, event sources, and scheduled jobs are intentionally out of scope for the current MVP.
