// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oracle/oci-functions-operator/internal/invoker"
)

func TestSelectInvokerDefaultsToFake(t *testing.T) {
	selectedInvoker, mode, err := selectInvoker("")
	if err != nil {
		t.Fatalf("selectInvoker returned error: %v", err)
	}
	if mode != invokerModeFake {
		t.Fatalf("mode = %q, want %q", mode, invokerModeFake)
	}

	response, err := selectedInvoker.Invoke(context.Background(), invoker.Request{
		Target: invoker.Target{FunctionName: "hello"},
		Index:  7,
	})
	if err != nil {
		t.Fatalf("fake invoke returned error: %v", err)
	}
	if response.InvocationID != "fake-hello-7" || response.StatusCode != 202 {
		t.Fatalf("fake response = %#v, want deterministic success", response)
	}
}

func TestSelectInvokerSupportsExplicitFake(t *testing.T) {
	selectedInvoker, mode, err := selectInvoker("  FAKE  ")
	if err != nil {
		t.Fatalf("selectInvoker returned error: %v", err)
	}
	if mode != invokerModeFake {
		t.Fatalf("mode = %q, want %q", mode, invokerModeFake)
	}
	if selectedInvoker == nil {
		t.Fatalf("selectedInvoker is nil, want fake invoker")
	}
}

func TestSelectInvokerSupportsOCI(t *testing.T) {
	resetOCIInvoker := replaceOCIInvoker(func() (invoker.Interface, error) {
		return invoker.Fake{}, nil
	})
	defer resetOCIInvoker()

	selectedInvoker, mode, err := selectInvoker(invokerModeOCI)
	if err != nil {
		t.Fatalf("selectInvoker returned error: %v", err)
	}
	if selectedInvoker == nil {
		t.Fatalf("selectedInvoker is nil, want OCI invoker")
	}
	if mode != invokerModeOCI {
		t.Fatalf("mode = %q, want %q", mode, invokerModeOCI)
	}
}

func TestSelectInvokerPropagatesOCIConstructionError(t *testing.T) {
	expectedErr := errors.New("missing OCI endpoint")
	resetOCIInvoker := replaceOCIInvoker(func() (invoker.Interface, error) {
		return nil, expectedErr
	})
	defer resetOCIInvoker()

	selectedInvoker, mode, err := selectInvoker(invokerModeOCI)
	if err == nil {
		t.Fatalf("selectInvoker returned nil error, want OCI construction error")
	}
	if selectedInvoker != nil {
		t.Fatalf("selectedInvoker = %#v, want nil on error", selectedInvoker)
	}
	if mode != invokerModeOCI {
		t.Fatalf("mode = %q, want %q", mode, invokerModeOCI)
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("error = %v, want %v", err, expectedErr)
	}
}

func TestSelectInvokerRejectsUnknownMode(t *testing.T) {
	selectedInvoker, mode, err := selectInvoker("bogus")
	if err == nil {
		t.Fatalf("selectInvoker returned nil error, want unsupported mode error")
	}
	if selectedInvoker != nil {
		t.Fatalf("selectedInvoker = %#v, want nil on error", selectedInvoker)
	}
	if mode != "bogus" {
		t.Fatalf("mode = %q, want normalized unknown mode", mode)
	}
	if !strings.Contains(err.Error(), "unsupported "+invokerModeEnv) {
		t.Fatalf("error = %q, want unsupported mode message", err)
	}
}

func replaceOCIInvoker(replacement func() (invoker.Interface, error)) func() {
	previous := newOCIInvoker
	newOCIInvoker = replacement
	return func() {
		newOCIInvoker = previous
	}
}
