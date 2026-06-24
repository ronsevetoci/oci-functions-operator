# Helm Install

Helm is the recommended OKE deployment path for the OCI Functions Operator. The chart packages CRDs, RBAC, service account, deployment settings, metrics service, image values, and OCI Workload Identity environment defaults in one place.

The chart lives at:

```sh
charts/oci-functions-operator
```

## Defaults

The default values target OKE with Workload Identity:

```yaml
oci:
  invokerMode: oci
  authMode: workload
  resourcePrincipalVersion: "2.2"
  resourcePrincipalRegion: ""

serviceAccount:
  create: true
  name: oci-functions-operator-controller-manager
```

The chart does not mount OCI config files, PEM keys, developer credentials, or a global invoke endpoint. Function invoke endpoints come from each `Function` resource.

Set `oci.resourcePrincipalRegion` when your OKE Workload Identity environment requires an explicit region, for example `me-jeddah-1`.

## Install

```sh
helm install oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --create-namespace \
  --set image.repository=ghcr.io/ronsevet/oci-functions-operator/controller \
  --set image.tag=<tag>
```

If needed, include the OKE region:

```sh
helm upgrade --install oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --create-namespace \
  --set image.repository=ghcr.io/ronsevet/oci-functions-operator/controller \
  --set image.tag=<tag> \
  --set oci.resourcePrincipalRegion=me-jeddah-1
```

## Upgrade

```sh
helm upgrade oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system \
  --set image.tag=<new-tag>
```

Helm installs chart CRDs on first install, but Helm does not automatically upgrade CRDs from the `crds/` directory during a normal `helm upgrade`. After API schema changes, apply CRDs deliberately:

```sh
kubectl apply -f charts/oci-functions-operator/crds/
```

Then run the Helm upgrade.

## Uninstall

```sh
helm uninstall oci-functions-operator \
  --namespace oci-functions-operator-system
```

Helm uninstall removes namespaced chart resources, ClusterRoles, and bindings, but CRDs installed from `crds/` are intentionally left behind by Helm. Remove CRDs manually only after deleting custom resources you care about.

## Image Values

```yaml
image:
  repository: ghcr.io/ronsevet/oci-functions-operator/controller
  tag: ""
  pullPolicy: IfNotPresent
```

When `image.tag` is empty, the chart uses `Chart.appVersion`, currently `latest`.

## Service Account And Workload Identity

The default service account is:

```text
oci-functions-operator-controller-manager
```

Your OCI IAM Workload Identity policy must match the deployed namespace and service account. With the install command above, that means:

```text
namespace = oci-functions-operator-system
service_account = oci-functions-operator-controller-manager
```

Add service account annotations if your environment needs them:

```yaml
serviceAccount:
  annotations:
    example.com/key: value
```

## Extra Environment

Use `extraEnv` for additional literal values or Kubernetes `valueFrom` entries:

```yaml
extraEnv:
- name: OCI_RESOURCE_PRINCIPAL_REGION
  value: me-jeddah-1
```

Do not define the same environment variable twice. The chart's built-in OCI env vars use `value`, not `valueFrom`, so the rendered defaults avoid Kubernetes errors such as:

```text
env[0].valueFrom may not be specified when value is not empty
```

## Validate Rendering

```sh
helm lint charts/oci-functions-operator

helm template oci-functions-operator charts/oci-functions-operator \
  --namespace oci-functions-operator-system
```

Development helpers:

```sh
make helm-chart
make helm-template
```

`make helm-chart` refreshes chart CRDs from `config/crd/bases`.

Check installed permissions after deployment:

```sh
kubectl auth can-i get functions.functions.oci.oracle.com \
  --as=system:serviceaccount:oci-functions-operator-system:oci-functions-operator-controller-manager

kubectl auth can-i update functions.functions.oci.oracle.com/status \
  --as=system:serviceaccount:oci-functions-operator-system:oci-functions-operator-controller-manager

kubectl auth can-i get functionjobs.functions.oci.oracle.com \
  --as=system:serviceaccount:oci-functions-operator-system:oci-functions-operator-controller-manager

kubectl auth can-i get functioneventtriggers.functions.oci.oracle.com \
  --as=system:serviceaccount:oci-functions-operator-system:oci-functions-operator-controller-manager
```

## Troubleshooting

ImagePullBackOff:

- Confirm `image.repository` and `image.tag`.
- Confirm OKE nodes can pull from the image registry.
- Set `imagePullSecrets` if the operator image is private.

Missing CRDs:

- Confirm Helm installed the CRDs:
  `kubectl get crd functions.functions.oci.oracle.com functionjobs.functions.oci.oracle.com functioneventtriggers.functions.oci.oracle.com`
- If CRDs were skipped or removed, apply:
  `kubectl apply -f charts/oci-functions-operator/crds/`

Stale CRDs after API changes:

- Helm does not upgrade `crds/` entries during normal upgrades.
- Apply the chart CRDs with `kubectl apply -f charts/oci-functions-operator/crds/`, then run `helm upgrade`.

Missing RBAC:

- Confirm `helm template` contains the ClusterRole rules for `functions`, `functionjobs`, `functioneventtriggers`, their `status` and `finalizers`, and core `events`.
- Re-run the Helm upgrade if the ClusterRole drifted.

Wrong service account in IAM policy:

- Compare `kubectl -n oci-functions-operator-system get deploy oci-functions-operator-controller-manager -o jsonpath='{.spec.template.spec.serviceAccountName}'` with the IAM policy condition.
- The namespace and service account in OCI IAM must match the chart release.

`OCI_AUTH_MODE` accidentally set to `config` in OKE:

- OKE should normally use `oci.authMode=workload`.
- Check rendered env:
  `kubectl -n oci-functions-operator-system get deploy oci-functions-operator-controller-manager -o yaml`
- Do not mount local OCI config files or PEM keys into the OKE deployment.
