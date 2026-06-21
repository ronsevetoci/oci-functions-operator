// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"reflect"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// FunctionReconciler reconciles a Function object.
type FunctionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functions,verbs=get;list;watch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functions/finalizers,verbs=update

// Reconcile updates Function status from the requested source of truth.
func (r *FunctionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var function functionsv1alpha1.Function
	if err := r.Get(ctx, req.NamespacedName, &function); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	now := metav1.Now()
	desired := function.Status
	desired.ObservedGeneration = function.Generation

	desiredObject := function.DeepCopy()
	desiredObject.Status = desired

	functionID := function.Spec.FunctionID
	if functionID == "" {
		functionID = function.Spec.ExistingFunctionOCID
	}

	switch {
	case functionID != "":
		desiredObject.Status.Phase = functionsv1alpha1.FunctionPhaseReady
		desiredObject.Status.FunctionOCID = functionID
		desiredObject.Status.Message = "Using existing OCI Function."
		desiredObject.SetCondition(metav1.Condition{
			Type:               functionsv1alpha1.FunctionConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "ExistingFunctionResolved",
			Message:            "Existing OCI Function OCID is configured.",
			ObservedGeneration: function.Generation,
			LastTransitionTime: now,
		})
	case function.Spec.Config != nil:
		desiredObject.Status.Phase = functionsv1alpha1.FunctionPhasePending
		desiredObject.Status.FunctionOCID = ""
		desiredObject.Status.Message = "Function lifecycle management is not implemented yet."
		desiredObject.SetCondition(metav1.Condition{
			Type:               functionsv1alpha1.FunctionConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "LifecycleManagementNotImplemented",
			Message:            "Desired function config is accepted, but OCI lifecycle management is not implemented yet.",
			ObservedGeneration: function.Generation,
			LastTransitionTime: now,
		})
	default:
		desiredObject.Status.Phase = functionsv1alpha1.FunctionPhaseError
		desiredObject.Status.FunctionOCID = ""
		desiredObject.Status.Message = "Function must specify functionId, existingFunctionOcid, or config."
		desiredObject.SetCondition(metav1.Condition{
			Type:               functionsv1alpha1.FunctionConditionReady,
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidSpec",
			Message:            "Function must specify functionId, existingFunctionOcid, or config.",
			ObservedGeneration: function.Generation,
			LastTransitionTime: now,
		})
	}

	desired = desiredObject.Status
	if reflect.DeepEqual(function.Status, desired) {
		return ctrl.Result{}, nil
	}

	desired.LastSyncTime = &now
	function.Status = desired
	if err := r.Status().Update(ctx, &function); err != nil {
		return ctrl.Result{}, err
	}

	logger.V(1).Info("updated Function status", "phase", function.Status.Phase)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FunctionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&functionsv1alpha1.Function{}).
		Complete(r)
}
