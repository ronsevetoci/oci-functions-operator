// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"

	"github.com/oracle/oci-functions-operator/internal/invoker"
)

const (
	invokerModeEnv  = "INVOKER_MODE"
	invokerModeFake = "fake"
	invokerModeOCI  = "oci"
)

var newOCIInvoker = func() (invoker.Interface, error) {
	return invoker.NewOCIFromEnvironment()
}

func selectInvoker(mode string) (invoker.Interface, string, error) {
	normalizedMode := strings.ToLower(strings.TrimSpace(mode))
	if normalizedMode == "" {
		normalizedMode = invokerModeFake
	}

	switch normalizedMode {
	case invokerModeFake:
		return invoker.Fake{}, normalizedMode, nil
	case invokerModeOCI:
		selectedInvoker, err := newOCIInvoker()
		if err != nil {
			return nil, normalizedMode, err
		}
		return selectedInvoker, normalizedMode, nil
	default:
		return nil, normalizedMode, fmt.Errorf("unsupported %s %q; supported values are %q and %q", invokerModeEnv, mode, invokerModeFake, invokerModeOCI)
	}
}
