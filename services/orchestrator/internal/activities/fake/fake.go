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

// Package fake provides in-memory implementations of the activity dependency
// interfaces, for unit tests and for a runnable local-dev worker before the real
// adapters (database client, packages/clients, GitHub client) are ported.
package fake

import (
	"context"
	"sync"

	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/activities"
	"github.com/wso2/labs-agentic-engineer/services/orchestrator/internal/types"
)

// Design returns a fixed component list (configurable per project).
type Design struct {
	ByProject map[string][]activities.ComponentSpec
	Default   []activities.ComponentSpec
}

func (d *Design) Components(_ context.Context, _, project string) ([]activities.ComponentSpec, error) {
	if c, ok := d.ByProject[project]; ok {
		return c, nil
	}
	return d.Default, nil
}

// Checker returns a fixed gate-check result.
type Checker struct{ Result types.GateChecksResult }

func (c *Checker) RunChecks(_ context.Context, _ types.GateChecksInput) (types.GateChecksResult, error) {
	return c.Result, nil
}

// Dispatcher records ensured workspaces and dispatched tasks; idempotent.
type Dispatcher struct {
	mu         sync.Mutex
	Workspaces map[string]int // org -> EnsureOrgWorkspace call count
	Dispatched map[string]int // taskID -> DispatchTask call count
	Deployed   map[string]int // taskID -> DeployTask call count
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{Workspaces: map[string]int{}, Dispatched: map[string]int{}, Deployed: map[string]int{}}
}

func (d *Dispatcher) DeployTask(_ context.Context, in types.TaskLifecycleInput) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Deployed[in.TaskID]++
	return nil
}

func (d *Dispatcher) EnsureOrgWorkspace(_ context.Context, org string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Workspaces[org]++
	return nil
}

func (d *Dispatcher) DispatchTask(_ context.Context, in types.TaskLifecycleInput) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Dispatched[in.TaskID]++ // idempotency is the adapter's job; the fake records attempts
	return nil
}

// Merger records merged PRs; idempotent.
type Merger struct {
	mu     sync.Mutex
	Merged map[string]int
}

func NewMerger() *Merger { return &Merger{Merged: map[string]int{}} }

func (m *Merger) MergePR(_ context.Context, in types.TaskLifecycleInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Merged[in.TaskID]++
	return nil
}
