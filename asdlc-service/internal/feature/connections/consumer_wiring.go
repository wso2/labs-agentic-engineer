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

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// ConsumerWiring wires an external connection's outputs into a consuming
// component via the NATIVE OpenChoreo path: it sets the consumer Workload's
// `spec.dependencies.resources[]` (ref → the connection Resource, envBindings →
// output→env). OC then resolves each Resource's outputs (the ESO-materialized
// Secret + ConfigMap) into the pod env and gates the consumer on
// `ResourceDependenciesReady`. (The ReleaseBinding-env secretKeyRef path does
// NOT work for this — it resolves through an OC SecretReference CR, not the raw
// materialized Secret.) The connection's per-env binding gives the output names.
type ConsumerWiring struct {
	rc    openchoreo.ResourceClient
	tasks TaskStore
	envs  []string // project environments (just "development" today)
}

func NewConsumerWiring(rc openchoreo.ResourceClient, tasks TaskStore, envs []string) *ConsumerWiring {
	if len(envs) == 0 {
		envs = []string{"development"}
	}
	return &ConsumerWiring{rc: rc, tasks: tasks, envs: envs}
}

// scopedWorkloadName is the OC Workload CR name for a component:
// <project>-<component>-workload (the build's generate-workload-cr naming).
func scopedWorkloadName(project, component string) string {
	return project + "-" + component + "-workload"
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

// WireConsumer sets the consumer Workload's spec.dependencies.resources[] so OC
// resolves each bound connection's outputs into the pod env. The envBindings map
// each output name (== the config key == the env var name) to itself; the output
// set is read from the connection's development-env binding. A soft no-op until
// the connection is provisioned (binding has outputs) and the Workload exists —
// the post-deploy cascade re-drives it. orgHandle is the OC namespace.
func (w *ConsumerWiring) WireConsumer(ctx context.Context, orgHandle, projectName, componentName string, connNames, _ []string) error {
	resources := make([]openchoreo.WorkloadResourceDep, 0, len(connNames))
	for _, conn := range connNames {
		b, err := w.rc.GetBinding(ctx, orgHandle, connectionBindingName(projectName, conn, "development"))
		if err != nil || b == nil || b.Status == nil || len(b.Status.Outputs) == 0 {
			// Not provisioned yet / transient — skip; the cascade re-drives.
			continue
		}
		envBindings := make(map[string]string, len(b.Status.Outputs))
		for _, out := range b.Status.Outputs {
			envBindings[out.Name] = out.Name
		}
		resources = append(resources, openchoreo.WorkloadResourceDep{
			Ref:         connectionResourceName(projectName, conn),
			EnvBindings: envBindings,
		})
	}
	if len(resources) == 0 {
		return nil
	}
	if err := w.rc.PatchWorkloadResourceDeps(ctx, orgHandle, scopedWorkloadName(projectName, componentName), resources); err != nil {
		return fmt.Errorf("connections: wire consumer %q workload deps: %w", componentName, err)
	}
	return nil
}
