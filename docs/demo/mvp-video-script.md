# MVP Video Script

Secondary guide. Use [MVP demo flow](mvp-demo-flow.md) for commands; this file is only the short narration outline.

## Opening

"This is the MVP of the OCI Functions Operator for OKE. The operator keeps OCI Functions as OCI Functions, but gives users Kubernetes-native resources for Applications, Functions, invocation, OCI Events triggers, and in-cluster event emission."

## Preparation

"Before the demo, we verify Workload Identity IAM, Mac tools, OCI CLI auth, the Fn-compatible Jeddah OCIR image, and network egress from the Functions application to OCIR."

## Install

"Helm is the supported OKE install path. We apply CRDs explicitly first because Helm installs CRDs on fresh install, but normal Helm upgrades do not upgrade CRDs from the chart `crds/` directory."

## Function

"The `FunctionApplication` CRD maps to the real OCI Functions Application, including subnet, NSG, and app-level config. The `Function` CRD maps to the OCI Function inside it. When reconciliation completes, Kubernetes status shows the application ID, function ID, invoke endpoint, and `Ready=True`."

## FunctionJob

"The `FunctionJob` CRD invokes a referenced Function. It fans out inline JSON payloads, applies retry settings, and reports aggregate plus per-payload status."

## OCI Events Trigger

"For Object Storage `createobject`, `FunctionEventTrigger` creates an OCI Events Rule. The rule action targets the real OCI Function OCID from `Function.status.functionId`."

## FunctionEvent

"For Kubernetes-native events, `FunctionEvent` lets an in-cluster producer emit a small event object. Matching `functionevent.*` triggers invoke the referenced Function without creating an OCI Events Rule."

## Proof

"Kubernetes status proves reconciliation and routing. OCI Console metrics, logs, and Events Rules prove that the real OCI Functions invocation path ran."

## Close

"The MVP demonstrates five useful primitives: manage a Functions Application, manage a Function, invoke it as a job, connect OCI service events, and emit Kubernetes-native events. Managed deletion is explicit and opt-in with `deletionPolicy: Delete`; application deletion is blocked if functions remain. Schedules, watches, queues, and workflows remain intentionally out of scope."
