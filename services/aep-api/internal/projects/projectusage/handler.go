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

package projectusage

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/projects"
)

// Handler serves get-project-usage (#245): per-phase actuals assembled from
// the two capture stores, with costUsd derived at read time (ADR-0011).
// Phase mapping: spec = agent turns; build = coding + build executions;
// validation = ops executions (validation runs execute as ops; provision is
// infra work that reports no usage).
type Handler struct {
	turns  projects.TurnUsageReader
	execs  projects.ExecUsageReader
	pricer *modelcost.Pricer
}

// New returns the slice's handler. Unwired ports (nil) degrade to zero usage
// — the component-test harness contract; the real edge always wires both.
func New(turns projects.TurnUsageReader, execs projects.ExecUsageReader, pricer *modelcost.Pricer) *Handler {
	return &Handler{turns: turns, execs: execs, pricer: pricer}
}

func (h *Handler) GetProjectUsage(ctx context.Context, request gen.GetProjectUsageRequestObject) (gen.GetProjectUsageResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)

	var specAll, draft contracts.TokenUsage
	if h.turns != nil {
		var err error
		specAll, draft, err = h.turns.ProjectTurnUsage(ctx, org, request.ProjectName)
		if err != nil {
			return nil, apierr.Internal("load turn usage")
		}
	}
	var byKind map[string]contracts.TokenUsage
	if h.execs != nil {
		var err error
		byKind, err = h.execs.SumUsageByKind(ctx, org, request.ProjectName)
		if err != nil {
			return nil, apierr.Internal("load execution usage")
		}
	}
	build := byKind[string(taskmeta.KindCoding)].Add(byKind[string(taskmeta.KindBuild)])
	validation := byKind[string(taskmeta.KindOps)]

	return gen.GetProjectUsage200JSONResponse(gen.ProjectUsage{
		Spec:       h.genUsage(specAll),
		Build:      h.genUsage(build),
		Validation: h.genUsage(validation),
		DraftCycle: h.genUsage(draft),
	}), nil
}

func (h *Handler) genUsage(u contracts.TokenUsage) gen.Usage {
	out := gen.Usage{
		InputTokens:         u.InputTokens,
		OutputTokens:        u.OutputTokens,
		CacheReadTokens:     u.CacheReadTokens,
		CacheCreationTokens: u.CacheCreationTokens,
		Model:               u.Model,
	}
	if h.pricer != nil {
		out.CostUsd = h.pricer.Cost(u)
	}
	return out
}
