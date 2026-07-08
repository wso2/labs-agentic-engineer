// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package orchestration

import (
	"context"
	"fmt"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// fakeTaskDriver lets DispatchTask return a canned error.
type fakeTaskDriver struct {
	dispatchErr error
}

func (f fakeTaskDriver) DispatchTask(context.Context, contract.TaskLifecycleInput) error { return f.dispatchErr }
func (f fakeTaskDriver) DeployTask(context.Context, contract.TaskLifecycleInput) error   { return nil }
func (f fakeTaskDriver) AutoMerge(context.Context, contract.TaskLifecycleInput) error     { return nil }

func newTestAPI(t *testing.T, svc *InternalService) (humatest.TestAPI, string) {
	t.Helper()
	SetInternalBearer("test-secret")
	t.Cleanup(func() { SetInternalBearer("") })
	_, api := humatest.New(t)
	RegisterInternalOrchestration(api, svc)
	return api, "Bearer test-secret"
}

// TestDispatchTask_QuotaExceeded_Returns429 covers the §R3.4 retriable-error
// mapping at the HTTP layer: a ClusterGatewayProxy ErrQuotaExceeded from the
// TaskDriver surfaces as 429, not the generic 500, so the orchestrator's
// activity retry policy can treat it as transient backoff.
func TestDispatchTask_QuotaExceeded_Returns429(t *testing.T) {
	svc := NewInternalService(nil, nil, fakeTaskDriver{
		dispatchErr: fmt.Errorf("dispatcher: apply Job: %w", clustergatewayproxy.ErrQuotaExceeded),
	})
	api, bearer := newTestAPI(t, svc)

	resp := api.Post("/internal/v1/orchestration/tasks/dispatch",
		"Authorization: "+bearer,
		map[string]any{"Org": "acme", "Project": "widgets", "TaskID": "t1", "ComponentName": "order-service", "CodeReview": "human"})
	if resp.Code != 429 {
		t.Fatalf("status = %d, want 429; body: %s", resp.Code, resp.Body.String())
	}
}

// TestDispatchTask_OtherError_Returns500 confirms non-quota failures keep
// the existing 500 behavior (no over-broad retriable classification).
func TestDispatchTask_OtherError_Returns500(t *testing.T) {
	svc := NewInternalService(nil, nil, fakeTaskDriver{dispatchErr: fmt.Errorf("boom")})
	api, bearer := newTestAPI(t, svc)

	resp := api.Post("/internal/v1/orchestration/tasks/dispatch",
		"Authorization: "+bearer,
		map[string]any{"Org": "acme", "Project": "widgets", "TaskID": "t1", "ComponentName": "order-service", "CodeReview": "human"})
	if resp.Code != 500 {
		t.Fatalf("status = %d, want 500; body: %s", resp.Code, resp.Body.String())
	}
}

// TestDispatchTask_MissingBearer_Returns401 pins the §R3.1 auth gate at the
// HTTP layer for this same endpoint.
func TestDispatchTask_MissingBearer_Returns401(t *testing.T) {
	svc := NewInternalService(nil, nil, fakeTaskDriver{})
	api, _ := newTestAPI(t, svc)

	resp := api.Post("/internal/v1/orchestration/tasks/dispatch",
		map[string]any{"Org": "acme", "Project": "widgets", "TaskID": "t1", "ComponentName": "order-service", "CodeReview": "human"})
	if resp.Code != 401 {
		t.Fatalf("status = %d, want 401; body: %s", resp.Code, resp.Body.String())
	}
}
