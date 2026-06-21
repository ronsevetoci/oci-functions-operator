// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import "context"

// DesiredFunction is the SDK-free desired state for a managed OCI Function.
type DesiredFunction struct {
	Region           string
	CompartmentID    string
	ApplicationName  string
	SubnetIDs        []string
	DisplayName      string
	Image            string
	MemoryInMBs      int64
	TimeoutInSeconds int
	Config           map[string]string
	FreeformTags     map[string]string
}

// FunctionState is the SDK-free observed state for a managed OCI Function.
type FunctionState struct {
	ApplicationID  string
	FunctionID     string
	InvokeEndpoint string
	Ready          bool
	Message        string
}

// Manager reconciles OCI Functions lifecycle behind a small SDK-free contract.
type Manager interface {
	EnsureFunction(ctx context.Context, desired DesiredFunction) (FunctionState, error)
}
