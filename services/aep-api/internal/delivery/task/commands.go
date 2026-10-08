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

package task

import (
	"context"
	"fmt"
	"strings"
)

// Commands is the Task write surface: one operation, handing an existing
// GitHub issue to the coding agent — from the console's Issue card, or by the
// issue's own agent (hand_to_coding_agent).
//
// There is no execute/hold/unhold here any more. Work is dispatched by the RUN
// SUPERVISOR over a milestone, not per issue, so the only write a human or an
// external tool makes is adoption: put an issue in a version's milestone and
// let the run pick it up.
type Commands struct {
	components ComponentEnsurer
	adopter    Adopter
}

// NewCommands wires the command surface. components may be nil — the only
// caller that needs it is the pre-check below, which then degrades to skipping
// it rather than failing every call.
func NewCommands(components ComponentEnsurer, adopter Adopter) *Commands {
	return &Commands{components: components, adopter: adopter}
}

// PromoteAndExecute hands an ad-hoc GitHub issue — one filed outside the
// spec-plan pipeline, e.g. by the OpenChoreo SRE/RCA agent handoff — to the
// coding agent.
//
// It is adoption, and nothing more: the issue body the caller wrote is left
// exactly as it is (bodies are prose the agent reads, and nothing platform-side
// parses them), the issue joins the DEPLOYED version's milestone armed, and an
// incident run is started over that milestone unless one is already live — in
// which case the live run picks the issue up at its next cycle boundary. An
// issue adoption will not take (closed, or not the coding agent's) is refused
// with the adopter's sentinel, which the route answers as a 409.
//
// componentName must name a component the platform already knows about. The
// check is synchronous here so an unknown name (e.g. a caller's prefix-
// stripping bug) fails this call rather than surfacing later inside a cycle.
//
// Idempotent: a repeated call re-adopts an issue that is already in the
// milestone, which is a no-op.
func (c *Commands) PromoteAndExecute(ctx context.Context, orgID, projectID, componentName string, issueNumber int) error {
	if strings.TrimSpace(componentName) == "" {
		return ErrComponentNameRequired
	}
	if c.components != nil {
		if err := c.components.EnsureComponent(ctx, orgID, projectID, componentName); err != nil {
			return fmt.Errorf("promote task from issue: %w", err)
		}
	}
	if c.adopter == nil {
		return fmt.Errorf("promote task from issue: no adopter wired")
	}
	return c.adopter.AdoptIssue(ctx, orgID, projectID, issueNumber)
}
