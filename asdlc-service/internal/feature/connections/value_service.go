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

package connections

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// connectionLookup is the slice of the registry the value service reads.
// *Registry satisfies it.
type connectionLookup interface {
	Get(ctx context.Context, orgID, name string) (*models.Connection, error)
}

// TaskStore is the slice of the component-task repo the value service needs to
// complete a config-collection task. *repositories.ComponentTaskRepository
// satisfies it.
type TaskStore interface {
	ListByProjectID(ctx context.Context, orgID, projectID string) ([]models.ComponentTask, error)
	Update(ctx context.Context, t *models.ComponentTask) error
}

// RedispatchFunc re-runs the dispatch gating loop after a config-collection task
// completes, so component tasks gated on the connection get dispatched. Wraps
// codingagent.DispatchService.DispatchTasks in the composition root (avoids a
// package dependency on codingagent's DispatchResult).
type RedispatchFunc func(ctx context.Context, orgID, projectID string) error

// ValueService handles per-env connection value submission (plan §3 value flow):
// split the submitted values by the connection's schema → provision the OC
// Resource model → mark the config-collection task `deployed` (= provisioned) →
// re-dispatch the gated component tasks (the cascade).
type ValueService struct {
	lookup     connectionLookup
	prov       *Provisioner
	tasks      TaskStore
	redispatch RedispatchFunc
}

func NewValueService(registry *Registry, prov *Provisioner, tasks TaskStore, redispatch RedispatchFunc) *ValueService {
	return &ValueService{lookup: registry, prov: prov, tasks: tasks, redispatch: redispatch}
}

// SaveValues provisions a registered connection for a project from the user's
// per-env values. `orgID` is the OC org handle (== the OC namespace + SM-API org
// id locally); `projectID` is the project slug. `envValues` maps environment →
// {key: value}; values are split into plain/secret by the connection's
// registered schema (the user never marks which is which — the schema does).
func (v *ValueService) SaveValues(ctx context.Context, orgID, projectID, connName string, envValues map[string]map[string]string) error {
	conn, err := v.lookup.Get(ctx, orgID, connName)
	if err != nil {
		return err
	}
	if conn == nil {
		return fmt.Errorf("connections: %q is not registered for the org", connName)
	}
	byEnv := make(map[string]EnvConnectionValues, len(envValues))
	for env, vals := range envValues {
		byEnv[env] = splitBySchema(conn.ConfigSchema, vals)
	}
	if _, err := v.prov.ProvisionConnection(ctx, orgID, orgID, projectID, conn, byEnv); err != nil {
		return fmt.Errorf("connections: provision %q: %w", connName, err)
	}
	// Mark the config-collection task provisioned; reaching `deployed` is what
	// the dependent component tasks gate on (best-effort — provisioning already
	// succeeded).
	if err := v.completeConfigTask(ctx, orgID, projectID, connName); err != nil {
		slog.WarnContext(ctx, "connections: failed to complete config-collection task",
			"connection", connName, "project", projectID, "error", err)
	}
	// Cascade: re-evaluate gating so component tasks waiting on this connection dispatch.
	if v.redispatch != nil {
		if err := v.redispatch(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "connections: re-dispatch after config-collection failed",
				"project", projectID, "error", err)
		}
	}
	return nil
}

func (v *ValueService) completeConfigTask(ctx context.Context, orgID, projectID, connName string) error {
	if v.tasks == nil {
		return nil
	}
	tasks, err := v.tasks.ListByProjectID(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	for i := range tasks {
		t := &tasks[i]
		if t.Type == models.TaskTypeConfigCollection && t.ConnectionName == connName &&
			t.Status != string(models.TaskStatusDeployed) {
			t.Status = string(models.TaskStatusDeployed)
			return v.tasks.Update(ctx, t)
		}
	}
	return nil
}

// splitBySchema sorts a flat key→value map into plain vs secret by the
// connection's registered schema. Unknown keys default to plain (a value the
// schema doesn't know can't be a credential we're tracking).
func splitBySchema(schema []models.ConfigKey, values map[string]string) EnvConnectionValues {
	secret := make(map[string]bool, len(schema))
	for _, k := range schema {
		secret[k.Key] = k.Secret
	}
	ev := EnvConnectionValues{Plain: map[string]string{}, Secret: map[string]string{}}
	for k, val := range values {
		if secret[k] {
			ev.Secret[k] = val
		} else {
			ev.Plain[k] = val
		}
	}
	return ev
}
