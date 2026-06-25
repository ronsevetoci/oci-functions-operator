# MVP Demo Flow

Primary demo image:

```text
ghcr.io/ronsevetoci/oci-functions-operator/controller:v0.1.0
```

This is the primary OKE demo path. Run the commands directly from the sections below.

## 1. Pre-Demo Preparation

Set demo values:

```sh
export OCI_REGION="me-jeddah-1"
export OPERATOR_IMAGE_REPOSITORY="ghcr.io/ronsevetoci/oci-functions-operator/controller"
export OPERATOR_IMAGE_TAG="v0.1.0"
export COMPARTMENT_OCID="<function-compartment-ocid>"
export SUBNET_OCID="<functions-subnet-ocid>"
export NSG_OCID="<functions-nsg-ocid>"
export FUNCTION_IMAGE="jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:fn-v1"
export BUCKET_NAME="<object-storage-bucket-name>"
```

Required IAM policies:

```text
Allow any-user to inspect compartments in tenancy where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to manage functions-family in compartment <function-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to use virtual-network-family in compartment <function-network-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to manage cloudevents-rules in compartment <events-rule-compartment> where all {request.principal.type = 'workload', request.principal.namespace = 'oci-functions-operator-system', request.principal.service_account = 'oci-functions-operator-controller-manager', request.principal.cluster_id = '<oke-cluster-ocid>'}
Allow any-user to use fn-invocation in compartment <function-compartment> where all {request.principal.type = 'eventrule'}
```

Use `functions-family`, not `function-family`. Object Storage rule creation does not require `object-family`, but bucket events must be enabled.

Mac tool prerequisites:

```sh
kubectl version --client
helm version
oci --version
fn --version
docker version
```

OCI CLI auth:

```sh
oci iam region list --output table
oci ce cluster get --cluster-id "<oke-cluster-ocid>" --region "$OCI_REGION"
```

Optional function image build/push:

```sh
cd examples/hello-function
fn build
fn push --registry "jed.ocir.io/<TENANCY_NAMESPACE>"
```

## 2. Install Operator With Helm

Apply CRDs first. This is safe on fresh install and required before upgrades when CRDs changed:

```sh
kubectl apply -f charts/oci-functions-operator/crds/
```

Install or upgrade:

```sh
helm upgrade --install oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --create-namespace \
  --set image.repository="$OPERATOR_IMAGE_REPOSITORY" \
  --set image.tag="$OPERATOR_IMAGE_TAG" \
  --set oci.region="$OCI_REGION"
```

Verify:

```sh
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
kubectl get crd functionapplications.functions.oci.oracle.com functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com functioneventtriggers.functions.oci.oracle.com functionevents.functions.oci.oracle.com
kubectl -n oci-functions-operator-system get deploy oci-functions-operator-controller-manager -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

Expected: rollout succeeds, all five CRDs exist, and the image ends with `:v0.1.0`.

## 3. Use Case 1: FunctionApplication And Function Manage OCI Resources

Create the OCI Functions Application explicitly:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionApplication
metadata:
  name: managed-hello-app
  namespace: default
spec:
  mode: Managed
  deletionPolicy: Retain
  region: ${OCI_REGION}
  compartmentId: ${COMPARTMENT_OCID}
  displayName: oke-functions-operator-demo
  subnetIds:
  - ${SUBNET_OCID}
  nsgIds:
  - ${NSG_OCID}
  config:
    APP_GREETING: "hello from application config"
EOF

kubectl wait --for=condition=Ready functionapplication/managed-hello-app --timeout=10m
kubectl get functionapplications
kubectl describe functionapplication managed-hello-app
```

Expected: `managed-hello-app` reaches `PHASE=Ready`, `Ready=True`, and has `status.applicationId`.

Create the OCI Function inside that application:

```sh
cat <<EOF | kubectl apply -f -
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: managed-hello
  namespace: default
spec:
  mode: Managed
  deletionPolicy: Retain
  applicationRef:
    name: managed-hello-app
  config:
    displayName: managed-hello
    image: ${FUNCTION_IMAGE}
    memoryInMBs: 256
    timeoutInSeconds: 120
    config:
      GREETING: "hello from oke functions operator"
EOF

kubectl wait --for=condition=Ready function/managed-hello --timeout=10m
kubectl get functions
kubectl describe function managed-hello
```

Expected: `managed-hello` reaches `PHASE=Ready`, `Ready=True`, and has `status.applicationId`, `status.functionId`, and `status.invokeEndpoint`.

## 4. Use Case 2: FunctionJob Invokes Function

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

kubectl get functionjobs -w
kubectl describe functionjob managed-hello-job
```

Expected: `managed-hello-job` reaches `PHASE=Succeeded`, `SUCCEEDED=2`, `FAILED=0`.

## 5. Use Case 3: FunctionEventTrigger Creates OCI Events Rule For Object Storage CreateObject

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

kubectl get functioneventtrigger object-created-trigger
kubectl describe functioneventtrigger object-created-trigger
```

Expected: `object-created-trigger` reaches `PHASE=Ready` and has an OCI Events `RULE ID`.

## 6. Use Case 4: FunctionEvent CRD Emits Kubernetes-Native Event And Invokes Function

```sh
kubectl apply -f config/samples/functions_v1alpha1_functioneventtrigger_order_created.yaml
kubectl get functioneventtriggers

kubectl apply -f config/samples/functions_v1alpha1_functionevent_order_created.yaml
kubectl get functionevents
kubectl describe functionevent order-created-abc123
```

Expected: `order-created-trigger` reaches `Ready` without a Rule ID, and `order-created-abc123` reaches `Processed`.

## 7. Prove Invocation In OCI Console, Logs, Or Metrics

Kubernetes checks:

```sh
kubectl get function managed-hello -o jsonpath='{.status.functionId}{"\n"}'
kubectl get functionjob managed-hello-job -o yaml
kubectl get functionevent order-created-abc123 -o yaml
kubectl get events --sort-by=.lastTimestamp
```

OCI checks:

- Open Developer Services, Functions, Applications, `oke-functions-operator-demo`.
- Open function `managed-hello` and check invocation metrics.
- Check function logs if logging is enabled.
- Open Events Service, Rules, and confirm `object-created-trigger` targets the same function OCID.
- Upload an object to `${BUCKET_NAME}` and check Function metrics/logs for a new invocation.

## 8. Cleanup Notes

```sh
kubectl delete functionevent order-created-abc123 --ignore-not-found
kubectl delete functioneventtrigger order-created-trigger --ignore-not-found
kubectl delete functioneventtrigger object-created-trigger --ignore-not-found
kubectl delete functionjob managed-hello-job --ignore-not-found
kubectl delete function managed-hello --ignore-not-found
kubectl delete functionapplication managed-hello-app --ignore-not-found

helm uninstall oci-functions-operator --namespace oci-functions-operator-system
```

Helm leaves CRDs behind by design. Managed `Function` and `FunctionApplication` deletion defaults to `deletionPolicy: Retain`, so Kubernetes objects are removed and OCI resources stay in place. To let the operator delete the managed OCI Function during cleanup, set `Function.spec.deletionPolicy: Delete` before deleting the `Function`. To let the operator delete the OCI Application, set `FunctionApplication.spec.deletionPolicy: Delete`; deletion is blocked and retried if functions still remain in the application.

## 9. Short Talk Track For Each Section

- Preparation: "We confirm IAM, tools, auth, image, and network before touching the operator."
- Install: "Helm is the OKE install path; CRDs are applied explicitly for upgrade safety."
- FunctionApplication: "`FunctionApplication` maps to the real OCI Functions Application and owns subnet, NSG, and app-level config."
- Function: "A Kubernetes `Function` maps to the OCI Function inside that application."
- FunctionJob: "A Kubernetes `FunctionJob` invokes the Function and shows payload-level status."
- OCI Events trigger: "`FunctionEventTrigger` creates an OCI Events Rule for Object Storage createobject."
- FunctionEvent: "`FunctionEvent` gives in-cluster apps a Kubernetes-native event emission path."
- Proof: "Kubernetes status shows reconciliation; OCI Console metrics/logs show real invocation."
- Cleanup: "Delete Kubernetes objects first, then clean up OCI resources deliberately."
