# Function Workflows

Function workflows are the v2 foundation for Kubernetes-native orchestration around OCI Functions. This first alpha is intentionally small: it runs a static DAG by creating child `FunctionJob` resources and aggregating their status.

## Resources

`FunctionWorkflow` is the reusable DAG template. It contains named nodes, and each node references a `Function` in the same namespace:

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

`FunctionWorkflowRun` is one execution of a workflow:

```yaml
spec:
  workflowRef:
    name: hello-workflow
```

The run controller reads the referenced workflow, validates the node graph, creates child `FunctionJob` resources for eligible nodes, and writes aggregate status to `FunctionWorkflowRun.status`.

## Execution Model

- A node with no `dependsOn` can start first.
- A node starts only after all dependencies complete successfully.
- Each node creates one child `FunctionJob` owned by the `FunctionWorkflowRun`.
- Node `payload` is passed to that child job as a single `spec.payload` item.
- Node `parallelism` and `retryLimit` are passed through to the child job.
- Child job names are deterministic, so repeated reconciles do not create duplicate jobs.
- If a node job fails, dependent nodes are skipped and the run fails with a clear condition.

Use `kubectl describe functionworkflowrun <name>` or inspect YAML to see node-level status, child job references, start times, completion times, and failure messages.

## Current Alpha Limits

- Static DAGs only.
- No expression language.
- No `foreach`.
- No output passing between nodes.
- No schedules.
- No `FunctionTrigger` yet.
- No OCI Events, Queue, Streaming, Object Storage, or Kubernetes resource-watch triggers yet.
- Workflow nodes rely on existing `FunctionJob` behavior for invocation, retries, and per-payload status.

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

Inspect the workflow run and child jobs:

```sh
kubectl describe functionworkflowrun hello-workflow-run
kubectl get functionjobs
kubectl get functionworkflowrun hello-workflow-run -o yaml
```

The sample uses this three-node DAG:

- `prepare`
- `process`, which depends on `prepare`
- `notify`, which depends on `process`

In fake mode, the child `FunctionJob` resources should succeed and the workflow run should eventually reach `status.phase=Complete`.

## Future Direction

The next v2 layer is expected to add `FunctionTrigger` as a separate resource for starting workflow runs from OCI Events, Queue, Object Storage, Kubernetes Events, and Kubernetes resource watches. This change does not implement triggers; it only establishes the workflow template/run API and the child-job execution foundation.
