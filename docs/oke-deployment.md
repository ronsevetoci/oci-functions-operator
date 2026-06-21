# Deploy The OCI Functions Operator On OKE

This guide deploys the current MVP to Oracle Kubernetes Engine (OKE). OCI mode uses OKE Workload Identity by default and does not mount a developer `~/.oci/config` file or PEM key.

## What Gets Installed

- CRDs for `Function` and `FunctionJob`.
- A controller manager Deployment in `oci-functions-operator-system`.
- RBAC for watching `Function` and `FunctionJob` resources and writing status/events.
- OCI mode configured for OKE Workload Identity.

## Prerequisites

- `kubectl` points at the target OKE cluster.
- The operator image is reachable by OKE. The current demo image is `ghcr.io/ronsevetoci/oci-functions-operator/controller:dev`.
- OKE Workload Identity is enabled/available for the cluster and service account. Use an OKE cluster type/version that supports Workload Identity in your tenancy.
- OCI IAM policy allows this Kubernetes workload to manage OCI Functions resources and invoke functions.
- For managed mode: a compartment OCID, subnet OCIDs, optional NSG OCIDs, and a same-region OCIR function image OCI Functions can pull.
- For existing mode: an existing function OCID and that function's invoke endpoint.

## Install CRDs

From the repository root:

```sh
make manifests
kubectl apply -k config/crd
kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com
```

## Deploy The Manager With Fake Mode

Fake mode is useful for checking the Kubernetes installation path before wiring OCI permissions.

```sh
export OPERATOR_IMAGE="ghcr.io/ronsevetoci/oci-functions-operator/controller:dev"

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

If `OCI_AUTH_MODE` is unset and `INVOKER_MODE=oci`, the manager defaults to `workload`.

Required manager environment for OCI mode on OKE:

- `INVOKER_MODE=oci`
- `OCI_AUTH_MODE=workload`
- `OCI_RESOURCE_PRINCIPAL_VERSION=2.2`
- `OCI_RESOURCE_PRINCIPAL_REGION=<cluster-or-workload-region>`, for example `me-jeddah-1`

There is no manager-level `OCI_FUNCTIONS_INVOKE_ENDPOINT`. Existing Functions put the endpoint in `Function.spec.invokeEndpoint`; managed Functions discover it into `Function.status.invokeEndpoint`.

The SDK also uses the pod service account token, Kubernetes service account CA, and `KUBERNETES_SERVICE_HOST` provided by Kubernetes/OKE.

## IAM Policy

Scope policies as tightly as your tenancy model allows. The workload principal conditions should match the namespace, service account, and OKE cluster OCID used by this deployment.

For managed mode, the operator needs to ensure OCI Functions applications/functions and invoke the resolved function. You can grant broad `manage functions-family` permissions, or use narrower permissions for `fn-apps`, `fn-functions`, and invocation according to your tenancy policy model.

Example with narrower resource families:

```text
Allow any-user to manage fn-apps in compartment <functions-compartment> where all {
  request.principal.type = 'workload',
  request.principal.namespace = 'oci-functions-operator-system',
  request.principal.service_account = 'oci-functions-operator-controller-manager',
  request.principal.cluster_id = '<oke-cluster-ocid>'
}

Allow any-user to manage fn-functions in compartment <functions-compartment> where all {
  request.principal.type = 'workload',
  request.principal.namespace = 'oci-functions-operator-system',
  request.principal.service_account = 'oci-functions-operator-controller-manager',
  request.principal.cluster_id = '<oke-cluster-ocid>'
}
```

If the application subnets live in a different compartment, grant network use there:

```text
Allow any-user to use virtual-network-family in compartment <network-compartment> where all {
  request.principal.type = 'workload',
  request.principal.namespace = 'oci-functions-operator-system',
  request.principal.service_account = 'oci-functions-operator-controller-manager',
  request.principal.cluster_id = '<oke-cluster-ocid>'
}
```

If the function image is in a private OCIR repository, add the appropriate repository read policy for the Functions application principal in your registry compartment/tenancy. Public OCIR repositories usually avoid normal repo-read IAM for public pulls, but network egress is still required.

For existing-mode invocation only, you may be able to narrow policy to `use fn-functions` for the target compartment instead of `manage`.

## Network For Managed Functions

Managed mode creates or updates an OCI Functions application. OCI Functions pulls the function image from OCIR during invocation, so the application network must allow that pull path:

- The Functions application subnet must have a route to Oracle Services Network/OCIR, usually through a Service Gateway.
- Subnet security lists must allow the required HTTPS egress.
- If `spec.config.nsgIds` attaches NSGs to the Functions application, those NSGs must allow egress TCP 443 to Oracle Services Network/OCIR.

Missing NSG egress can surface only at invocation time as:

```text
FunctionInvokeImageNotAvailable: Failed to pull function image
```

A public OCIR repository or otherwise accessible repository does not avoid the need for network egress from the Functions application.

## Deploy The Manager With OCI Mode

Set cluster-specific values:

```sh
export OPERATOR_IMAGE="ghcr.io/ronsevetoci/oci-functions-operator/controller:dev"
export OCI_RESOURCE_PRINCIPAL_REGION="me-jeddah-1"
```

Apply the OCI mode overlay, then replace the sample ConfigMap values:

```sh
kubectl apply -k config/overlays/oci-mode

kubectl -n oci-functions-operator-system create configmap oci-functions-operator-oci-mode \
  --from-literal=INVOKER_MODE=oci \
  --from-literal=OCI_AUTH_MODE=workload \
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

## Managed Function Mode

Create a managed `Function`. This example targets Jeddah with region identifier `me-jeddah-1`:

```sh
export COMPARTMENT_OCID="ocid1.compartment.oc1..exampleuniqueid"
export SUBNET_OCID="ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid"
export NSG_OCID="ocid1.networksecuritygroup.oc1.me-jeddah-1.exampleuniqueid"
export FUNCTION_IMAGE="jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:<tag>"

cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: oci-managed-hello
spec:
  mode: Managed
  config:
    region: me-jeddah-1
    compartmentId: ${COMPARTMENT_OCID}
    applicationName: oci-functions-operator-demo
    subnetIds:
    - ${SUBNET_OCID}
    # Optional: uncomment when the Functions application should use NSGs.
    # The NSG must allow egress TCP 443 to Oracle Services Network/OCIR.
    # nsgIds:
    # - ${NSG_OCID}
    displayName: oci-managed-hello
    image: ${FUNCTION_IMAGE}
    memoryInMBs: 128
    timeoutInSeconds: 30
    config:
      LOG_LEVEL: info
EOF
```

Watch status until `Ready=True` and these fields are populated:

```sh
kubectl get function oci-managed-hello -o yaml
```

- `status.applicationId`
- `status.functionId`
- `status.invokeEndpoint`

## Existing Function Mode

Create a `Function` that references an existing OCI Function:

```sh
export FUNCTION_OCID="ocid1.fnfunc.oc1.iad.exampleuniqueid"
export FUNCTION_INVOKE_ENDPOINT="https://functions.us-ashburn-1.oci.oraclecloud.com"

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

The operator copies the OCID and endpoint into status and marks the `Function` Ready.

## Submit A FunctionJob

Set the function resource name you want to invoke:

```sh
export FUNCTION_RESOURCE_NAME="oci-managed-hello"
# or:
# export FUNCTION_RESOURCE_NAME="oci-existing-hello"
```

Create a `FunctionJob`:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: oci-hello-job
spec:
  functionRef:
    name: ${FUNCTION_RESOURCE_NAME}
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
kubectl get events --field-selector involvedObject.kind=Function,involvedObject.name=${FUNCTION_RESOURCE_NAME} --sort-by=.lastTimestamp
kubectl get functionjob oci-hello-job -o yaml
kubectl describe functionjob oci-hello-job
kubectl get events --field-selector involvedObject.kind=FunctionJob,involvedObject.name=oci-hello-job --sort-by=.lastTimestamp
```

## Troubleshooting

### Workload Identity Auth Errors

Symptoms:

- Manager fails startup with `configure OCI Workload Identity auth provider`.
- `Function` status or `FunctionJob` status contains `oci auth error`.
- Manager logs mention `401`, `403`, `resource principal`, `Workload Identity`, service account token, or `OCI_RESOURCE_PRINCIPAL_*`.

Checks:

- Confirm `OCI_AUTH_MODE=workload` in the manager pod.
- Confirm `OCI_RESOURCE_PRINCIPAL_VERSION=2.2` and `OCI_RESOURCE_PRINCIPAL_REGION=<region>` are set.
- Confirm the pod uses service account `oci-functions-operator-controller-manager`.
- Confirm the service account token is mounted in the pod.
- Confirm the OKE cluster supports Workload Identity for the workload.
- Confirm the IAM policy matches the namespace, service account, cluster OCID, and target compartments.

### Managed Function Reconcile Errors

Symptoms:

- `Function.status.phase=Error`.
- `Function.status.message` mentions listing, creating, getting, or updating OCI Functions applications/functions.

Checks:

- Confirm `spec.config.region` is a valid OCI region identifier such as `me-jeddah-1`.
- Confirm `spec.config.compartmentId` is the Functions compartment.
- Confirm `spec.config.subnetIds` are valid and usable by OCI Functions.
- If `spec.config.nsgIds` is set, confirm the NSG OCIDs are valid and can be attached to the Functions application.
- Confirm IAM policy allows managing `fn-apps` and `fn-functions`.
- Confirm the function image is in same-region OCIR and OCI Functions can pull it.

### Function Image Pull Failures

Symptoms:

- Managed `Function` becomes Ready, but `FunctionJob` invocation fails.
- OCI returns `FunctionInvokeImageNotAvailable: Failed to pull function image`.

Checks:

- Confirm the function image is an OCI Functions-compatible Fn image.
- Confirm the image is in the expected same-region registry, such as Jeddah OCIR `jed.ocir.io`.
- Confirm the Functions application subnet route table sends Oracle Services Network/OCIR traffic through a Service Gateway.
- Confirm subnet security lists allow HTTPS egress.
- If the Functions application has NSGs from `spec.config.nsgIds`, confirm those NSGs allow egress TCP 443 to Oracle Services Network/OCIR.
- Remember that public OCIR repository visibility does not bypass network egress requirements.

### Placeholder Controller Image

Symptoms:

- The manager pod cannot pull `controller:latest`.
- The deployment stays in `ImagePullBackOff`.

Checks:

- Build and push the operator/controller image to a registry OKE can pull.
- Set the deployment image:

```sh
kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
```

- `controller:latest` in the base manifest is only a local scaffold placeholder.

### Invoke Endpoint Errors

Symptoms:

- Existing-mode `Function` is not Ready because `spec.invokeEndpoint` is empty.
- Managed-mode `Function` remains Pending because OCI has not returned an invoke endpoint yet.
- `FunctionJob.status.lastError` contains `oci endpoint error`.

Checks:

- Existing mode: confirm `spec.invokeEndpoint` comes from the function `invokeEndpoint`.
- Do not include `/20181201` or another API path.
- Managed mode: inspect `Function.status.invokeEndpoint`.
- Confirm the endpoint region matches the function region.
- Confirm the OKE cluster can reach the endpoint.
- Confirm DNS and egress rules for worker nodes.

### Bad Function OCID

Symptoms:

- Existing-mode `Function` is not Ready if `spec.functionId` is missing.
- `FunctionJob` fails clearly if the referenced `Function` is not Ready.
- `status.lastError` contains `oci function OCID error` or a 404 from OCI.

Checks:

- Existing mode: use `spec.functionId` and `spec.invokeEndpoint`.
- Managed mode: use `status.functionId` populated by the `Function` controller.
- Confirm the OCID starts with `ocid1.fnfunc`.
- Confirm the function exists in the same region as the invoke endpoint.
- Confirm the workload identity policy allows invoking that function.

### Missing Kubernetes RBAC

Symptoms:

- Manager logs include `forbidden`.
- `Function` or `FunctionJob` status does not update.
- Events are missing.

Checks:

- Reapply RBAC: `kubectl apply -k config/rbac`.
- Confirm the deployment uses service account `oci-functions-operator-controller-manager`.
- Confirm generated `ClusterRole` includes `functions/status`, `functionjobs/status`, and core `events`.
- Reapply the full default or OCI overlay if RBAC drifted.

## Local Config Auth

`OCI_AUTH_MODE=config` is for local development runs, not the OKE deployment path. See [oci-mode-demo.md](oci-mode-demo.md) for the local `go run` workflow that uses `OCI_CONFIG_FILE` and `OCI_CONFIG_PROFILE`.

## Current MVP Boundary

This deployment supports existing Function references, managed application/function reconciliation, and `FunctionJob` invocation. Image build/push workflows, Function deletion, event sources, schedules, and Function deployment packaging remain out of scope.
