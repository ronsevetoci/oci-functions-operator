// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/oracle/oci-go-sdk/v65/common"
	ociauth "github.com/oracle/oci-go-sdk/v65/common/auth"
	ocifunctions "github.com/oracle/oci-go-sdk/v65/functions"
)

const (
	// EnvOCIAuthMode selects the OCI SDK auth provider used in OCI lifecycle mode.
	EnvOCIAuthMode = "OCI_AUTH_MODE"
	// EnvOCIConfigProfile optionally selects a profile from the OCI config file.
	EnvOCIConfigProfile = "OCI_CONFIG_PROFILE"
	// EnvOCIConfigFile optionally selects a non-default OCI config file path.
	EnvOCIConfigFile = "OCI_CONFIG_FILE"

	// OCIAuthModeWorkload uses the OKE Workload Identity auth provider.
	OCIAuthModeWorkload = "workload"
	// OCIAuthModeConfig uses a local OCI config file/profile.
	OCIAuthModeConfig = "config"
)

type functionsManagementClient interface {
	SetRegion(region string)
	ListApplications(context.Context, ocifunctions.ListApplicationsRequest) (ocifunctions.ListApplicationsResponse, error)
	CreateApplication(context.Context, ocifunctions.CreateApplicationRequest) (ocifunctions.CreateApplicationResponse, error)
	GetApplication(context.Context, ocifunctions.GetApplicationRequest) (ocifunctions.GetApplicationResponse, error)
	ListFunctions(context.Context, ocifunctions.ListFunctionsRequest) (ocifunctions.ListFunctionsResponse, error)
	CreateFunction(context.Context, ocifunctions.CreateFunctionRequest) (ocifunctions.CreateFunctionResponse, error)
	GetFunction(context.Context, ocifunctions.GetFunctionRequest) (ocifunctions.GetFunctionResponse, error)
	UpdateFunction(context.Context, ocifunctions.UpdateFunctionRequest) (ocifunctions.UpdateFunctionResponse, error)
}

type managementClientFactory func(common.ConfigurationProvider) (functionsManagementClient, error)
type workloadIdentityProviderFactory func() (common.ConfigurationProvider, error)
type configFileProviderFactory func() common.ConfigurationProvider

// OCIOptions configures an OCI lifecycle manager.
type OCIOptions struct {
	AuthMode                        string
	ConfigProvider                  common.ConfigurationProvider
	WorkloadIdentityProviderFactory workloadIdentityProviderFactory
	ConfigFileProviderFactory       configFileProviderFactory
	ClientFactory                   managementClientFactory
}

// OCI manages OCI Functions lifecycle through the OCI Go SDK.
type OCI struct {
	client functionsManagementClient
}

// NewOCIFromEnvironment constructs an OCI lifecycle manager from OCI-related environment variables.
func NewOCIFromEnvironment() (*OCI, error) {
	return NewOCI(OCIOptions{AuthMode: getenv(EnvOCIAuthMode)})
}

// NewOCI constructs an OCI lifecycle manager.
func NewOCI(options OCIOptions) (*OCI, error) {
	configProvider := options.ConfigProvider
	var err error
	if configProvider == nil {
		configProvider, err = configProviderForAuthMode(options)
		if err != nil {
			return nil, err
		}
	}

	clientFactory := options.ClientFactory
	if clientFactory == nil {
		clientFactory = newFunctionsManagementClient
	}

	client, err := clientFactory(configProvider)
	if err != nil {
		return nil, fmt.Errorf("configure OCI Functions management client: %w", err)
	}

	return &OCI{client: client}, nil
}

func newFunctionsManagementClient(configProvider common.ConfigurationProvider) (functionsManagementClient, error) {
	client, err := ocifunctions.NewFunctionsManagementClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, err
	}
	return &client, nil
}

// EnsureFunction ensures the OCI application and function exist and match the desired function config.
func (o *OCI) EnsureFunction(ctx context.Context, desired DesiredFunction) (FunctionState, error) {
	if o == nil || o.client == nil {
		return FunctionState{}, fmt.Errorf("oci lifecycle manager is not configured")
	}
	if err := validateDesiredFunction(desired); err != nil {
		return FunctionState{}, err
	}

	o.client.SetRegion(desired.Region)

	application, err := o.ensureApplication(ctx, desired)
	if err != nil {
		return FunctionState{}, err
	}

	state := FunctionState{ApplicationID: stringValue(application.Id)}
	if application.LifecycleState != "" && application.LifecycleState != ocifunctions.ApplicationLifecycleStateActive {
		state.Message = fmt.Sprintf("OCI application %q is %s.", desired.ApplicationName, application.LifecycleState)
		return state, nil
	}

	function, err := o.ensureFunction(ctx, desired, state.ApplicationID)
	if err != nil {
		return state, err
	}

	state.FunctionID = stringValue(function.Id)
	state.InvokeEndpoint = strings.TrimSpace(stringValue(function.InvokeEndpoint))
	if function.LifecycleState != "" && function.LifecycleState != ocifunctions.FunctionLifecycleStateActive {
		state.Message = fmt.Sprintf("OCI function %q is %s.", desired.DisplayName, function.LifecycleState)
		return state, nil
	}
	if state.FunctionID == "" || state.InvokeEndpoint == "" {
		state.Message = "OCI function exists but invoke endpoint is not available yet."
		return state, nil
	}

	state.Ready = true
	state.Message = "Managed OCI Function is ready."
	return state, nil
}

func (o *OCI) ensureApplication(ctx context.Context, desired DesiredFunction) (ocifunctions.Application, error) {
	response, err := o.client.ListApplications(ctx, ocifunctions.ListApplicationsRequest{
		CompartmentId: common.String(desired.CompartmentID),
		DisplayName:   common.String(desired.ApplicationName),
		Limit:         common.Int(50),
	})
	if err != nil {
		return ocifunctions.Application{}, fmt.Errorf("list OCI Functions applications: %w", err)
	}
	for _, item := range response.Items {
		if stringValue(item.DisplayName) != desired.ApplicationName {
			continue
		}
		if item.LifecycleState == ocifunctions.ApplicationLifecycleStateDeleted || item.LifecycleState == ocifunctions.ApplicationLifecycleStateDeleting {
			continue
		}
		return o.getApplication(ctx, stringValue(item.Id))
	}

	created, err := o.client.CreateApplication(ctx, ocifunctions.CreateApplicationRequest{
		CreateApplicationDetails: ocifunctions.CreateApplicationDetails{
			CompartmentId: common.String(desired.CompartmentID),
			DisplayName:   common.String(desired.ApplicationName),
			SubnetIds:     append([]string(nil), desired.SubnetIDs...),
			FreeformTags:  copyStringMap(desired.FreeformTags),
		},
	})
	if err != nil {
		return ocifunctions.Application{}, fmt.Errorf("create OCI Functions application %q: %w", desired.ApplicationName, err)
	}
	return created.Application, nil
}

func (o *OCI) getApplication(ctx context.Context, applicationID string) (ocifunctions.Application, error) {
	if applicationID == "" {
		return ocifunctions.Application{}, fmt.Errorf("OCI application lookup returned an empty application OCID")
	}
	response, err := o.client.GetApplication(ctx, ocifunctions.GetApplicationRequest{ApplicationId: common.String(applicationID)})
	if err != nil {
		return ocifunctions.Application{}, fmt.Errorf("get OCI Functions application %s: %w", applicationID, err)
	}
	return response.Application, nil
}

func (o *OCI) ensureFunction(ctx context.Context, desired DesiredFunction, applicationID string) (ocifunctions.Function, error) {
	response, err := o.client.ListFunctions(ctx, ocifunctions.ListFunctionsRequest{
		ApplicationId: common.String(applicationID),
		DisplayName:   common.String(desired.DisplayName),
		Limit:         common.Int(50),
	})
	if err != nil {
		return ocifunctions.Function{}, fmt.Errorf("list OCI Functions in application %s: %w", applicationID, err)
	}
	for _, item := range response.Items {
		if stringValue(item.DisplayName) != desired.DisplayName {
			continue
		}
		if item.LifecycleState == ocifunctions.FunctionLifecycleStateDeleted || item.LifecycleState == ocifunctions.FunctionLifecycleStateDeleting {
			continue
		}
		function, err := o.getFunction(ctx, stringValue(item.Id))
		if err != nil {
			return ocifunctions.Function{}, err
		}
		if functionNeedsUpdate(function, desired) {
			updated, err := o.client.UpdateFunction(ctx, ocifunctions.UpdateFunctionRequest{
				FunctionId: common.String(stringValue(function.Id)),
				UpdateFunctionDetails: ocifunctions.UpdateFunctionDetails{
					Image:            common.String(desired.Image),
					MemoryInMBs:      common.Int64(desired.MemoryInMBs),
					TimeoutInSeconds: common.Int(desired.TimeoutInSeconds),
					Config:           copyStringMap(desired.Config),
					FreeformTags:     copyStringMap(desired.FreeformTags),
				},
			})
			if err != nil {
				return ocifunctions.Function{}, fmt.Errorf("update OCI Function %s: %w", stringValue(function.Id), err)
			}
			return updated.Function, nil
		}
		return function, nil
	}

	created, err := o.client.CreateFunction(ctx, ocifunctions.CreateFunctionRequest{
		CreateFunctionDetails: ocifunctions.CreateFunctionDetails{
			ApplicationId:    common.String(applicationID),
			DisplayName:      common.String(desired.DisplayName),
			Image:            common.String(desired.Image),
			MemoryInMBs:      common.Int64(desired.MemoryInMBs),
			TimeoutInSeconds: common.Int(desired.TimeoutInSeconds),
			Config:           copyStringMap(desired.Config),
			FreeformTags:     copyStringMap(desired.FreeformTags),
		},
	})
	if err != nil {
		return ocifunctions.Function{}, fmt.Errorf("create OCI Function %q: %w", desired.DisplayName, err)
	}
	return created.Function, nil
}

func (o *OCI) getFunction(ctx context.Context, functionID string) (ocifunctions.Function, error) {
	if functionID == "" {
		return ocifunctions.Function{}, fmt.Errorf("OCI function lookup returned an empty function OCID")
	}
	response, err := o.client.GetFunction(ctx, ocifunctions.GetFunctionRequest{FunctionId: common.String(functionID)})
	if err != nil {
		return ocifunctions.Function{}, fmt.Errorf("get OCI Function %s: %w", functionID, err)
	}
	return response.Function, nil
}

func functionNeedsUpdate(function ocifunctions.Function, desired DesiredFunction) bool {
	if stringValue(function.Image) != desired.Image {
		return true
	}
	if int64Value(function.MemoryInMBs) != desired.MemoryInMBs {
		return true
	}
	if intValue(function.TimeoutInSeconds) != desired.TimeoutInSeconds {
		return true
	}
	return !reflect.DeepEqual(nilToEmptyMap(function.Config), nilToEmptyMap(desired.Config))
}

func validateDesiredFunction(desired DesiredFunction) error {
	switch {
	case strings.TrimSpace(desired.Region) == "":
		return fmt.Errorf("managed Function requires spec.config.region")
	case strings.TrimSpace(desired.CompartmentID) == "":
		return fmt.Errorf("managed Function requires spec.config.compartmentId")
	case strings.TrimSpace(desired.ApplicationName) == "":
		return fmt.Errorf("managed Function requires spec.config.applicationName")
	case len(desired.SubnetIDs) == 0:
		return fmt.Errorf("managed Function requires spec.config.subnetIds")
	case strings.TrimSpace(desired.DisplayName) == "":
		return fmt.Errorf("managed Function requires spec.config.displayName")
	case strings.TrimSpace(desired.Image) == "":
		return fmt.Errorf("managed Function requires spec.config.image")
	case desired.MemoryInMBs <= 0:
		return fmt.Errorf("managed Function requires spec.config.memoryInMBs")
	case desired.TimeoutInSeconds <= 0:
		return fmt.Errorf("managed Function requires spec.config.timeoutInSeconds")
	}
	return nil
}

func configProviderForAuthMode(options OCIOptions) (common.ConfigurationProvider, error) {
	authMode, err := normalizeOCIAuthMode(options.AuthMode)
	if err != nil {
		return nil, err
	}

	switch authMode {
	case OCIAuthModeWorkload:
		providerFactory := options.WorkloadIdentityProviderFactory
		if providerFactory == nil {
			providerFactory = workloadIdentityConfigProviderFromEnvironment
		}
		configProvider, err := providerFactory()
		if err != nil {
			return nil, fmt.Errorf("configure OCI Workload Identity auth provider: %w", err)
		}
		return configProvider, nil
	case OCIAuthModeConfig:
		providerFactory := options.ConfigFileProviderFactory
		if providerFactory == nil {
			providerFactory = ociConfigProviderFromEnvironment
		}
		return providerFactory(), nil
	default:
		return nil, fmt.Errorf("unsupported %s %q; supported values are %q and %q", EnvOCIAuthMode, options.AuthMode, OCIAuthModeWorkload, OCIAuthModeConfig)
	}
}

func normalizeOCIAuthMode(value string) (string, error) {
	authMode := strings.ToLower(strings.TrimSpace(value))
	if authMode == "" {
		return OCIAuthModeWorkload, nil
	}
	switch authMode {
	case OCIAuthModeWorkload, OCIAuthModeConfig:
		return authMode, nil
	default:
		return "", fmt.Errorf("unsupported %s %q; supported values are %q and %q", EnvOCIAuthMode, value, OCIAuthModeWorkload, OCIAuthModeConfig)
	}
}

func workloadIdentityConfigProviderFromEnvironment() (common.ConfigurationProvider, error) {
	return ociauth.OkeWorkloadIdentityConfigurationProvider()
}

func ociConfigProviderFromEnvironment() common.ConfigurationProvider {
	profile := strings.TrimSpace(getenv(EnvOCIConfigProfile))
	if profile != "" {
		return common.CustomProfileConfigProvider(getenv(EnvOCIConfigFile), profile)
	}
	return common.DefaultConfigProvider()
}

func getenv(key string) string {
	return strings.TrimSpace(strings.Trim(os.Getenv(key), "\x00"))
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func nilToEmptyMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}
