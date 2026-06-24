# Function Event Triggers

`FunctionEventTrigger` declaratively manages an OCI Events Rule that invokes an OCI Function referenced by this operator's `Function` CRD.

This is OCI Events to OCI Functions invocation. It is not a Kubernetes watch trigger, not `FunctionJob` fanout, and not DAG/workflow orchestration.

## Resource Model

Create or reference a `Function` first. The trigger controller waits until that `Function` has:

- `Ready=True`
- `status.functionId` populated

Then it creates or updates an OCI Events Rule in `spec.compartmentId` with a FAAS action targeting that function OCID.

```yaml
apiVersion: functions.oci.oracle.com/v1alpha1
kind: FunctionEventTrigger
metadata:
  name: object-created-trigger
spec:
  functionRef:
    name: managed-hello
  compartmentId: <COMPARTMENT_OCID>
  displayName: object-created-trigger
  description: Invoke managed-hello when objects are created
  isEnabled: true
  deletionPolicy: Delete
  condition:
    eventType:
    - com.oraclecloud.objectstorage.createobject
    data:
      additionalDetails:
        bucketName: <BUCKET_NAME>
```

The controller stores the OCI Events Rule OCID in `status.ruleId`.

`condition.eventType` is a Kubernetes list field. For the common single-value case, the controller renders one item as the scalar OCI Events condition value:

```json
{"eventType":"com.oraclecloud.objectstorage.createobject","data":{"additionalDetails":{"bucketName":"my-bucket"}}}
```

Structured `condition.data` also renders single-item arrays as scalar values for compatibility with OCI Events. Multiple event types or multiple values for one data attribute are rejected with `InvalidCondition`; create separate `FunctionEventTrigger` resources for multiple values.

## Raw Conditions

For exact OCI Events condition JSON, use `condition.rawJson` instead of the structured fields:

```yaml
condition:
  rawJson: |
    {
      "eventType": "com.oraclecloud.objectstorage.createobject",
      "data": {
        "additionalDetails": {
          "bucketName": "my-bucket"
        }
      }
    }
```

`rawJson` is mutually exclusive with `eventType` and `data`. It must already be valid OCI Events condition JSON, so use scalar values rather than arrays for single event types or data attributes.

## Deletion Policy

- `deletionPolicy: Delete` is the default. Deleting the Kubernetes trigger deletes the OCI Events Rule.
- `deletionPolicy: Retain` removes the Kubernetes object but leaves the OCI Events Rule in OCI.

## Run The Sample

Edit placeholders in the sample:

```sh
cp config/samples/functions_v1alpha1_functioneventtrigger_object_created.yaml /tmp/object-created-trigger.yaml
```

Set:

- `<COMPARTMENT_OCID>` to the compartment where the OCI Events Rule should live.
- `<BUCKET_NAME>` to the Object Storage bucket name.
- `spec.functionRef.name` to a `Function` in the same namespace, if not `managed-hello`.

Apply:

```sh
kubectl apply -f /tmp/object-created-trigger.yaml
kubectl get functioneventtriggers
kubectl describe functioneventtrigger object-created-trigger
```

Expected high-level status:

```text
NAME                     PHASE   RULE ID                                      FUNCTION        AGE
object-created-trigger   Ready   ocid1.eventrule.oc1.me-jeddah-1...          managed-hello   30s
```

## IAM And Invocation

The operator workload identity needs permission to manage OCI Events rules in the target compartment. OCI Events also needs permission to invoke the target Function. Keep both sides in mind:

- Operator workload: manage Events rules.
- Events rule principal: invoke the Function action target.

Operator workload policy:

```text
Allow any-user to manage cloudevents-rules in compartment <events-rule-compartment> where all {
  request.principal.type = 'workload',
  request.principal.namespace = 'oci-functions-operator-system',
  request.principal.service_account = 'oci-functions-operator-controller-manager',
  request.principal.cluster_id = '<oke-cluster-ocid>'
}
```

Events rule invocation policy:

```text
Allow any-user to use fn-invocation in compartment <function-compartment> where all {
  request.principal.type = 'eventrule'
}
```

Use the namespace, service account, cluster OCID, and compartments from your Helm deployment and target Functions. A missing workload policy commonly appears as `404 NotAuthorizedOrNotFound` on `CreateRule`. A missing invocation policy can let the rule reconcile but stop matching events from invoking the Function.

See [OKE deployment](oke-deployment.md) for the broader Workload Identity policy context.

## Troubleshooting

Referenced Function not Ready:

- Check `kubectl get function <name> -o yaml`.
- Confirm `Ready=True`.
- Confirm `status.functionId` is populated.
- Managed Functions must finish OCI application/function reconciliation first.

Missing Events IAM permission:

- `FunctionEventTrigger.status.phase` becomes `Error`.
- `status.message` usually contains an OCI authorization or permission error.
- Check operator pod logs and Kubernetes events for `RuleError`.

Invalid event condition JSON:

- `condition.rawJson` must be a JSON object.
- OCI Events condition values must be scalar strings in the common single-value case; arrays are rejected before calling OCI.
- Structured `condition.eventType` currently accepts one value. Use separate triggers for multiple event types.
- Structured `condition.data` must be valid object-shaped YAML/JSON.
- `status.conditions[?type=="RuleReady"]` uses reason `InvalidCondition` when validation fails.

Rule created but Function not invoked:

- Confirm the rule is enabled.
- Confirm the FAAS action target is the expected Function OCID.
- Confirm OCI policy allows Events to invoke the target Function if required in your tenancy.
- Confirm the event actually occurs in the rule compartment or a child compartment.

Wrong compartment, bucket, or event type:

- OCI Events Rules match events emitted in the rule compartment and child compartments.
- Object Storage bucket filters must match the event payload shape.
- Use the exact event type, for example `com.oraclecloud.objectstorage.createobject`.
