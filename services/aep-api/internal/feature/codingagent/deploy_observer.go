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

package codingagent

import (
	"context"
	"log/slog"
)

// MultiDeployObserver fans a component-deployed notification out to several
// DeployObservers (the cross-project access grant + the env-config.js re-emit),
// isolating errors: one observer failing is logged and the others still run.
//
// Emission is best-effort — the aggregate always returns nil so a failing
// side-effect never re-drives the ExecWatcher's terminal build-success write.
// This mirrors the legacy dispatch cascade's warn-and-continue semantics, where
// each post-deploy hook (trait sync, env-config re-emit, access grant) was
// independently swallowed.
type MultiDeployObserver struct {
	observers []DeployObserver
}

// NewMultiDeployObserver builds the fan-out over the given observers, dropping
// any nil entries so an unwired optional observer is simply absent.
func NewMultiDeployObserver(observers ...DeployObserver) *MultiDeployObserver {
	nonNil := make([]DeployObserver, 0, len(observers))
	for _, o := range observers {
		if o != nil {
			nonNil = append(nonNil, o)
		}
	}
	return &MultiDeployObserver{observers: nonNil}
}

// OnComponentDeployed invokes every observer in order, logging and continuing on
// error so one failure cannot mask another observer's work. Always returns nil
// (best-effort).
func (m *MultiDeployObserver) OnComponentDeployed(ctx context.Context, orgID, projectID, component string) error {
	for _, o := range m.observers {
		if err := o.OnComponentDeployed(ctx, orgID, projectID, component); err != nil {
			slog.WarnContext(ctx, "deploy observer: one observer failed (best-effort); continuing",
				"orgID", orgID, "projectID", projectID, "component", component, "error", err)
		}
	}
	return nil
}

// Compile-time proof the fan-out satisfies the port it aggregates.
var _ DeployObserver = (*MultiDeployObserver)(nil)
