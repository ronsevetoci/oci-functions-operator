// Copyright 2026.
// SPDX-License-Identifier: Apache-2.0

package lifecycle

import (
	"context"
	"reflect"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocifunctions "github.com/oracle/oci-go-sdk/v65/functions"
)

func TestEnsureFunctionCreatesApplicationAndFunctionInJeddah(t *testing.T) {
	ctx := context.Background()
	fakeClient := &fakeManagementClient{}

	manager, err := NewOCI(OCIOptions{
		ConfigProvider: common.NewRawConfigurationProvider("tenancy", "user", "me-jeddah-1", "fingerprint", "private-key", nil),
		ClientFactory: func(common.ConfigurationProvider) (functionsManagementClient, error) {
			return fakeClient, nil
		},
	})
	if err != nil {
		t.Fatalf("NewOCI returned error: %v", err)
	}

	desired := DesiredFunction{
		Region:           "me-jeddah-1",
		CompartmentID:    "ocid1.compartment.oc1..exampleuniqueid",
		ApplicationName:  "demo-app",
		SubnetIDs:        []string{"ocid1.subnet.oc1.me-jeddah-1.exampleuniqueid"},
		DisplayName:      "hello",
		Image:            "me-jeddah-1.ocir.io/example/functions/hello:latest",
		MemoryInMBs:      256,
		TimeoutInSeconds: 60,
		Config:           map[string]string{"GREETING": "hello"},
		FreeformTags:     map[string]string{"managed-by": "oci-functions-operator"},
	}

	state, err := manager.EnsureFunction(ctx, desired)
	if err != nil {
		t.Fatalf("EnsureFunction returned error: %v", err)
	}
	if fakeClient.region != "me-jeddah-1" {
		t.Fatalf("region = %q, want me-jeddah-1", fakeClient.region)
	}
	if !state.Ready {
		t.Fatalf("state.Ready = false, want true")
	}
	if state.ApplicationID != fakeApplicationID || state.FunctionID != fakeFunctionID || state.InvokeEndpoint != fakeInvokeEndpoint {
		t.Fatalf("state = %#v, want created application/function IDs and endpoint", state)
	}
	if fakeClient.createdApplication.CompartmentId == nil || *fakeClient.createdApplication.CompartmentId != desired.CompartmentID {
		t.Fatalf("created application compartment = %#v, want %q", fakeClient.createdApplication.CompartmentId, desired.CompartmentID)
	}
	if !reflect.DeepEqual(fakeClient.createdApplication.SubnetIds, desired.SubnetIDs) {
		t.Fatalf("created application subnets = %#v, want %#v", fakeClient.createdApplication.SubnetIds, desired.SubnetIDs)
	}
	if fakeClient.createdFunction.ApplicationId == nil || *fakeClient.createdFunction.ApplicationId != fakeApplicationID {
		t.Fatalf("created function application ID = %#v, want %q", fakeClient.createdFunction.ApplicationId, fakeApplicationID)
	}
	if fakeClient.createdFunction.MemoryInMBs == nil || *fakeClient.createdFunction.MemoryInMBs != desired.MemoryInMBs {
		t.Fatalf("created function memory = %#v, want %d", fakeClient.createdFunction.MemoryInMBs, desired.MemoryInMBs)
	}
	if fakeClient.createdFunction.TimeoutInSeconds == nil || *fakeClient.createdFunction.TimeoutInSeconds != desired.TimeoutInSeconds {
		t.Fatalf("created function timeout = %#v, want %d", fakeClient.createdFunction.TimeoutInSeconds, desired.TimeoutInSeconds)
	}
	if !reflect.DeepEqual(fakeClient.createdFunction.Config, desired.Config) {
		t.Fatalf("created function config = %#v, want %#v", fakeClient.createdFunction.Config, desired.Config)
	}
}

const (
	fakeApplicationID  = "ocid1.fnapp.oc1.me-jeddah-1.exampleuniqueid"
	fakeFunctionID     = "ocid1.fnfunc.oc1.me-jeddah-1.exampleuniqueid"
	fakeInvokeEndpoint = "https://functions.me-jeddah-1.oci.oraclecloud.com"
)

type fakeManagementClient struct {
	region             string
	createdApplication ocifunctions.CreateApplicationDetails
	createdFunction    ocifunctions.CreateFunctionDetails
}

func (f *fakeManagementClient) SetRegion(region string) {
	f.region = region
}

func (f *fakeManagementClient) ListApplications(context.Context, ocifunctions.ListApplicationsRequest) (ocifunctions.ListApplicationsResponse, error) {
	return ocifunctions.ListApplicationsResponse{}, nil
}

func (f *fakeManagementClient) CreateApplication(_ context.Context, request ocifunctions.CreateApplicationRequest) (ocifunctions.CreateApplicationResponse, error) {
	f.createdApplication = request.CreateApplicationDetails
	return ocifunctions.CreateApplicationResponse{
		Application: ocifunctions.Application{
			Id:             common.String(fakeApplicationID),
			DisplayName:    request.CreateApplicationDetails.DisplayName,
			LifecycleState: ocifunctions.ApplicationLifecycleStateActive,
		},
	}, nil
}

func (f *fakeManagementClient) GetApplication(context.Context, ocifunctions.GetApplicationRequest) (ocifunctions.GetApplicationResponse, error) {
	return ocifunctions.GetApplicationResponse{
		Application: ocifunctions.Application{
			Id:             common.String(fakeApplicationID),
			DisplayName:    common.String("demo-app"),
			LifecycleState: ocifunctions.ApplicationLifecycleStateActive,
		},
	}, nil
}

func (f *fakeManagementClient) ListFunctions(context.Context, ocifunctions.ListFunctionsRequest) (ocifunctions.ListFunctionsResponse, error) {
	return ocifunctions.ListFunctionsResponse{}, nil
}

func (f *fakeManagementClient) CreateFunction(_ context.Context, request ocifunctions.CreateFunctionRequest) (ocifunctions.CreateFunctionResponse, error) {
	f.createdFunction = request.CreateFunctionDetails
	return ocifunctions.CreateFunctionResponse{
		Function: ocifunctions.Function{
			Id:             common.String(fakeFunctionID),
			DisplayName:    request.CreateFunctionDetails.DisplayName,
			InvokeEndpoint: common.String(fakeInvokeEndpoint),
			LifecycleState: ocifunctions.FunctionLifecycleStateActive,
		},
	}, nil
}

func (f *fakeManagementClient) GetFunction(context.Context, ocifunctions.GetFunctionRequest) (ocifunctions.GetFunctionResponse, error) {
	return ocifunctions.GetFunctionResponse{
		Function: ocifunctions.Function{
			Id:             common.String(fakeFunctionID),
			DisplayName:    common.String("hello"),
			InvokeEndpoint: common.String(fakeInvokeEndpoint),
			LifecycleState: ocifunctions.FunctionLifecycleStateActive,
		},
	}, nil
}

func (f *fakeManagementClient) UpdateFunction(context.Context, ocifunctions.UpdateFunctionRequest) (ocifunctions.UpdateFunctionResponse, error) {
	return ocifunctions.UpdateFunctionResponse{}, nil
}
