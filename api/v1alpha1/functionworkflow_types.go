// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// FunctionWorkflowConditionValid indicates whether the workflow template is structurally valid.
	FunctionWorkflowConditionValid = "Valid"

	// FunctionWorkflowRunConditionRunning indicates whether a workflow run has active or pending work.
	FunctionWorkflowRunConditionRunning = "Running"
	// FunctionWorkflowRunConditionComplete indicates whether all workflow nodes completed successfully.
	FunctionWorkflowRunConditionComplete = "Complete"
	// FunctionWorkflowRunConditionFailed indicates whether the workflow run reached a failed terminal state.
	FunctionWorkflowRunConditionFailed = "Failed"
)

// FunctionWorkflowRunPhase summarizes workflow run progress.
// +kubebuilder:validation:Enum=Pending;Running;Complete;Failed
type FunctionWorkflowRunPhase string

const (
	FunctionWorkflowRunPhasePending  FunctionWorkflowRunPhase = "Pending"
	FunctionWorkflowRunPhaseRunning  FunctionWorkflowRunPhase = "Running"
	FunctionWorkflowRunPhaseComplete FunctionWorkflowRunPhase = "Complete"
	FunctionWorkflowRunPhaseFailed   FunctionWorkflowRunPhase = "Failed"
)

// FunctionWorkflowNodePhase summarizes one node inside a workflow run.
// +kubebuilder:validation:Enum=Pending;Running;Complete;Failed;Skipped
type FunctionWorkflowNodePhase string

const (
	FunctionWorkflowNodePhasePending  FunctionWorkflowNodePhase = "Pending"
	FunctionWorkflowNodePhaseRunning  FunctionWorkflowNodePhase = "Running"
	FunctionWorkflowNodePhaseComplete FunctionWorkflowNodePhase = "Complete"
	FunctionWorkflowNodePhaseFailed   FunctionWorkflowNodePhase = "Failed"
	FunctionWorkflowNodePhaseSkipped  FunctionWorkflowNodePhase = "Skipped"
)

// FunctionWorkflowSpec defines the desired DAG template for workflow runs.
type FunctionWorkflowSpec struct {
	// Nodes are the named function invocation steps in this DAG.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Nodes []FunctionWorkflowNode `json:"nodes"`
}

// FunctionWorkflowNode describes one function invocation node in a workflow DAG.
// +kubebuilder:validation:XValidation:rule="has(self.functionRef) != has(self.function)",message="exactly one of functionRef or function is required"
type FunctionWorkflowNode struct {
	// Name is the stable node identifier used by dependsOn and status.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// FunctionRef references a Function in the same namespace as the workflow run.
	// +optional
	FunctionRef *FunctionReference `json:"functionRef,omitempty"`

	// Function is an inline Function spec used to create a child Function for this workflow run.
	// +optional
	Function *FunctionSpec `json:"function,omitempty"`

	// DependsOn lists node names that must complete successfully before this node starts.
	// +optional
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=63
	// +kubebuilder:validation:items:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +listType=set
	DependsOn []string `json:"dependsOn,omitempty"`

	// Payload is the inline JSON object passed to the child FunctionJob as a single payload item.
	// +optional
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:validation:Type=object
	// +kubebuilder:pruning:PreserveUnknownFields
	Payload *runtime.RawExtension `json:"payload,omitempty"`

	// Parallelism is passed through to the child FunctionJob.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Parallelism int32 `json:"parallelism,omitempty"`

	// RetryLimit is passed through to the child FunctionJob.
	// +optional
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	RetryLimit int32 `json:"retryLimit,omitempty"`
}

// FunctionWorkflowStatus defines the observed state of FunctionWorkflow.
type FunctionWorkflowStatus struct {
	// ObservedGeneration is the last metadata.generation observed by a controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions contain workflow template validation state when available.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={oci,functions}
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// FunctionWorkflow is the Schema for reusable function workflow DAG templates.
type FunctionWorkflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FunctionWorkflowSpec   `json:"spec,omitempty"`
	Status FunctionWorkflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// FunctionWorkflowList contains a list of FunctionWorkflow.
type FunctionWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FunctionWorkflow `json:"items"`
}

// FunctionWorkflowRunSpec defines one execution of a FunctionWorkflow.
type FunctionWorkflowRunSpec struct {
	// WorkflowRef references a FunctionWorkflow in the same namespace.
	WorkflowRef FunctionWorkflowReference `json:"workflowRef"`
}

// FunctionWorkflowReference names a FunctionWorkflow in the same namespace as a run.
type FunctionWorkflowReference struct {
	// Name is the referenced FunctionWorkflow name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// FunctionWorkflowRunStatus defines the observed state of FunctionWorkflowRun.
type FunctionWorkflowRunStatus struct {
	// ObservedGeneration is the last metadata.generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ObservedWorkflowGeneration is the referenced workflow generation used for this status.
	// +optional
	ObservedWorkflowGeneration int64 `json:"observedWorkflowGeneration,omitempty"`

	// Phase is a compact state summary for kubectl output.
	// +optional
	Phase FunctionWorkflowRunPhase `json:"phase,omitempty"`

	// StartedAt is when the controller first observed the workflow run.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletionTime is when the workflow run reached a terminal phase.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// NodeCount is the total number of nodes observed from the workflow template.
	// +optional
	NodeCount int32 `json:"nodeCount,omitempty"`

	// CompletedNodes is the number of nodes that completed successfully.
	// +optional
	CompletedNodes int32 `json:"completedNodes,omitempty"`

	// FailedNodes is the number of nodes that failed.
	// +optional
	FailedNodes int32 `json:"failedNodes,omitempty"`

	// SkippedNodes is the number of nodes skipped because dependencies did not complete successfully.
	// +optional
	SkippedNodes int32 `json:"skippedNodes,omitempty"`

	// Nodes records per-node execution state.
	// +optional
	// +listType=map
	// +listMapKey=name
	Nodes []FunctionWorkflowRunNodeStatus `json:"nodes,omitempty"`

	// Conditions contain detailed workflow run state transitions.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// FunctionWorkflowRunNodeStatus records execution state for one workflow node.
type FunctionWorkflowRunNodeStatus struct {
	// Name is the workflow node name.
	Name string `json:"name"`

	// Phase is the observed node execution phase.
	// +optional
	Phase FunctionWorkflowNodePhase `json:"phase,omitempty"`

	// FunctionJobRef references the child FunctionJob created for this node.
	// +optional
	FunctionJobRef *corev1.LocalObjectReference `json:"functionJobRef,omitempty"`

	// FunctionRef references the existing Function used by this node when spec.nodes[].functionRef is set.
	// +optional
	FunctionRef *corev1.LocalObjectReference `json:"functionRef,omitempty"`

	// ChildFunctionRef references the child Function created for this node when spec.nodes[].function is set.
	// +optional
	ChildFunctionRef *corev1.LocalObjectReference `json:"childFunctionRef,omitempty"`

	// Message is a short human-readable node status summary.
	// +optional
	Message string `json:"message,omitempty"`

	// StartedAt is when the node child FunctionJob was first observed or created.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// CompletionTime is when the node reached a terminal phase.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={oci,functions}
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=".status.nodeCount"
// +kubebuilder:printcolumn:name="Complete",type=integer,JSONPath=".status.completedNodes"
// +kubebuilder:printcolumn:name="Failed",type=integer,JSONPath=".status.failedNodes"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// FunctionWorkflowRun is the Schema for one execution of a FunctionWorkflow.
type FunctionWorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FunctionWorkflowRunSpec   `json:"spec,omitempty"`
	Status FunctionWorkflowRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// FunctionWorkflowRunList contains a list of FunctionWorkflowRun.
type FunctionWorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FunctionWorkflowRun `json:"items"`
}

// SetCondition adds or updates a FunctionWorkflow status condition.
func (w *FunctionWorkflow) SetCondition(condition metav1.Condition) {
	meta.SetStatusCondition(&w.Status.Conditions, condition)
}

// SetCondition adds or updates a FunctionWorkflowRun status condition.
func (r *FunctionWorkflowRun) SetCondition(condition metav1.Condition) {
	meta.SetStatusCondition(&r.Status.Conditions, condition)
}

// IsTerminal reports whether the FunctionWorkflowRun has reached a final state.
func (r *FunctionWorkflowRun) IsTerminal() bool {
	return r.Status.Phase == FunctionWorkflowRunPhaseComplete || r.Status.Phase == FunctionWorkflowRunPhaseFailed
}

func init() {
	SchemeBuilder.Register(&FunctionWorkflow{}, &FunctionWorkflowList{}, &FunctionWorkflowRun{}, &FunctionWorkflowRunList{})
}
