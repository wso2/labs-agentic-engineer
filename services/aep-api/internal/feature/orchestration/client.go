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

// Package orchestration is aep-api's dial-only Temporal client for the
// development-flow orchestration. The BFF starts, signals, and queries the
// workflows that services/orchestrator executes; it runs no worker. All
// workflow-boundary names/IDs/DTOs come from the shared contract module so the
// starter and the worker cannot drift.
package orchestration

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/wso2/aep/aep-api/internal/config"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)


// Client wraps a Temporal SDK client for start/signal/query only.
type Client struct {
	tc        client.Client
	taskQueue string
}

// New dials the Temporal frontend. The caller owns Close(). A dial failure is
// returned (not fatal here) so the BFF wiring can treat orchestration as
// optional — logging a warning and disabling the feature when Temporal is not
// reachable, rather than failing to boot.
func New(cfg config.TemporalConfig) (*Client, error) {
	tc, err := client.Dial(client.Options{HostPort: cfg.HostPort, Namespace: cfg.Namespace})
	if err != nil {
		return nil, fmt.Errorf("dial temporal at %s (ns %q): %w", cfg.HostPort, cfg.Namespace, err)
	}
	return &Client{tc: tc, taskQueue: cfg.TaskQueue}, nil
}

// Close releases the underlying Temporal connection.
func (c *Client) Close() { c.tc.Close() }

// StartCycle starts a DevelopmentFlowWorkflow for a change cycle and returns its
// workflow ID. The ID is deterministic (contract.DevFlowWorkflowID), so a
// retried trigger while the cycle is still running fails with
// WorkflowExecutionAlreadyStarted — callers may treat that as an idempotent
// no-op. The workflow is started by its shared contract name (aep-api never
// imports the orchestrator's workflow package).
func (c *Client) StartCycle(ctx context.Context, in contract.DevelopmentFlowInput) (string, error) {
	wfID := contract.DevFlowWorkflowID(in.Org, in.Project, in.CycleID)
	if _, err := c.tc.ExecuteWorkflow(ctx,
		client.StartWorkflowOptions{ID: wfID, TaskQueue: c.taskQueue},
		contract.WorkflowDevelopmentFlow, in,
	); err != nil {
		// A retried start for a still-running cycle is idempotent: the cycle
		// already exists, so return its ID with no error (the deterministic ID is
		// the dedup key).
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &already) {
			return wfID, nil
		}
		return "", fmt.Errorf("start cycle %s: %w", wfID, err)
	}
	return wfID, nil
}

// Signal sends a signal to a workflow by ID (a cycle ID or a task ID). arg may
// be nil for the payload-less gate signals (Approve/Revise/Back/Complete).
func (c *Client) Signal(ctx context.Context, workflowID, signalName string, arg any) error {
	if err := c.tc.SignalWorkflow(ctx, workflowID, "", signalName, arg); err != nil {
		return fmt.Errorf("signal %s to %s: %w", signalName, workflowID, err)
	}
	return nil
}

// QueryCycle reads a cycle's durable position (phase, gates passed, task states)
// via the GetCycleState query. Read-only; hits the running workflow.
func (c *Client) QueryCycle(ctx context.Context, workflowID string) (contract.CycleStateView, error) {
	var st contract.CycleStateView
	val, err := c.tc.QueryWorkflow(ctx, workflowID, "", contract.QueryGetCycleState)
	if err != nil {
		return st, fmt.Errorf("query cycle %s: %w", workflowID, err)
	}
	if err := val.Get(&st); err != nil {
		return st, fmt.Errorf("decode cycle state %s: %w", workflowID, err)
	}
	return st, nil
}

// QueryTask reads a task child workflow's durable position via GetTaskState.
func (c *Client) QueryTask(ctx context.Context, org, project, taskID string) (contract.TaskStateView, error) {
	var st contract.TaskStateView
	wfID := contract.TaskWorkflowID(org, project, taskID)
	val, err := c.tc.QueryWorkflow(ctx, wfID, "", contract.QueryGetTaskState)
	if err != nil {
		return st, fmt.Errorf("query task %s: %w", wfID, err)
	}
	if err := val.Get(&st); err != nil {
		return st, fmt.Errorf("decode task state %s: %w", wfID, err)
	}
	return st, nil
}
