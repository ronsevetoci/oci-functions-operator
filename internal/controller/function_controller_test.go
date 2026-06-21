// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"testing"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	if updated.Status.Phase != functionsv1alpha1.FunctionPhaseReady {
		t.Fatalf("phase = %q, want %q", updated.Status.Phase, functionsv1alpha1.FunctionPhaseReady)
	}
	if updated.Status.FunctionOCID != function.Spec.FunctionID {
		t.Fatalf("function OCID = %q, want %q", updated.Status.FunctionOCID, function.Spec.FunctionID)
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
