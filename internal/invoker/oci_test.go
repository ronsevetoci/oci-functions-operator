// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package invoker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocifunctions "github.com/oracle/oci-go-sdk/v65/functions"
)

func TestNormalizeInvokeEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      string
		wantError string
	}{
		{
			name:      "missing",
			wantError: EnvOCIFunctionsInvokeEndpoint + " is required",
		},
		{
			name:      "missing scheme",
			value:     "functions.us-ashburn-1.oci.oraclecloud.com",
			wantError: "must start with https:// or http://",
		},
		{
			name:      "missing host",
			value:     "https://",
			wantError: "endpoint must include a host",
		},
		{
			name:      "path rejected",
			value:     "https://functions.us-ashburn-1.oci.oraclecloud.com/20181201",
			wantError: "must not include a path",
		},
		{
			name:  "trims trailing slash",
			value: " https://functions.us-ashburn-1.oci.oraclecloud.com/ ",
			want:  "https://functions.us-ashburn-1.oci.oraclecloud.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeInvokeEndpoint(tt.value)
			if tt.wantError != "" {
				if err == nil {
					t.Fatalf("normalizeInvokeEndpoint returned nil error, want %q", tt.wantError)
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %q, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeInvokeEndpoint returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewOCIRequiresEndpointBeforeConfig(t *testing.T) {
	_, err := NewOCI(OCIOptions{})
	if err == nil {
		t.Fatalf("NewOCI returned nil error, want endpoint error")
	}
	if !strings.Contains(err.Error(), EnvOCIFunctionsInvokeEndpoint) {
		t.Fatalf("error = %q, want endpoint name", err)
	}
}

func TestNewOCIFromEnvironmentRequiresEndpoint(t *testing.T) {
	t.Setenv(EnvOCIFunctionsInvokeEndpoint, "")

	_, err := NewOCIFromEnvironment()
	if err == nil {
		t.Fatalf("NewOCIFromEnvironment returned nil error, want endpoint error")
	}
	if !strings.Contains(err.Error(), EnvOCIFunctionsInvokeEndpoint) {
		t.Fatalf("error = %q, want endpoint name", err)
	}
}

func TestNewOCIWrapsConfigProviderErrors(t *testing.T) {
	_, err := NewOCI(OCIOptions{
		InvokeEndpoint: "https://functions.us-ashburn-1.oci.oraclecloud.com",
		ConfigProvider: common.NewRawConfigurationProvider("", "", "", "", "", nil),
	})
	if err == nil {
		t.Fatalf("NewOCI returned nil error, want config provider error")
	}
	if !strings.Contains(err.Error(), "configure OCI Functions invoke client") {
		t.Fatalf("error = %q, want OCI client configuration context", err)
	}
}

func TestOCIInvokeClassifiesTimeout(t *testing.T) {
	fakeClient := &fakeFunctionsInvokeClient{err: context.DeadlineExceeded}
	ociInvoker := &OCI{client: fakeClient}

	_, err := ociInvoker.Invoke(context.Background(), Request{
		Target: Target{FunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid"},
	})
	if err == nil {
		t.Fatalf("Invoke returned nil error, want timeout error")
	}
	if !strings.Contains(err.Error(), "oci timeout") {
		t.Fatalf("error = %q, want timeout classification", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want wrapped context deadline", err)
	}
}

func TestOCIInvokeRequiresFunctionOCID(t *testing.T) {
	ociInvoker := &OCI{client: &fakeFunctionsInvokeClient{}}

	_, err := ociInvoker.Invoke(context.Background(), Request{})
	if err == nil {
		t.Fatalf("Invoke returned nil error, want missing function OCID error")
	}
	if !strings.Contains(err.Error(), "function OCID") {
		t.Fatalf("error = %q, want function OCID message", err)
	}
}

func TestOCIInvokeMapsRequestAndResponse(t *testing.T) {
	fakeClient := &fakeFunctionsInvokeClient{
		response: ocifunctions.InvokeFunctionResponse{
			RawResponse: &http.Response{
				StatusCode: http.StatusAccepted,
				Header: http.Header{
					"Fn-Call-Id":     []string{"call-id"},
					"Opc-Request-Id": []string{"opc-request-id"},
				},
			},
			Content: io.NopCloser(strings.NewReader(`{"ok":true}`)),
		},
	}
	ociInvoker := &OCI{client: fakeClient}

	response, err := ociInvoker.Invoke(context.Background(), Request{
		Target: Target{FunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid"},
		Index:  3,
		Body:   []byte(`{"input":true}`),
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}

	if fakeClient.functionID == nil || *fakeClient.functionID != "ocid1.fnfunc.oc1.iad.exampleuniqueid" {
		t.Fatalf("function ID = %v, want request target OCID", fakeClient.functionID)
	}
	if string(fakeClient.body) != `{"input":true}` {
		t.Fatalf("request body = %s, want inline payload", fakeClient.body)
	}
	if fakeClient.invokeType != ocifunctions.InvokeFunctionFnInvokeTypeSync {
		t.Fatalf("invoke type = %q, want sync", fakeClient.invokeType)
	}
	if response.InvocationID != "call-id" {
		t.Fatalf("invocation ID = %q, want Fn-Call-Id", response.InvocationID)
	}
	if response.OCIRequestID != "opc-request-id" {
		t.Fatalf("OCI request ID = %q, want opc-request-id", response.OCIRequestID)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
	if string(response.Body) != `{"ok":true}` {
		t.Fatalf("response body = %s, want function response body", response.Body)
	}
}

func TestOCIInvokeFallsBackToOpcRequestID(t *testing.T) {
	opcRequestID := "opc-request-id"
	fakeClient := &fakeFunctionsInvokeClient{
		response: ocifunctions.InvokeFunctionResponse{
			OpcRequestId: common.String(opcRequestID),
			RawResponse:  &http.Response{StatusCode: http.StatusOK},
		},
	}
	ociInvoker := &OCI{client: fakeClient}

	response, err := ociInvoker.Invoke(context.Background(), Request{
		Target: Target{FunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid"},
	})
	if err != nil {
		t.Fatalf("Invoke returned error: %v", err)
	}
	if response.InvocationID != opcRequestID {
		t.Fatalf("invocation ID = %q, want opc request ID", response.InvocationID)
	}
	if response.OCIRequestID != opcRequestID {
		t.Fatalf("OCI request ID = %q, want opc request ID", response.OCIRequestID)
	}
}

func TestOCIInvokeFailsOnNon2xxResponse(t *testing.T) {
	largeBody := strings.Repeat("x", maxOCIErrorBodyBytes+100)
	fakeClient := &fakeFunctionsInvokeClient{
		response: ocifunctions.InvokeFunctionResponse{
			RawResponse: &http.Response{
				StatusCode: http.StatusBadGateway,
				Header: http.Header{
					"Opc-Request-Id": []string{"opc-request-id"},
				},
			},
			Content: io.NopCloser(strings.NewReader(largeBody)),
		},
	}
	ociInvoker := &OCI{client: fakeClient}

	response, err := ociInvoker.Invoke(context.Background(), Request{
		Target: Target{FunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid"},
	})
	if err == nil {
		t.Fatalf("Invoke returned nil error, want non-2xx error")
	}
	if !strings.Contains(err.Error(), "oci non-2xx response") {
		t.Fatalf("error = %q, want non-2xx classification", err)
	}
	if !strings.Contains(err.Error(), "status=502") || !strings.Contains(err.Error(), "ociRequestId=opc-request-id") {
		t.Fatalf("error = %q, want status and request ID", err)
	}
	if !strings.Contains(err.Error(), "(truncated)") {
		t.Fatalf("error = %q, want truncated body marker", err)
	}
	if len(err.Error()) > maxOCIErrorBodyBytes+250 {
		t.Fatalf("error length = %d, want bounded non-2xx message", len(err.Error()))
	}
	if response.OCIRequestID != "opc-request-id" || response.StatusCode != http.StatusBadGateway {
		t.Fatalf("response metadata = %#v, want request ID and 502 status", response)
	}
}

func TestOCIInvokeReturnsResponseMetadataOnSDKError(t *testing.T) {
	expectedErr := errors.New("sdk failure")
	fakeClient := &fakeFunctionsInvokeClient{
		err: expectedErr,
		response: ocifunctions.InvokeFunctionResponse{
			RawResponse: &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Opc-Request-Id": []string{"opc-request-id"}},
			},
		},
	}
	ociInvoker := &OCI{client: fakeClient}

	response, err := ociInvoker.Invoke(context.Background(), Request{
		Target: Target{FunctionOCID: "ocid1.fnfunc.oc1.iad.exampleuniqueid"},
	})
	if err == nil {
		t.Fatalf("Invoke returned nil error, want SDK error")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("error = %v, want wrapped %v", err, expectedErr)
	}
	if response.OCIRequestID != "opc-request-id" || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("response metadata = %#v, want request ID and status from failed response", response)
	}
}

type fakeFunctionsInvokeClient struct {
	response   ocifunctions.InvokeFunctionResponse
	err        error
	functionID *string
	body       []byte
	invokeType ocifunctions.InvokeFunctionFnInvokeTypeEnum
}

func (f *fakeFunctionsInvokeClient) InvokeFunction(_ context.Context, request ocifunctions.InvokeFunctionRequest) (ocifunctions.InvokeFunctionResponse, error) {
	f.functionID = request.FunctionId
	f.invokeType = request.FnInvokeType
	if request.InvokeFunctionBody != nil {
		body, err := io.ReadAll(request.InvokeFunctionBody)
		if err != nil {
			return ocifunctions.InvokeFunctionResponse{}, err
		}
		f.body = body
	}
	return f.response, f.err
}
