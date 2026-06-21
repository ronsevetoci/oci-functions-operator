# Deploy The OCI Functions Operator On OKE

This guide deploys the current MVP to Oracle Kubernetes Engine (OKE). The MVP invokes existing OCI Functions only. It does not create, update, or delete OCI Functions.

## What Gets Installed

- CRDs for `Function` and `FunctionJob`.
- A controller manager Deployment in `oci-functions-operator-system`.
- RBAC for watching `Function` and `FunctionJob` resources and writing status/events.
- OCI mode configured for OKE Workload Identity.

## Prerequisites

- `kubectl` points at the target OKE cluster.
- The operator image is built and pushed to a registry reachable by OKE.
- OKE Workload Identity is available for the cluster and service account.
- An existing OCI Function is already deployed.
- You know the function OCID and function `invokeEndpoint`.
- OCI IAM policy allows this Kubernetes workload to invoke the target function.

## Install CRDs

From the repository root:

```sh
make manifests
kubectl apply -k config/crd
kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com
```

## Deploy The Manager With Fake Mode

Fake mode is useful for checking the Kubernetes installation path before wiring OCI invocation.

```sh
export OPERATOR_IMAGE="<region>.ocir.io/<namespace>/oci-functions-operator:<tag>"

kubectl apply -k config/default
kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
```

The base manifest sets `INVOKER_MODE=fake`.

## OKE Workload Identity Auth

For OKE, OCI mode uses the OCI Go SDK OKE Workload Identity provider:

```text
INVOKER_MODE=oci
OCI_AUTH_MODE=workload
```

The operator does not mount a developer `~/.oci/config` file or private PEM key in the OKE path.

Required manager environment for OCI mode on OKE:

- `INVOKER_MODE=oci`
- `OCI_AUTH_MODE=workload`
- `OCI_FUNCTIONS_INVOKE_ENDPOINT`: the target function invoke endpoint, without an API path.
- `OCI_RESOURCE_PRINCIPAL_VERSION`: use `2.2` for the current SDK workload identity provider.
- `OCI_RESOURCE_PRINCIPAL_REGION`: the OCI region for the workload identity provider, such as `us-ashburn-1`.

The SDK also uses the pod service account token, Kubernetes service account CA, and `KUBERNETES_SERVICE_HOST` provided by Kubernetes/OKE.

### IAM Policy

Grant the OKE workload permission to invoke the existing function. A typical policy shape is:

```text
Allow any-user to use fn-functions in compartment <functions-compartment> where all {
  request.principal.type = 'workload',
  request.principal.namespace = 'oci-functions-operator-system',
  request.principal.service_account = 'oci-functions-operator-controller-manager',
  request.principal.cluster_id = '<oke-cluster-ocid>'
}
```

Adjust the compartment, resource family, and conditions to match your tenancy policy model.

## Deploy The Manager With OCI Mode

Set cluster-specific values:

```sh
export OPERATOR_IMAGE="<region>.ocir.io/<namespace>/oci-functions-operator:<tag>"
export OCI_FUNCTIONS_INVOKE_ENDPOINT="https://<function-invoke-endpoint>"
export OCI_RESOURCE_PRINCIPAL_REGION="<oci-region>"
```

Apply the OCI mode overlay, then replace the sample ConfigMap values:

```sh
kubectl apply -k config/overlays/oci-mode

kubectl -n oci-functions-operator-system create configmap oci-functions-operator-oci-mode \
  --from-literal=INVOKER_MODE=oci \
  --from-literal=OCI_AUTH_MODE=workload \
  --from-literal=OCI_FUNCTIONS_INVOKE_ENDPOINT="$OCI_FUNCTIONS_INVOKE_ENDPOINT" \
  --from-literal=OCI_RESOURCE_PRINCIPAL_VERSION=2.2 \
  --from-literal=OCI_RESOURCE_PRINCIPAL_REGION="$OCI_RESOURCE_PRINCIPAL_REGION" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
kubectl -n oci-functions-operator-system rollout restart deployment/oci-functions-operator-controller-manager
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
```

Confirm no local OCI credential Secret is mounted:

```sh
kubectl -n oci-functions-operator-system get deployment oci-functions-operator-controller-manager -o yaml
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

### Workload Identity Auth Errors

Symptoms:

- Manager fails startup with `configure OCI Workload Identity auth provider`.
- `FunctionJob` status contains `oci auth error`.
- Manager logs mention `401`, `403`, `resource principal`, `Workload Identity`, service account token, or `OCI_RESOURCE_PRINCIPAL_*`.

Checks:

- Confirm `OCI_AUTH_MODE=workload` in the manager pod.
- Confirm `OCI_RESOURCE_PRINCIPAL_VERSION=2.2` and `OCI_RESOURCE_PRINCIPAL_REGION=<region>` are set.
- Confirm the pod uses service account `oci-functions-operator-controller-manager`.
- Confirm the service account token is mounted in the pod.
- Confirm the OKE cluster supports Workload Identity for the workload.
- Confirm the IAM policy matches the namespace, service account, cluster OCID, and function compartment.

### Invoke Endpoint Errors

Symptoms:

- Manager fails startup if `OCI_FUNCTIONS_INVOKE_ENDPOINT` is empty.
- `status.lastError` contains `oci endpoint error`.

Checks:

- Confirm the endpoint comes from the function `invokeEndpoint`.
- Do not include `/20181201` or another API path.
- Confirm the endpoint region matches `OCI_RESOURCE_PRINCIPAL_REGION`.
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
- Confirm the workload identity policy allows invoking that function.

### Missing Kubernetes RBAC

Symptoms:

- Manager logs include `forbidden`.
- `FunctionJob` status does not update.
- Events are missing.

Checks:

- Reapply RBAC: `kubectl apply -k config/rbac`.
- Confirm the deployment uses service account `oci-functions-operator-controller-manager`.
- Confirm generated `ClusterRole` includes `functionjobs/status`, `functions`, and core `events`.
- Reapply the full default or OCI overlay if RBAC drifted.

## Local Config Auth

`OCI_AUTH_MODE=config` is for local development runs, not the OKE deployment path. See [oci-mode-demo.md](oci-mode-demo.md) for the local `go run` workflow that uses `OCI_CONFIG_FILE` and `OCI_CONFIG_PROFILE`.

## Current MVP Boundary

This deployment supports invoking existing OCI Functions. Function lifecycle management, application creation, image deployment, event sources, and scheduled jobs are intentionally out of scope for the current MVP.
