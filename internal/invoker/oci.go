// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package invoker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocifunctions "github.com/oracle/oci-go-sdk/v65/functions"
)

const (
	// EnvOCIFunctionsInvokeEndpoint is the endpoint used by the OCI Functions invoke client.
	EnvOCIFunctionsInvokeEndpoint = "OCI_FUNCTIONS_INVOKE_ENDPOINT"
	// EnvOCIConfigProfile optionally selects a profile from the OCI config file.
	EnvOCIConfigProfile = "OCI_CONFIG_PROFILE"
	// EnvOCIConfigFile optionally selects a non-default OCI config file path.
	EnvOCIConfigFile = "OCI_CONFIG_FILE"
)

const maxOCIErrorBodyBytes = 512

type functionsInvokeClient interface {
	InvokeFunction(ctx context.Context, request ocifunctions.InvokeFunctionRequest) (ocifunctions.InvokeFunctionResponse, error)
}

// OCIOptions configures an OCI-backed invoker.
type OCIOptions struct {
	InvokeEndpoint string
	ConfigProvider common.ConfigurationProvider
}

// OCI invokes OCI Functions through the OCI Go SDK.
type OCI struct {
	client functionsInvokeClient
}

// RequiresFunctionID reports that OCI mode requires Function.spec.functionId.
func (o *OCI) RequiresFunctionID() bool {
	return true
}

// NewOCIFromEnvironment constructs an OCI invoker from local OCI SDK configuration.
func NewOCIFromEnvironment() (*OCI, error) {
	return NewOCI(OCIOptions{
		InvokeEndpoint: os.Getenv(EnvOCIFunctionsInvokeEndpoint),
		ConfigProvider: ociConfigProviderFromEnvironment(),
	})
}

// NewOCI constructs an OCI invoker.
func NewOCI(options OCIOptions) (*OCI, error) {
	endpoint, err := normalizeInvokeEndpoint(options.InvokeEndpoint)
	if err != nil {
		return nil, err
	}

	configProvider := options.ConfigProvider
	if configProvider == nil {
		configProvider = ociConfigProviderFromEnvironment()
	}

	client, err := ocifunctions.NewFunctionsInvokeClientWithConfigurationProvider(configProvider, endpoint)
	if err != nil {
		return nil, fmt.Errorf("configure OCI Functions invoke client: %w", err)
	}

	return &OCI{client: client}, nil
}

func ociConfigProviderFromEnvironment() common.ConfigurationProvider {
	profile := strings.TrimSpace(os.Getenv(EnvOCIConfigProfile))
	if profile != "" {
		return common.CustomProfileConfigProvider(os.Getenv(EnvOCIConfigFile), profile)
	}
	return common.DefaultConfigProvider()
}

func normalizeInvokeEndpoint(value string) (string, error) {
	endpoint := strings.TrimSpace(value)
	if endpoint == "" {
		return "", fmt.Errorf("%s is required when INVOKER_MODE=oci", EnvOCIFunctionsInvokeEndpoint)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid %s %q: %w", EnvOCIFunctionsInvokeEndpoint, endpoint, err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("invalid %s %q: endpoint must start with https:// or http://", EnvOCIFunctionsInvokeEndpoint, endpoint)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid %s %q: endpoint must include a host", EnvOCIFunctionsInvokeEndpoint, endpoint)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("invalid %s %q: endpoint must not include a path; the OCI SDK adds the Functions API path", EnvOCIFunctionsInvokeEndpoint, endpoint)
	}

	return strings.TrimRight(endpoint, "/"), nil
}

// Invoke invokes an existing OCI Function by OCID.
func (o *OCI) Invoke(ctx context.Context, request Request) (Response, error) {
	functionID := strings.TrimSpace(request.Target.FunctionOCID)
	if functionID == "" {
		return Response{}, fmt.Errorf("oci invoker requires target function OCID")
	}
	if o == nil || o.client == nil {
		return Response{}, fmt.Errorf("oci invoker is not configured")
	}

	ociRequest := ocifunctions.InvokeFunctionRequest{
		FunctionId:   common.String(functionID),
		FnInvokeType: ocifunctions.InvokeFunctionFnInvokeTypeSync,
	}
	if request.Body != nil {
		ociRequest.InvokeFunctionBody = io.NopCloser(bytes.NewReader(request.Body))
	}

	ociResponse, err := o.client.InvokeFunction(ctx, ociRequest)
	response := responseFromOCI(ociResponse)
	if err != nil {
		if response.OCIRequestID == "" {
			response.OCIRequestID = ociRequestIDFromError(err)
		}
		if response.StatusCode == 0 {
			response.StatusCode = int32(statusCodeFromError(err))
		}
		return response, classifyOCIInvokeError(functionID, response, err)
	}

	if ociResponse.Content != nil {
		defer ociResponse.Content.Close()
		body, readErr := io.ReadAll(ociResponse.Content)
		if readErr != nil {
			return response, fmt.Errorf("read OCI Function %s response body: %w", functionID, readErr)
		}
		response.Body = body
	}
	if response.StatusCode != 0 && (response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices) {
		return response, non2xxOCIResponseError(functionID, response, response.Body)
	}

	return response, nil
}

func responseFromOCI(response ocifunctions.InvokeFunctionResponse) Response {
	result := Response{}
	if response.RawResponse != nil {
		result.StatusCode = int32(response.RawResponse.StatusCode)
		if callID := response.RawResponse.Header.Get("Fn-Call-Id"); callID != "" {
			result.InvocationID = callID
		}
		result.OCIRequestID = response.RawResponse.Header.Get("opc-request-id")
		if result.InvocationID == "" {
			result.InvocationID = result.OCIRequestID
		}
	}
	if response.OpcRequestId != nil {
		if result.OCIRequestID == "" {
			result.OCIRequestID = *response.OpcRequestId
		}
		if result.InvocationID == "" {
			result.InvocationID = *response.OpcRequestId
		}
	}
	if result.OCIRequestID == "" {
		result.OCIRequestID = result.InvocationID
	}
	return result
}

func classifyOCIInvokeError(functionID string, response Response, err error) error {
	details := ociErrorDetails(response)
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || isNetTimeout(err) {
		return fmt.Errorf("oci timeout invoking function %s%s: %w", functionID, details, err)
	}
	if isEndpointError(err) {
		return fmt.Errorf("oci endpoint error invoking function %s%s: %w", functionID, details, err)
	}
	if serviceError, ok := common.IsServiceError(err); ok {
		statusCode := serviceError.GetHTTPStatusCode()
		if response.OCIRequestID == "" {
			response.OCIRequestID = serviceError.GetOpcRequestID()
		}
		if response.StatusCode == 0 {
			response.StatusCode = int32(statusCode)
		}
		serviceDetails := ociErrorDetails(response)
		if serviceError.GetCode() != "" {
			serviceDetails += fmt.Sprintf(" code=%s", serviceError.GetCode())
		}

		switch {
		case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
			return fmt.Errorf("oci auth error invoking function %s%s: %s", functionID, serviceDetails, serviceError.GetMessage())
		case statusCode == http.StatusNotFound:
			return fmt.Errorf("oci function OCID error invoking function %s%s: %s", functionID, serviceDetails, serviceError.GetMessage())
		default:
			return fmt.Errorf("oci service error invoking function %s%s: %s", functionID, serviceDetails, serviceError.GetMessage())
		}
	}
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s: %w", non2xxOCIResponseError(functionID, response, response.Body).Error(), err)
	}
	return fmt.Errorf("oci invoke error invoking function %s%s: %w", functionID, details, err)
}

func non2xxOCIResponseError(functionID string, response Response, body []byte) error {
	return fmt.Errorf("oci non-2xx response invoking function %s%s body=%q", functionID, ociErrorDetails(response), truncateForStatus(body))
}

func ociErrorDetails(response Response) string {
	parts := []string{}
	if response.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", response.StatusCode))
	}
	if response.OCIRequestID != "" {
		parts = append(parts, fmt.Sprintf("ociRequestId=%s", response.OCIRequestID))
	}
	if response.InvocationID != "" && response.InvocationID != response.OCIRequestID {
		parts = append(parts, fmt.Sprintf("invocationId=%s", response.InvocationID))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func truncateForStatus(body []byte) string {
	value := strings.TrimSpace(string(body))
	if len(value) <= maxOCIErrorBodyBytes {
		return value
	}
	return value[:maxOCIErrorBodyBytes] + "...(truncated)"
}

func ociRequestIDFromError(err error) string {
	if serviceError, ok := common.IsServiceError(err); ok {
		return serviceError.GetOpcRequestID()
	}
	return ""
}

func statusCodeFromError(err error) int {
	if serviceError, ok := common.IsServiceError(err); ok {
		return serviceError.GetHTTPStatusCode()
	}
	return 0
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isEndpointError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
