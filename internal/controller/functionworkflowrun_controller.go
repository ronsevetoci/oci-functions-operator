// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	workflowNodeAnnotation = "functions.oci.oracle.com/workflow-node"
)

// FunctionWorkflowRunReconciler reconciles a FunctionWorkflowRun object.
type FunctionWorkflowRunReconciler struct {
	client.Client
	Scheme *apiruntime.Scheme
}

// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functionworkflowruns,verbs=get;list;watch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functionworkflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functionworkflowruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functionworkflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functionjobs,verbs=get;list;watch;create;update;patch

// Reconcile executes the next eligible workflow nodes by creating child FunctionJobs.
func (r *FunctionWorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var run functionsv1alpha1.FunctionWorkflowRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if run.IsTerminal() {
		return ctrl.Result{}, nil
	}

	now := metav1.Now()
	desiredObject := run.DeepCopy()
	desiredObject.Status.ObservedGeneration = run.Generation
	if desiredObject.Status.StartedAt == nil {
		desiredObject.Status.StartedAt = &now
	}

	if run.Spec.WorkflowRef.Name == "" {
		markWorkflowRunFailed(desiredObject, now, "InvalidWorkflowRef", "spec.workflowRef.name is required.")
		return ctrl.Result{}, r.updateFunctionWorkflowRunStatus(ctx, &run, desiredObject.Status)
	}

	var workflow functionsv1alpha1.FunctionWorkflow
	workflowKey := types.NamespacedName{Namespace: run.Namespace, Name: run.Spec.WorkflowRef.Name}
	if err := r.Get(ctx, workflowKey, &workflow); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		message := fmt.Sprintf("Referenced FunctionWorkflow %q was not found in namespace %q.", run.Spec.WorkflowRef.Name, run.Namespace)
		markWorkflowRunPending(desiredObject, now, "WorkflowNotFound", message)
		return ctrl.Result{}, r.updateFunctionWorkflowRunStatus(ctx, &run, desiredObject.Status)
	}
	desiredObject.Status.ObservedWorkflowGeneration = workflow.Generation

	nodeOrder, validationErr := validateWorkflowTemplate(workflow.Spec.Nodes)
	if validationErr != nil {
		desiredObject.Status.Nodes = validationNodeStatuses(workflow.Spec.Nodes, validationErr)
		markWorkflowRunFailed(desiredObject, now, validationErr.reason, validationErr.message)
		return ctrl.Result{}, r.updateFunctionWorkflowRunStatus(ctx, &run, desiredObject.Status)
	}

	childJobs, err := r.childFunctionJobsByNode(ctx, &run)
	if err != nil {
		return ctrl.Result{}, err
	}

	statuses, reconcileErr := r.reconcileWorkflowNodes(ctx, &run, &workflow, nodeOrder, childJobs, desiredObject.Status.Nodes, now)
	desiredObject.Status.Nodes = statuses
	if reconcileErr != nil {
		markWorkflowRunFailed(desiredObject, now, "FunctionJobCreateFailed", reconcileErr.Error())
		return ctrl.Result{}, r.updateFunctionWorkflowRunStatus(ctx, &run, desiredObject.Status)
	}

	summarizeWorkflowRun(desiredObject, now)
	if err := r.updateFunctionWorkflowRunStatus(ctx, &run, desiredObject.Status); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("updated FunctionWorkflowRun status", "phase", desiredObject.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *FunctionWorkflowRunReconciler) reconcileWorkflowNodes(
	ctx context.Context,
	run *functionsv1alpha1.FunctionWorkflowRun,
	workflow *functionsv1alpha1.FunctionWorkflow,
	nodeOrder []string,
	childJobs map[string]*functionsv1alpha1.FunctionJob,
	previousStatuses []functionsv1alpha1.FunctionWorkflowRunNodeStatus,
	now metav1.Time,
) ([]functionsv1alpha1.FunctionWorkflowRunNodeStatus, error) {
	nodeByName := make(map[string]functionsv1alpha1.FunctionWorkflowNode, len(workflow.Spec.Nodes))
	for _, node := range workflow.Spec.Nodes {
		nodeByName[node.Name] = node
	}

	statusByName := initializeWorkflowNodeStatuses(workflow.Spec.Nodes, previousStatuses, childJobs)
	for _, nodeName := range nodeOrder {
		node := nodeByName[nodeName]
		status := statusByName[nodeName]
		if _, hasJob := childJobs[nodeName]; hasJob {
			statusByName[nodeName] = status
			continue
		}

		failedDependency := firstFailedDependency(node, statusByName)
		if failedDependency != "" {
			status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseSkipped
			status.Message = fmt.Sprintf("Dependency %q did not complete successfully.", failedDependency)
			if status.CompletionTime == nil {
				status.CompletionTime = &now
			}
			statusByName[nodeName] = status
			continue
		}

		waiting := incompleteDependencies(node, statusByName)
		if len(waiting) > 0 {
			status.Phase = functionsv1alpha1.FunctionWorkflowNodePhasePending
			status.Message = fmt.Sprintf("Waiting for dependencies: %s.", strings.Join(waiting, ", "))
			statusByName[nodeName] = status
			continue
		}

		job, err := r.ensureNodeFunctionJob(ctx, run, node)
		if err != nil {
			status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseFailed
			status.Message = err.Error()
			if status.CompletionTime == nil {
				status.CompletionTime = &now
			}
			statusByName[nodeName] = status
			return workflowNodeStatusesInSpecOrder(workflow.Spec.Nodes, statusByName), err
		}

		childJobs[nodeName] = job
		status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseRunning
		status.FunctionJobRef = &corev1.LocalObjectReference{Name: job.Name}
		status.Message = fmt.Sprintf("Created FunctionJob %q.", job.Name)
		if status.StartedAt == nil {
			status.StartedAt = &now
		}
		status.CompletionTime = nil
		statusByName[nodeName] = status
	}

	return workflowNodeStatusesInSpecOrder(workflow.Spec.Nodes, statusByName), nil
}

func initializeWorkflowNodeStatuses(
	nodes []functionsv1alpha1.FunctionWorkflowNode,
	previousStatuses []functionsv1alpha1.FunctionWorkflowRunNodeStatus,
	childJobs map[string]*functionsv1alpha1.FunctionJob,
) map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus {
	previousByName := make(map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus, len(previousStatuses))
	for _, status := range previousStatuses {
		previousByName[status.Name] = status
	}

	statusByName := make(map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus, len(nodes))
	for _, node := range nodes {
		status := previousByName[node.Name]
		status.Name = node.Name
		if status.Phase == "" {
			status.Phase = functionsv1alpha1.FunctionWorkflowNodePhasePending
		}
		if job, ok := childJobs[node.Name]; ok {
			status = statusFromFunctionJob(node.Name, status, job)
		}
		statusByName[node.Name] = status
	}
	return statusByName
}

func statusFromFunctionJob(nodeName string, previous functionsv1alpha1.FunctionWorkflowRunNodeStatus, job *functionsv1alpha1.FunctionJob) functionsv1alpha1.FunctionWorkflowRunNodeStatus {
	status := previous
	status.Name = nodeName
	status.FunctionJobRef = &corev1.LocalObjectReference{Name: job.Name}
	if job.Status.StartTime != nil {
		status.StartedAt = job.Status.StartTime
	}
	switch job.Status.Phase {
	case functionsv1alpha1.FunctionJobPhaseSucceeded:
		status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseComplete
		status.Message = fmt.Sprintf("FunctionJob %q completed successfully.", job.Name)
		status.CompletionTime = job.Status.CompletionTime
	case functionsv1alpha1.FunctionJobPhaseFailed:
		status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseFailed
		status.Message = fmt.Sprintf("FunctionJob %q failed.", job.Name)
		if job.Status.LastError != "" {
			status.Message = fmt.Sprintf("FunctionJob %q failed: %s", job.Name, job.Status.LastError)
		}
		status.CompletionTime = job.Status.CompletionTime
	default:
		status.Phase = functionsv1alpha1.FunctionWorkflowNodePhaseRunning
		status.Message = fmt.Sprintf("Waiting for FunctionJob %q to complete.", job.Name)
		status.CompletionTime = nil
	}
	return status
}

func firstFailedDependency(node functionsv1alpha1.FunctionWorkflowNode, statusByName map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus) string {
	for _, dependency := range node.DependsOn {
		switch statusByName[dependency].Phase {
		case functionsv1alpha1.FunctionWorkflowNodePhaseFailed, functionsv1alpha1.FunctionWorkflowNodePhaseSkipped:
			return dependency
		}
	}
	return ""
}

func incompleteDependencies(node functionsv1alpha1.FunctionWorkflowNode, statusByName map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus) []string {
	waiting := []string{}
	for _, dependency := range node.DependsOn {
		if statusByName[dependency].Phase != functionsv1alpha1.FunctionWorkflowNodePhaseComplete {
			waiting = append(waiting, dependency)
		}
	}
	sort.Strings(waiting)
	return waiting
}

func workflowNodeStatusesInSpecOrder(nodes []functionsv1alpha1.FunctionWorkflowNode, statusByName map[string]functionsv1alpha1.FunctionWorkflowRunNodeStatus) []functionsv1alpha1.FunctionWorkflowRunNodeStatus {
	statuses := make([]functionsv1alpha1.FunctionWorkflowRunNodeStatus, 0, len(nodes))
	for _, node := range nodes {
		statuses = append(statuses, statusByName[node.Name])
	}
	return statuses
}

func (r *FunctionWorkflowRunReconciler) ensureNodeFunctionJob(ctx context.Context, run *functionsv1alpha1.FunctionWorkflowRun, node functionsv1alpha1.FunctionWorkflowNode) (*functionsv1alpha1.FunctionJob, error) {
	jobName := workflowRunNodeJobName(run.Name, node.Name)
	var existing functionsv1alpha1.FunctionJob
	key := types.NamespacedName{Namespace: run.Namespace, Name: jobName}
	if err := r.Get(ctx, key, &existing); err == nil {
		if !metav1.IsControlledBy(&existing, run) {
			return nil, fmt.Errorf("FunctionJob %q already exists and is not controlled by FunctionWorkflowRun %q", jobName, run.Name)
		}
		if existing.Annotations == nil || existing.Annotations[workflowNodeAnnotation] != node.Name {
			return nil, fmt.Errorf("FunctionJob %q is controlled by FunctionWorkflowRun %q but is not associated with node %q", jobName, run.Name, node.Name)
		}
		return &existing, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	job := &functionsv1alpha1.FunctionJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: run.Namespace,
			Annotations: map[string]string{
				workflowNodeAnnotation: node.Name,
			},
		},
		Spec: functionsv1alpha1.FunctionJobSpec{
			FunctionRef: node.FunctionRef,
			Payload:     copyRawExtension(node.Payload),
			Parallelism: effectiveWorkflowNodeParallelism(node),
			RetryLimit:  effectiveWorkflowNodeRetryLimit(node),
		},
	}
	if err := ctrl.SetControllerReference(run, job, r.Scheme); err != nil {
		return nil, err
	}
	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			var created functionsv1alpha1.FunctionJob
			if getErr := r.Get(ctx, key, &created); getErr != nil {
				return nil, getErr
			}
			if metav1.IsControlledBy(&created, run) && created.Annotations[workflowNodeAnnotation] == node.Name {
				return &created, nil
			}
		}
		return nil, err
	}
	return job, nil
}

func (r *FunctionWorkflowRunReconciler) childFunctionJobsByNode(ctx context.Context, run *functionsv1alpha1.FunctionWorkflowRun) (map[string]*functionsv1alpha1.FunctionJob, error) {
	var jobs functionsv1alpha1.FunctionJobList
	if err := r.List(ctx, &jobs, client.InNamespace(run.Namespace)); err != nil {
		return nil, err
	}

	byNode := make(map[string]*functionsv1alpha1.FunctionJob)
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if !metav1.IsControlledBy(job, run) {
			continue
		}
		nodeName := job.Annotations[workflowNodeAnnotation]
		if nodeName == "" {
			continue
		}
		byNode[nodeName] = job
	}
	return byNode, nil
}

func summarizeWorkflowRun(run *functionsv1alpha1.FunctionWorkflowRun, now metav1.Time) {
	if len(run.Status.Nodes) == 0 {
		run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhasePending
		setWorkflowRunConditions(run, now, workflowRunConditionSet{
			Running:  conditionState{Status: metav1.ConditionFalse, Reason: "NoNodes", Message: "WorkflowRun has no nodes to run."},
			Complete: conditionState{Status: metav1.ConditionFalse, Reason: "NoNodes", Message: "WorkflowRun has not completed."},
			Failed:   conditionState{Status: metav1.ConditionFalse, Reason: "NoNodes", Message: "WorkflowRun has not failed."},
		})
		return
	}

	allComplete := true
	anyRunning := false
	for _, node := range run.Status.Nodes {
		switch node.Phase {
		case functionsv1alpha1.FunctionWorkflowNodePhaseFailed, functionsv1alpha1.FunctionWorkflowNodePhaseSkipped:
			run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhaseFailed
			if run.Status.CompletionTime == nil {
				run.Status.CompletionTime = &now
			}
			setWorkflowRunConditions(run, now, workflowRunConditionSet{
				Running:  conditionState{Status: metav1.ConditionFalse, Reason: "NodeFailed", Message: "WorkflowRun has no remaining runnable nodes."},
				Complete: conditionState{Status: metav1.ConditionFalse, Reason: "NodeFailed", Message: "WorkflowRun did not complete."},
				Failed:   conditionState{Status: metav1.ConditionTrue, Reason: "NodeFailed", Message: "One or more workflow nodes failed or were skipped."},
			})
			return
		case functionsv1alpha1.FunctionWorkflowNodePhaseComplete:
		case functionsv1alpha1.FunctionWorkflowNodePhaseRunning:
			anyRunning = true
			allComplete = false
		default:
			allComplete = false
		}
	}

	if allComplete {
		run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhaseComplete
		if run.Status.CompletionTime == nil {
			run.Status.CompletionTime = &now
		}
		setWorkflowRunConditions(run, now, workflowRunConditionSet{
			Running:  conditionState{Status: metav1.ConditionFalse, Reason: "AllNodesComplete", Message: "WorkflowRun has no running nodes."},
			Complete: conditionState{Status: metav1.ConditionTrue, Reason: "AllNodesComplete", Message: "All workflow nodes completed successfully."},
			Failed:   conditionState{Status: metav1.ConditionFalse, Reason: "AllNodesComplete", Message: "No workflow node failures were observed."},
		})
		return
	}

	run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhaseRunning
	run.Status.CompletionTime = nil
	reason := "NodesPending"
	message := "WorkflowRun is waiting for eligible nodes."
	if anyRunning {
		reason = "NodesRunning"
		message = "WorkflowRun has active child FunctionJobs."
	}
	setWorkflowRunConditions(run, now, workflowRunConditionSet{
		Running:  conditionState{Status: metav1.ConditionTrue, Reason: reason, Message: message},
		Complete: conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun has not completed."},
		Failed:   conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun has not failed."},
	})
}

func markWorkflowRunFailed(run *functionsv1alpha1.FunctionWorkflowRun, now metav1.Time, reason, message string) {
	run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhaseFailed
	if run.Status.CompletionTime == nil {
		run.Status.CompletionTime = &now
	}
	setWorkflowRunConditions(run, now, workflowRunConditionSet{
		Running:  conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun cannot make progress."},
		Complete: conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun did not complete."},
		Failed:   conditionState{Status: metav1.ConditionTrue, Reason: reason, Message: message},
	})
}

func markWorkflowRunPending(run *functionsv1alpha1.FunctionWorkflowRun, now metav1.Time, reason, message string) {
	run.Status.Phase = functionsv1alpha1.FunctionWorkflowRunPhasePending
	run.Status.CompletionTime = nil
	setWorkflowRunConditions(run, now, workflowRunConditionSet{
		Running:  conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: message},
		Complete: conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun has not completed."},
		Failed:   conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: "WorkflowRun has not failed."},
	})
}

type workflowRunConditionSet struct {
	Running  conditionState
	Complete conditionState
	Failed   conditionState
}

func setWorkflowRunConditions(run *functionsv1alpha1.FunctionWorkflowRun, now metav1.Time, conditions workflowRunConditionSet) {
	setWorkflowRunCondition(run, now, functionsv1alpha1.FunctionWorkflowRunConditionRunning, conditions.Running)
	setWorkflowRunCondition(run, now, functionsv1alpha1.FunctionWorkflowRunConditionComplete, conditions.Complete)
	setWorkflowRunCondition(run, now, functionsv1alpha1.FunctionWorkflowRunConditionFailed, conditions.Failed)
}

func setWorkflowRunCondition(run *functionsv1alpha1.FunctionWorkflowRun, now metav1.Time, conditionType string, condition conditionState) {
	run.SetCondition(metav1.Condition{
		Type:               conditionType,
		Status:             condition.Status,
		Reason:             condition.Reason,
		Message:            condition.Message,
		ObservedGeneration: run.Generation,
		LastTransitionTime: now,
	})
}

func (r *FunctionWorkflowRunReconciler) updateFunctionWorkflowRunStatus(ctx context.Context, run *functionsv1alpha1.FunctionWorkflowRun, desired functionsv1alpha1.FunctionWorkflowRunStatus) error {
	if reflect.DeepEqual(run.Status, desired) {
		return nil
	}
	run.Status = desired
	return r.Status().Update(ctx, run)
}

type workflowValidationError struct {
	reason  string
	message string
	node    string
}

func (e *workflowValidationError) Error() string {
	return e.message
}

func validateWorkflowTemplate(nodes []functionsv1alpha1.FunctionWorkflowNode) ([]string, *workflowValidationError) {
	if len(nodes) == 0 {
		return nil, &workflowValidationError{reason: "EmptyWorkflow", message: "FunctionWorkflow spec.nodes must contain at least one node."}
	}

	nodeByName := make(map[string]functionsv1alpha1.FunctionWorkflowNode, len(nodes))
	for _, node := range nodes {
		switch {
		case node.Name == "":
			return nil, &workflowValidationError{reason: "InvalidNode", message: "Workflow node name is required."}
		case node.FunctionRef.Name == "":
			return nil, &workflowValidationError{reason: "InvalidNode", node: node.Name, message: fmt.Sprintf("Workflow node %q requires spec.nodes[].functionRef.name.", node.Name)}
		}
		if _, exists := nodeByName[node.Name]; exists {
			return nil, &workflowValidationError{reason: "DuplicateNode", node: node.Name, message: fmt.Sprintf("Workflow node name %q must be unique.", node.Name)}
		}
		nodeByName[node.Name] = node
	}

	for _, node := range nodes {
		for _, dependency := range node.DependsOn {
			if _, ok := nodeByName[dependency]; !ok {
				return nil, &workflowValidationError{reason: "InvalidDependency", node: node.Name, message: fmt.Sprintf("Workflow node %q depends on missing node %q.", node.Name, dependency)}
			}
		}
	}

	visitState := make(map[string]int, len(nodes))
	order := make([]string, 0, len(nodes))
	var visit func(string) *workflowValidationError
	visit = func(name string) *workflowValidationError {
		switch visitState[name] {
		case 1:
			return &workflowValidationError{reason: "DependencyCycle", node: name, message: fmt.Sprintf("Workflow dependencies contain a cycle at node %q.", name)}
		case 2:
			return nil
		}
		visitState[name] = 1
		node := nodeByName[name]
		for _, dependency := range node.DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visitState[name] = 2
		order = append(order, name)
		return nil
	}

	for _, node := range nodes {
		if err := visit(node.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func validationNodeStatuses(nodes []functionsv1alpha1.FunctionWorkflowNode, validationErr *workflowValidationError) []functionsv1alpha1.FunctionWorkflowRunNodeStatus {
	statuses := make([]functionsv1alpha1.FunctionWorkflowRunNodeStatus, 0, len(nodes))
	for _, node := range nodes {
		phase := functionsv1alpha1.FunctionWorkflowNodePhasePending
		message := "Workflow validation did not reach this node."
		if validationErr.node == node.Name || (validationErr.node == "" && node.Name == "") {
			phase = functionsv1alpha1.FunctionWorkflowNodePhaseFailed
			message = validationErr.message
		}
		statuses = append(statuses, functionsv1alpha1.FunctionWorkflowRunNodeStatus{
			Name:    node.Name,
			Phase:   phase,
			Message: message,
		})
	}
	return statuses
}

func workflowRunNodeJobName(runName, nodeName string) string {
	base := sanitizeDNSLabel(runName + "-" + nodeName)
	if len(base) <= 63 {
		return base
	}
	sum := sha256.Sum256([]byte(runName + "/" + nodeName))
	suffix := hex.EncodeToString(sum[:])[:10]
	prefixLength := 63 - len(suffix) - 1
	prefix := strings.Trim(base[:prefixLength], "-")
	if prefix == "" {
		return "workflow-" + suffix
	}
	return prefix + "-" + suffix
}

func sanitizeDNSLabel(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if allowed {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if r == '-' || r == '.' || unicode.IsSpace(r) {
			if builder.Len() > 0 && !lastHyphen {
				builder.WriteRune('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "workflow-node"
	}
	return out
}

func copyRawExtension(in *apiruntime.RawExtension) *apiruntime.RawExtension {
	if in == nil {
		return nil
	}
	out := &apiruntime.RawExtension{
		Raw: append([]byte(nil), in.Raw...),
	}
	if in.Object != nil {
		out.Object = in.Object.DeepCopyObject()
	}
	return out
}

func effectiveWorkflowNodeParallelism(node functionsv1alpha1.FunctionWorkflowNode) int32 {
	if node.Parallelism < 1 {
		return 1
	}
	return node.Parallelism
}

func effectiveWorkflowNodeRetryLimit(node functionsv1alpha1.FunctionWorkflowNode) int32 {
	if node.RetryLimit < 0 {
		return 0
	}
	return node.RetryLimit
}

// SetupWithManager sets up the controller with the Manager.
func (r *FunctionWorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&functionsv1alpha1.FunctionWorkflowRun{}).
		Owns(&functionsv1alpha1.FunctionJob{}).
		Complete(r)
}
