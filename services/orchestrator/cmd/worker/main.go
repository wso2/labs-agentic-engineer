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

// Command worker is the AEP orchestrator: the single process that runs the
// Temporal worker and executes the development-flow and task-lifecycle
// workflows + their activities.
//
// O1: connects to Temporal, registers the PingWorkflow + Ping activity, and
// runs the worker. The real workflows land in O3+.
package main

import (
	"log"

	"go.temporal.io/sdk/workflow"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/config"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/deps"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/temporal"
	orchworker "github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/worker"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/workflows"
)

func main() {
	cfg := config.Load()

	c, err := temporal.NewClient(cfg.TemporalHostPort, cfg.TemporalNamespace)
	if err != nil {
		log.Fatalf("orchestrator: %v", err)
	}
	defer c.Close()

	w := orchworker.New(c, cfg.TaskQueue)
	w.RegisterWorkflow(workflows.PingWorkflow)
	// Register the cycle + task workflows under the shared contract names so
	// aep-api (which starts them by name) and the worker never drift.
	w.RegisterWorkflowWithOptions(workflows.DevelopmentFlowWorkflow,
		workflow.RegisterOptions{Name: orchestration.WorkflowDevelopmentFlow})
	w.RegisterWorkflowWithOptions(workflows.TaskLifecycleWorkflow,
		workflow.RegisterOptions{Name: orchestration.WorkflowTaskLifecycle})
	w.RegisterActivity(deps.NewActivities(cfg))

	log.Printf("orchestrator worker starting — temporal=%s ns=%s queue=%s",
		cfg.TemporalHostPort, cfg.TemporalNamespace, cfg.TaskQueue)
	if err := orchworker.Run(w); err != nil {
		log.Fatalf("orchestrator: worker stopped: %v", err)
	}
}
