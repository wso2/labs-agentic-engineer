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

package provisioning

import (
	"context"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// EndpointResolver resolves a dependency's OC published endpoint — the
// org-service (namespace-visible) and same-project (project-visible) targets the
// declarative-wiring comment names. *endpoints.Catalog satisfies it; the return
// type is the OC client DTO, so this port needs no dependencies/endpoints edge.
type EndpointResolver interface {
	ResolveNamespaceVisible(ctx context.Context, orgHandle, name string) (openchoreo.WorkloadEndpointInfo, bool, error)
	ResolveProjectEndpoint(ctx context.Context, orgHandle, project, ocComponent string) (openchoreo.WorkloadEndpointInfo, bool, error)
}

// WiringResolver posts the ADR-0004 "Platform-resolved dependencies" comment on a
// consumer coding task's issue at dispatch: it resolves the component's dependency
// targets (org-service + sibling endpoints, external + platform-resource binding
// outputs) into the YAML block the coding agent copies into workload.yaml. The
// platform NEVER patches the Workload CR — this only comments. Satisfies
// codingagent.DependencyWiring.
type WiringResolver struct {
	design    DesignReader
	endpoints EndpointResolver
	bindings  BindingReader
	issues    IssueClient
}

// NewWiringResolver wires the resolver. A nil endpoints resolver skips the
// endpoint branches; a nil bindings reader skips the resource branches.
func NewWiringResolver(design DesignReader, endpoints EndpointResolver, bindings BindingReader, issues IssueClient) *WiringResolver {
	return &WiringResolver{design: design, endpoints: endpoints, bindings: bindings, issues: issues}
}

// workloadDeps is the flat OC WorkloadDescriptor `dependencies:` block, rendered
// to the YAML the coding agent merges into its component's workload.yaml. Byte
// parity with the upstream builder (dispatch_service.go): sub-keys are emitted
// only when non-empty (omitempty).
type workloadDeps struct {
	Endpoints []workloadEndpointDepYAML `yaml:"endpoints,omitempty"`
	Resources []workloadResourceDepYAML `yaml:"resources,omitempty"`
}

type workloadEndpointDepYAML struct {
	Project     string            `yaml:"project,omitempty"` // omit if same project
	Component   string            `yaml:"component"`
	Name        string            `yaml:"name"`
	Visibility  string            `yaml:"visibility"`
	EnvBindings map[string]string `yaml:"envBindings"` // {address: <ENV>}
}

type workloadResourceDepYAML struct {
	Ref         string            `yaml:"ref"`
	EnvBindings map[string]string `yaml:"envBindings"` // {<output>: <ENV>}
}

// PostResolvedDeps resolves the component's dependency targets and posts the
// declarative-wiring comment on its issue. Best-effort: an empty resolution posts
// nothing (returns nil). Satisfies codingagent.DependencyWiring.
func (w *WiringResolver) PostResolvedDeps(ctx context.Context, orgID, projectID string, issueNumber int, component string) error {
	if w.issues == nil || issueNumber == 0 {
		return nil
	}
	block, err := w.resolveDependenciesYAML(ctx, orgID, projectID, component)
	if err != nil {
		return err
	}
	if block == "" {
		return nil
	}
	body := "**Platform-resolved dependencies** — add the following to this component's " +
		"`workload.yaml` (merge into any existing `dependencies:`). The platform has " +
		"already resolved the targets + env-var bindings; copy it verbatim. OpenChoreo " +
		"injects these addresses/outputs into your pod env at runtime:\n\n```yaml\n" + block + "```"
	return w.issues.CommentIssue(ctx, orgID, projectID, issueNumber, body)
}

// resolveDependenciesYAML resolves a component's consumer deps — cross-project
// org-service endpoints, same-project component siblings, bound external-resource
// outputs, and platform-resource outputs — into the workload.yaml dependencies
// block. Returns "" when nothing resolves. orgID is the OC namespace (orgHandle).
func (w *WiringResolver) resolveDependenciesYAML(ctx context.Context, orgID, projectID, component string) (string, error) {
	comp, ok, err := w.findComponent(ctx, orgID, projectID, component)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", nil
	}

	var deps workloadDeps

	if w.endpoints != nil {
		// org-service endpoints (cross-project, visibility namespace). Skip any
		// provider not yet published namespace-visible — the cascade re-drives.
		for _, name := range comp.OrgServiceDependsOn() {
			target, ok, rerr := w.endpoints.ResolveNamespaceVisible(ctx, orgID, name)
			if rerr != nil {
				return "", fmt.Errorf("resolve org-service %q: %w", name, rerr)
			}
			if !ok {
				continue
			}
			deps.Endpoints = append(deps.Endpoints, workloadEndpointDepYAML{
				Project:     target.Project,
				Component:   target.Component,
				Name:        target.Name,
				Visibility:  "namespace",
				EnvBindings: map[string]string{"address": orgServiceURLEnv(name)},
			})
		}
		// same-project component siblings (visibility project). The sibling's OC
		// component name is `<project>-<logicalName>`; the env var keys on the
		// LOGICAL dep name. Project is omitted (same project).
		for _, depName := range comp.ComponentDependsOn() {
			ocComponent := projectID + "-" + depName
			target, ok, rerr := w.endpoints.ResolveProjectEndpoint(ctx, orgID, projectID, ocComponent)
			if rerr != nil {
				return "", fmt.Errorf("resolve same-project component %q: %w", depName, rerr)
			}
			if !ok {
				continue
			}
			deps.Endpoints = append(deps.Endpoints, workloadEndpointDepYAML{
				Component:   target.Component,
				Name:        target.Name,
				Visibility:  "project",
				EnvBindings: map[string]string{"address": orgServiceURLEnv(depName)},
			})
		}
	}

	if w.bindings != nil {
		// external-resource outputs. Outputs are pre-namespaced by the resource
		// schema, so env-var name == output name verbatim. Skip any not provisioned.
		for _, name := range externalDepNames(comp) {
			b, berr := w.bindings.GetBinding(ctx, orgID, resources.ExternalResourceBindingName(projectID, name, defaultEnv))
			if berr != nil || b == nil || b.Status == nil || len(b.Status.Outputs) == 0 {
				continue
			}
			envBindings := make(map[string]string, len(b.Status.Outputs))
			for _, out := range b.Status.Outputs {
				envBindings[out.Name] = out.Name
			}
			deps.Resources = append(deps.Resources, workloadResourceDepYAML{
				Ref:         resources.ExternalResourceName(projectID, name),
				EnvBindings: envBindings,
			})
		}
		// platform-resource outputs. Outputs are generic (host, port, …), so prefix
		// with the dep name: envVarName(depName, output). Binding + ref names share
		// the external-resource `<project>-<name>` form.
		for _, depName := range platformDepNames(comp) {
			b, berr := w.bindings.GetBinding(ctx, orgID, resources.ExternalResourceBindingName(projectID, depName, defaultEnv))
			if berr != nil || b == nil || b.Status == nil || len(b.Status.Outputs) == 0 {
				continue
			}
			envBindings := make(map[string]string, len(b.Status.Outputs))
			for _, out := range b.Status.Outputs {
				envBindings[out.Name] = envVarName(depName, out.Name)
			}
			deps.Resources = append(deps.Resources, workloadResourceDepYAML{
				Ref:         resources.ExternalResourceName(projectID, depName),
				EnvBindings: envBindings,
			})
		}
	}

	if len(deps.Endpoints) == 0 && len(deps.Resources) == 0 {
		return "", nil
	}
	out, err := yaml.Marshal(map[string]workloadDeps{"dependencies": deps})
	if err != nil {
		return "", fmt.Errorf("marshal dependencies yaml: %w", err)
	}
	return string(out), nil
}

func (w *WiringResolver) findComponent(ctx context.Context, orgID, projectID, component string) (models.DesignComponent, bool, error) {
	comps, err := w.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return models.DesignComponent{}, false, fmt.Errorf("read design: %w", err)
	}
	for i := range comps {
		if strings.EqualFold(comps[i].Name, component) {
			return comps[i], true, nil
		}
	}
	return models.DesignComponent{}, false, nil
}

func externalDepNames(c models.DesignComponent) []string {
	var out []string
	for _, d := range c.Dependencies {
		if d.Kind == models.DependencyKindExternal {
			out = append(out, d.Name)
		}
	}
	return out
}

func platformDepNames(c models.DesignComponent) []string {
	var out []string
	for _, d := range c.Dependencies {
		if d.Kind == models.DependencyKindPlatformResource {
			out = append(out, d.Name)
		}
	}
	return out
}

// orgServiceURLEnv mirrors endpoints.OrgServiceURLEnv: <UPPER_SNAKE>_URL (a local
// copy keeps this feature's edge to dependencies/endpoints out). e.g.
// "employee-api" → "EMPLOYEE_API_URL".
func orgServiceURLEnv(name string) string {
	return envVarName(name, "") + "URL"
}

// envVarName builds a valid C_IDENTIFIER env-var name from a dep name + output
// name. Delegates to resources.EnvVarName — the single source of truth shared
// with runtimeconfig's window._env_ keys, so the pod env var and the SPA config
// key for the same dep+output are byte-identical. "orders-db" + "host" →
// "ORDERS_DB_HOST".
func envVarName(depName, outName string) string {
	return resources.EnvVarName(depName, outName)
}
