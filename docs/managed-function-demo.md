# Managed Function Demo

This walkthrough uses `mode: Managed`, so the operator creates or updates the OCI Functions application/function and discovers the invoke endpoint into `Function.status.invokeEndpoint`.

OKE deployments should run the manager with Workload Identity:

```text
INVOKER_MODE=oci
OCI_AUTH_MODE=workload
```

There is no global `OCI_FUNCTIONS_INVOKE_ENDPOINT` in managed mode.

## Prerequisites

- The CRDs are installed with `kubectl apply -k config/crd`.
- The manager is running in OCI mode on OKE with `OCI_AUTH_MODE=workload`.
- The manager service account has IAM permissions to manage OCI Functions applications/functions and invoke functions in the target compartment.
- You have a Jeddah subnet OCID that OCI Functions can use.
- You have an OCI Functions-compatible container image reachable from OCI Functions.

The image must be built for OCI Functions and stored where OCI Functions can pull it, such as OCIR with the right repository access.

## 1. Edit The Managed Function Sample

Open `config/samples/functions_v1alpha1_function_managed.yaml` and replace:

- `<COMPARTMENT_OCID>` with the compartment OCID where the application/function should be managed.
- `<SUBNET_OCID>` with a subnet OCID in Jeddah that OCI Functions can use.
- `<FUNCTION_IMAGE>` with the full function image reference.

The sample uses:

- region: `me-jeddah-1`
- application name: `oke-functions-operator-demo`
- Function resource name: `managed-hello`
- OCI function display name: `managed-hello`

Current sample shape:

```yaml
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: managed-hello
  namespace: default
spec:
  mode: Managed
  config:
    region: me-jeddah-1
    compartmentId: <COMPARTMENT_OCID>
    applicationName: oke-functions-operator-demo
    subnetIds:
    - <SUBNET_OCID>
    displayName: managed-hello
    image: <FUNCTION_IMAGE>
    memoryInMBs: 256
    timeoutInSeconds: 120
    config:
      GREETING: "hello from oke functions operator"
```

## 2. Apply And Watch The Function

```sh
kubectl apply -f config/samples/functions_v1alpha1_function_managed.yaml
kubectl get functions -w
```

In another terminal, describe the Function:

```sh
kubectl describe function managed-hello
```

Wait for `Ready=True`. Managed mode should populate:

- `status.applicationId`
- `status.functionId`
- `status.invokeEndpoint`

## 3. Submit A FunctionJob

The sample FunctionJob references `managed-hello`:

```sh
kubectl apply -f config/samples/functions_v1alpha1_functionjob_managed.yaml
kubectl get functionjobs -w
```

Describe the job:

```sh
kubectl describe functionjob managed-hello-job
```

The FunctionJob sample sends two JSON object payloads:

```yaml
payloads:
- name: Ron
  index: 0
- name: Ron
  index: 1
```

## Troubleshooting

### Workload Identity Or IAM Permission Failure

Symptoms:

- Manager logs mention Workload Identity, resource principal, `401`, or `403`.
- `Function.status.phase` becomes `Error`.
- `FunctionJob.status.lastError` contains an OCI auth error.

Checks:

- Confirm the manager pod has `INVOKER_MODE=oci` and `OCI_AUTH_MODE=workload`.
- Confirm `OCI_RESOURCE_PRINCIPAL_VERSION=2.2` and `OCI_RESOURCE_PRINCIPAL_REGION=me-jeddah-1`.
- Confirm IAM policy matches the OKE cluster OCID, namespace, and service account.
- Confirm the workload can manage `fn-apps` and `fn-functions` in the target compartment.

### Wrong Compartment

Symptoms:

- The operator cannot list or create applications.
- Status mentions `list OCI Functions applications` or `create OCI Functions application`.

Checks:

- Replace `<COMPARTMENT_OCID>` with the real compartment OCID.
- Confirm the compartment is in the tenancy used by Workload Identity.
- Confirm IAM policy is scoped to that compartment or an ancestor compartment.

### Wrong Subnet

Symptoms:

- Application creation fails.
- Status or manager logs mention subnet, VCN, network, or authorization errors.

Checks:

- Replace `<SUBNET_OCID>` with a subnet in `me-jeddah-1`.
- Confirm OCI Functions is allowed to use the subnet.
- Confirm IAM policy allows network use if the subnet is in a different compartment.

### Image Pull Or Access Failure

Symptoms:

- The Function is created but does not become usable.
- OCI reports image or repository access errors.

Checks:

- Replace `<FUNCTION_IMAGE>` with a valid OCI Functions-compatible image.
- Confirm the image region and repository are reachable from OCI Functions.
- Confirm repository policies allow OCI Functions to pull the image.

### Invoke Endpoint Not Discovered

Symptoms:

- `Function.status.functionId` is set, but `status.invokeEndpoint` is empty.
- `Function.status.phase` remains `Pending`.

Checks:

- Describe the Function and read the Ready condition message.
- Confirm the OCI Function lifecycle state is active.
- Confirm the application/function were created in `me-jeddah-1`.
- Reconcile again after OCI finishes provisioning the function metadata.

### Function Not Ready So FunctionJob Refuses To Run

Symptoms:

- `FunctionJob.status.phase` is `Failed`.
- The job status says the referenced Function is not Ready.
- No payload invocation occurs.

Checks:

- Run `kubectl describe function managed-hello`.
- Confirm `Ready=True`.
- Confirm `status.functionId` and `status.invokeEndpoint` are populated.
- Recreate the FunctionJob after the Function is Ready.
