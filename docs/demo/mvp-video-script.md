# MVP Video Script

Target length: 5-7 minutes.

Final operator image:

```text
ghcr.io/ronsevet/oci-functions-operator/controller:mvp-events-functionevents-v1
```

## 0:00-0:45 - Opening

Today I am showing the MVP of the OCI Functions Operator for OKE.

The goal is simple: keep OCI Functions as OCI Functions, but expose the management and invocation workflow through Kubernetes-native resources.

The MVP has four CRDs:

- `Function` represents either an existing OCI Function or a managed OCI Functions application/function.
- `FunctionJob` invokes a referenced Function with payload fanout, retries, and per-payload status.
- `FunctionEventTrigger` connects events to a Function. It can create OCI Events Rules for OCI service events, or route Kubernetes-native `functionevent.*` events.
- `FunctionEvent` lets an application inside Kubernetes emit an event object that the operator routes directly to matching triggers.

## 0:45-1:45 - Deployment

This deployment uses Helm, which is the supported OKE install path.

The chart configures OCI mode and OKE Workload Identity by default. There is no local OCI config file, no PEM key Secret, and no Resource Principal environment setup.

Run:

```sh
helm upgrade --install oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --create-namespace \
  --set image.repository=ghcr.io/ronsevet/oci-functions-operator/controller \
  --set image.tag=mvp-events-functionevents-v1 \
  --set oci.region=me-jeddah-1
```

Then show:

```sh
kubectl -n oci-functions-operator-system rollout status deployment/oci-functions-operator-controller-manager
kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com functioneventtriggers.functions.oci.oracle.com functionevents.functions.oci.oracle.com
```

Narration note: Helm fresh install installs CRDs, but Helm upgrade does not upgrade CRDs. Before upgrades after API changes, run `kubectl apply -f charts/oci-functions-operator/crds/`.

## 1:45-3:00 - Managed Function

Now create a managed `Function`.

This is where the operator proves the v1 lifecycle path: it creates or updates an OCI Functions application and function, attaches the configured subnet and optional NSGs, points at a same-region OCIR image, and waits until the function has an invoke endpoint.

Apply the managed Function manifest from the runbook, then show:

```sh
kubectl wait --for=condition=Ready function/managed-hello --timeout=10m
kubectl get functions
kubectl describe function managed-hello
```

Call out the fields:

- `status.applicationId`
- `status.functionId`
- `status.invokeEndpoint`
- `Ready=True`

Narration note: the function runtime image is not the operator image. It must be a Fn-compatible image in same-region OCIR, and the Functions application subnet and NSG must allow HTTPS egress to OCIR.

## 3:00-4:00 - FunctionJob Invocation

Next, submit a `FunctionJob`.

This is the Kubernetes-native invocation API. The job references the `Function`, carries inline JSON payloads, controls parallelism and retry limit, and records aggregate and per-payload status.

Run:

```sh
kubectl apply -f config/samples/functions_v1alpha1_functionjob_managed.yaml
kubectl get functionjobs -w
kubectl describe functionjob managed-hello-job
```

Expected result:

```text
NAME                PHASE       SUCCEEDED   FAILED   AGE
managed-hello-job   Succeeded   2           0        20s
```

Call out that `FunctionJob` will not invoke until the referenced `Function` is Ready.

## 4:00-5:15 - Kubernetes-Native FunctionEvent

Now show the new Kubernetes-native event path.

First create a `FunctionEventTrigger` that listens for `functionevent.order.created`. This trigger does not create an OCI Events Rule; it is just a Kubernetes-native route to the same `Function`.

```sh
kubectl apply -f config/samples/functions_v1alpha1_functioneventtrigger_order_created.yaml
kubectl get functioneventtriggers
```

Then emit a `FunctionEvent`:

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

Narration note: this gives apps inside Kubernetes a small, explicit event object without adding schedules, watches, queues, or a workflow engine yet.

## 5:15-6:15 - OCI Events Trigger

Finally, show the OCI Events-backed trigger shape.

For OCI service events such as Object Storage object creation, `FunctionEventTrigger` creates an OCI Events Rule with a FAAS action targeting `Function.status.functionId`.

Show the Object Storage trigger manifest from the runbook:

```sh
kubectl get functioneventtrigger object-created-trigger
kubectl describe functioneventtrigger object-created-trigger
```

Expected:

```text
NAME                     PHASE   RULE ID                              FUNCTION        AGE
object-created-trigger   Ready   ocid1.eventrule.oc1.me-jeddah-1...   managed-hello   30s
```

Narration note: the IAM policy has two sides. The operator workload manages Events rules, and the `eventrule` principal must be allowed to use `fn-invocation` in the function compartment.

## 6:15-7:00 - Close

This MVP proves the core product loop:

- manage a Function from Kubernetes,
- invoke it as a Kubernetes job-style resource,
- connect OCI Events to Functions,
- and emit Kubernetes-native FunctionEvents without leaving the cluster API.

The intentional boundary is also important. This is not a workflow engine yet, does not implement schedules, and does not pretend OCI Functions are Pods. It is a small, testable foundation for OKE-native function operations.
