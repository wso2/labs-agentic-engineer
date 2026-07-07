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

package cycle

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

type cycleProjectInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type flowStateOutput struct{ Body *contract.CycleStateView }
type signalAckOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// gateOp is one console gate button → workflow signal mapping.
type gateOp struct {
	action  string // URL segment + OperationID suffix
	signal  string // contract signal name
	summary string
}

// gateOps is the console-facing gate surface for a development cycle. Each maps
// a REST action to a DevelopmentFlowWorkflow signal.
var gateOps = []gateOp{
	{"approve-requirements", contract.SignalApproveRequirements, "Approve requirements (advance to design)"},
	{"revise-requirements", contract.SignalReviseRequirements, "Revise requirements (stay in requirements)"},
	{"approve-design", contract.SignalApproveDesign, "Approve design (advance to implement)"},
	{"revise-design", contract.SignalReviseDesign, "Revise design (stay in design)"},
	{"back-to-requirements", contract.SignalBackToRequirements, "Return the cycle to requirements"},
	{"back-to-design", contract.SignalBackToDesign, "Return the cycle to design"},
	{"complete", contract.SignalMarkComplete, "Mark the cycle complete"},
}

// RegisterCycle registers the development-flow (cycle) operations: the
// flow-state query and the gate-signal buttons. All are org-scoped + tenant
// gated via humakit.OrgScopedInput.
func RegisterCycle(api huma.API, svc *Service) {
	prefix := "/projects/{projectName}/cycle"

	huma.Register(api, huma.Operation{
		OperationID: "get-cycle-state",
		Method:      http.MethodGet,
		Path:        prefix + "/state",
		Summary:     "Get the development cycle's durable flow state",
		Tags:        []string{"DevelopmentFlow"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *cycleProjectInput) (*flowStateOutput, error) {
		st, err := svc.GetFlowState(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, mapCycleErr(ctx, in.ProjectName, "get-cycle-state", err)
		}
		return &flowStateOutput{Body: st}, nil
	})

	for _, op := range gateOps {
		op := op // capture (loop-var safety across Go versions)
		huma.Register(api, huma.Operation{
			OperationID: "cycle-" + op.action,
			Method:      http.MethodPost,
			Path:        prefix + "/" + op.action,
			Summary:     op.summary,
			Tags:        []string{"DevelopmentFlow"},
			Security:    humakit.SecurityUserJWT,
		}, func(ctx context.Context, in *cycleProjectInput) (*signalAckOutput, error) {
			if err := svc.Signal(ctx, in.OrgHandle, in.ProjectName, op.signal); err != nil {
				return nil, mapCycleErr(ctx, in.ProjectName, "cycle-"+op.action, err)
			}
			out := &signalAckOutput{}
			out.Body.Status = "accepted"
			return out, nil
		})
	}
}

// mapCycleErr translates cycle-service errors to HTTP status codes.
func mapCycleErr(ctx context.Context, project, op string, err error) error {
	switch {
	case errors.Is(err, ErrOrchestrationDisabled):
		return huma.Error503ServiceUnavailable("orchestration is not available")
	case errors.Is(err, ErrNoActiveCycle):
		return huma.Error404NotFound("no active development cycle for project")
	default:
		slog.ErrorContext(ctx, "cycle operation failed", "op", op, "project", project, "error", err)
		return huma.Error500InternalServerError("development cycle operation failed")
	}
}
