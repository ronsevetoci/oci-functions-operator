# Function Workflows

Function workflows are the v2 foundation for Kubernetes-native orchestration around OCI Functions. This first alpha is intentionally small: it runs a static DAG by creating child `FunctionJob` resources and aggregating their status.

## Resources

`FunctionWorkflow` is the reusable DAG template. It contains named nodes. Each node chooses exactly one function source:

- `functionRef`: reference an existing `Function` in the same namespace.
- `function`: inline `Function.spec` used by the workflow run controller to create a child `Function`.

Use `functionRef` when the `Function` lifecycle is managed separately:

```yaml
spec:
  nodes:
  - name: prepare
    functionRef:
      name: existing-hello
    payload:
      step: prepare
  - name: process
    functionRef:
      name: existing-hello
    dependsOn:
    - prepare
```

Use inline `function` when the workflow should create the `Function` for the run:

```yaml
spec:
  nodes:
  - name: prepare
    function:
      mode: Managed
      config:
        region: me-jeddah-1
        compartmentId: <COMPARTMENT_OCID>
        applicationName: oke-functions-operator-workflow
        subnetIds:
        - <SUBNET_OCID>
        # nsgIds:
        # - <NSG_OCID>
        displayName: managed-inline-prepare
        image: jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:<tag>
        memoryInMBs: 256
        timeoutInSeconds: 120
    payload:
      step: prepare
```

`FunctionWorkflowRun` is one execution of a workflow:

```yaml
spec:
  workflowRef:
    name: hello-workflow
```

The run controller reads the referenced workflow, validates the node graph, creates child `Function` resources for inline nodes, creates child `FunctionJob` resources for eligible ready nodes, and writes aggregate status to `FunctionWorkflowRun.status`.

## Execution Model

- A node with no `dependsOn` can start first.
- A node starts only after all dependencies complete successfully.
- A `functionRef` node creates its child `FunctionJob` as soon as dependencies complete.
- An inline `function` node first creates a deterministic child `Function`, waits for `Ready=True`, then creates its child `FunctionJob`.
- Child `Function` and `FunctionJob` resources are owned by the `FunctionWorkflowRun`.
- Node `payload` is passed to that child job as a single `spec.payload` item.
- Node `parallelism` and `retryLimit` are passed through to the child job.
- Child Function names and child job names are deterministic, so repeated reconciles do not create duplicate resources.
- If a node job fails, dependent nodes are skipped and the run fails with a clear condition.

Use `kubectl describe functionworkflowrun <name>` or inspect YAML to see node-level status, `functionRef` or `childFunctionRef`, child job references, start times, completion times, and failure messages.

## Inline Function Requirements

Inline `function` uses the existing `Function` CRD and `FunctionReconciler`; the workflow controller does not implement OCI lifecycle logic itself. The child `Function` has an owner reference to the `FunctionWorkflowRun`, so deleting the run allows Kubernetes garbage collection to remove child `Function` and `FunctionJob` resources. OCI-side deletion/finalizers are still not implemented by this operator.

Inline managed Functions have the same requirements as standalone managed Functions:

- The manager must run in OCI mode with OKE Workload Identity and IAM permissions for Functions and network resources.
- The function runtime image must be an OCI Functions-compatible Fn image in same-region OCIR, such as `jed.ocir.io/...` for Jeddah.
- The Functions application subnet must route to Oracle Services Network/OCIR.
- If `nsgIds` are set, attached NSGs must allow egress TCP 443 to Oracle Services Network/OCIR.
- Public OCIR repositories still require network egress from the Functions application.

The sample inline managed workflow uses placeholders and is not included in `config/samples/kustomization.yaml`:

```sh
kubectl apply -f config/samples/functions_v1alpha1_functionworkflow_inline_managed.yaml
kubectl apply -f config/samples/functions_v1alpha1_functionworkflowrun_inline_managed.yaml
```

## Current Alpha Limits

- Static DAGs only.
- No expression language.
- No `foreach`.
- No output passing between nodes.
- No schedules.
- No `FunctionTrigger` yet.
- No OCI Events, Queue, Streaming, Object Storage, or Kubernetes resource-watch triggers yet.
- Workflow nodes rely on existing `FunctionJob` behavior for invocation, retries, and per-payload status.
- Inline Functions rely on existing `Function` behavior for OCI lifecycle and readiness.

## Run The Sample

Install current CRDs and run the manager in fake mode:

```sh
make generate
make manifests
kubectl apply -k config/crd
INVOKER_MODE=fake go run ./cmd
```

Apply the safe sample set from another terminal:

```sh
kubectl apply -k config/samples
kubectl get functionworkflowruns -w
```

Expected `kubectl get` output after the sample completes:

```text
NAME                 PHASE      NODES   COMPLETE   FAILED   AGE
hello-workflow-run   Complete   3       3          0        30s
```

Inspect the workflow run and child jobs:

```sh
kubectl describe functionworkflowrun hello-workflow-run
kubectl get functionjobs
kubectl get functionworkflowrun hello-workflow-run -o yaml
```

`kubectl describe functionworkflowrun hello-workflow-run` should show `Running`, `Complete`, and `Failed` conditions, node status entries with child `FunctionJob` references, and events similar to:

```text
Type    Reason                    Message
Normal  WorkflowStarted           FunctionWorkflowRun started workflow "hello-workflow".
Normal  NodeFunctionJobCreated    Node "prepare" created FunctionJob "hello-workflow-run-prepare".
Normal  NodeCompleted             Node "prepare" completed.
Normal  NodeFunctionJobCreated    Node "process" created FunctionJob "hello-workflow-run-process".
Normal  NodeCompleted             Node "process" completed.
Normal  NodeFunctionJobCreated    Node "notify" created FunctionJob "hello-workflow-run-notify".
Normal  NodeCompleted             Node "notify" completed.
Normal  WorkflowComplete          FunctionWorkflowRun completed: 3 node(s) completed successfully.
```

The sample uses this three-node DAG:

- `prepare`
- `process`, which depends on `prepare`
- `notify`, which depends on `process`

In fake mode, the child `FunctionJob` resources should succeed and the workflow run should eventually reach `status.phase=Complete`.

## Future Direction

The next v2 layer is expected to add `FunctionTrigger` as a separate resource for starting workflow runs from OCI Events, Queue, Object Storage, Kubernetes Events, and Kubernetes resource watches. This change does not implement triggers; it only establishes the workflow template/run API and the child-job execution foundation.
