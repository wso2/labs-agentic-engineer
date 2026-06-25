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

package openchoreo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"

	"github.com/wso2/asdlc/asdlc-service/clients/httpx"
	"github.com/wso2/asdlc/asdlc-service/clients/requests"
)

// ResourceClient authors the OpenChoreo Resource model (openchoreo.dev/v1alpha1,
// shipped v1.1) used to wire external connections (and later platform-resources)
// into consuming Workloads. The generated `gen` client predates v1.1 and has no
// Resource types, so this is hand-rolled over the same authenticated transport.
//
// Authoring chain (the BFF owns every step — Resources have NO AutoDeploy):
//
//	EnsureResourceType  → per-connection ResourceType (schema + ConfigMap/ExternalSecret
//	                      template + outputs), get-or-create, immutable once created
//	ApplyResource       → one Resource per project; the OC controller cuts an
//	                      immutable ResourceRelease and writes status.latestRelease
//	EnsureBinding       → one ResourceReleaseBinding per environment, with
//	                      spec.resourceRelease PINNED to status.latestRelease (the
//	                      `occ resource promote` contract — no controller does this)
//
// The consumer Workload then references the Resource via
// spec.dependencies.resources[].ref + envBindings; its ReleaseBinding gates on
// the native `ResourceDependenciesReady` condition.
type ResourceClient interface {
	// EnsureResourceType get-or-creates a namespaced ResourceType. Idempotent on
	// (namespace, name): a 409 returns the existing one. ResourceTypes are treated
	// as immutable — a changed schema must use a new name (suffix), never an edit.
	EnsureResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error)

	// ApplyResource creates (or returns existing on 409) a Resource. The
	// controller asynchronously cuts a ResourceRelease; call GetResource to read
	// status.latestRelease once it settles.
	ApplyResource(ctx context.Context, namespace string, r *Resource) (*Resource, error)

	// GetResource reads a Resource (incl. status.latestRelease).
	GetResource(ctx context.Context, namespace, name string) (*Resource, error)

	// EnsureBinding authors a per-env ResourceReleaseBinding and PINS
	// spec.resourceRelease (no AutoDeploy controller exists). Idempotent: a 409
	// PUTs the binding to reconcile the pin / per-env configs.
	EnsureBinding(ctx context.Context, namespace string, b *ResourceReleaseBinding) (*ResourceReleaseBinding, error)

	// GetBinding reads a ResourceReleaseBinding (incl. status.conditions).
	GetBinding(ctx context.Context, namespace, name string) (*ResourceReleaseBinding, error)

	// DeleteBinding removes a per-env binding. Step 1 of the 2-step connection
	// delete (bindings cascade their DP objects via retainPolicy: Delete).
	DeleteBinding(ctx context.Context, namespace, name string) error

	// DeleteResource removes a Resource. Step 2 of the delete — its finalizer
	// blocks until all bindings referencing it are gone.
	DeleteResource(ctx context.Context, namespace, name string) error

	// ListClusterResourceTypes discovers the installed cluster-scoped
	// ClusterResourceTypes (the platform-resource catalog, P5 — read-only).
	ListClusterResourceTypes(ctx context.Context) ([]ResourceType, error)
}

// ---- wire DTOs (openchoreo.dev/v1alpha1) -----------------------------------

const (
	ocAPIVersion            = "openchoreo.dev/v1alpha1"
	kindResourceType        = "ResourceType"
	kindResource            = "Resource"
	kindResourceReleaseBind = "ResourceReleaseBinding"
	retainPolicyDelete      = "Delete"
)

// OCObjectMeta is the slice of k8s metadata the BFF sets on Resource-model CRs.
type OCObjectMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// OCKeyRef is a {name,key} reference into a Secret/ConfigMap (both CEL-templatable).
type OCKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// SchemaSection wraps a raw OpenAPI v3 / JSON Schema object.
type SchemaSection struct {
	OpenAPIV3Schema map[string]any `json:"openAPIV3Schema"`
}

// ResourceTypeOutput declares one consumer-bindable output. Exactly ONE of
// Value / SecretKeyRef / ConfigMapKeyRef is set (OC enforces via XValidation).
type ResourceTypeOutput struct {
	Name            string    `json:"name"`
	Value           string    `json:"value,omitempty"`
	SecretKeyRef    *OCKeyRef `json:"secretKeyRef,omitempty"`
	ConfigMapKeyRef *OCKeyRef `json:"configMapKeyRef,omitempty"`
}

// ResourceTypeManifest is one DP object the ResourceType renders. Template is a
// raw k8s manifest with `${...}` CEL expressions; readyWhen gates ResourcesReady.
type ResourceTypeManifest struct {
	ID          string          `json:"id"`
	IncludeWhen string          `json:"includeWhen,omitempty"`
	ReadyWhen   string          `json:"readyWhen,omitempty"`
	Template    json.RawMessage `json:"template"`
}

type ResourceTypeSpec struct {
	Parameters         *SchemaSection         `json:"parameters,omitempty"`
	EnvironmentConfigs *SchemaSection         `json:"environmentConfigs,omitempty"`
	RetainPolicy       string                 `json:"retainPolicy,omitempty"`
	Outputs            []ResourceTypeOutput   `json:"outputs,omitempty"`
	Resources          []ResourceTypeManifest `json:"resources"` // MinItems=1 (OC-enforced)
}

type ResourceType struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   OCObjectMeta     `json:"metadata"`
	Spec       ResourceTypeSpec `json:"spec"`
}

type ResourceOwner struct {
	ProjectName string `json:"projectName"`
}

type ResourceTypeRef struct {
	Kind string `json:"kind,omitempty"` // ResourceType | ClusterResourceType (default ResourceType)
	Name string `json:"name"`
}

type ResourceSpec struct {
	Owner      ResourceOwner   `json:"owner"`
	Type       ResourceTypeRef `json:"type"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

type ResourceLatestRelease struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

type ResourceStatus struct {
	LatestRelease *ResourceLatestRelease `json:"latestRelease,omitempty"`
}

type Resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   OCObjectMeta    `json:"metadata"`
	Spec       ResourceSpec    `json:"spec"`
	Status     *ResourceStatus `json:"status,omitempty"`
}

type ResourceReleaseBindingOwner struct {
	ProjectName  string `json:"projectName"`
	ResourceName string `json:"resourceName"`
}

type ResourceReleaseBindingSpec struct {
	Owner                          ResourceReleaseBindingOwner `json:"owner"`
	Environment                    string                      `json:"environment"`
	ResourceRelease                string                      `json:"resourceRelease,omitempty"` // the PIN
	RetainPolicy                   string                      `json:"retainPolicy,omitempty"`
	ResourceTypeEnvironmentConfigs json.RawMessage             `json:"resourceTypeEnvironmentConfigs,omitempty"`
}

type OCCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"` // "True" | "False" | "Unknown"
	Reason string `json:"reason,omitempty"`
}

type ResourceReleaseBindingStatus struct {
	Conditions []OCCondition `json:"conditions,omitempty"`
}

type ResourceReleaseBinding struct {
	APIVersion string                        `json:"apiVersion"`
	Kind       string                        `json:"kind"`
	Metadata   OCObjectMeta                  `json:"metadata"`
	Spec       ResourceReleaseBindingSpec    `json:"spec"`
	Status     *ResourceReleaseBindingStatus `json:"status,omitempty"`
}

// IsReady reports whether the binding's aggregate Ready condition is True.
func (b *ResourceReleaseBinding) IsReady() bool {
	if b == nil || b.Status == nil {
		return false
	}
	for _, c := range b.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}

// ---- client ----------------------------------------------------------------

// httpDoer is satisfied by *http.Client and by requests.RetryableHTTPClient.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type resourceClient struct {
	baseURL string
	http    httpDoer
	cfg     Config
}

// NewResourceClient builds a Resource-model client over the same three-layer
// authenticated transport as the gen client (correlation-id → retry/401-refresh
// → service-identity auth). Resource authoring runs as orchestration, so the
// auth path is always service-identity (no inbound-user-JWT forwarding).
func NewResourceClient(cfg Config) ResourceClient {
	retryCfg := cfg.RetryConfig
	if retryCfg.RetryOnStatus == nil {
		retryCfg.RetryOnStatus = func(status int) bool {
			if status == http.StatusUnauthorized {
				if cfg.AuthProvider != nil {
					slog.Info("openchoreo(resource): 401, invalidating cached token and retrying")
					cfg.AuthProvider.Invalidate()
				}
				return true
			}
			return slices.Contains(requests.TransientHTTPErrorCodes, status)
		}
	}
	inner := &http.Client{Transport: httpx.WrapTransport(nil)}
	return &resourceClient{
		baseURL: cfg.BaseURL,
		http:    requests.NewRetryableHTTPClient(inner, retryCfg),
		cfg:     cfg,
	}
}

// applyAuth mirrors transport.go's service-identity branch: Host + X-Use-OpenAPI,
// X-Impersonate-Org (resolver × namespace-in-path), and the M2M service token.
func (c *resourceClient) applyAuth(ctx context.Context, req *http.Request) error {
	if c.cfg.HostHeader != "" {
		req.Host = c.cfg.HostHeader
	}
	req.Header.Set("X-Use-OpenAPI", "true")
	if c.cfg.ImpersonateOrgResolver != nil {
		if ns := namespaceFromPath(req.URL.Path); ns != "" {
			orgUUID, err := c.cfg.ImpersonateOrgResolver(ctx, ns)
			if err != nil {
				return fmt.Errorf("openchoreo(resource): resolve impersonation org for namespace %q: %w", ns, err)
			}
			if orgUUID != "" {
				req.Header.Set("X-Impersonate-Org", orgUUID)
			}
		}
	}
	if c.cfg.AuthProvider != nil {
		tok, err := c.cfg.AuthProvider.Token()
		if err != nil {
			return fmt.Errorf("openchoreo(resource): service token fetch failed: %w", err)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	return nil
}

// do issues a single authenticated request. `body` is JSON-encoded (nil ⇒ none);
// on a 2xx the response is decoded into `out` (nil ⇒ discarded). Returns the
// status code so callers can branch on 409 (conflict) / 404.
func (c *resourceClient) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("openchoreo(resource): marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return 0, fmt.Errorf("openchoreo(resource): build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.applyAuth(ctx, req); err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openchoreo(resource): %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return resp.StatusCode, fmt.Errorf("openchoreo(resource): decode %s %s: %w", method, path, err)
			}
		}
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("openchoreo(resource): %s %s → %d: %s", method, path, resp.StatusCode, string(raw))
}

func nsBase(ns string) string {
	return "/api/v1/namespaces/" + ns
}

func (c *resourceClient) EnsureResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error) {
	rt.APIVersion, rt.Kind = ocAPIVersion, kindResourceType
	rt.Metadata.Namespace = namespace
	out := &ResourceType{}
	code, err := c.do(ctx, http.MethodPost, nsBase(namespace)+"/resourcetypes", rt, out)
	switch {
	case err == nil:
		return out, nil
	case code == http.StatusConflict:
		// Already exists — ResourceTypes are immutable, so return the existing one.
		got := &ResourceType{}
		if _, gerr := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resourcetypes/"+rt.Metadata.Name, nil, got); gerr != nil {
			return nil, fmt.Errorf("ensure resourcetype %q: conflict but refetch failed: %w", rt.Metadata.Name, gerr)
		}
		return got, nil
	default:
		return nil, fmt.Errorf("ensure resourcetype %q: %w", rt.Metadata.Name, err)
	}
}

func (c *resourceClient) ApplyResource(ctx context.Context, namespace string, r *Resource) (*Resource, error) {
	r.APIVersion, r.Kind = ocAPIVersion, kindResource
	r.Metadata.Namespace = namespace
	out := &Resource{}
	code, err := c.do(ctx, http.MethodPost, nsBase(namespace)+"/resources", r, out)
	switch {
	case err == nil:
		return out, nil
	case code == http.StatusConflict:
		return c.GetResource(ctx, namespace, r.Metadata.Name)
	default:
		return nil, fmt.Errorf("apply resource %q: %w", r.Metadata.Name, err)
	}
}

func (c *resourceClient) GetResource(ctx context.Context, namespace, name string) (*Resource, error) {
	out := &Resource{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resources/"+name, nil, out); err != nil {
		return nil, fmt.Errorf("get resource %q: %w", name, err)
	}
	return out, nil
}

func (c *resourceClient) EnsureBinding(ctx context.Context, namespace string, b *ResourceReleaseBinding) (*ResourceReleaseBinding, error) {
	b.APIVersion, b.Kind = ocAPIVersion, kindResourceReleaseBind
	b.Metadata.Namespace = namespace
	out := &ResourceReleaseBinding{}
	code, err := c.do(ctx, http.MethodPost, nsBase(namespace)+"/resourcereleasebindings", b, out)
	switch {
	case err == nil:
		return out, nil
	case code == http.StatusConflict:
		// Exists — PUT to reconcile the pin / per-env configs (PUT is a keyed update).
		put := &ResourceReleaseBinding{}
		if _, perr := c.do(ctx, http.MethodPut, nsBase(namespace)+"/resourcereleasebindings/"+b.Metadata.Name, b, put); perr != nil {
			return nil, fmt.Errorf("ensure binding %q: conflict but update failed: %w", b.Metadata.Name, perr)
		}
		return put, nil
	default:
		return nil, fmt.Errorf("ensure binding %q: %w", b.Metadata.Name, err)
	}
}

func (c *resourceClient) GetBinding(ctx context.Context, namespace, name string) (*ResourceReleaseBinding, error) {
	out := &ResourceReleaseBinding{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resourcereleasebindings/"+name, nil, out); err != nil {
		return nil, fmt.Errorf("get binding %q: %w", name, err)
	}
	return out, nil
}

func (c *resourceClient) DeleteBinding(ctx context.Context, namespace, name string) error {
	code, err := c.do(ctx, http.MethodDelete, nsBase(namespace)+"/resourcereleasebindings/"+name, nil, nil)
	if err != nil && code != http.StatusNotFound {
		return fmt.Errorf("delete binding %q: %w", name, err)
	}
	return nil
}

func (c *resourceClient) DeleteResource(ctx context.Context, namespace, name string) error {
	code, err := c.do(ctx, http.MethodDelete, nsBase(namespace)+"/resources/"+name, nil, nil)
	if err != nil && code != http.StatusNotFound {
		return fmt.Errorf("delete resource %q: %w", name, err)
	}
	return nil
}

// resourceTypeList is the OC list envelope for (cluster)resourcetypes.
type resourceTypeList struct {
	Items []ResourceType `json:"items"`
}

func (c *resourceClient) ListClusterResourceTypes(ctx context.Context) ([]ResourceType, error) {
	out := &resourceTypeList{}
	if _, err := c.do(ctx, http.MethodGet, "/api/v1/clusterresourcetypes", nil, out); err != nil {
		return nil, fmt.Errorf("list clusterresourcetypes: %w", err)
	}
	return out.Items, nil
}
