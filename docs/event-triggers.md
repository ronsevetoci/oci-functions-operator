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
        bucketName:
        - <BUCKET_NAME>
```

The controller stores the OCI Events Rule OCID in `status.ruleId`.

## Raw Conditions

For exact OCI Events condition JSON, use `condition.rawJson` instead of the structured fields:

```yaml
condition:
  rawJson: |
    {
      "eventType": ["com.oraclecloud.objectstorage.createobject"],
      "data": {
        "additionalDetails": {
          "bucketName": ["my-bucket"]
        }
      }
    }
```

`rawJson` is mutually exclusive with `eventType` and `data`.

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

The operator workload identity needs permission to manage OCI Events rules in the target compartment. OCI may also require a policy that allows the Events service to invoke the target Function. Keep both sides in mind:

- Operator workload: manage Events rules.
- Events service: invoke the Function action target.

See [OKE deployment](oke-deployment.md) for policy examples.

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
