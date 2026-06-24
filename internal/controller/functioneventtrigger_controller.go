// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	functionsv1alpha1 "github.com/oracle/oci-functions-operator/api/v1alpha1"
	"github.com/oracle/oci-functions-operator/internal/eventtrigger"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	functionEventTriggerFinalizer = "functioneventtriggers.functions.oci.oracle.com/finalizer"

	functionEventTriggerOCIFailureRequeue = 30 * time.Second

	functionEventTriggerEventWaitingForFunction = "WaitingForFunction"
	functionEventTriggerEventRuleCreated        = "RuleCreated"
	functionEventTriggerEventRuleUpdated        = "RuleUpdated"
	functionEventTriggerEventRuleDeleted        = "RuleDeleted"
	functionEventTriggerEventRuleError          = "RuleError"
)

// FunctionEventTriggerReconciler reconciles a FunctionEventTrigger object.
type FunctionEventTriggerReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Manager eventtrigger.Manager

	Recorder record.EventRecorder

	warningEventMu         sync.Mutex
	warningEventSignatures map[types.NamespacedName]string
}

// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functioneventtriggers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functioneventtriggers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functioneventtriggers/finalizers,verbs=update
// +kubebuilder:rbac:groups=functions.oci.oracle.com,resources=functions,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile ensures an OCI Events rule invokes the referenced OCI Function.
func (r *FunctionEventTriggerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var trigger functionsv1alpha1.FunctionEventTrigger
	if err := r.Get(ctx, req.NamespacedName, &trigger); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !trigger.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &trigger)
	}

	if !controllerutil.ContainsFinalizer(&trigger, functionEventTriggerFinalizer) {
		controllerutil.AddFinalizer(&trigger, functionEventTriggerFinalizer)
		if err := r.Update(ctx, &trigger); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	now := metav1.Now()
	desiredObject := trigger.DeepCopy()
	desiredObject.Status.ObservedGeneration = trigger.Generation

	conditionJSON, err := conditionJSONFromSpec(trigger.Spec.Condition)
	if err != nil {
		message := fmt.Sprintf("Invalid OCI Events condition: %v", err)
		markFunctionEventTriggerError(desiredObject, now, "InvalidCondition", message)
		r.recordWarningEventIfChanged(&trigger, functionEventTriggerEventRuleError, message)
		return ctrl.Result{}, r.patchFunctionEventTriggerStatusIfChanged(ctx, &trigger, desiredObject.Status)
	}

	function, result, ready := r.resolveTriggerFunction(ctx, &trigger, desiredObject, now)
	if !ready {
		return result, r.patchFunctionEventTriggerStatusIfChanged(ctx, &trigger, desiredObject.Status)
	}

	if r.Manager == nil {
		message := "Event trigger manager is not configured; run the manager with INVOKER_MODE=oci to manage OCI Events rules."
		markFunctionEventTriggerPending(desiredObject, now, "EventTriggerManagerNotConfigured", message)
		return ctrl.Result{RequeueAfter: functionEventTriggerOCIFailureRequeue}, r.patchFunctionEventTriggerStatusIfChanged(ctx, &trigger, desiredObject.Status)
	}

	state, err := r.Manager.EnsureRule(ctx, desiredRuleFromTrigger(&trigger, &function, conditionJSON))
	r.recordRuleEvents(&trigger, state.Events)
	if err != nil {
		desiredObject.Status.RuleID = state.RuleID
		message := err.Error()
		markFunctionEventTriggerError(desiredObject, now, "RuleReconcileError", message)
		r.recordWarningEventIfChanged(&trigger, functionEventTriggerEventRuleError, "FunctionEventTrigger rule reconcile failed: "+message)
		return ctrl.Result{RequeueAfter: functionEventTriggerOCIFailureRequeue}, r.patchFunctionEventTriggerStatusIfChanged(ctx, &trigger, desiredObject.Status)
	}

	r.clearWarningEventSignature(&trigger)
	desiredObject.Status.RuleID = state.RuleID
	desiredObject.Status.Message = state.Message
	if desiredObject.Status.Message == "" {
		desiredObject.Status.Message = "OCI Events rule is ready."
	}
	if state.Ready {
		desiredObject.Status.Phase = functionsv1alpha1.FunctionEventTriggerPhaseReady
		setFunctionEventTriggerConditions(desiredObject, now, conditionState{Status: metav1.ConditionTrue, Reason: "FunctionReady", Message: "Referenced Function is ready."}, conditionState{Status: metav1.ConditionTrue, Reason: "RuleReady", Message: desiredObject.Status.Message})
	} else {
		desiredObject.Status.Phase = functionsv1alpha1.FunctionEventTriggerPhasePending
		setFunctionEventTriggerConditions(desiredObject, now, conditionState{Status: metav1.ConditionTrue, Reason: "FunctionReady", Message: "Referenced Function is ready."}, conditionState{Status: metav1.ConditionFalse, Reason: "RulePending", Message: desiredObject.Status.Message})
	}

	if err := r.patchFunctionEventTriggerStatusIfChanged(ctx, &trigger, desiredObject.Status); err != nil {
		return ctrl.Result{}, err
	}
	logger.V(1).Info("updated FunctionEventTrigger status", "phase", desiredObject.Status.Phase, "ruleID", desiredObject.Status.RuleID)
	if !state.Ready {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *FunctionEventTriggerReconciler) reconcileDelete(ctx context.Context, trigger *functionsv1alpha1.FunctionEventTrigger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(trigger, functionEventTriggerFinalizer) {
		return ctrl.Result{}, nil
	}

	if trigger.DeletionPolicy() == functionsv1alpha1.FunctionEventTriggerDeletionPolicyDelete && trigger.Status.RuleID != "" {
		if r.Manager == nil {
			r.recordWarningEventIfChanged(trigger, functionEventTriggerEventRuleError, fmt.Sprintf("Cannot delete OCI Events rule %s because event trigger manager is not configured.", trigger.Status.RuleID))
			return ctrl.Result{RequeueAfter: functionEventTriggerOCIFailureRequeue}, nil
		}
		state, err := r.Manager.DeleteRule(ctx, trigger.Status.RuleID)
		r.recordRuleEvents(trigger, state.Events)
		if err != nil {
			r.recordWarningEventIfChanged(trigger, functionEventTriggerEventRuleError, "FunctionEventTrigger rule delete failed: "+err.Error())
			return ctrl.Result{RequeueAfter: functionEventTriggerOCIFailureRequeue}, nil
		}
	}

	r.clearWarningEventSignature(trigger)
	controllerutil.RemoveFinalizer(trigger, functionEventTriggerFinalizer)
	if err := r.Update(ctx, trigger); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *FunctionEventTriggerReconciler) resolveTriggerFunction(
	ctx context.Context,
	trigger *functionsv1alpha1.FunctionEventTrigger,
	desired *functionsv1alpha1.FunctionEventTrigger,
	now metav1.Time,
) (functionsv1alpha1.Function, ctrl.Result, bool) {
	var function functionsv1alpha1.Function
	if trigger.Spec.FunctionRef.Name == "" {
		message := "spec.functionRef.name is required."
		markFunctionEventTriggerError(desired, now, "InvalidFunctionRef", message)
		return function, ctrl.Result{}, false
	}

	key := types.NamespacedName{Namespace: trigger.Namespace, Name: trigger.Spec.FunctionRef.Name}
	if err := r.Get(ctx, key, &function); err != nil {
		if !apierrors.IsNotFound(err) {
			markFunctionEventTriggerError(desired, now, "FunctionLookupError", err.Error())
			return function, ctrl.Result{RequeueAfter: 30 * time.Second}, false
		}
		message := fmt.Sprintf("Waiting for Function %q to exist in namespace %q.", trigger.Spec.FunctionRef.Name, trigger.Namespace)
		markFunctionEventTriggerWaitingForFunction(desired, now, "FunctionNotFound", message)
		r.recordEvent(trigger, corev1.EventTypeNormal, functionEventTriggerEventWaitingForFunction, message)
		return function, ctrl.Result{RequeueAfter: 30 * time.Second}, false
	}

	if !meta.IsStatusConditionTrue(function.Status.Conditions, functionsv1alpha1.FunctionConditionReady) || strings.TrimSpace(function.Status.FunctionID) == "" {
		message := functionNotReadyForEventTriggerMessage(&function)
		markFunctionEventTriggerWaitingForFunction(desired, now, "FunctionNotReady", message)
		r.recordEvent(trigger, corev1.EventTypeNormal, functionEventTriggerEventWaitingForFunction, message)
		return function, ctrl.Result{RequeueAfter: 30 * time.Second}, false
	}

	return function, ctrl.Result{}, true
}

func functionNotReadyForEventTriggerMessage(function *functionsv1alpha1.Function) string {
	details := []string{}
	if function.Status.Phase != "" {
		details = append(details, fmt.Sprintf("phase=%s", function.Status.Phase))
	}
	if function.Status.FunctionID == "" {
		details = append(details, "status.functionId is empty")
	}
	condition := meta.FindStatusCondition(function.Status.Conditions, functionsv1alpha1.FunctionConditionReady)
	if condition != nil {
		details = append(details, fmt.Sprintf("Ready=%s reason=%s", condition.Status, condition.Reason))
	}
	if function.Status.Message != "" {
		details = append(details, "message="+function.Status.Message)
	}
	if len(details) == 0 {
		return fmt.Sprintf("Waiting for Function %q to become Ready and populate status.functionId.", function.Name)
	}
	return fmt.Sprintf("Waiting for Function %q to become Ready and populate status.functionId: %s.", function.Name, strings.Join(details, "; "))
}

func desiredRuleFromTrigger(trigger *functionsv1alpha1.FunctionEventTrigger, function *functionsv1alpha1.Function, conditionJSON string) eventtrigger.DesiredRule {
	return eventtrigger.DesiredRule{
		CompartmentID: strings.TrimSpace(trigger.Spec.CompartmentID),
		DisplayName:   strings.TrimSpace(trigger.Spec.DisplayName),
		Description:   strings.TrimSpace(trigger.Spec.Description),
		IsEnabled:     trigger.RuleEnabled(),
		ConditionJSON: conditionJSON,
		FunctionID:    strings.TrimSpace(function.Status.FunctionID),
		RuleID:        strings.TrimSpace(trigger.Status.RuleID),
		TriggerName:   trigger.Name,
		Namespace:     trigger.Namespace,
		UID:           string(trigger.UID),
	}
}

func conditionJSONFromSpec(condition functionsv1alpha1.FunctionEventCondition) (string, error) {
	if strings.TrimSpace(condition.RawJSON) != "" {
		return normalizeConditionJSON(condition.RawJSON)
	}

	document := map[string]interface{}{}
	if len(condition.EventType) > 0 {
		document["eventType"] = condition.EventType
	}
	if condition.Data != nil && len(condition.Data.Raw) > 0 {
		var data interface{}
		if err := json.Unmarshal(condition.Data.Raw, &data); err != nil {
			return "", fmt.Errorf("condition.data must be valid JSON: %w", err)
		}
		document["data"] = data
	}
	if len(document) == 0 {
		return "", fmt.Errorf("condition must set rawJson or at least one structured field")
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeConditionJSON(value string) (string, error) {
	var decoded interface{}
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", err
	}
	if _, ok := decoded.(map[string]interface{}); !ok {
		return "", fmt.Errorf("rawJson must be a JSON object")
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func markFunctionEventTriggerWaitingForFunction(trigger *functionsv1alpha1.FunctionEventTrigger, now metav1.Time, reason, message string) {
	trigger.Status.Phase = functionsv1alpha1.FunctionEventTriggerPhasePending
	trigger.Status.Message = message
	setFunctionEventTriggerConditions(trigger, now,
		conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: message},
		conditionState{Status: metav1.ConditionFalse, Reason: "WaitingForFunction", Message: "OCI Events rule is waiting for the referenced Function."},
	)
}

func markFunctionEventTriggerPending(trigger *functionsv1alpha1.FunctionEventTrigger, now metav1.Time, reason, message string) {
	trigger.Status.Phase = functionsv1alpha1.FunctionEventTriggerPhasePending
	trigger.Status.Message = message
	setFunctionEventTriggerConditions(trigger, now,
		conditionState{Status: metav1.ConditionTrue, Reason: "FunctionReady", Message: "Referenced Function is ready."},
		conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: message},
	)
}

func markFunctionEventTriggerError(trigger *functionsv1alpha1.FunctionEventTrigger, now metav1.Time, reason, message string) {
	trigger.Status.Phase = functionsv1alpha1.FunctionEventTriggerPhaseError
	trigger.Status.Message = message
	setFunctionEventTriggerConditions(trigger, now,
		conditionState{Status: metav1.ConditionUnknown, Reason: reason, Message: "Function resolution did not complete."},
		conditionState{Status: metav1.ConditionFalse, Reason: reason, Message: message},
	)
}

func setFunctionEventTriggerConditions(trigger *functionsv1alpha1.FunctionEventTrigger, now metav1.Time, functionResolved, ruleReady conditionState) {
	setFunctionEventTriggerCondition(trigger, now, functionsv1alpha1.FunctionEventTriggerConditionFunctionResolved, functionResolved)
	setFunctionEventTriggerCondition(trigger, now, functionsv1alpha1.FunctionEventTriggerConditionRuleReady, ruleReady)
}

func setFunctionEventTriggerCondition(trigger *functionsv1alpha1.FunctionEventTrigger, now metav1.Time, conditionType string, condition conditionState) {
	lastTransitionTime := now
	if existing := meta.FindStatusCondition(trigger.Status.Conditions, conditionType); existing != nil &&
		existing.Status == condition.Status &&
		existing.Reason == condition.Reason &&
		existing.Message == condition.Message &&
		existing.ObservedGeneration == trigger.Generation {
		lastTransitionTime = existing.LastTransitionTime
	}

	trigger.SetCondition(metav1.Condition{
		Type:               conditionType,
		Status:             condition.Status,
		Reason:             condition.Reason,
		Message:            condition.Message,
		ObservedGeneration: trigger.Generation,
		LastTransitionTime: lastTransitionTime,
	})
}

func (r *FunctionEventTriggerReconciler) patchFunctionEventTriggerStatusIfChanged(ctx context.Context, trigger *functionsv1alpha1.FunctionEventTrigger, desired functionsv1alpha1.FunctionEventTriggerStatus) error {
	if functionEventTriggerStatusEqual(trigger.Status, desired) {
		return nil
	}
	now := metav1.Now()
	desired.LastSyncTime = &now
	trigger.Status = desired
	return r.Status().Update(ctx, trigger)
}

func functionEventTriggerStatusEqual(current, desired functionsv1alpha1.FunctionEventTriggerStatus) bool {
	current.LastSyncTime = nil
	desired.LastSyncTime = nil
	return reflect.DeepEqual(current, desired)
}

func (r *FunctionEventTriggerReconciler) recordRuleEvents(trigger *functionsv1alpha1.FunctionEventTrigger, events []eventtrigger.Event) {
	for _, event := range events {
		eventType := corev1.EventTypeNormal
		if event.Type == eventtrigger.EventTypeWarning {
			eventType = corev1.EventTypeWarning
		}
		reason := event.Reason
		switch reason {
		case "RuleCreated":
			reason = functionEventTriggerEventRuleCreated
		case "RuleUpdated":
			reason = functionEventTriggerEventRuleUpdated
		case "RuleDeleted":
			reason = functionEventTriggerEventRuleDeleted
		case "":
			reason = functionEventTriggerEventRuleError
		}
		if eventType == corev1.EventTypeWarning {
			r.recordWarningEventIfChanged(trigger, reason, event.Message)
			continue
		}
		r.recordEvent(trigger, eventType, reason, event.Message)
	}
}

func (r *FunctionEventTriggerReconciler) recordWarningEventIfChanged(trigger *functionsv1alpha1.FunctionEventTrigger, reason, message string) {
	if r.Recorder == nil {
		return
	}

	key := types.NamespacedName{Name: trigger.Name, Namespace: trigger.Namespace}
	signature := reason + "\x00" + message

	r.warningEventMu.Lock()
	if r.warningEventSignatures == nil {
		r.warningEventSignatures = map[types.NamespacedName]string{}
	}
	if r.warningEventSignatures[key] == signature {
		r.warningEventMu.Unlock()
		return
	}
	r.warningEventSignatures[key] = signature
	r.warningEventMu.Unlock()

	r.recordEvent(trigger, corev1.EventTypeWarning, reason, message)
}

func (r *FunctionEventTriggerReconciler) clearWarningEventSignature(trigger *functionsv1alpha1.FunctionEventTrigger) {
	r.warningEventMu.Lock()
	delete(r.warningEventSignatures, types.NamespacedName{Name: trigger.Name, Namespace: trigger.Namespace})
	r.warningEventMu.Unlock()
}

func (r *FunctionEventTriggerReconciler) recordEvent(trigger *functionsv1alpha1.FunctionEventTrigger, eventType, reason, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Event(trigger, eventType, reason, message)
}

// SetupWithManager sets up the controller with the Manager.
func (r *FunctionEventTriggerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&functionsv1alpha1.FunctionEventTrigger{}).
		Complete(r)
}
