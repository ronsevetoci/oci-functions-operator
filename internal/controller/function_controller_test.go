// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	"github.com/oracle/oci-functions-operator/internal/lifecycle"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFunctionReconcilerMarksExistingOCIDReady(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := &functionsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "default"},
		Spec: functionsv1alpha1.FunctionSpec{
			ExistingFunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid",
			InvokeEndpoint:       "https://functions.us-ashburn-1.oci.oraclecloud.com",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseReady {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseReady)
	}
	if updated.Status.FunctionOCID != function.Spec.ExistingFunctionOCID {
		t.Fatalf("function OCID = %q, want %q", updated.Status.FunctionOCID, function.Spec.ExistingFunctionOCID)
	}
	if updated.Status.FunctionID != function.Spec.ExistingFunctionOCID {
		t.Fatalf("function ID = %q, want %q", updated.Status.FunctionID, function.Spec.ExistingFunctionOCID)
	}
	if updated.Status.InvokeEndpoint != function.Spec.InvokeEndpoint {
		t.Fatalf("invoke endpoint = %q, want %q", updated.Status.InvokeEndpoint, function.Spec.InvokeEndpoint)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("Ready condition = %#v, want true", condition)
	}
}

func TestFunctionReconcilerMarksFunctionIDReady(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := &functionsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "default"},
		Spec: functionsv1alpha1.FunctionSpec{
			FunctionID:     "ocid1.fnfunc.oc1.iad.exampleuniqueid",
			InvokeEndpoint: "https://functions.us-ashburn-1.oci.oraclecloud.com",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseReady {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseReady)
	}
	if updated.Status.FunctionOCID != function.Spec.FunctionID {
		t.Fatalf("function OCID = %q, want %q", updated.Status.FunctionOCID, function.Spec.FunctionID)
	}
	if updated.Status.FunctionID != function.Spec.FunctionID {
		t.Fatalf("function ID = %q, want %q", updated.Status.FunctionID, function.Spec.FunctionID)
	}
}

func TestFunctionReconcilerExistingModeRequiresInvokeEndpoint(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := &functionsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "default"},
		Spec: functionsv1alpha1.FunctionSpec{
			Mode:       functionsv1alpha1.FunctionModeExisting,
			FunctionID: "ocid1.fnfunc.oc1.iad.exampleuniqueid",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseError {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseError)
	}
	if !strings.Contains(updated.Status.Message, "spec.invokeEndpoint") {
		t.Fatalf("message = %q, want invokeEndpoint guidance", updated.Status.Message)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "InvalidExistingFunction" {
		t.Fatalf("Ready condition = %#v, want InvalidExistingFunction false", condition)
	}
}

func TestFunctionReconcilerManagedModeWaitsForFunctionIDAndInvokeEndpoint(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	manager := &fakeLifecycleManager{
		state: lifecycle.FunctionState{
			ApplicationID: "ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid",
			FunctionID:    "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid",
			Message:       "OCI function exists but invoke endpoint is not available yet.",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatalf("requeueAfter = 0, want managed Function to requeue while endpoint is missing")
	}
	if manager.calls != 1 {
		t.Fatalf("manager calls = %d, want 1", manager.calls)
	}
	if manager.desired.Region != "me-jeddah-1" {
		t.Fatalf("desired region = %q, want me-jeddah-1", manager.desired.Region)
	}
	if !reflect.DeepEqual(manager.desired.ApplicationNSGIDs, function.Spec.Config.NSGIDs) {
		t.Fatalf("desired application NSGs = %#v, want %#v", manager.desired.ApplicationNSGIDs, function.Spec.Config.NSGIDs)
	}
	if !manager.desired.ManageApplicationNSGIDs {
		t.Fatalf("desired ManageApplicationNSGIDs = false, want true when spec.config.nsgIds is set")
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhasePending {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhasePending)
	}
	if updated.Status.ApplicationID != manager.state.ApplicationID || updated.Status.FunctionID != manager.state.FunctionID {
		t.Fatalf("status IDs = %#v, want manager state", updated.Status)
	}
	if updated.Status.InvokeEndpoint != "" {
		t.Fatalf("invoke endpoint = %q, want empty while pending", updated.Status.InvokeEndpoint)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "ManagedFunctionPending" {
		t.Fatalf("Ready condition = %#v, want ManagedFunctionPending false", condition)
	}
}

func TestFunctionReconcilerManagedModeMarksReady(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	manager := &fakeLifecycleManager{
		state: lifecycle.FunctionState{
			ApplicationID:  "ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid",
			FunctionID:     "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid",
			InvokeEndpoint: "https://functions.me-jeddah-1.oci.oraclecloud.com",
			Ready:          true,
			Message:        "Managed OCI Function is ready.",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("requeueAfter = %s, want no requeue after ready", result.RequeueAfter)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseReady {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseReady)
	}
	if updated.Status.ApplicationID != manager.state.ApplicationID ||
		updated.Status.FunctionID != manager.state.FunctionID ||
		updated.Status.InvokeEndpoint != manager.state.InvokeEndpoint {
		t.Fatalf("status = %#v, want manager state", updated.Status)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition == nil || condition.Status != metav1.ConditionTrue || condition.Reason != "ManagedFunctionReady" {
		t.Fatalf("Ready condition = %#v, want ManagedFunctionReady true", condition)
	}
}

func TestFunctionReconcilerManagedModeRecordsLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	manager := &fakeLifecycleManager{
		state: lifecycle.FunctionState{
			ApplicationID:  "ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid",
			FunctionID:     "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid",
			InvokeEndpoint: "https://functions.me-jeddah-1.oci.oraclecloud.com",
			Ready:          true,
			Message:        "Managed OCI Function is ready.",
			Events: []lifecycle.Event{{
				Type:    lifecycle.EventTypeNormal,
				Reason:  "ApplicationNSGsUpdated",
				Message: "Updated OCI Functions application \"demo-app\" NSG configuration to [ocid1.networksecuritygroup.oc1.me-jeddah-1.exampleuniqueid].",
			}},
		},
	}
	recorder := record.NewFakeRecorder(10)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager, Recorder: recorder}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	assertEventContains(t, drainEvents(recorder), "Normal ApplicationNSGsUpdated")
}

func TestFunctionReconcilerManagedModeRecordsApplicationNSGUpdateFailure(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	manager := &fakeLifecycleManager{
		state: lifecycle.FunctionState{
			ApplicationID: "ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid",
			Events: []lifecycle.Event{{
				Type:    lifecycle.EventTypeWarning,
				Reason:  "ApplicationNSGUpdateFailed",
				Message: "Failed to update OCI Functions application \"demo-app\" NSG configuration to [ocid1.networksecuritygroup.oc1.me-jeddah-1.exampleuniqueid]: not authorized",
			}},
		},
		err: errors.New("update OCI Functions application ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid NSG configuration: not authorized"),
	}
	recorder := record.NewFakeRecorder(10)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager, Recorder: recorder}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseError {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseError)
	}
	if updated.Status.ApplicationID != manager.state.ApplicationID {
		t.Fatalf("application ID = %q, want %q", updated.Status.ApplicationID, manager.state.ApplicationID)
	}
	if !strings.Contains(updated.Status.Message, "NSG configuration") {
		t.Fatalf("message = %q, want NSG update context", updated.Status.Message)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "ManagedFunctionError" {
		t.Fatalf("Ready condition = %#v, want ManagedFunctionError false", condition)
	}
	assertEventContains(t, drainEvents(recorder), "Warning ApplicationNSGUpdateFailed")
}

func TestFunctionReconcilerManagedRetainDoesNotDeleteOCIOnCRDeletion(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	function.Finalizers = []string{functionFinalizer}
	function.Status.FunctionID = "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid"
	manager := &fakeLifecycleManager{}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	if err := client.Delete(ctx, function); err != nil {
		t.Fatalf("delete Function: %v", err)
	}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if manager.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0 for Retain policy", manager.deleteCalls)
	}
	var updated functionsv1alpha1.Function
	err = client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated)
	if err == nil {
		t.Fatalf("Function still exists after finalizer removal, finalizers=%#v", updated.Finalizers)
	}
}

func TestFunctionReconcilerManagedDeleteDeletesOCIAndRemovesFinalizer(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	function.Spec.DeletionPolicy = functionsv1alpha1.FunctionDeletionPolicyDelete
	function.Finalizers = []string{functionFinalizer}
	function.Status.FunctionID = "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid"
	manager := &fakeLifecycleManager{}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	if err := client.Delete(ctx, function); err != nil {
		t.Fatalf("delete Function: %v", err)
	}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if manager.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", manager.deleteCalls)
	}
	if manager.deleteTarget.FunctionID != function.Status.FunctionID {
		t.Fatalf("delete target function ID = %q, want %q", manager.deleteTarget.FunctionID, function.Status.FunctionID)
	}
	var updated functionsv1alpha1.Function
	err = client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated)
	if err == nil {
		t.Fatalf("Function still exists after successful delete, finalizers=%#v", updated.Finalizers)
	}
}

func TestFunctionReconcilerExistingDeletePolicyNeverDeletesOCIResources(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := &functionsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "default"},
		Spec: functionsv1alpha1.FunctionSpec{
			Mode:           functionsv1alpha1.FunctionModeExisting,
			FunctionID:     "ocid1.fnfunc.oc1.iad.exampleuniqueid",
			InvokeEndpoint: "https://functions.us-ashburn-1.oci.oraclecloud.com",
			DeletionPolicy: functionsv1alpha1.FunctionDeletionPolicyDelete,
		},
	}
	manager := &fakeLifecycleManager{}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if manager.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0 for Existing mode", manager.deleteCalls)
	}

	var updated functionsv1alpha1.Function
	if err := client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("get updated Function: %v", err)
	}
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseError {
		t.Fatalf("phase = %q, want Error", updated.Status.Phase)
	}
	if !strings.Contains(updated.Status.Message, "Existing mode never deletes OCI resources") {
		t.Fatalf("message = %q, want existing-mode delete policy guidance", updated.Status.Message)
	}
}

func TestFunctionReconcilerManagedDeleteTreatsMissingOCIFunctionAsSuccessfulCleanup(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	function.Spec.DeletionPolicy = functionsv1alpha1.FunctionDeletionPolicyDelete
	function.Finalizers = []string{functionFinalizer}
	function.Status.FunctionID = "ocid1.fnfunc.oc1.me-jeddah-1.missing"
	manager := &fakeLifecycleManager{
		deleteState: lifecycle.FunctionDeletionState{
			FunctionID: function.Status.FunctionID,
			Message:    "Managed OCI Function was not found; nothing to delete. OCI Functions application retained.",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	if err := client.Delete(ctx, function); err != nil {
		t.Fatalf("delete Function: %v", err)
	}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if manager.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", manager.deleteCalls)
	}
	var updated functionsv1alpha1.Function
	err = client.Get(ctx, types.NamespacedName{Name: "hello", Namespace: "default"}, &updated)
	if err == nil {
		t.Fatalf("Function still exists after not-found cleanup, finalizers=%#v", updated.Finalizers)
	}
}

func TestFunctionReconcilerManagedDeleteBacksOffAfterFailedCleanupStatus(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme(t)
	function := managedFunction("hello", "default")
	function.Spec.DeletionPolicy = functionsv1alpha1.FunctionDeletionPolicyDelete
	function.Finalizers = []string{functionFinalizer}
	function.Status.Phase = functionsv1alpha1.FunctionPhaseError
	function.Status.Message = "Delete managed OCI Function failed: 404 NotAuthorizedOrNotFound: Authorization failed or requested resource not found."
	now := metav1.Now()
	function.Status.LastSyncTime = &now
	meta.SetStatusCondition(&function.Status.Conditions, metav1.Condition{
		Type:               functionsv1alpha1.FunctionConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ManagedFunctionDeleteFailed",
		Message:            function.Status.Message,
		ObservedGeneration: function.Generation,
		LastTransitionTime: now,
	})
	manager := &fakeLifecycleManager{deleteErr: errors.New("should not be called during backoff")}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&functionsv1alpha1.Function{}).
		WithObjects(function).
		Build()
	reconciler := &FunctionReconciler{Client: client, Scheme: scheme, Manager: manager}

	if err := client.Delete(ctx, function); err != nil {
		t.Fatalf("delete Function: %v", err)
	}
	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "hello", Namespace: "default"}})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if manager.deleteCalls != 0 {
		t.Fatalf("deleteCalls = %d, want 0 while deletion failure backoff is active", manager.deleteCalls)
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > functionDeleteRequeue {
		t.Fatalf("RequeueAfter = %s, want active backoff up to %s", result.RequeueAfter, functionDeleteRequeue)
	}
}

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := functionsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add functions scheme: %v", err)
	}
	return scheme
}

func managedFunction(name, namespace string) *functionsv1alpha1.Function {
	return &functionsv1alpha1.Function{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: functionsv1alpha1.FunctionSpec{
			Mode: functionsv1alpha1.FunctionModeManaged,
			Config: &functionsv1alpha1.FunctionConfig{
				Region:           "me-jeddah-1",
				CompartmentID:    "ocid1.compartment.oc1..exampleuniqueid",
				ApplicationName:  "demo-app",
				SubnetIDs:        []string{"ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid"},
				NSGIDs:           []string{"ocid1.networksecuritygroup.oc1.me-jeddah-1.exampleuniqueid"},
				DisplayName:      "hello",
				Image:            "me-jeddah-1.ocir.io/example/functions/hello:latest",
				MemoryInMBs:      256,
				TimeoutInSeconds: 60,
				Config:           map[string]string{"GREETING": "hello"},
			},
		},
	}
}

type fakeLifecycleManager struct {
	state        lifecycle.FunctionState
	err          error
	desired      lifecycle.DesiredFunction
	calls        int
	deleteState  lifecycle.FunctionDeletionState
	deleteErr    error
	deleteTarget lifecycle.ManagedFunctionDeleteTarget
	deleteCalls  int
}

func (f *fakeLifecycleManager) EnsureFunction(_ context.Context, desired lifecycle.DesiredFunction) (lifecycle.FunctionState, error) {
	f.calls++
	f.desired = desired
	return f.state, f.err
}

func (f *fakeLifecycleManager) DeleteManagedFunction(_ context.Context, target lifecycle.ManagedFunctionDeleteTarget) (lifecycle.FunctionDeletionState, error) {
	f.deleteCalls++
	f.deleteTarget = target
	if f.deleteState.Message == "" && f.deleteErr == nil {
		f.deleteState = lifecycle.FunctionDeletionState{
			FunctionID: target.FunctionID,
			Deleted:    true,
			Message:    "Deleted managed OCI Function " + target.FunctionID + ". OCI Functions application retained.",
		}
	}
	return f.deleteState, f.deleteErr
}
