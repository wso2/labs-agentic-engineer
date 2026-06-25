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
	"encoding/json"
	"fmt"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// componentEnvPatcher is the slice of the OC component client the consumer
// wiring needs. openchoreo.ComponentClient satisfies it.
type componentEnvPatcher interface {
	UpdateComponentWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []models.WorkflowEnvVarRef) error
}

// bindingReader is the slice of the Resource client the consumer wiring needs.
// openchoreo.ResourceClient satisfies it.
type bindingReader interface {
	GetBinding(ctx context.Context, namespace, name string) (*openchoreo.ResourceReleaseBinding, error)
}

// ConsumerWiring injects an external connection's resolved outputs into a
// consuming component's runtime env (plan §6 consumption). Because the Resource
// model's DP object names are OC-generated (hashed), the BFF reads the per-env
// binding's `status.outputs[]` for the concrete secretKeyRef/configMapKeyRef and
// patches them onto the consumer's ReleaseBindings via
// UpdateComponentWorkflowEnvVars — the same no-rebuild ReleaseBinding-patch path
// the runtime-config (env-config.js) pipeline uses.
type ConsumerWiring struct {
	rc    bindingReader
	comp  componentEnvPatcher
	tasks TaskStore
	envs  []string // project environments (just "development" today)
}

func NewConsumerWiring(rc bindingReader, comp componentEnvPatcher, tasks TaskStore, envs []string) *ConsumerWiring {
	if len(envs) == 0 {
		envs = []string{"development"}
	}
	return &ConsumerWiring{rc: rc, comp: comp, tasks: tasks, envs: envs}
}

// EmitForProjectConnections re-wires the connection env onto every component in
// the project that binds an `external` connection (idempotent; soft no-op when a
// consumer's ReleaseBindings don't exist yet). Driven from the post-deploy
// cascade — the cascade-hook port mirrors EmitForProjectSPAs (env-config.js).
func (w *ConsumerWiring) EmitForProjectConnections(ctx context.Context, orgID, projectID string) error {
	if w == nil || w.tasks == nil {
		return nil
	}
	tasks, err := w.tasks.ListByProjectID(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	for i := range tasks {
		t := &tasks[i]
		if t.Type == models.TaskTypeConfigCollection || len(t.DependsOnConnections) == 0 {
			continue
		}
		if err := w.WireConsumer(ctx, orgID, projectID, t.ComponentName, []string(t.DependsOnConnections), w.envs); err != nil {
			return err
		}
	}
	return nil
}

// connectionBindingName mirrors the provisioner's per-env binding name.
func connectionBindingName(project, conn, env string) string {
	return connectionResourceName(project, conn) + "-" + env
}

// WireConsumer patches the connection env onto a consuming component across the
// given environments, built from each connection's resolved binding outputs.
// Idempotent (UpdateComponentWorkflowEnvVars replaces the env block) and a soft
// no-op when the consumer's ReleaseBindings don't exist yet (pre-first-deploy) —
// the caller re-drives it from the post-deploy cascade. orgHandle is the OC
// namespace; envs are the project's environments (just "development" today).
func (w *ConsumerWiring) WireConsumer(ctx context.Context, orgHandle, projectName, componentName string, connNames, envs []string) error {
	envVars := make([]models.WorkflowEnvVarRef, 0)
	for _, conn := range connNames {
		for _, env := range envs {
			b, err := w.rc.GetBinding(ctx, orgHandle, connectionBindingName(projectName, conn, env))
			if err != nil || b == nil || b.Status == nil {
				// Not provisioned yet / transient — skip; the cascade re-drives.
				continue
			}
			// Plain (configMap-backed) outputs are passed as literals (the BFF
			// WorkflowEnvVarRef has no configMapKeyRef); read them off the binding's
			// per-env configs (the values the user supplied).
			var plain map[string]string
			if len(b.Spec.ResourceTypeEnvironmentConfigs) > 0 {
				_ = json.Unmarshal(b.Spec.ResourceTypeEnvironmentConfigs, &plain)
			}
			for _, out := range b.Status.Outputs {
				switch {
				case out.SecretKeyRef != nil:
					envVars = append(envVars, models.WorkflowEnvVarRef{
						Key: out.Name,
						ValueFrom: &models.WorkflowEnvVarValueRef{
							SecretKeyRef: &models.WorkflowSecretKeyRef{Name: out.SecretKeyRef.Name, Key: out.SecretKeyRef.Key},
						},
					})
				case out.ConfigMapKeyRef != nil:
					if v, ok := plain[out.Name]; ok {
						envVars = append(envVars, models.WorkflowEnvVarRef{Key: out.Name, Value: v})
					}
				case out.Value != "":
					envVars = append(envVars, models.WorkflowEnvVarRef{Key: out.Name, Value: out.Value})
				}
			}
		}
	}
	if len(envVars) == 0 {
		return nil
	}
	if err := w.comp.UpdateComponentWorkflowEnvVars(ctx, orgHandle, projectName, componentName, envVars); err != nil {
		return fmt.Errorf("connections: wire consumer %q env: %w", componentName, err)
	}
	return nil
}
