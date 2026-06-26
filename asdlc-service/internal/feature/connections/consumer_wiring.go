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
	"strings"

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
	rc      openchoreo.ResourceClient
	tasks   TaskStore
	catalog *OrgEndpointCatalog // org-service endpoint resolution (P3); nil ⇒ org-service wiring is a no-op
	envs    []string            // project environments (just "development" today)
}

func NewConsumerWiring(rc openchoreo.ResourceClient, tasks TaskStore, catalog *OrgEndpointCatalog, envs []string) *ConsumerWiring {
	if len(envs) == 0 {
		envs = []string{"development"}
	}
	return &ConsumerWiring{rc: rc, tasks: tasks, catalog: catalog, envs: envs}
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
		if t.Type == models.TaskTypeConfigCollection {
			continue
		}
		if len(t.DependsOnConnections) > 0 {
			if err := w.WireConsumer(ctx, orgID, projectID, t.ComponentName, []string(t.DependsOnConnections), w.envs); err != nil {
				return err
			}
		}
		// P3: wire cross-project org-service deps as OC WorkloadConnections.
		if len(t.DependsOnOrgServices) > 0 {
			if err := w.WireOrgServiceConsumer(ctx, orgID, projectID, t.ComponentName, []string(t.DependsOnOrgServices)); err != nil {
				return err
			}
		}
	}
	return nil
}

// orgServiceURLEnv is the consumer env var OC binds the resolved org-service
// address to — same `<UPPER_SNAKE>_URL` convention SPAs read via window._env_,
// so a Go/Node consumer reads e.g. EMPLOYEE_API_URL regardless of how the URL
// is delivered.
func orgServiceURLEnv(name string) string {
	var b strings.Builder
	prevAlnum := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
			prevAlnum = true
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevAlnum = true
		case r == '-' || r == '_':
			if prevAlnum {
				b.WriteByte('_')
			}
			prevAlnum = false
		}
	}
	return strings.TrimSuffix(b.String(), "_") + "_URL"
}

// WireOrgServiceConsumer sets the consumer Workload's
// spec.dependencies.endpoints[] (OC WorkloadConnection) for each cross-project
// org-service dep: the org endpoint catalog resolves the dep name to a
// namespace-visible provider endpoint and OC injects its internal address into
// the consumer pod env (<NAME>_URL). A dep whose target isn't yet published
// namespace-visible is skipped (soft no-op) — the cascade re-drives once the
// provider redeploys with namespace visibility. orgHandle is the OC namespace.
func (w *ConsumerWiring) WireOrgServiceConsumer(ctx context.Context, orgHandle, projectName, componentName string, orgServiceNames []string) error {
	if w.catalog == nil || len(orgServiceNames) == 0 {
		return nil
	}
	endpoints := make([]openchoreo.WorkloadEndpointDep, 0, len(orgServiceNames))
	for _, name := range orgServiceNames {
		target, ok, err := w.catalog.ResolveNamespaceVisible(ctx, orgHandle, name)
		if err != nil {
			return fmt.Errorf("connections: resolve org-service %q: %w", name, err)
		}
		if !ok {
			continue // provider not published namespace-visible yet; cascade re-drives
		}
		endpoints = append(endpoints, openchoreo.WorkloadEndpointDep{
			Project:    target.Project,
			Component:  target.Component,
			Name:       target.Name,
			Visibility: "namespace",
			AddressEnv: orgServiceURLEnv(name),
		})
	}
	if len(endpoints) == 0 {
		return nil
	}
	if err := w.rc.PatchWorkloadEndpointDeps(ctx, orgHandle, scopedWorkloadName(projectName, componentName), endpoints); err != nil {
		return fmt.Errorf("connections: wire org-service consumer %q workload deps: %w", componentName, err)
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
