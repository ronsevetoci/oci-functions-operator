// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// FunctionConditionReady indicates whether the referenced or desired OCI Function is ready to invoke.
	FunctionConditionReady = "Ready"
)

// FunctionPhase summarizes the controller-observed state of a Function.
// +kubebuilder:validation:Enum=Pending;Ready;Error
type FunctionPhase string

const (
	FunctionPhasePending FunctionPhase = "Pending"
	FunctionPhaseReady   FunctionPhase = "Ready"
	FunctionPhaseError   FunctionPhase = "Error"
)

// FunctionSpec defines the desired state of Function.
// +kubebuilder:validation:XValidation:rule="has(self.functionId) || has(self.existingFunctionOcid) || has(self.config)",message="one of spec.functionId, spec.existingFunctionOcid, or spec.config is required"
// +kubebuilder:validation:XValidation:rule="!(has(self.config) && (has(self.functionId) || has(self.existingFunctionOcid)))",message="spec.config is mutually exclusive with existing function references"
// +kubebuilder:validation:XValidation:rule="!(has(self.functionId) && has(self.existingFunctionOcid))",message="spec.functionId and spec.existingFunctionOcid are mutually exclusive"
type FunctionSpec struct {
	// FunctionID points at an existing OCI Functions function OCID.
	// +optional
	// +kubebuilder:validation:Pattern=^ocid1\.fnfunc\..+
	FunctionID string `json:"functionId,omitempty"`

	// ExistingFunctionOCID points at an existing OCI Functions function.
	// Deprecated: use FunctionID.
	// +optional
	// +kubebuilder:validation:Pattern=^ocid1\.fnfunc\..+
	ExistingFunctionOCID string `json:"existingFunctionOcid,omitempty"`

	// Config describes desired function configuration for future lifecycle management.
	// +optional
	Config *FunctionConfig `json:"config,omitempty"`
}

// FunctionConfig contains the minimal OCI Functions configuration this API will manage later.
type FunctionConfig struct {
	// ApplicationOCID is the OCI Functions application OCID that owns the function.
	// +kubebuilder:validation:Pattern=^ocid1\.fnapp\..+
	ApplicationOCID string `json:"applicationOcid"`

	// DisplayName is the OCI function display name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	DisplayName string `json:"displayName"`

	// Image is the container image reference for the function.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// MemoryInMB is the memory size to configure when lifecycle management is implemented.
	// +optional
	// +kubebuilder:default=128
	// +kubebuilder:validation:Enum=128;256;512;1024;2048
	MemoryInMB int32 `json:"memoryInMB,omitempty"`

	// TimeoutInSeconds is the max execution time for the function.
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=300
	TimeoutInSeconds int32 `json:"timeoutInSeconds,omitempty"`

	// Environment is a future OCI function config map.
	// +optional
	Environment map[string]string `json:"environment,omitempty"`

	// FreeformTags are OCI freeform tags to apply when lifecycle management is implemented.
	// +optional
	FreeformTags map[string]string `json:"freeformTags,omitempty"`
}

// FunctionStatus defines the observed state of Function.
type FunctionStatus struct {
	// ObservedGeneration is the last metadata.generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase is a compact state summary for kubectl output.
	// +optional
	Phase FunctionPhase `json:"phase,omitempty"`

	// FunctionOCID is the resolved OCI function OCID when known.
	// +optional
	FunctionOCID string `json:"functionOcid,omitempty"`

	// Message is a short human-readable status summary.
	// +optional
	Message string `json:"message,omitempty"`

	// LastSyncTime is the last time the controller updated observed status.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions contain detailed state transitions.
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
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Function OCID",type=string,JSONPath=".status.functionOcid"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// Function is the Schema for the functions API.
type Function struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FunctionSpec   `json:"spec,omitempty"`
	Status FunctionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// FunctionList contains a list of Function.
type FunctionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Function `json:"items"`
}

// SetCondition adds or updates a Function status condition.
func (f *Function) SetCondition(condition metav1.Condition) {
	meta.SetStatusCondition(&f.Status.Conditions, condition)
}

// IsReady reports whether the Function is ready to invoke.
func (f *Function) IsReady() bool {
	return meta.IsStatusConditionTrue(f.Status.Conditions, FunctionConditionReady)
}

func init() {
	SchemeBuilder.Register(&Function{}, &FunctionList{})
}
