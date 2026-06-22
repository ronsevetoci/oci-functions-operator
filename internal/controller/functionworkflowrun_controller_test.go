// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"strings"
	"testing"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFunctionWorkflowRunCreatesFirstEligibleNodeFunctionJob(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	recorder := record.NewFakeRecorder(10)
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme, Recorder: recorder}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	jobs := functionJobsForRun(t, ctx, k8sClient, run)
	if got, want := len(jobs), 1; got != want {
		t.Fatalf("child FunctionJob count = %d, want %d", got, want)
	}
	job := jobsByWorkflowNode(t, jobs)["prepare"]
	if job == nil {
		t.Fatalf("prepare FunctionJob was not created")
	}
	if job.Name != workflowRunNodeJobName(run.Name, "prepare") {
		t.Fatalf("prepare job name = %q, want deterministic node name", job.Name)
	}
	if job.Spec.FunctionRef.Name != "prepare-function" {
		t.Fatalf("functionRef.name = %q, want prepare-function", job.Spec.FunctionRef.Name)
	}
	if string(job.Spec.Payload.Raw) != `{"step":"prepare"}` {
		t.Fatalf("payload = %s, want prepare payload", string(job.Spec.Payload.Raw))
	}
	if job.Spec.Parallelism != 2 || job.Spec.RetryLimit != 1 {
		t.Fatalf("parallelism/retryLimit = %d/%d, want 2/1", job.Spec.Parallelism, job.Spec.RetryLimit)
	}
	updated := getWorkflowRun(t, ctx, k8sClient, run)
	prepareStatus := workflowNodeStatusByName(t, updated.Status.Nodes, "prepare")
	if prepareStatus.FunctionRef == nil || prepareStatus.FunctionRef.Name != "prepare-function" {
		t.Fatalf("prepare functionRef status = %#v, want prepare-function", prepareStatus.FunctionRef)
	}
	if got := len(functionsForRun(t, ctx, k8sClient, run)); got != 0 {
		t.Fatalf("child Function count = %d, want 0 for functionRef mode", got)
	}
	if updated.Status.NodeCount != 3 || updated.Status.CompletedNodes != 0 || updated.Status.FailedNodes != 0 {
		t.Fatalf("node counters = nodes:%d complete:%d failed:%d, want 3/0/0", updated.Status.NodeCount, updated.Status.CompletedNodes, updated.Status.FailedNodes)
	}
	events := drainEvents(recorder)
	assertEventContains(t, events, "Normal WorkflowStarted")
	assertEventContains(t, events, "Normal NodeFunctionJobCreated")
}

func TestFunctionWorkflowRunDependentNodeWaitsUntilDependencyCompletes(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	recorder := record.NewFakeRecorder(20)
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme, Recorder: recorder}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	jobs := jobsByWorkflowNode(t, functionJobsForRun(t, ctx, k8sClient, run))
	if jobs["process"] != nil {
		t.Fatalf("process FunctionJob was created before prepare completed")
	}
	updated := getWorkflowRun(t, ctx, k8sClient, run)
	status := workflowNodeStatusByName(t, updated.Status.Nodes, "process")
	if status.Phase != functionsv1alpha1.FunctionWorkflowNodePhasePending || !strings.Contains(status.Message, "prepare") {
		t.Fatalf("process status = %#v, want Pending waiting on prepare", status)
	}
}

func TestFunctionWorkflowRunStartsDependentNodeAfterDependencySucceeds(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	recorder := record.NewFakeRecorder(20)
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme, Recorder: recorder}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	completeFunctionJob(t, ctx, k8sClient, run.Namespace, workflowRunNodeJobName(run.Name, "prepare"))
	reconcileWorkflowRun(t, ctx, reconciler, run)

	jobs := jobsByWorkflowNode(t, functionJobsForRun(t, ctx, k8sClient, run))
	if jobs["process"] == nil {
		t.Fatalf("process FunctionJob was not created after prepare succeeded")
	}
	if jobs["notify"] != nil {
		t.Fatalf("notify FunctionJob was created before process completed")
	}
}

func TestFunctionWorkflowRunCompletesWhenAllNodeJobsComplete(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	recorder := record.NewFakeRecorder(20)
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme, Recorder: recorder}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	completeFunctionJob(t, ctx, k8sClient, run.Namespace, workflowRunNodeJobName(run.Name, "prepare"))
	reconcileWorkflowRun(t, ctx, reconciler, run)
	completeFunctionJob(t, ctx, k8sClient, run.Namespace, workflowRunNodeJobName(run.Name, "process"))
	reconcileWorkflowRun(t, ctx, reconciler, run)
	completeFunctionJob(t, ctx, k8sClient, run.Namespace, workflowRunNodeJobName(run.Name, "notify"))
	reconcileWorkflowRun(t, ctx, reconciler, run)

	updated := getWorkflowRun(t, ctx, k8sClient, run)
	if updated.Status.Phase != functionsv1alpha1.FunctionWorkflowRunPhaseComplete {
		t.Fatalf("phase = %q, want Complete", updated.Status.Phase)
	}
	if updated.Status.CompletionTime == nil {
		t.Fatalf("completionTime is nil, want terminal timestamp")
	}
	if updated.Status.NodeCount != 3 || updated.Status.CompletedNodes != 3 || updated.Status.FailedNodes != 0 {
		t.Fatalf("node counters = nodes:%d complete:%d failed:%d, want 3/3/0", updated.Status.NodeCount, updated.Status.CompletedNodes, updated.Status.FailedNodes)
	}
	complete := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionComplete)
	if complete == nil || complete.Status != metav1.ConditionTrue {
		t.Fatalf("Complete condition = %#v, want true", complete)
	}
	events := drainEvents(recorder)
	assertEventContains(t, events, "Normal NodeCompleted")
	assertEventContains(t, events, "Normal WorkflowComplete")
}

func TestFunctionWorkflowRunFailsWhenNodeFunctionJobFails(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	recorder := record.NewFakeRecorder(20)
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme, Recorder: recorder}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	failFunctionJob(t, ctx, k8sClient, run.Namespace, workflowRunNodeJobName(run.Name, "prepare"), "boom")
	reconcileWorkflowRun(t, ctx, reconciler, run)

	updated := getWorkflowRun(t, ctx, k8sClient, run)
	if updated.Status.Phase != functionsv1alpha1.FunctionWorkflowRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", updated.Status.Phase)
	}
	prepare := workflowNodeStatusByName(t, updated.Status.Nodes, "prepare")
	if prepare.Phase != functionsv1alpha1.FunctionWorkflowNodePhaseFailed || !strings.Contains(prepare.Message, "boom") {
		t.Fatalf("prepare status = %#v, want Failed with child error", prepare)
	}
	process := workflowNodeStatusByName(t, updated.Status.Nodes, "process")
	if process.Phase != functionsv1alpha1.FunctionWorkflowNodePhaseSkipped {
		t.Fatalf("process phase = %q, want Skipped after failed dependency", process.Phase)
	}
	if updated.Status.NodeCount != 3 || updated.Status.CompletedNodes != 0 || updated.Status.FailedNodes != 1 || updated.Status.SkippedNodes != 2 {
		t.Fatalf("node counters = nodes:%d complete:%d failed:%d skipped:%d, want 3/0/1/2", updated.Status.NodeCount, updated.Status.CompletedNodes, updated.Status.FailedNodes, updated.Status.SkippedNodes)
	}
	failed := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionFailed)
	if failed == nil || failed.Status != metav1.ConditionTrue {
		t.Fatalf("Failed condition = %#v, want true", failed)
	}
	events := drainEvents(recorder)
	assertEventContains(t, events, "Warning NodeFailed")
	assertEventContains(t, events, "Warning NodeSkipped")
	assertEventContains(t, events, "Warning WorkflowFailed")
}

func TestFunctionWorkflowRunInvalidDependencyFailsWithClearStatus(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	workflow.Spec.Nodes[1].DependsOn = []string{"missing"}
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	updated := getWorkflowRun(t, ctx, k8sClient, run)
	if updated.Status.Phase != functionsv1alpha1.FunctionWorkflowRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", updated.Status.Phase)
	}
	failed := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionFailed)
	if failed == nil || failed.Reason != "InvalidDependency" || !strings.Contains(failed.Message, "missing node") {
		t.Fatalf("Failed condition = %#v, want InvalidDependency with missing node guidance", failed)
	}
	if got := len(functionJobsForRun(t, ctx, k8sClient, run)); got != 0 {
		t.Fatalf("child FunctionJob count = %d, want 0 for invalid workflow", got)
	}
}

func TestFunctionWorkflowRunDoesNotCreateDuplicateFunctionJobsOnRepeatedReconcile(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	reconcileWorkflowRun(t, ctx, reconciler, run)

	jobs := functionJobsForRun(t, ctx, k8sClient, run)
	if got, want := len(jobs), 1; got != want {
		t.Fatalf("child FunctionJob count = %d, want %d after repeated reconcile", got, want)
	}
	if jobsByWorkflowNode(t, jobs)["prepare"] == nil {
		t.Fatalf("prepare FunctionJob is missing after repeated reconcile")
	}
}

func TestFunctionWorkflowRunInlineFunctionCreatesChildFunction(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := inlineFunctionWorkflow("inline-demo", "default")
	run := workflowRun("inline-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.Function{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	functions := functionsForRun(t, ctx, k8sClient, run)
	if got, want := len(functions), 1; got != want {
		t.Fatalf("child Function count = %d, want %d", got, want)
	}
	function := functionsByWorkflowNode(t, functions)["prepare"]
	if function == nil {
		t.Fatalf("prepare child Function was not created")
	}
	if function.Name != workflowRunNodeFunctionName(run.Name, "prepare") {
		t.Fatalf("child Function name = %q, want deterministic node name", function.Name)
	}
	if function.Spec.Config == nil || function.Spec.Config.DisplayName != "inline-prepare" {
		t.Fatalf("child Function spec = %#v, want inline managed spec", function.Spec)
	}
	if got := len(functionJobsForRun(t, ctx, k8sClient, run)); got != 0 {
		t.Fatalf("child FunctionJob count = %d, want 0 while Function is not Ready", got)
	}
	status := workflowNodeStatusByName(t, getWorkflowRun(t, ctx, k8sClient, run).Status.Nodes, "prepare")
	if status.ChildFunctionRef == nil || status.ChildFunctionRef.Name != function.Name {
		t.Fatalf("childFunctionRef = %#v, want %q", status.ChildFunctionRef, function.Name)
	}
	if status.Phase != functionsv1alpha1.FunctionWorkflowNodePhaseRunning || !strings.Contains(status.Message, "Ready=True") {
		t.Fatalf("node status = %#v, want Running waiting for Function readiness", status)
	}
}

func TestFunctionWorkflowRunInlineFunctionWaitsUntilChildFunctionReady(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := inlineFunctionWorkflow("inline-demo", "default")
	run := workflowRun("inline-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.Function{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	reconcileWorkflowRun(t, ctx, reconciler, run)

	if got := len(functionJobsForRun(t, ctx, k8sClient, run)); got != 0 {
		t.Fatalf("child FunctionJob count = %d, want 0 while child Function is not Ready", got)
	}
	status := workflowNodeStatusByName(t, getWorkflowRun(t, ctx, k8sClient, run).Status.Nodes, "prepare")
	if !strings.Contains(status.Message, "Ready=True") {
		t.Fatalf("node message = %q, want Ready wait message", status.Message)
	}
}

func TestFunctionWorkflowRunInlineFunctionCreatesFunctionJobAfterChildFunctionReady(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := inlineFunctionWorkflow("inline-demo", "default")
	run := workflowRun("inline-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.Function{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	markFunctionReady(t, ctx, k8sClient, run.Namespace, workflowRunNodeFunctionName(run.Name, "prepare"))
	reconcileWorkflowRun(t, ctx, reconciler, run)

	jobs := jobsByWorkflowNode(t, functionJobsForRun(t, ctx, k8sClient, run))
	job := jobs["prepare"]
	if job == nil {
		t.Fatalf("prepare FunctionJob was not created after child Function became Ready")
	}
	if job.Spec.FunctionRef.Name != workflowRunNodeFunctionName(run.Name, "prepare") {
		t.Fatalf("FunctionJob functionRef.name = %q, want child Function name", job.Spec.FunctionRef.Name)
	}
	status := workflowNodeStatusByName(t, getWorkflowRun(t, ctx, k8sClient, run).Status.Nodes, "prepare")
	if status.ChildFunctionRef == nil || status.FunctionJobRef == nil {
		t.Fatalf("node status refs = child:%#v job:%#v, want both refs", status.ChildFunctionRef, status.FunctionJobRef)
	}
}

func TestFunctionWorkflowRunInlineFunctionDoesNotCreateDuplicateChildFunctionOnRepeatedReconcile(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := inlineFunctionWorkflow("inline-demo", "default")
	run := workflowRun("inline-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.Function{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)
	reconcileWorkflowRun(t, ctx, reconciler, run)

	if got, want := len(functionsForRun(t, ctx, k8sClient, run)), 1; got != want {
		t.Fatalf("child Function count = %d, want %d after repeated reconcile", got, want)
	}
}

func TestFunctionWorkflowRunNodeWithoutFunctionFailsClearly(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	workflow.Spec.Nodes[0].FunctionRef = nil
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	updated := getWorkflowRun(t, ctx, k8sClient, run)
	if updated.Status.Phase != functionsv1alpha1.FunctionWorkflowRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", updated.Status.Phase)
	}
	failed := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionFailed)
	if failed == nil || failed.Reason != "InvalidNodeFunction" || !strings.Contains(failed.Message, "exactly one of functionRef or function") {
		t.Fatalf("Failed condition = %#v, want clear missing function validation", failed)
	}
}

func TestFunctionWorkflowRunNodeWithFunctionRefAndFunctionFailsClearly(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	workflow := threeNodeWorkflow("demo", "default")
	workflow.Spec.Nodes[0].Function = inlineManagedFunctionSpec("inline-prepare")
	run := workflowRun("demo-run", "default", workflow.Name)

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.FunctionWorkflowRun{}, &functionsv1alpha1.FunctionJob{}).
		WithObjects(workflow, run).
		Build()
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

	reconcileWorkflowRun(t, ctx, reconciler, run)

	updated := getWorkflowRun(t, ctx, k8sClient, run)
	if updated.Status.Phase != functionsv1alpha1.FunctionWorkflowRunPhaseFailed {
		t.Fatalf("phase = %q, want Failed", updated.Status.Phase)
	}
	failed := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionFailed)
	if failed == nil || failed.Reason != "InvalidNodeFunction" || !strings.Contains(failed.Message, "exactly one of functionRef or function") {
		t.Fatalf("Failed condition = %#v, want clear mutually-exclusive function validation", failed)
	}
}

func threeNodeWorkflow(name, namespace string) *functionsv1alpha1.FunctionWorkflow {
	return &functionsv1alpha1.FunctionWorkflow{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: functionsv1alpha1.FunctionWorkflowSpec{
			Nodes: []functionsv1alpha1.FunctionWorkflowNode{
				{
					Name:        "prepare",
					FunctionRef: &functionsv1alpha1.FunctionReference{Name: "prepare-function"},
					Payload:     rawPayloadPtr(`{"step":"prepare"}`),
					Parallelism: 2,
					RetryLimit:  1,
				},
				{
					Name:        "process",
					FunctionRef: &functionsv1alpha1.FunctionReference{Name: "process-function"},
					DependsOn:   []string{"prepare"},
					Payload:     rawPayloadPtr(`{"step":"process"}`),
				},
				{
					Name:        "notify",
					FunctionRef: &functionsv1alpha1.FunctionReference{Name: "notify-function"},
					DependsOn:   []string{"process"},
					Payload:     rawPayloadPtr(`{"step":"notify"}`),
				},
			},
		},
	}
}

func inlineFunctionWorkflow(name, namespace string) *functionsv1alpha1.FunctionWorkflow {
	return &functionsv1alpha1.FunctionWorkflow{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: functionsv1alpha1.FunctionWorkflowSpec{
			Nodes: []functionsv1alpha1.FunctionWorkflowNode{{
				Name:     "prepare",
				Function: inlineManagedFunctionSpec("inline-prepare"),
				Payload:  rawPayloadPtr(`{"step":"prepare"}`),
			}},
		},
	}
}

func inlineManagedFunctionSpec(displayName string) *functionsv1alpha1.FunctionSpec {
	return &functionsv1alpha1.FunctionSpec{
		Mode: functionsv1alpha1.FunctionModeManaged,
		Config: &functionsv1alpha1.FunctionConfig{
			Region:           "me-jeddah-1",
			CompartmentID:    "ocid1.compartment.oc1..exampleuniqueid",
			ApplicationName:  "workflow-app",
			SubnetIDs:        []string{"ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid"},
			NSGIDs:           []string{"ocid1.networksecuritygroup.oc1.me-jeddah-1.exampleuniqueid"},
			DisplayName:      displayName,
			Image:            "jed.ocir.io/example/hello-function:0.0.1",
			MemoryInMBs:      128,
			TimeoutInSeconds: 30,
		},
	}
}

func workflowRun(name, namespace, workflowName string) *functionsv1alpha1.FunctionWorkflowRun {
	return &functionsv1alpha1.FunctionWorkflowRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("uid-" + name),
		},
		Spec: functionsv1alpha1.FunctionWorkflowRunSpec{
			WorkflowRef: functionsv1alpha1.FunctionWorkflowReference{Name: workflowName},
		},
	}
}

func rawPayloadPtr(value string) *apiruntime.RawExtension {
	payload := rawPayload(value)
	return &payload
}

func reconcileWorkflowRun(t *testing.T, ctx context.Context, reconciler *FunctionWorkflowRunReconciler, run *functionsv1alpha1.FunctionWorkflowRun) {
	t.Helper()

	request := ctrl.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: run.Namespace}}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("reconcile FunctionWorkflowRun: %v", err)
	}
}

func getWorkflowRun(t *testing.T, ctx context.Context, k8sClient client.Client, run *functionsv1alpha1.FunctionWorkflowRun) functionsv1alpha1.FunctionWorkflowRun {
	t.Helper()

	var updated functionsv1alpha1.FunctionWorkflowRun
	key := types.NamespacedName{Name: run.Name, Namespace: run.Namespace}
	if err := k8sClient.Get(ctx, key, &updated); err != nil {
		t.Fatalf("get FunctionWorkflowRun: %v", err)
	}
	return updated
}

func functionJobsForRun(t *testing.T, ctx context.Context, k8sClient client.Client, run *functionsv1alpha1.FunctionWorkflowRun) []functionsv1alpha1.FunctionJob {
	t.Helper()

	var list functionsv1alpha1.FunctionJobList
	if err := k8sClient.List(ctx, &list, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list FunctionJobs: %v", err)
	}
	jobs := []functionsv1alpha1.FunctionJob{}
	for _, job := range list.Items {
		if metav1.IsControlledBy(&job, run) {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func functionsForRun(t *testing.T, ctx context.Context, k8sClient client.Client, run *functionsv1alpha1.FunctionWorkflowRun) []functionsv1alpha1.Function {
	t.Helper()

	var list functionsv1alpha1.FunctionList
	if err := k8sClient.List(ctx, &list, client.InNamespace(run.Namespace)); err != nil {
		t.Fatalf("list Functions: %v", err)
	}
	functions := []functionsv1alpha1.Function{}
	for _, function := range list.Items {
		if metav1.IsControlledBy(&function, run) {
			functions = append(functions, function)
		}
	}
	return functions
}

func jobsByWorkflowNode(t *testing.T, jobs []functionsv1alpha1.FunctionJob) map[string]*functionsv1alpha1.FunctionJob {
	t.Helper()

	byNode := map[string]*functionsv1alpha1.FunctionJob{}
	for i := range jobs {
		job := &jobs[i]
		node := job.Annotations[workflowNodeAnnotation]
		if node == "" {
			t.Fatalf("FunctionJob %q is missing workflow node annotation", job.Name)
		}
		byNode[node] = job
	}
	return byNode
}

func functionsByWorkflowNode(t *testing.T, functions []functionsv1alpha1.Function) map[string]*functionsv1alpha1.Function {
	t.Helper()

	byNode := map[string]*functionsv1alpha1.Function{}
	for i := range functions {
		function := &functions[i]
		node := function.Annotations[workflowNodeAnnotation]
		if node == "" {
			t.Fatalf("Function %q is missing workflow node annotation", function.Name)
		}
		byNode[node] = function
	}
	return byNode
}

func workflowNodeStatusByName(t *testing.T, statuses []functionsv1alpha1.FunctionWorkflowRunNodeStatus, name string) functionsv1alpha1.FunctionWorkflowRunNodeStatus {
	t.Helper()

	for _, status := range statuses {
		if status.Name == name {
			return status
		}
	}
	t.Fatalf("missing node status %q in %#v", name, statuses)
	return functionsv1alpha1.FunctionWorkflowRunNodeStatus{}
}

func completeFunctionJob(t *testing.T, ctx context.Context, k8sClient client.Client, namespace, name string) {
	t.Helper()
	updateFunctionJobPhase(t, ctx, k8sClient, namespace, name, functionsv1alpha1.FunctionJobPhaseSucceeded, "")
}

func failFunctionJob(t *testing.T, ctx context.Context, k8sClient client.Client, namespace, name, message string) {
	t.Helper()
	updateFunctionJobPhase(t, ctx, k8sClient, namespace, name, functionsv1alpha1.FunctionJobPhaseFailed, message)
}

func markFunctionReady(t *testing.T, ctx context.Context, k8sClient client.Client, namespace, name string) {
	t.Helper()

	var function functionsv1alpha1.Function
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := k8sClient.Get(ctx, key, &function); err != nil {
		t.Fatalf("get Function %s: %v", name, err)
	}
	now := metav1.Now()
	function.Status.Phase = functionsv1alpha1.FunctionPhaseReady
	function.Status.FunctionID = "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid"
	function.Status.FunctionOCID = function.Status.FunctionID
	function.Status.InvokeEndpoint = "https://functions.me-jeddah-1.oci.oraclecloud.com"
	function.Status.Conditions = []metav1.Condition{{
		Type:               functionsv1alpha1.FunctionConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ManagedFunctionReady",
		Message:            "Managed OCI Function is ready.",
		ObservedGeneration: function.Generation,
		LastTransitionTime: now,
	}}
	if err := k8sClient.Status().Update(ctx, &function); err != nil {
		t.Fatalf("update Function status %s: %v", name, err)
	}
}

func updateFunctionJobPhase(t *testing.T, ctx context.Context, k8sClient client.Client, namespace, name string, phase functionsv1alpha1.FunctionJobPhase, lastError string) {
	t.Helper()

	var job functionsv1alpha1.FunctionJob
	key := types.NamespacedName{Name: name, Namespace: namespace}
	if err := k8sClient.Get(ctx, key, &job); err != nil {
		t.Fatalf("get FunctionJob %s: %v", name, err)
	}
	now := metav1.Now()
	if job.Status.StartTime == nil {
		job.Status.StartTime = &now
	}
	job.Status.Phase = phase
	job.Status.LastError = lastError
	if phase == functionsv1alpha1.FunctionJobPhaseSucceeded || phase == functionsv1alpha1.FunctionJobPhaseFailed {
		job.Status.CompletionTime = &now
	}
	if err := k8sClient.Status().Update(ctx, &job); err != nil {
		t.Fatalf("update FunctionJob status %s: %v", name, err)
	}
}
