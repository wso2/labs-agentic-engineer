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

package projects

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// Deps is what this domain must be handed to exist: the concrete project
// service plus the component/config service ports the slices call. Constructor
// injection only.
//
// It lives in the domain ROOT, but the thing that CONSUMES it (the aggregator
// that builds the slice handlers) lives in httpapi/ — see httpapi/doc.go for why
// the domain's composition cannot sit here.
type Deps struct {
	// ProjectSvc is the concrete project CRUD + status service behind the five
	// project ops (list / create / get / delete / status).
	ProjectSvc *Service
	// ComponentSvc is the component read + build + deploy service behind the
	// component and build ops.
	ComponentSvc ComponentService
	// ConfigSvc is the component env-var config service behind the two config ops.
	ConfigSvc ConfigService
	// TurnUsage + ExecUsage + Pricer back get-project-usage (#245): the two
	// capture stores' rollups and the read-time USD derivation (ADR-0011).
	// Nil-tolerant — unwired ports answer zero usage.
	TurnUsage TurnUsageReader
	ExecUsage ExecUsageReader
	Pricer    *modelcost.Pricer
}

// TurnUsageReader is the spec phase's usage-rollup port (#245): lifetime
// spec/design turn spend plus the current drafting cycle. Satisfied by
// spec.UsageReader.
type TurnUsageReader interface {
	ProjectTurnUsage(ctx context.Context, orgID, projectID string) (all, draftCycle contracts.TokenUsage, err error)
}

// ExecUsageReader is the execution phases' usage-rollup port: captured usage
// grouped by execution kind. Satisfied by delivery.ExecutionRepository.
type ExecUsageReader interface {
	SumUsageByKind(ctx context.Context, orgID, projectID string) (map[string]contracts.TokenUsage, error)
}
