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
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

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
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

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
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

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
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

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
	complete := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionComplete)
	if complete == nil || complete.Status != metav1.ConditionTrue {
		t.Fatalf("Complete condition = %#v, want true", complete)
	}
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
	reconciler := &FunctionWorkflowRunReconciler{Client: k8sClient, Scheme: scheme}

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
	failed := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionWorkflowRunConditionFailed)
	if failed == nil || failed.Status != metav1.ConditionTrue {
		t.Fatalf("Failed condition = %#v, want true", failed)
	}
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

func threeNodeWorkflow(name, namespace string) *functionsv1alpha1.FunctionWorkflow {
	return &functionsv1alpha1.FunctionWorkflow{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: functionsv1alpha1.FunctionWorkflowSpec{
			Nodes: []functionsv1alpha1.FunctionWorkflowNode{
				{
					Name:        "prepare",
					FunctionRef: functionsv1alpha1.FunctionReference{Name: "prepare-function"},
					Payload:     rawPayloadPtr(`{"step":"prepare"}`),
					Parallelism: 2,
					RetryLimit:  1,
				},
				{
					Name:        "process",
					FunctionRef: functionsv1alpha1.FunctionReference{Name: "process-function"},
					DependsOn:   []string{"prepare"},
					Payload:     rawPayloadPtr(`{"step":"process"}`),
				},
				{
					Name:        "notify",
					FunctionRef: functionsv1alpha1.FunctionReference{Name: "notify-function"},
					DependsOn:   []string{"process"},
					Payload:     rawPayloadPtr(`{"step":"notify"}`),
				},
			},
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
