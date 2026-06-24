# MVP Demo Runbook

This runbook is the handoff path for the MVP build of the OCI Functions Operator for OKE.

Final operator image:

```text
ghcr.io/ronsevet/oci-functions-operator/controller:mvp-events-functionevents-v1
```

## Prerequisites

- `kubectl` is configured for the target OKE cluster.
- Helm is installed locally.
- OKE Workload Identity is available for the cluster.
- The service account is `oci-functions-operator-controller-manager` in namespace `oci-functions-operator-system`.
- The final operator image above is reachable by OKE worker nodes.
- A same-region Jeddah OCIR Fn-compatible function image exists, for example `jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:fn-v1`.
- The OCI Functions application subnet has a route to Oracle Services Network/OCIR, usually through a Service Gateway.
- Any NSG attached to the Functions application allows egress TCP 443 to Oracle Services Network/OCIR.
- For the optional OCI Events segment, Object Storage bucket events are enabled on the bucket.

## Required Variables

Set these before the demo:

```sh
export OPERATOR_IMAGE_REPOSITORY="ghcr.io/ronsevet/oci-functions-operator/controller"
export OPERATOR_IMAGE_TAG="mvp-events-functionevents-v1"
export OCI_REGION="me-jeddah-1"

export COMPARTMENT_OCID="<COMPARTMENT_OCID>"
export SUBNET_OCID="<SUBNET_OCID>"
export NSG_OCID="<NSG_OCID>"
export FUNCTION_IMAGE="jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:fn-v1"
export BUCKET_NAME="<BUCKET_NAME>"
```

## CRD Install Note

Helm fresh install installs CRDs, but Helm upgrade does not upgrade CRDs from the chart `crds/` directory. Before upgrading an existing release after API schema changes, run:

```sh
kubectl apply -f charts/oci-functions-operator/crds/
```

Then run the Helm upgrade.

## IAM Policy Summary

Use least-privilege policies scoped to the demo compartments and workload identity. The workload condition must match the OKE namespace, service account, and cluster OCID.

```text
Allow any-user to inspect compartments in tenancy where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to manage cloudevents-rules in compartment <events-rule-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to manage functions-family in compartment <function-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to use virtual-network-family in compartment <function-network-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to use fn-invocation in compartment <function-compartment> where all {request.principal.type = 'eventrule'}
```

Use `functions-family` with the trailing `s`. `function-family` is incorrect. Object Storage event conditions do not require `object-family` permissions for rule creation, but bucket events must be enabled on the bucket.

## Demo Commands

Install or upgrade the operator:

```sh
helm upgrade --install oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --create-namespace \
  --set image.repository="$OPERATOR_IMAGE_REPOSITORY" \
  --set image.tag="$OPERATOR_IMAGE_TAG" \
  --set oci.region="$OCI_REGION"
```

Expected signal:

```text
Release "oci-functions-operator" has been installed or upgraded.
```

Check rollout and CRDs:

```sh
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com functioneventtriggers.functions.oci.oracle.com functionevents.functions.oci.oracle.com
```

Expected:

```text
deployment "oci-functions-operator-controller-manager" successfully rolled out
NAME                                             CREATED AT
functions.functions.oci.oracle.com              ...
functionjobs.functions.oci.oracle.com           ...
functioneventtriggers.functions.oci.oracle.com  ...
functionevents.functions.oci.oracle.com         ...
```

Create the managed `Function`:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: managed-hello
  namespace: default
spec:
  mode: Managed
  config:
    region: ${OCI_REGION}
    compartmentId: ${COMPARTMENT_OCID}
    applicationName: oke-functions-operator-demo
    subnetIds:
    - ${SUBNET_OCID}
    nsgIds:
    - ${NSG_OCID}
    displayName: managed-hello
    image: ${FUNCTION_IMAGE}
    memoryInMBs: 256
    timeoutInSeconds: 120
    config:
      GREETING: "hello from oke functions operator"
EOF
```

Wait for readiness:

```sh
kubectl wait --for=condition=Ready function/managed-hello --timeout=10m
kubectl get functions
kubectl describe function managed-hello
```

Expected:

```text
NAME            PHASE   FUNCTION ID                              READY   AGE
managed-hello   Ready   ocid1.fnfunc.oc1.me-jeddah-1...          True    2m
```

Create a `FunctionJob`:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: managed-hello-job
  namespace: default
spec:
  functionRef:
    name: managed-hello
  payloads:
  - name: Ron
    index: 0
  - name: Ron
    index: 1
  parallelism: 1
  retryLimit: 1
EOF
```

Watch the job:

```sh
kubectl get functionjobs -w
kubectl describe functionjob managed-hello-job
```

Expected:

```text
NAME                PHASE       SUCCEEDED   FAILED   AGE
managed-hello-job   Succeeded   2           0        20s
```

Create a Kubernetes-native `FunctionEventTrigger`:

```sh
kubectl apply -f config/samples/functions_v1alpha1_functioneventtrigger_order_created.yaml
kubectl get functioneventtriggers
```

Expected:

```text
NAME                    PHASE   RULE ID   FUNCTION        AGE
order-created-trigger   Ready             managed-hello   5s
```

Emit a `FunctionEvent`:

```sh
kubectl apply -f config/samples/functions_v1alpha1_functionevent_order_created.yaml
kubectl get functionevents
kubectl describe functionevent order-created-abc123
```

Expected:

```text
NAME                   PHASE       EVENT TYPE                    MATCHED                   AGE
order-created-abc123   Processed   functionevent.order.created   [order-created-trigger]   10s
```

Optional: create an OCI Events-backed Object Storage trigger:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionEventTrigger
metadata:
  name: object-created-trigger
  namespace: default
spec:
  functionRef:
    name: managed-hello
  compartmentId: ${COMPARTMENT_OCID}
  displayName: object-created-trigger
  description: Invoke managed-hello when objects are created
  isEnabled: true
  deletionPolicy: Delete
  condition:
    eventType:
    - com.oraclecloud.objectstorage.createobject
    data:
      additionalDetails:
        bucketName: ${BUCKET_NAME}
EOF
```

Check the rule:

```sh
kubectl get functioneventtrigger object-created-trigger
kubectl describe functioneventtrigger object-created-trigger
```

Expected:

```text
NAME                     PHASE   RULE ID                              FUNCTION        AGE
object-created-trigger   Ready   ocid1.eventrule.oc1.me-jeddah-1...   managed-hello   30s
```

## Cleanup

Delete demo resources:

```sh
kubectl delete functionevent order-created-abc123 --ignore-not-found
kubectl delete functioneventtrigger order-created-trigger --ignore-not-found
kubectl delete functioneventtrigger object-created-trigger --ignore-not-found
kubectl delete functionjob managed-hello-job --ignore-not-found
kubectl delete function managed-hello --ignore-not-found
```

Uninstall the operator:

```sh
helm uninstall oci-functions-operator --namespace oci-functions-operator-system
```

CRDs are intentionally left behind by Helm. Remove them only after deleting custom resources you care about:

```sh
kubectl delete crd functionevents.functions.oci.oracle.com functioneventtriggers.functions.oci.oracle.com functionjobs.functions.oci.oracle.com functions.functions.oci.oracle.com
```

Managed `Function` deletion in this MVP removes the Kubernetes object but does not delete OCI Functions applications or functions. Clean up any demo OCI Functions resources manually if they should not remain.
