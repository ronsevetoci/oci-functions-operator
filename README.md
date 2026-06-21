# OCI Functions Operator

Kubernetes operator for managing and invoking OCI Functions through Kubernetes-native APIs.

## Documentation

- [Design overview](docs/design.md)
- [OKE deployment guide](docs/oke-deployment.md)
- [Managed Function demo](docs/managed-function-demo.md)
- [OCI mode end-to-end demo](docs/oci-mode-demo.md)

## Invoker Modes

The manager selects its function invoker with the `INVOKER_MODE` environment variable.

Supported values:

- `fake`: local deterministic invoker. This is the default.
- `oci`: OCI Go SDK-backed lifecycle and invocation for OCI Functions.

When `INVOKER_MODE=oci`, `OCI_AUTH_MODE` selects OCI SDK authentication:

- `workload`: OKE Workload Identity. This is the default for OCI mode.
- `config`: OCI config file/profile auth for local development only.

## Demo Image

The current pushed demo image is:

```sh
ghcr.io/ronsevetoci/oci-functions-operator/controller:dev
```

Use it as `OPERATOR_IMAGE` when following the [OKE deployment guide](docs/oke-deployment.md).

## Run Locally With Fake Mode

Install or refresh generated manifests:

```sh
make generate
make manifests
kubectl apply -k config/crd
```

Run the manager against your current kubeconfig:

```sh
INVOKER_MODE=fake go run ./cmd
```

In another terminal, create sample resources:

```sh
kubectl apply -f config/samples/functions_v1alpha1_function_existing.yaml
kubectl apply -f config/samples/functions_v1alpha1_functionjob.yaml
kubectl get functions,functionjobs
```

`INVOKER_MODE` can be omitted for local development because `fake` is the default.

You can also run the safe fake-mode demo helper:

```sh
scripts/check-demo-prereqs.sh
scripts/demo-fake.sh
```

## Run With OCI Mode

OCI mode defaults to OKE Workload Identity when `OCI_AUTH_MODE` is unset. For local development outside OKE, set `OCI_AUTH_MODE=config` and use OCI config-file auth:

- The OCI config file defaults to `$HOME/.oci/config`.
- `OCI_CONFIG_FILE` can point at a different config file.
- `OCI_CONFIG_PROFILE` can select a non-`DEFAULT` profile.
- The selected config/profile must have permission to manage or invoke the target OCI Function resources.

Example environment:

```sh
export INVOKER_MODE=oci
export OCI_AUTH_MODE=config
export OCI_CONFIG_FILE="$HOME/.oci/config"
export OCI_CONFIG_PROFILE=DEFAULT
```

Then run:

```sh
go run ./cmd
```

Existing-mode `Function` resources point at an existing OCI Function OCID and carry that function's invoke endpoint in `spec.invokeEndpoint`:

```yaml
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: existing-hello
spec:
  mode: Existing
  functionId: ocid1.fnfunc.oc1.iad.exampleuniqueid
  invokeEndpoint: https://functions.us-ashburn-1.oci.oraclecloud.com
```

Managed-mode `Function` resources declare desired OCI Functions state. The controller creates or updates the OCI application/function, then writes `status.functionId` and `status.invokeEndpoint`:

```yaml
apiVersion: functions.oci.oracle.com/v1alpha1
kind: Function
metadata:
  name: managed-hello
spec:
  mode: Managed
  config:
    region: me-jeddah-1
    compartmentId: ocid1.compartment.oc1..exampleuniqueid
    applicationName: oci-functions-operator-demo
    subnetIds:
    - ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid
    displayName: managed-hello
    image: me-jeddah-1.ocir.io/example/functions/hello:latest
    memoryInMBs: 128
    timeoutInSeconds: 30
```

Create a `FunctionJob` that references the `Function` and supplies inline JSON payloads:

```yaml
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionJob
metadata:
  name: hello-job
spec:
  functionRef:
    name: existing-hello
  payloads:
  - message: hello from kubernetes
    requestId: sample-001
  parallelism: 1
  retryLimit: 2
```

The controller records the OCI `Fn-Call-Id` header when present, otherwise it uses the OCI `opc-request-id`, in each payload status `invocationId`.

For a complete walkthrough, see [docs/oci-mode-demo.md](docs/oci-mode-demo.md).
