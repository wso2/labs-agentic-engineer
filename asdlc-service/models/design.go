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

package models

// DesignComponent describes a single component within a design.
// This matches the structured output schema from the AI Agent SDK.
type DesignComponent struct {
	Name                       string          `json:"name"`
	ComponentType              string          `json:"componentType"`
	Language                   string          `json:"language"`
	Dependencies               []Dependency    `json:"dependencies"`
	Origin                     string          `json:"origin,omitempty"` // "source" (default) | "image"
	Image                      string          `json:"image,omitempty"`  // container ref when Origin == "image"
	Config                     []ConfigKey     `json:"config,omitempty"` // user-provided runtime config vars on THIS component
	Entrypoint                 string          `json:"entrypoint"`
	Buildpack                  string          `json:"buildpack"`
	AppPath                    string          `json:"appPath"`
	OpenAPISpec                string          `json:"openAPISpec"`
	ComponentAgentInstructions string          `json:"componentAgentInstructions"`
	CallerIdentity             *CallerIdentity `json:"callerIdentity,omitempty"`
	ExposesAPI                 *ExposesAPI     `json:"exposesAPI,omitempty"`
}

// DependencyKind discriminates the unified Dependency entry.
type DependencyKind = string

const (
	DependencyKindComponent        DependencyKind = "component"
	DependencyKindOrgService       DependencyKind = "org-service"
	DependencyKindExternal         DependencyKind = "external"
	DependencyKindPlatformResource DependencyKind = "platform-resource"
)

// Dependency is the unified, kind-discriminated dependency entry on a
// component. It subsumes the legacy DependsOn (sibling components) and
// DependentApis (external HTTP APIs). Go has no native discriminated union,
// so a single struct carries every kind's fields; `Kind` selects which are
// meaningful (Config for external; ResourceType/Parameters for
// platform-resource; the rest common). Mirrors the agents-service Zod
// `Dependency`.
type Dependency struct {
	Kind        string `json:"kind" yaml:"kind"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Status      string `json:"status,omitempty" yaml:"status,omitempty"` // resolved|ambiguous|unresolved
	// external: the config key schema the consuming component codes against.
	Config []ConfigKey `json:"config,omitempty" yaml:"config,omitempty"`
	// platform-resource: the registered (Cluster)ResourceType + provisioning params.
	ResourceType string            `json:"resourceType,omitempty" yaml:"resourceType,omitempty"`
	Parameters   map[string]string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	// resolution UI: candidates attached when Status == ambiguous.
	Candidates []DependencyCandidate `json:"candidates,omitempty" yaml:"candidates,omitempty"`
}

// ConfigKey is one env-var key a component reads at runtime. For an external
// connection these keys form the connection's schema (drives the OC
// ResourceType in P2). Secret keys route through the secret path.
type ConfigKey struct {
	Key             string `json:"key" yaml:"key"`
	Secret          bool   `json:"secret" yaml:"secret"`
	CredentialClass string `json:"credentialClass,omitempty" yaml:"credentialClass,omitempty"` // publishable|secret
}

// DependencyCandidate is one option attached to an ambiguous dependency.
type DependencyCandidate struct {
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	URL         string `json:"url,omitempty" yaml:"url,omitempty"`
}

// ComponentDependsOn returns the names of this component's sibling-component
// dependencies — the successor to the legacy DependsOn []string field. Used
// for task deploy-gating, runtime-config URL wiring, and task diffing.
func (c DesignComponent) ComponentDependsOn() []string {
	out := make([]string, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindComponent {
			out = append(out, d.Name)
		}
	}
	return out
}

// ExternalDependencies returns the external + org-service dependencies — the
// connection-bearing entries (successor to DependentApis). Used to surface
// connection context into issue bodies and to drive value collection (P2).
func (c DesignComponent) ExternalDependencies() []Dependency {
	out := make([]Dependency, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindExternal || d.Kind == DependencyKindOrgService {
			out = append(out, d)
		}
	}
	return out
}

// ExposesAPI declares HTTP API exposure policy for a service component.
// Absent / nil ⇒ public (no gateway hop). `Auth: "end-user-required"` ⇒
// the API Platform gateway validates an end-user JWT and injects
// UserContext (default "X-User-Id") before forwarding upstream.
type ExposesAPI struct {
	Managed     bool   `json:"managed,omitempty"`
	Auth        string `json:"auth,omitempty"`        // "end-user-required" | "service-required" | "none"
	UserContext string `json:"userContext,omitempty"` // injected header name
}

// CallerIdentity declares the caller-identity intent for a web-app
// component. `Mode: "end-user"` ⇒ the SPA performs OIDC Authorization
// Code + PKCE against the platform IDP and the BFF declares the
// per-project OAuth client lazily on first dispatch.
type CallerIdentity struct {
	Mode string `json:"mode,omitempty"` // "end-user" | "service-account" | "none"
}

// DesignComponents is a slice of DesignComponent.
type DesignComponents []DesignComponent

type Design struct {
	ProjectID         string            `json:"projectId"`
	OrgID             string            `json:"-"`
	Overview          string            `json:"overview"`
	Components        DesignComponents  `json:"components"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	Versions          []ArtifactVersion `json:"versions,omitempty"`
	HasUnsavedChanges bool              `json:"hasUnsavedChanges"`
	SourceSpec        string            `json:"sourceSpec,omitempty"`
}
