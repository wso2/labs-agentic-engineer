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
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// GuardrailOutcome is what the last deploy did with one of an agent's declared
// guardrails, as the governance record holds it.
type GuardrailOutcome struct {
	Policy, Status, Reason string
}

// GuardrailOutcomeReader reads an agent's guardrail outcomes, per environment.
// It is the governance slice's record, reached through a port rather than an
// import: which guardrails landed is that slice's to decide, and a second copy
// of the rule here would drift into a Deployments page that disagrees with it.
type GuardrailOutcomeReader interface {
	GuardrailOutcomes(ctx context.Context, org, project, component string) (map[string][]GuardrailOutcome, error)
}

// SetGuardrailOutcomes wires the reader. Nil — an install with no Agent
// Manager — leaves every row without guardrails.
func (s *componentService) SetGuardrailOutcomes(r GuardrailOutcomeReader) {
	s.guardrailOutcomes = r
}

// withGuardrailOutcomes fills Guardrails on each row whose agent the last
// deploy applied guardrails for, in that row's environment.
//
// Best effort, like the Agent Manager link: an unreadable record leaves the
// rows without guardrails and the list intact.
func (s *componentService) withGuardrailOutcomes(ctx context.Context, orgName, projectName, componentName string, list *gen.DeploymentList) {
	if list == nil || s.guardrailOutcomes == nil {
		return
	}
	byEnv, err := s.guardrailOutcomes.GuardrailOutcomes(ctx, orgName, projectName, componentName)
	if err != nil {
		slog.WarnContext(ctx, "deployments: could not read the agent's guardrail outcomes",
			"org", orgName, "project", projectName, "component", componentName, "error", err)
		return
	}
	for i := range list.Items {
		row := &list.Items[i]
		outcomes, ok := byEnv[row.Environment]
		if !ok {
			continue
		}
		row.Guardrails = make([]gen.DeploymentGuardrail, 0, len(outcomes))
		for _, o := range outcomes {
			row.Guardrails = append(row.Guardrails, gen.DeploymentGuardrail{
				Policy: o.Policy, Status: gen.DeploymentGuardrailStatus(o.Status), Reason: o.Reason,
			})
		}
	}
}
