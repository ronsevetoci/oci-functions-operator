# Debugging Functions

Use this checklist when a managed `Function` or `FunctionJob` does not behave as expected.

## Operator Image Pulls

`controller:latest` in the base manager manifest is a scaffold placeholder. For OKE:

1. Build and push the operator/controller image to a registry OKE can pull.
2. Set the deployment image:

```sh
kubectl -n oci-functions-operator-system set image deployment/oci-functions-operator-controller-manager manager="$OPERATOR_IMAGE"
```

GHCR is acceptable for the operator image if OKE can pull it.

## Stale CRDs

If `kubectl apply` reports unknown fields or current status/spec fields do not appear, refresh the CRDs:

```sh
make manifests
kubectl apply -k config/crd
```

An error around a current field such as `status.functionId` usually means the cluster still has an older CRD schema.

## Function Runtime Images

The function runtime image is separate from the operator image. It must be:

- an OCI Functions-compatible Fn image,
- stored in same-region OCIR,
- tagged with an existing path/tag, and
- built for the OCI Functions application shape, such as `linux/amd64` for `GENERIC_X86`.

For Jeddah, use `jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:<tag>`.

Do not use GHCR for the function runtime image.

## FunctionInvokeImageNotAvailable

`FunctionInvokeImageNotAvailable: Failed to pull function image` can be caused by:

- image tag missing,
- wrong registry region,
- private OCIR repository missing read permission for the Functions application principal,
- subnet route table missing Service Gateway, NAT, internet, or another valid outbound path to OCIR,
- NSG egress missing,
- image not Fn-compatible, or
- architecture mismatch.

Public OCIR repositories usually avoid normal repo-read IAM for public pulls, but public visibility does not solve subnet or NSG egress. If `spec.config.nsgIds` attaches NSGs to the Functions application, those NSGs must allow egress TCP 443 to Oracle Services Network/OCIR.

## Function Not Ready

`FunctionJob` will not invoke until the referenced `Function` is Ready.

Check:

```sh
kubectl describe function <name>
kubectl get function <name> -o yaml
```

Managed Functions should have:

- `status.applicationId`,
- `status.functionId`,
- `status.invokeEndpoint`, and
- `Ready=True`.

## Workload Identity

For OKE:

- `INVOKER_MODE=oci`
- `OCI_AUTH_MODE=workload`
- `OCI_RESOURCE_PRINCIPAL_VERSION=2.2`
- `OCI_RESOURCE_PRINCIPAL_REGION=<region>`

The manager should not mount a local OCI config file, PEM key, or `oci-functions-operator-oci-config` credential Secret in the OKE path.

Confirm IAM policy matches the namespace, service account, OKE cluster OCID, Functions compartment, and network compartments used by the managed application.
