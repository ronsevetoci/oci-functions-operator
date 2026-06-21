# OCI Functions Operator

Kubernetes-native management and job-style invocation of OCI Functions from OKE.

The operator adds two namespaced CRDs:

- `Function`: references an existing OCI Function or manages an OCI Functions application/function.
- `FunctionJob`: invokes a referenced `Function`, fans out inline JSON payloads, retries failed invocations, and records aggregate/per-payload status.

## Two Images

The operator image and function runtime image are different artifacts:

- Operator/controller image: runs as a Kubernetes Deployment in OKE. It can be in GHCR, OCIR, or any registry OKE can pull from.
- Function runtime image: runs in OCI Functions. It must be an OCI Functions-compatible Fn image in same-region OCIR, for example `jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:<tag>` for Jeddah.

Do not use GHCR for the OCI Functions runtime image. OCI Functions pulls the runtime image from the Functions application network during invocation, so the application subnet/NSG must have egress to Oracle Services Network/OCIR even when the OCIR repository is public.

## Start Here

- [Managed Function demo](docs/managed-function-demo.md): primary OKE walkthrough for managed application/function creation and invocation.
- [Deploy operator to OKE](docs/oke-deployment.md): Workload Identity, IAM, network, and overlay setup.
- [Design overview](docs/design.md): CRDs, controllers, lifecycle, invoker contracts, and limitations.
- [Local existing Function demo](docs/oci-mode-demo.md): local `OCI_AUTH_MODE=config` path against an already-created OCI Function.
- [Debugging Functions](docs/debugging-functions.md): image, CRD, Workload Identity, NSG, and invocation failure checks.
- [Validation notes](docs/validation-notes.md): template for recording real OCI-mode runs.
- [Sample function image](examples/hello-function/README.md): Fn-compatible Python function runtime image for the managed demo.

## Modes

`INVOKER_MODE=fake` is the default. It requires no OCI auth, creates no OCI resources, and is useful only for CRD/controller/status demos.

`INVOKER_MODE=oci` uses the OCI Go SDK:

- On OKE, use `OCI_AUTH_MODE=workload` with OKE Workload Identity.
- For local development only, use `OCI_AUTH_MODE=config` with `OCI_CONFIG_FILE` and `OCI_CONFIG_PROFILE`.

Existing mode requires `spec.functionId` and `spec.invokeEndpoint` on the `Function`. Managed mode uses `spec.config` to create/update the OCI Functions application and function, then writes `status.applicationId`, `status.functionId`, and `status.invokeEndpoint`.

## Local Fake Demo

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

In another terminal:

```sh
scripts/check-demo-prereqs.sh
scripts/demo-fake.sh
```

Fake mode proves only the Kubernetes reconciliation/status path. It does not prove OCI auth, OCI Functions network egress, OCIR image access, or function image compatibility.

## Primary OKE Path

For OKE managed mode:

1. Build and push the operator/controller image to a registry OKE can pull.
2. Build a Fn-compatible function runtime image and push it to same-region OCIR.
3. Deploy the operator with `INVOKER_MODE=oci` and `OCI_AUTH_MODE=workload`.
4. Apply a managed `Function` with `spec.config.region`, `compartmentId`, `applicationName`, `subnetIds`, optional `nsgIds`, and same-region OCIR `image`.
5. Submit a `FunctionJob` after the `Function` is Ready.

See [docs/managed-function-demo.md](docs/managed-function-demo.md) for the full sequence.
