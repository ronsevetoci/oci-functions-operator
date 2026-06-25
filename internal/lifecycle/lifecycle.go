// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import "context"

// DesiredFunction is the SDK-free desired state for a managed OCI Function.
type DesiredFunction struct {
	Region                  string
	CompartmentID           string
	ApplicationName         string
	SubnetIDs               []string
	ApplicationNSGIDs       []string
	ManageApplicationNSGIDs bool
	DisplayName             string
	Image                   string
	MemoryInMBs             int64
	TimeoutInSeconds        int
	Config                  map[string]string
	FreeformTags            map[string]string
}

// ManagedFunctionDeleteTarget identifies a managed OCI Function to delete.
type ManagedFunctionDeleteTarget struct {
	Region          string
	CompartmentID   string
	ApplicationName string
	DisplayName     string
	FunctionID      string
}

// EventType is the SDK-free severity for lifecycle events.
type EventType string

const (
	// EventTypeNormal describes an expected lifecycle change.
	EventTypeNormal EventType = "Normal"
	// EventTypeWarning describes a lifecycle failure.
	EventTypeWarning EventType = "Warning"
)

// Event describes an operator-visible lifecycle action.
type Event struct {
	Type    EventType
	Reason  string
	Message string
}

// FunctionState is the SDK-free observed state for a managed OCI Function.
type FunctionState struct {
	ApplicationID  string
	FunctionID     string
	InvokeEndpoint string
	Ready          bool
	Message        string
	Events         []Event
}

// FunctionDeletionState is the SDK-free outcome of managed OCI Function cleanup.
type FunctionDeletionState struct {
	ApplicationID string
	FunctionID    string
	Deleted       bool
	Message       string
	Events        []Event
}

// Manager reconciles OCI Functions lifecycle behind a small SDK-free contract.
type Manager interface {
	EnsureFunction(ctx context.Context, desired DesiredFunction) (FunctionState, error)
	DeleteManagedFunction(ctx context.Context, target ManagedFunctionDeleteTarget) (FunctionDeletionState, error)
}
