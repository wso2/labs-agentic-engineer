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

// Package deps is the orchestrator's composition root: it wires concrete
// dependency adapters into the activities. This is the single place real
// adapters get plugged in as their backing services/clients are ported:
//
//	Design   -> database service client (PlanTasks reads the approved design)
//	Checker  -> automated gate runner (tests/lint/agent self-review)
//	Dispatch -> packages/clients k8s/OpenChoreo (EnsureNamespace + ResourceQuota + Job)
//	Merger   -> GitHub client (auto-merge)
//
// Until those land (O4-real), NewActivities wires nil dependencies, which the
// activity methods treat as safe no-ops, so the worker still builds and runs.
package deps

import (
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/config"
)

// NewActivities builds the activity set for the worker. When aep-api's internal
// base URL is configured, activities delegate to its S2S orchestration surface;
// otherwise they remain nil-backed no-ops for local workflow tests.
func NewActivities(cfg config.Config) *activities.Activities {
	var api *activities.HTTPClient
	if cfg.AEPAPIInternalBaseURL != "" {
		api = activities.NewHTTPClient(cfg.AEPAPIInternalBaseURL, cfg.AEPAPIInternalBearer, nil)
	}
	return activities.New(
		api, // Design   — aep-api reads the approved design
		api, // Checker  — aep-api runs tests/lint/self-review checks
		api, // Dispatch — aep-api preserves coding-agent/build/deploy integrations
		api, // Merger   — aep-api owns GitHub PR side effects
	)
}
