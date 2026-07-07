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

// Package workflows holds the orchestrator's Temporal workflow definitions.
// Workflow code is pure and deterministic: it must not perform I/O or import
// I/O packages — all side effects go through activities (ADR-0004). Activities
// are invoked by registered name to keep this package decoupled from the
// activities package.
package workflows

import (
	"time"

	"go.temporal.io/sdk/workflow"
)

// pingActivityName mirrors activities.PingActivityName; duplicated as a local
// const so this deterministic package needs no import of the activities package.
const pingActivityName = "Ping"

// PingWorkflow is the O1 smoke-test workflow: it calls the Ping activity and
// returns its result. Proves the worker round-trips workflow + activity.
func PingWorkflow(ctx workflow.Context, msg string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
	})
	var result string
	if err := workflow.ExecuteActivity(ctx, pingActivityName, msg).Get(ctx, &result); err != nil {
		return "", err
	}
	return result, nil
}
