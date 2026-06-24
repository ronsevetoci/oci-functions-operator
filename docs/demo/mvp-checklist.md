# MVP Demo Checklist

Use this before a live demo or video recording.

Final operator image:

```text
ghcr.io/ronsevet/oci-functions-operator/controller:mvp-events-functionevents-v1
```

## Local Repository

- [ ] `git status --short` only shows intentional handoff changes.
- [ ] Generated CRDs are current.
- [ ] Helm chart CRDs are synced from `config/crd/bases`.
- [ ] The final operator image tag is used in README and demo/install docs.
- [ ] No OKE install doc tells users to apply Kustomize overlays for the supported path.

Run:

```sh
make generate
make manifests
make test
make helm-chart
helm lint charts/oci-functions-operator
helm template oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --set oci.region=me-jeddah-1 \
  --include-crds
git diff --check
```

## Cluster And Helm

- [ ] `kubectl config current-context` points at the demo OKE cluster.
- [ ] `helm version` succeeds.
- [ ] The operator namespace is either absent or ready to be reused: `oci-functions-operator-system`.
- [ ] If upgrading an existing release after API changes, CRDs were refreshed first.

CRD upgrade reminder:

```sh
kubectl apply -f charts/oci-functions-operator/crds/
```

## OCI And IAM

- [ ] OKE Workload Identity is enabled for the cluster.
- [ ] IAM policy conditions match namespace `oci-functions-operator-system`.
- [ ] IAM policy conditions match service account `oci-functions-operator-controller-manager`.
- [ ] IAM policy conditions match the OKE cluster OCID.
- [ ] Workload can inspect compartments in tenancy.
- [ ] Workload can manage `functions-family` in the function compartment.
- [ ] Workload can use `virtual-network-family` in the function network compartment.
- [ ] Workload can manage `cloudevents-rules` in the Events rule compartment.
- [ ] `eventrule` principal can use `fn-invocation` in the function compartment.
- [ ] Policies use `functions-family`, not `function-family`.
- [ ] No permanent `manage all-resources` policy is being used for the demo.

## Network And Images

- [ ] The operator image is reachable by OKE:
  `ghcr.io/ronsevet/oci-functions-operator/controller:mvp-events-functionevents-v1`.
- [ ] The function runtime image is Fn-compatible and in same-region Jeddah OCIR.
- [ ] The function runtime image tag exists, for example `jed.ocir.io/<TENANCY_NAMESPACE>/hello-function:fn-v1`.
- [ ] The OCI Functions application subnet has a route to Oracle Services Network/OCIR.
- [ ] Subnet security lists allow HTTPS egress.
- [ ] If `nsgIds` are used, each NSG allows egress TCP 443 to Oracle Services Network/OCIR.
- [ ] The function image architecture matches the OCI Functions application shape.

## Demo Resources

- [ ] `COMPARTMENT_OCID` is set.
- [ ] `SUBNET_OCID` is set.
- [ ] `NSG_OCID` is set, or the runbook manifest is adjusted to omit `nsgIds`.
- [ ] `FUNCTION_IMAGE` points to Jeddah OCIR.
- [ ] `BUCKET_NAME` is set for the optional OCI Events trigger segment.
- [ ] Object Storage bucket events are enabled if showing the OCI Events segment.
- [ ] No stale demo objects exist:

```sh
kubectl delete functionevent order-created-abc123 --ignore-not-found
kubectl delete functioneventtrigger order-created-trigger --ignore-not-found
kubectl delete functioneventtrigger object-created-trigger --ignore-not-found
kubectl delete functionjob managed-hello-job --ignore-not-found
kubectl delete function managed-hello --ignore-not-found
```

## Expected Demo States

- [ ] Operator deployment rolls out successfully.
- [ ] All four CRDs exist: `Function`, `FunctionJob`, `FunctionEventTrigger`, `FunctionEvent`.
- [ ] `managed-hello` reaches `Ready=True`.
- [ ] `managed-hello` has `status.applicationId`, `status.functionId`, and `status.invokeEndpoint`.
- [ ] `managed-hello-job` reaches `Succeeded`.
- [ ] `managed-hello-job` shows `SUCCEEDED=2` and `FAILED=0`.
- [ ] `order-created-trigger` reaches `Ready` without an OCI Events Rule ID.
- [ ] `order-created-abc123` reaches `Processed`.
- [ ] Optional `object-created-trigger` reaches `Ready` with an OCI Events Rule OCID.

## Fast Failure Checks

- [ ] Manager pod is not `ImagePullBackOff`.
- [ ] Manager logs do not mention missing `OCI_RESOURCE_PRINCIPAL_REGION`.
- [ ] Function status does not show IAM errors.
- [ ] FunctionJob status does not show `FunctionInvokeImageNotAvailable`.
- [ ] FunctionEventTrigger status does not show `404 NotAuthorizedOrNotFound`.
- [ ] CRD apply does not fail with CEL validation cost errors.
