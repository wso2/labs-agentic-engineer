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
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

type componentsInput struct {
	bearerAuth
	Body struct {
		Org     string `json:"org" required:"true"`
		Project string `json:"project" required:"true"`
	}
}

type componentsOutput struct {
	Body struct {
		Components []ComponentSpec `json:"components"`
	}
}

type gateCheckInput struct {
	bearerAuth
	Body contract.GateChecksInput
}

type gateCheckOutput struct {
	Body contract.GateChecksResult
}

type taskLifecycleInput struct {
	bearerAuth
	Body contract.TaskLifecycleInput
}

type noBodyOutput struct{}

// RegisterInternalOrchestration registers the orchestrator activity callback
// surface. It is internal-only and intentionally absent from the public API.
func RegisterInternalOrchestration(api huma.API, svc *InternalService) {
	huma.Register(api, huma.Operation{
		OperationID: "orchestration-design-components",
		Method:      http.MethodPost,
		Path:        "/internal/v1/orchestration/design/components",
		Summary:     "Read approved design component graph for orchestration",
		Tags:        []string{"Internal"},
		Security:    auth.SecurityRunner,
	}, func(ctx context.Context, in *componentsInput) (*componentsOutput, error) {
		components, err := svc.Components(ctx, in.Body.Org, in.Body.Project)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to read design components")
		}
		out := &componentsOutput{}
		out.Body.Components = components
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "orchestration-gate-check",
		Method:      http.MethodPost,
		Path:        "/internal/v1/orchestration/gate-check",
		Summary:     "Run automated orchestration gate checks",
		Tags:        []string{"Internal"},
		Security:    auth.SecurityRunner,
	}, func(ctx context.Context, in *gateCheckInput) (*gateCheckOutput, error) {
		res, err := svc.RunChecks(ctx, in.Body)
		if err != nil {
			return nil, huma.Error500InternalServerError("gate checks failed")
		}
		return &gateCheckOutput{Body: res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "orchestration-dispatch-task",
		Method:      http.MethodPost,
		Path:        "/internal/v1/orchestration/tasks/dispatch",
		Summary:     "Dispatch a task through the preserved aep-api executor path",
		Tags:        []string{"Internal"},
		Security:    auth.SecurityRunner,
	}, func(ctx context.Context, in *taskLifecycleInput) (*noBodyOutput, error) {
		if err := svc.DispatchTask(ctx, in.Body); err != nil {
			if errors.Is(err, clustergatewayproxy.ErrQuotaExceeded) {
				// Retriable (§R3.4): the org's ResourceQuota is exhausted, not a
				// hard dispatch failure — 429 (not 500) so Temporal's retry policy
				// backs off and tries again once a slot frees.
				return nil, huma.Error429TooManyRequests("org concurrency quota exceeded")
			}
			return nil, huma.Error500InternalServerError("task dispatch failed")
		}
		return &noBodyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "orchestration-deploy-task",
		Method:      http.MethodPost,
		Path:        "/internal/v1/orchestration/tasks/deploy",
		Summary:     "Deploy a task through the preserved aep-api deploy path",
		Tags:        []string{"Internal"},
		Security:    auth.SecurityRunner,
	}, func(ctx context.Context, in *taskLifecycleInput) (*noBodyOutput, error) {
		if err := svc.DeployTask(ctx, in.Body); err != nil {
			return nil, huma.Error500InternalServerError("task deploy failed")
		}
		return &noBodyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "orchestration-auto-merge",
		Method:      http.MethodPost,
		Path:        "/internal/v1/orchestration/tasks/auto-merge",
		Summary:     "Merge a task PR in auto code-review mode",
		Tags:        []string{"Internal"},
		Security:    auth.SecurityRunner,
	}, func(ctx context.Context, in *taskLifecycleInput) (*noBodyOutput, error) {
		if err := svc.AutoMerge(ctx, in.Body); err != nil {
			return nil, huma.Error500InternalServerError("task auto-merge failed")
		}
		return &noBodyOutput{}, nil
	})
}
