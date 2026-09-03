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
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/internal/clients/requests"
)

//go:generate go run github.com/matryer/moq@v0.7.1 -rm -fmt goimports -pkg mocks -out mocks/resource_client_mock.go . ResourceClient

// ResourceClient authors the OpenChoreo Resource model (openchoreo.dev/v1alpha1,
// shipped v1.1) used to wire external resources (and platform-resources) into
// consuming Workloads. The generated `gen` client is pinned to a spec version
// that predates v1.1 and has no Resource types, so this client is hand-rolled
// over the same authenticated transport (buildRetryConfig + authRequestEditor
// in transport.go) rather than over gen.ClientWithResponses.
//
// Authoring chain (the BFF owns every step — Resources have NO AutoDeploy):
//
//	EnsureResourceType  → per-dependency ResourceType (schema + ConfigMap/
//	                      ExternalSecret template + outputs), get-or-create,
//	                      immutable once created
//	ApplyResource       → one Resource per project; the OC controller cuts an
//	                      immutable ResourceRelease and writes status.latestRelease
//	EnsureBinding       → one ResourceReleaseBinding per environment, with
//	                      spec.resourceRelease PINNED to status.latestRelease (the
//	                      `occ resource promote` contract — no controller does this)
//
// The consumer Workload then references the Resource via
// spec.dependencies.resources[].ref + envBindings; its ReleaseBinding gates on
// the native `ResourceDependenciesReady` condition. Wiring that reference onto
// the Workload is out of this client's scope (it lives with the Component/
// Workload write path); this client only authors the Resource-model CRs
// themselves plus the read-only endpoint catalog below.
type ResourceClient interface {
	// EnsureResourceType get-or-creates a namespaced ResourceType. Idempotent on
	// (namespace, name): a 409 returns the existing one. ResourceTypes are treated
	// as immutable — a changed schema must use a new name (suffix), never an edit.
	EnsureResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error)

	// ApplyResource creates a Resource, or on 409 reconciles the existing one
	// via PUT so RT-version bumps and spec.parameters changes propagate. A 409
	// whose existing spec.type differs from the desired one is a HARD error
	// (cross-kind name collision) — never a silent overwrite. The returned
	// Resource carries the release known AT APPLY TIME in status.latestRelease
	// (empty on create, the pre-reconcile release on a 409 PUT); read it with
	// ReleaseName and pass it to WaitForReleaseChange so a reconcile waits for a
	// NEW release rather than re-pinning the stale one.
	ApplyResource(ctx context.Context, namespace string, r *Resource) (*Resource, error)

	// GetResource reads a Resource (incl. status.latestRelease).
	GetResource(ctx context.Context, namespace, name string) (*Resource, error)

	// EnsureBinding authors a per-env ResourceReleaseBinding and PINS
	// spec.resourceRelease (no AutoDeploy controller exists). Idempotent: a 409
	// PUTs the binding to reconcile the pin / per-env configs.
	EnsureBinding(ctx context.Context, namespace string, b *ResourceReleaseBinding) (*ResourceReleaseBinding, error)

	// GetBinding reads a ResourceReleaseBinding (incl. status.conditions).
	// Returns (nil, nil) on 404 — a binding may not exist yet (e.g. the
	// provision watcher hasn't run); callers treat that as "not yet created".
	GetBinding(ctx context.Context, namespace, name string) (*ResourceReleaseBinding, error)

	// PatchBindingEnvironmentConfigs merges the given keys into an existing
	// binding's spec.resourceTypeEnvironmentConfigs and re-applies it
	// (EnsureBinding semantics: PUT on 409). Read-merge-write — it GETs the
	// binding, overlays `configs` onto the existing env-config map (keys not in
	// `configs` are preserved), and re-applies ONLY when a value actually
	// changed. An idempotent re-patch carrying identical values is a no-op (no
	// EnsureBinding call), so a caller that re-runs on every deploy cascade does
	// not churn the CR / trigger a needless re-render. Errors when the binding
	// does not exist yet (the provisioner authors it first).
	PatchBindingEnvironmentConfigs(ctx context.Context, orgID, bindingName string, configs map[string]string) error

	// DeleteBinding removes a per-env binding. Step 1 of a 2-step
	// dependency delete (bindings cascade their DP objects via
	// retainPolicy: Delete). 404-tolerant (idempotent).
	DeleteBinding(ctx context.Context, namespace, name string) error

	// DeleteResource removes a Resource. Step 2 of the delete — its finalizer
	// blocks until all bindings referencing it are gone. 404-tolerant (idempotent).
	DeleteResource(ctx context.Context, namespace, name string) error

	// ListClusterResourceTypes discovers the installed cluster-scoped
	// ClusterResourceTypes (the platform-resource catalog — read-only).
	ListClusterResourceTypes(ctx context.Context) ([]ResourceType, error)

	// ListResourceTypes discovers the org-namespaced ResourceTypes registered
	// in namespace (the org-level ResourceType registry — read-only). Mirrors
	// ListClusterResourceTypes but scoped to one namespace instead of the
	// cluster.
	ListResourceTypes(ctx context.Context, namespace string) ([]ResourceType, error)

	// GetResourceType fetches one namespaced ResourceType by name. Returns the
	// ErrNotFound sentinel (via sentinelForStatus) when it does not exist.
	GetResourceType(ctx context.Context, namespace, name string) (*ResourceType, error)

	// DeleteResourceType removes a namespaced ResourceType. 404-tolerant
	// (idempotent), mirroring DeleteResource/DeleteBinding below. Used by the
	// org-settings external-resource delete surface (resources.
	// ExternalResourceCatalog.Delete) to prune every RT registered under a
	// logical external-resource name — ResourceTypes are immutable and never
	// edited in place, so more than one RT can carry the same
	// aep.wso2.com/external-name annotation (see ExternalResourceRTName).
	DeleteResourceType(ctx context.Context, namespace, name string) error

	// UpdateResourceType PUTs an existing namespaced ResourceType (same path
	// shape as GetResourceType / DeleteResourceType). Used to replace catalog
	// fields (description, consumption instructions, key descriptions,
	// resource-docs) when key identity — and therefore the hashed RT name —
	// is unchanged. EnsureResourceType stays get-or-create and never PUTs.
	UpdateResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error)

	// ListWorkloadEndpoints enumerates every provider-side endpoint declared by
	// the Workloads in an org's namespace (one row per endpoint, carrying owner +
	// visibility). This is the dynamic source for the org endpoint catalog: the
	// architect discovers org-service targets here, and resolution gates on
	// each row's namespace visibility. orgHandle is the org's namespace name
	// (OC namespaces are 1:1 with orgs).
	ListWorkloadEndpoints(ctx context.Context, orgHandle string) ([]WorkloadEndpointInfo, error)

	// ListWorkloadConsumerDeps enumerates consumer-side dependency refs declared
	// by Workloads owned by projectName in the org's namespace. It GETs the same
	// /workloads collection as ListWorkloadEndpoints but parses
	// spec.dependencies.resources[].ref and spec.dependencies.endpoints[] —
	// generated WorkloadSpec.Dependencies currently has endpoints only.
	// Workloads whose spec.owner.projectName does not match are omitted.
	ListWorkloadConsumerDeps(ctx context.Context, orgHandle, projectName string) ([]WorkloadConsumerDep, error)
}

// WorkloadEndpointInfo is one provider-side endpoint discovered by enumerating
// an org namespace's Workloads — the raw material for the org endpoint
// catalog. It names the owning project + component, the endpoint, and the
// extra visibilities the provider declared (project visibility is always
// implicit).
type WorkloadEndpointInfo struct {
	Project    string   // owner project (spec.owner.projectName)
	Component  string   // owner component (spec.owner.componentName)
	Workload   string   // workload name (metadata.name)
	Name       string   // endpoint name (spec.endpoints key)
	Type       string   // HTTP | gRPC | GraphQL | Websocket | TCP | UDP
	Port       int32    // endpoint port
	BasePath   string   // optional root path
	Visibility []string // explicit extra visibilities: namespace | internal | external

	// Schema is the endpoint's inline API definition, if the Workload declares one
	// (OpenChoreo populates spec.endpoints[].schema from workload.yaml `schemaFile:`,
	// or a directly-authored Workload CR). Empty for app-factory BYO-image workloads.
	SchemaType    string // e.g. "openapi", "proto", "graphql" (endpoint.Schema.Type)
	SchemaContent string // the inlined spec document (endpoint.Schema.Content)
}

// WorkloadConsumerDep is one Workload's consumer-side dependency refs —
// resources[].ref (OC Resource instance names) and endpoints[] (provider
// project/component + visibility). OwnerProject is spec.owner.projectName.
type WorkloadConsumerDep struct {
	OwnerProject   string
	OwnerComponent string
	ResourceRefs   []string
	Endpoints      []WorkloadConsumerEndpoint
}

// WorkloadConsumerEndpoint is one spec.dependencies.endpoints[] entry.
type WorkloadConsumerEndpoint struct {
	Project    string // provider project; empty means same project as the consumer
	Component  string
	Name       string
	Visibility string // project | namespace
}

// NamespaceVisible reports whether this endpoint is consumable cross-project
// within the same namespace (i.e. the provider published it for org-service use).
func (e WorkloadEndpointInfo) NamespaceVisible() bool {
	for _, v := range e.Visibility {
		if v == "namespace" {
			return true
		}
	}
	return false
}

// ---- wire DTOs (openchoreo.dev/v1alpha1) -----------------------------------

const (
	ocResourceAPIVersion    = "openchoreo.dev/v1alpha1"
	kindResourceType        = "ResourceType"
	kindResource            = "Resource"
	kindResourceReleaseBind = "ResourceReleaseBinding"
)

// OCObjectMeta is the slice of k8s metadata the BFF sets on Resource-model CRs.
// Annotations is read-only here: the BFF never sets it on Resource-model CRs it
// authors, but ListClusterResourceTypes decodes it off PE-authored
// ClusterResourceTypes, which carry the `aep.wso2.com/*` markers (see the
// `resources` package's markers.go) alongside Labels' `aep.wso2.com/role`.
type OCObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	// CreationTimestamp is k8s' own metadata.creationTimestamp (RFC3339,
	// stamped by OC on every object it creates). It rides along automatically
	// wherever OCObjectMeta is decoded (ListResourceTypes / GetResourceType /
	// EnsureResourceType, ListClusterResourceTypes, etc. all decode straight
	// into this struct via the generic do()/json.Unmarshal path — there is no
	// separate metadata decode to keep in sync). It exists so callers can
	// order same-named ResourceTypes by recency: ResourceTypes are immutable
	// and never deleted, so a schema change mints a brand-new RT (see
	// ExternalResourceRTName) while the old one persists — without this field
	// there was no way to tell which of two same-name RTs is current.
	CreationTimestamp time.Time `json:"creationTimestamp,omitempty"`
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

// ResourceTypeSpec is ResourceType.spec.
type ResourceTypeSpec struct {
	Parameters         *SchemaSection         `json:"parameters,omitempty"`
	EnvironmentConfigs *SchemaSection         `json:"environmentConfigs,omitempty"`
	RetainPolicy       string                 `json:"retainPolicy,omitempty"`
	Outputs            []ResourceTypeOutput   `json:"outputs,omitempty"`
	Resources          []ResourceTypeManifest `json:"resources"` // MinItems=1 (OC-enforced)
}

// ResourceType is the openchoreo.dev/v1alpha1 ResourceType CR.
type ResourceType struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   OCObjectMeta     `json:"metadata"`
	Spec       ResourceTypeSpec `json:"spec"`
}

// ResourceOwner is Resource.spec.owner.
type ResourceOwner struct {
	ProjectName string `json:"projectName"`
}

// ResourceTypeRef is Resource.spec.type — the ResourceType (or
// ClusterResourceType) a Resource instantiates.
type ResourceTypeRef struct {
	Kind string `json:"kind,omitempty"` // ResourceType | ClusterResourceType (default ResourceType)
	Name string `json:"name"`
}

// ResourceSpec is Resource.spec.
type ResourceSpec struct {
	Owner      ResourceOwner   `json:"owner"`
	Type       ResourceTypeRef `json:"type"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ResourceLatestRelease is Resource.status.latestRelease — the ResourceRelease
// name the per-env bindings pin to.
type ResourceLatestRelease struct {
	Name string `json:"name"`
	Hash string `json:"hash,omitempty"`
}

// ResourceStatus is Resource.status.
type ResourceStatus struct {
	LatestRelease *ResourceLatestRelease `json:"latestRelease,omitempty"`
	Conditions    []OCCondition          `json:"conditions,omitempty"`
}

// Resource is the openchoreo.dev/v1alpha1 Resource CR.
type Resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   OCObjectMeta    `json:"metadata"`
	Spec       ResourceSpec    `json:"spec"`
	Status     *ResourceStatus `json:"status,omitempty"`
}

// ResourceReleaseBindingOwner is ResourceReleaseBinding.spec.owner.
type ResourceReleaseBindingOwner struct {
	ProjectName  string `json:"projectName"`
	ResourceName string `json:"resourceName"`
}

// ResourceReleaseBindingSpec is ResourceReleaseBinding.spec.
type ResourceReleaseBindingSpec struct {
	Owner                          ResourceReleaseBindingOwner `json:"owner"`
	Environment                    string                      `json:"environment"`
	ResourceRelease                string                      `json:"resourceRelease,omitempty"` // the PIN
	RetainPolicy                   string                      `json:"retainPolicy,omitempty"`
	ResourceTypeEnvironmentConfigs json.RawMessage             `json:"resourceTypeEnvironmentConfigs,omitempty"`
}

// OCCondition mirrors a k8s CR's status.conditions[] entry.
type OCCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"` // "True" | "False" | "Unknown"
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// ResolvedOutput is one entry of a binding's status.outputs — a
// ResourceType output resolved to a concrete (hashed) DP object name. The BFF
// reads these to wire the consuming component's env (the names are
// OC-generated, not guessable).
type ResolvedOutput struct {
	Name            string    `json:"name"`
	Value           string    `json:"value,omitempty"`
	SecretKeyRef    *OCKeyRef `json:"secretKeyRef,omitempty"`
	ConfigMapKeyRef *OCKeyRef `json:"configMapKeyRef,omitempty"`
}

// ResourceReleaseBindingStatus is ResourceReleaseBinding.status.
type ResourceReleaseBindingStatus struct {
	Conditions []OCCondition    `json:"conditions,omitempty"`
	Outputs    []ResolvedOutput `json:"outputs,omitempty"`
}

// ResourceReleaseBinding is the openchoreo.dev/v1alpha1 ResourceReleaseBinding CR.
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

// ---- client -----------------------------------------------------------------

// resourceHTTPDoer is satisfied by *http.Client and by requests.RetryableHTTPClient.
type resourceHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type resourceClient struct {
	baseURL string
	http    resourceHTTPDoer
	editor  func(ctx context.Context, req *http.Request) error
}

// NewResourceClient builds a Resource-model client over the same
// authenticated transport stack as newGenClient (correlation-id →
// retry/401-refresh → auth request-editor), reusing buildRetryConfig and
// authRequestEditor from transport.go so the two clients never drift on auth
// or retry semantics.
func NewResourceClient(cfg Config) ResourceClient {
	if cfg.BaseURL == "" {
		panic(errors.New("init openchoreo resource client: Config.BaseURL is required"))
	}
	inner := &http.Client{Transport: httpx.WrapTransport(nil)}
	return &resourceClient{
		baseURL: cfg.BaseURL,
		http:    requests.NewRetryableHTTPClient(inner, buildRetryConfig(cfg)),
		editor:  authRequestEditor(cfg),
	}
}

// do issues a single authenticated request. `body` is JSON-encoded (nil ⇒ none);
// on a 2xx the response is decoded into `out` (nil ⇒ discarded). Returns the
// status code so callers can branch on 409 (conflict) / 404, and an error that
// (on non-2xx) wraps the matching sentinel from errors.go when one applies.
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
	if err := c.editor(ctx, req); err != nil {
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
	if sentinel := sentinelForStatus(resp.StatusCode); sentinel != nil {
		return resp.StatusCode, fmt.Errorf("%w: %s %s: %s", sentinel, method, path, string(raw))
	}
	return resp.StatusCode, fmt.Errorf("openchoreo(resource): %s %s → %d: %s", method, path, resp.StatusCode, string(raw))
}

func nsBase(ns string) string {
	return "/api/v1/namespaces/" + ns
}

func (c *resourceClient) EnsureResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error) {
	rt.APIVersion, rt.Kind = ocResourceAPIVersion, kindResourceType
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
	r.APIVersion, r.Kind = ocResourceAPIVersion, kindResource
	r.Metadata.Namespace = namespace
	out := &Resource{}
	code, err := c.do(ctx, http.MethodPost, nsBase(namespace)+"/resources", r, out)
	switch {
	case err == nil:
		// Freshly created — status.latestRelease is not cut yet, so callers see
		// an empty ReleaseName and wait-for-nonempty.
		return out, nil
	case code == http.StatusConflict:
		// Exists — reconcile via PUT so RT-version bumps and spec.parameters
		// changes propagate (mirrors EnsureBinding's 409→PUT), instead of the old
		// GetResource-and-return which silently dropped the new desired spec.
		//
		// Guard first: GET the existing Resource and REFUSE to overwrite it when
		// its spec.type differs from the desired one. Dependency names are unique
		// per project across kinds (external + platform-resource share ONE OC
		// Resource namespace per project, matched project-wide), so a differing
		// type means two different deps collided on one name — a blind PUT would
		// cross-wire them. Fail loud, naming both types.
		existing, gerr := c.GetResource(ctx, namespace, r.Metadata.Name)
		if gerr != nil {
			return nil, fmt.Errorf("apply resource %q: conflict but refetch failed: %w", r.Metadata.Name, gerr)
		}
		// Guard on the type KIND only. A differing Kind (ResourceType vs
		// ClusterResourceType) is the cross-wire hazard — an `external` dep and a
		// `platform-resource` dep collided on one project-scoped Resource name. A
		// differing Name at the SAME Kind is a legitimate reconcile (an external
		// RT version bump re-points the Resource at the freshly authored,
		// version-suffixed ResourceType), which the PUT below must propagate.
		if resourceTypeRefKind(existing.Spec.Type.Kind) != resourceTypeRefKind(r.Spec.Type.Kind) {
			return nil, fmt.Errorf(
				"apply resource %q: existing spec.type %s conflicts with desired %s — "+
					"dependency names must be unique per project across kinds; a differing kind means two deps collided on one name",
				r.Metadata.Name, formatTypeRef(existing.Spec.Type), formatTypeRef(r.Spec.Type))
		}
		// No-op reconcile: when the desired spec is identical to the existing one
		// there is nothing to propagate and the controller will never cut a new
		// release — a PUT + wait-for-CHANGE would hang until the poll timeout
		// (caught live in E2E: an idempotent re-save of unchanged values stalled
		// the /values endpoint). Report create-equivalent semantics instead:
		// strip the release so callers wait-for-nonempty, which their first poll
		// satisfies with the existing (still-correct) release.
		if specsEqual(existing.Spec, r.Spec) {
			out := *existing
			out.Status = nil
			return &out, nil
		}
		put := &Resource{}
		if _, perr := c.do(ctx, http.MethodPut, nsBase(namespace)+"/resources/"+r.Metadata.Name, r, put); perr != nil {
			return nil, fmt.Errorf("apply resource %q: conflict but update failed: %w", r.Metadata.Name, perr)
		}
		// Carry the PRE-reconcile status.latestRelease forward so callers can wait
		// for the controller to cut a NEW release for the changed spec rather than
		// re-pinning the stale one. A PUT to spec does not reset status, so the
		// response usually still reports it; if the server omitted it, fall back
		// to the pre-apply GET.
		if ReleaseName(put) == "" && ReleaseName(existing) != "" {
			put.Status = existing.Status
		}
		return put, nil
	default:
		return nil, fmt.Errorf("apply resource %q: %w", r.Metadata.Name, err)
	}
}

// specsEqual compares two ResourceSpecs by canonical JSON (Go's json.Marshal
// sorts map keys), normalising the type-ref Kind default first so an implicit
// "ResourceType" compares equal to an explicit one. Used to detect no-op
// reconciles on the ApplyResource 409 path.
func specsEqual(a, b ResourceSpec) bool {
	a.Type.Kind = resourceTypeRefKind(a.Type.Kind)
	b.Type.Kind = resourceTypeRefKind(b.Type.Kind)
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(aj, bj)
}

// resourceTypeRefKind normalises an empty Kind to its OC default ("ResourceType")
// so a Resource authored with an implicit kind compares equal to one that spells
// it out.
func resourceTypeRefKind(kind string) string {
	if kind == "" {
		return "ResourceType"
	}
	return kind
}

// formatTypeRef renders a spec.type ref as "Kind/Name" for error messages.
func formatTypeRef(t ResourceTypeRef) string {
	return resourceTypeRefKind(t.Kind) + "/" + t.Name
}

// ReleaseName returns a Resource's status.latestRelease name, or "" when no
// release has been cut yet (or r/its status is nil). Provisioners read it off
// ApplyResource's result to decide whether to wait-for-CHANGE (reconcile: a
// stale release is already present) or wait-for-nonempty (create).
func ReleaseName(r *Resource) string {
	if r != nil && r.Status != nil && r.Status.LatestRelease != nil {
		return r.Status.LatestRelease.Name
	}
	return ""
}

func (c *resourceClient) GetResource(ctx context.Context, namespace, name string) (*Resource, error) {
	out := &Resource{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resources/"+name, nil, out); err != nil {
		return nil, fmt.Errorf("get resource %q: %w", name, err)
	}
	return out, nil
}

func (c *resourceClient) EnsureBinding(ctx context.Context, namespace string, b *ResourceReleaseBinding) (*ResourceReleaseBinding, error) {
	b.APIVersion, b.Kind = ocResourceAPIVersion, kindResourceReleaseBind
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
	code, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resourcereleasebindings/"+name, nil, out)
	if err != nil {
		// Mirror DeleteBinding: suppress 404 — binding may not exist yet (e.g.
		// the provision watcher hasn't run). Callers receive (nil, nil) and
		// must treat it as "not yet created" (ready=false, no outputs).
		if code == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("get binding %q: %w", name, err)
	}
	return out, nil
}

func (c *resourceClient) PatchBindingEnvironmentConfigs(ctx context.Context, orgID, bindingName string, configs map[string]string) error {
	existing, err := c.GetBinding(ctx, orgID, bindingName)
	if err != nil {
		return fmt.Errorf("patch binding env configs %q: get: %w", bindingName, err)
	}
	if existing == nil {
		// The provisioner authors the binding first; a missing one means the
		// dependency isn't provisioned yet. Surface it so the caller defers.
		return fmt.Errorf("patch binding env configs %q: binding not found", bindingName)
	}

	// Decode the existing env-config map so unrelated keys survive the merge.
	merged := map[string]any{}
	if len(existing.Spec.ResourceTypeEnvironmentConfigs) > 0 {
		if err := json.Unmarshal(existing.Spec.ResourceTypeEnvironmentConfigs, &merged); err != nil {
			return fmt.Errorf("patch binding env configs %q: decode existing: %w", bindingName, err)
		}
	}

	changed := false
	for k, v := range configs {
		if cur, ok := merged[k]; !ok || cur != any(v) {
			merged[k] = v
			changed = true
		}
	}
	if !changed {
		// Binding already carries exactly these values — no-op (idempotent), so
		// a per-cascade re-patch never re-applies the CR.
		return nil
	}

	raw, err := json.Marshal(merged)
	if err != nil {
		return fmt.Errorf("patch binding env configs %q: marshal: %w", bindingName, err)
	}
	existing.Spec.ResourceTypeEnvironmentConfigs = json.RawMessage(raw)
	existing.Status = nil // write path is spec-only; never echo status back.
	if _, err := c.EnsureBinding(ctx, orgID, existing); err != nil {
		return fmt.Errorf("patch binding env configs %q: apply: %w", bindingName, err)
	}
	return nil
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

func (c *resourceClient) ListResourceTypes(ctx context.Context, namespace string) ([]ResourceType, error) {
	out := &resourceTypeList{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resourcetypes", nil, out); err != nil {
		return nil, fmt.Errorf("list resourcetypes in %q: %w", namespace, err)
	}
	return out.Items, nil
}

func (c *resourceClient) GetResourceType(ctx context.Context, namespace, name string) (*ResourceType, error) {
	out := &ResourceType{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/resourcetypes/"+name, nil, out); err != nil {
		return nil, fmt.Errorf("get resourcetype %q: %w", name, err)
	}
	return out, nil
}

func (c *resourceClient) DeleteResourceType(ctx context.Context, namespace, name string) error {
	code, err := c.do(ctx, http.MethodDelete, nsBase(namespace)+"/resourcetypes/"+name, nil, nil)
	if err != nil && code != http.StatusNotFound {
		return fmt.Errorf("delete resourcetype %q: %w", name, err)
	}
	return nil
}

func (c *resourceClient) UpdateResourceType(ctx context.Context, namespace string, rt *ResourceType) (*ResourceType, error) {
	rt.APIVersion, rt.Kind = ocResourceAPIVersion, kindResourceType
	rt.Metadata.Namespace = namespace
	out := &ResourceType{}
	if _, err := c.do(ctx, http.MethodPut, nsBase(namespace)+"/resourcetypes/"+rt.Metadata.Name, rt, out); err != nil {
		return nil, fmt.Errorf("update resourcetype %q: %w", rt.Metadata.Name, err)
	}
	return out, nil
}

// workloadList / workloadItem mirror just the slice of the Workload CR the
// catalog needs: the owner (project/component) and the endpoints map.
type workloadList struct {
	Items []workloadItem `json:"items"`
}

type workloadItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Owner struct {
			ProjectName   string `json:"projectName"`
			ComponentName string `json:"componentName"`
		} `json:"owner"`
		Endpoints map[string]struct {
			Type       string   `json:"type"`
			Port       int32    `json:"port"`
			BasePath   string   `json:"basePath"`
			Visibility []string `json:"visibility"`
			Schema     struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			} `json:"schema"`
		} `json:"endpoints"`
	} `json:"spec"`
}

func (c *resourceClient) ListWorkloadEndpoints(ctx context.Context, orgHandle string) ([]WorkloadEndpointInfo, error) {
	out := &workloadList{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(orgHandle)+"/workloads", nil, out); err != nil {
		return nil, fmt.Errorf("list workloads in %q: %w", orgHandle, err)
	}
	infos := make([]WorkloadEndpointInfo, 0)
	for _, w := range out.Items {
		for name, ep := range w.Spec.Endpoints {
			infos = append(infos, WorkloadEndpointInfo{
				Project:       w.Spec.Owner.ProjectName,
				Component:     w.Spec.Owner.ComponentName,
				Workload:      w.Metadata.Name,
				Name:          name,
				Type:          ep.Type,
				Port:          ep.Port,
				BasePath:      ep.BasePath,
				Visibility:    ep.Visibility,
				SchemaType:    ep.Schema.Type,
				SchemaContent: ep.Schema.Content,
			})
		}
	}
	return infos, nil
}

// workloadConsumerList is a separate decode shape from workloadList so
// ListWorkloadEndpoints keeps parsing only provider-side spec.endpoints.
type workloadConsumerList struct {
	Items []workloadConsumerItem `json:"items"`
}

type workloadConsumerItem struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Owner struct {
			ProjectName   string `json:"projectName"`
			ComponentName string `json:"componentName"`
		} `json:"owner"`
		Dependencies struct {
			Resources []struct {
				Ref string `json:"ref"`
			} `json:"resources"`
			Endpoints []struct {
				Project    string `json:"project"`
				Component  string `json:"component"`
				Name       string `json:"name"`
				Visibility string `json:"visibility"`
			} `json:"endpoints"`
		} `json:"dependencies"`
	} `json:"spec"`
}

func (c *resourceClient) ListWorkloadConsumerDeps(ctx context.Context, orgHandle, projectName string) ([]WorkloadConsumerDep, error) {
	out := &workloadConsumerList{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(orgHandle)+"/workloads", nil, out); err != nil {
		return nil, fmt.Errorf("list workloads in %q: %w", orgHandle, err)
	}
	deps := make([]WorkloadConsumerDep, 0)
	for _, w := range out.Items {
		if w.Spec.Owner.ProjectName != projectName {
			continue
		}
		refs := make([]string, 0, len(w.Spec.Dependencies.Resources))
		for _, r := range w.Spec.Dependencies.Resources {
			if r.Ref != "" {
				refs = append(refs, r.Ref)
			}
		}
		eps := make([]WorkloadConsumerEndpoint, 0, len(w.Spec.Dependencies.Endpoints))
		for _, ep := range w.Spec.Dependencies.Endpoints {
			if ep.Component == "" {
				continue
			}
			eps = append(eps, WorkloadConsumerEndpoint{
				Project:    ep.Project,
				Component:  ep.Component,
				Name:       ep.Name,
				Visibility: ep.Visibility,
			})
		}
		deps = append(deps, WorkloadConsumerDep{
			OwnerProject:   w.Spec.Owner.ProjectName,
			OwnerComponent: w.Spec.Owner.ComponentName,
			ResourceRefs:   refs,
			Endpoints:      eps,
		})
	}
	return deps, nil
}

const resourceTypeNotFound = "ResourceTypeNotFound"

// resourceTerminalCondition returns the Ready=False condition whose reason is
// known to be unrecoverable (a missing ClusterResourceType). Other Ready=False
// reasons are left for the poll loop — a controller may set False transiently.
func resourceTerminalCondition(r *Resource) *OCCondition {
	if r == nil || r.Status == nil {
		return nil
	}
	for i := range r.Status.Conditions {
		c := &r.Status.Conditions[i]
		if c.Type == "Ready" && c.Status == "False" && c.Reason == resourceTypeNotFound {
			return c
		}
	}
	return nil
}

// ErrReleaseWaitTimeout is a wait-boundary answer: the poll deadline expired
// without a new ResourceRelease. GetResource transport errors and
// context.Canceled / DeadlineExceeded are not this sentinel. A missing
// ClusterResourceType is ErrResourceTypeNotFound, not a timeout: that answer
// is terminal even when a prior release already exists.
var ErrReleaseWaitTimeout = errors.New("release wait timed out")

// ErrResourceTypeNotFound is a wait-boundary answer: the Resource reported
// Ready=False with reason ResourceTypeNotFound. The controller will never cut
// a release for a type that is not installed.
var ErrResourceTypeNotFound = errors.New("cluster resource type not found")

// WaitForReleaseChange polls GetResource until status.latestRelease.Name is
// non-empty AND differs from prior, returning the release name the caller pins
// a ResourceReleaseBinding to. `prior` is the release observed at apply time
// (ReleaseName on ApplyResource's result):
//
//   - "" when the Resource was freshly created — any non-empty release
//     satisfies the wait (the wait-for-nonempty case);
//   - the stale release when an EXISTING Resource was reconciled — the wait then
//     holds until the OC controller cuts the NEW release for the changed spec,
//     so a reconcile never pins the binding to the pre-reconcile release.
//
// A Ready=False condition with reason ResourceTypeNotFound is terminal: the
// controller will never cut a release, so the wait returns immediately with an
// error quoting the condition message. Other Ready=False reasons are not
// treated as terminal (a controller may set False transiently).
//
// Deadline expiry wraps ErrReleaseWaitTimeout; ResourceTypeNotFound wraps
// ErrResourceTypeNotFound. Callers tell a wait answer from a poll blip via
// those two sentinels, and tell a missing type from a slow reconcile via
// which sentinel they got.
// It bounds ONLY the (fast) release-cut — the controller hashing spec.parameters
// into an immutable ResourceRelease — never the readiness of the backing infra
// that release eventually provisions (a real database can take minutes; callers
// must poll GetBinding/IsReady separately for that). Dedupes the two copies of
// this polling loop the source repo carried (its external-dependency and
// platform-resource provisioners each had their own).
func WaitForReleaseChange(ctx context.Context, rc ResourceClient, namespace, resourceName, prior string, interval, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		got, err := rc.GetResource(ctx, namespace, resourceName)
		if err != nil {
			return "", fmt.Errorf("poll resource %q: %w", resourceName, err)
		}
		if c := resourceTerminalCondition(got); c != nil {
			msg := c.Message
			if msg == "" {
				msg = c.Reason
			}
			return "", fmt.Errorf("%w: resource %q: %s", ErrResourceTypeNotFound, resourceName, msg)
		}
		if name := ReleaseName(got); name != "" && name != prior {
			return name, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("%w: resource %q produced no new ResourceRelease within %s", ErrReleaseWaitTimeout, resourceName, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}
	}
}
