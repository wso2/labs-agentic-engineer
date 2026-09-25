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

package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ErrThunderApplicationAPIMissing is returned when the kube API this
// process talks to does not serve ThunderApplication (HTTP 404 on the
// CRD). Split-plane: CRs live on the data-plane cluster; aep-api on
// the control-plane cluster 404s unless KUBE_API_BASE_URL points at
// the dataplane. The wait still patches the SPA callback onto the
// ResourceReleaseBinding; it just cannot observe the CR.
var ErrThunderApplicationAPIMissing = errors.New("thunder application API not on this kube API")

// ThunderApplicationView is the deploy-wait projection of a ThunderApplication
// CR: the callback URL written on the spec, and whether that generation has
// been observed ready. This package consumes the view; it does not talk to
// Kubernetes.
type ThunderApplicationView struct {
	RedirectURIs       string
	Ready              bool
	Generation         int64
	ObservedGeneration int64
}

// ThunderApplicationReader fetches the ThunderApplication OpenChoreo rendered
// for a Resource name in an environment. (nil, nil) means the CR is not in
// the cluster yet — the wait stays pending. Lookup is by OC labels, not the
// rendered object name (r-<resource>-<env>-<hash8> in the dataplane NS).
type ThunderApplicationReader interface {
	FindByResource(ctx context.Context, resourceName, environment string) (*ThunderApplicationView, error)
}

// ConsumerURLMarker is the projects-owned projection of CRT consumer-URL
// markers the thunder wait keys on. Empty EnvConfig means the type is not a
// consumer-URL type (skip that dep). Path is always set by the app adapter
// (and by tests) when EnvConfig is set — e.g. "/callback".
type ConsumerURLMarker struct {
	EnvConfig string // CRT consumer-url-env-config value, e.g. "redirectUris"
	Path      string // path to append; tests and the app adapter always set this
}

// resourceMarkerCatalog is the CRT marker lookup the wait keys on
// (EnvConfig, never a resourceType name). The app adapter maps CRT
// TypeMarkers onto ConsumerURLMarker at the composition root.
type resourceMarkerCatalog interface {
	MarkersByName(ctx context.Context) (map[string]ConsumerURLMarker, error)
}

// bindingEnvironmentPatcher is the ResourceReleaseBinding env-config write
// the wait uses to register the SPA callback. openchoreo.ResourceClient
// satisfies it; the port stays this one method so the wait does not pull
// the rest of the Resource model.
type bindingEnvironmentPatcher interface {
	PatchBindingEnvironmentConfigs(ctx context.Context, orgID, bindingName string, configs map[string]string) error
}

// SetResourceCatalog wires the CRT marker lookup the thunder wait keys on.
// A nil catalog skips the wait (today's OC-only verdict).
func (s *DeploymentService) SetResourceCatalog(c resourceMarkerCatalog) {
	if s != nil {
		s.catalog = c
	}
}

// SetResourceClient wires the ResourceReleaseBinding env-config patcher.
// A nil client skips the wait.
func (s *DeploymentService) SetResourceClient(c bindingEnvironmentPatcher) {
	if s != nil {
		s.resourceClient = c
	}
}

// SetThunderApplicationReader wires the ThunderApplication CR reader.
// A nil reader skips the wait.
func (s *DeploymentService) SetThunderApplicationReader(r ThunderApplicationReader) {
	if s != nil {
		s.thunder = r
	}
}

// consumerDep is one consumer-URL dependency as a single read resolved it:
// which web apps declare it, the callback each of those has advertised so far,
// and the project's whole callback set.
//
// declaredBy and byComponent are deliberately separate. A web app that declares
// the dependency but whose external URL has not resolved yet appears in the
// first and not the second — it contributes no callback, and it is exactly the
// component that must stay pending.
type consumerDep struct {
	name   string
	marker ConsumerURLMarker
	// declaredBy is keyed by k8s component name.
	declaredBy map[string]bool
	// byComponent is k8s component name -> that component's own callback.
	byComponent map[string]string
	// callbacks is the project's whole resolved set, sorted and deduplicated.
	callbacks []string
}

// thunderPass is one read's view of the project's consumer-URL wiring.
//
// It exists because the value written to a SHARED dependency is a function of
// the WHOLE PROJECT, not of the component whose verdict is being folded.
// cell-design models `user-auth` as ONE external that several components edge
// into, so two web apps legitimately share one Thunder client, one
// ResourceReleaseBinding and one `redirectUris`. Registering from inside the
// per-component fold wrote that single field once per component, each call
// REPLACING the last: only the final web app in the loop was ever registered,
// and every other one stayed pending until the deploy budget expired — which is
// how a project with two SPAs could never deploy.
//
// Built from the DESIGN rather than from the wait set on purpose: a later cycle
// that redeploys only the api must not shrink the set and un-register a web app
// that is already live.
type thunderPass struct {
	deps []consumerDep
}

// newThunderPass resolves the project's consumer-URL wiring for one read.
//
// nil means there is nothing to register and nothing to wait for: the wait is
// unwired, the design is absent, or no web app declares a dependency whose CRT
// carries ConsumerURLEnvConfig.
//
// `withdrawing` names components this read already knows OpenChoreo is taking
// down. They are dropped outright — a web app being removed must not keep a
// redirect URI on the shared client, and it is not waited on either. A FAILED
// component is deliberately NOT dropped: its previous release is usually still
// serving at the same URL, and un-registering it because a NEW release failed to
// render would sign users out of an app that is working.
func (s *DeploymentService) newThunderPass(ctx context.Context, orgID, projectID string, withdrawing map[string]bool) (*thunderPass, error) {
	if s == nil || s.catalog == nil || s.resourceClient == nil || s.thunder == nil || s.store == nil {
		return nil, nil
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if spec.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("deployment: thunder wait: read design: %w", err)
	}
	if design == nil {
		return nil, nil
	}
	markers, err := s.catalog.MarkersByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("deployment: thunder wait: catalog: %w", err)
	}

	byName := map[string]*consumerDep{}
	var order []string
	for i := range design.Components {
		comp := &design.Components[i]
		if comp.ComponentType != spec.ComponentTypeWebApplication {
			continue
		}
		component := k8sname.ToK8sName(comp.Name)
		if withdrawing[component] {
			continue
		}
		for j := range comp.Dependencies {
			dep := comp.Dependencies[j]
			if dep.Kind != spec.DependencyKindPlatformResource {
				continue
			}
			marker := markers[dep.ResourceType]
			if marker.EnvConfig == "" {
				continue
			}
			cd := byName[dep.Name]
			if cd == nil {
				cd = &consumerDep{
					name:        dep.Name,
					marker:      marker,
					declaredBy:  map[string]bool{},
					byComponent: map[string]string{},
				}
				byName[dep.Name] = cd
				order = append(order, dep.Name)
			}
			cd.declaredBy[component] = true
		}
	}
	if len(order) == 0 {
		return nil, nil
	}

	// One external-URL read per web app, however many dependencies it declares.
	origins := map[string]string{}
	originOf := func(component string) string {
		if o, ok := origins[component]; ok {
			return o
		}
		o := strings.TrimRight(s.componentExternalURL(ctx, orgID, projectID, component), "/")
		origins[component] = o
		return o
	}

	pass := &thunderPass{deps: make([]consumerDep, 0, len(order))}
	for _, name := range order {
		cd := byName[name]
		for component := range cd.declaredBy {
			origin := originOf(component)
			if origin == "" {
				continue
			}
			cd.byComponent[component] = origin + cd.marker.Path
		}
		cd.callbacks = sortedCallbacks(cd.byComponent)
		pass.deps = append(pass.deps, *cd)
	}
	return pass, nil
}

// sortedCallbacks flattens the per-component callbacks into the value written
// to the binding.
//
// SORTED, and that is load-bearing rather than tidiness: the write is skipped
// when the value is unchanged (PatchBindingEnvironmentConfigs), so a set joined
// in map-iteration order would differ between two otherwise identical reads and
// make every deploy poll issue a real write — the write storm this change
// exists to stop. Deduplicated because two web apps served at one origin would
// otherwise register the same callback twice.
func sortedCallbacks(byComponent map[string]string) []string {
	if len(byComponent) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(byComponent))
	out := make([]string, 0, len(byComponent))
	for _, callback := range byComponent {
		if seen[callback] {
			continue
		}
		seen[callback] = true
		out = append(out, callback)
	}
	sort.Strings(out)
	return out
}

// registerConsumerCallbacks writes each shared dependency's WHOLE callback set,
// in ONE patch per dependency per read.
//
// Idempotent by construction: the value is a pure function of the design and the
// resolved origins, so once every web app's URL is up the value stops changing
// and the patch becomes a no-op the binding client skips. A dependency with no
// resolved callback yet is left alone rather than written empty — clearing a
// live app's registration to say "not ready" would sign it out mid-deploy.
func (s *DeploymentService) registerConsumerCallbacks(ctx context.Context, orgID, projectID string, pass *thunderPass) error {
	if pass == nil {
		return nil
	}
	for i := range pass.deps {
		dep := &pass.deps[i]
		if len(dep.callbacks) == 0 {
			continue
		}
		bindingName := ocname.ExternalResourceBindingName(projectID, dep.name, openchoreo.DevEnvironmentName)
		if err := s.resourceClient.PatchBindingEnvironmentConfigs(ctx, orgID, bindingName,
			map[string]string{dep.marker.EnvConfig: strings.Join(dep.callbacks, ",")}); err != nil {
			return fmt.Errorf("deployment: thunder wait: register callbacks for %q: %w", dep.name, err)
		}
	}
	return nil
}

// applyThunderWait holds a web-app's deploy verdict at pending until each
// consumer-URL dependency it declares carries THIS component's callback on the
// ThunderApplication CR (and that generation is ready). OpenChoreo Ready on the
// web-app binding is not enough: the placeholder
// https://pending.invalid/callback is not deployed.
//
// Read-only now — registerConsumerCallbacks did the writing, once, for the whole
// project. Failed and Undeploy verdicts are left alone; pending OC is not
// consulted.
//
// Every hold states its cause on st.Reason. A held component is PENDING, not
// failed, and the deploy budget reports the still-pending set as the failure
// when it expires — so without a reason here the fix issue that expiry mints
// names a component and no cause at all, and the agent that picks it up audits
// a container that was never broken.
func (s *DeploymentService) applyThunderWait(ctx context.Context, orgID, projectID, componentName string, pass *thunderPass, summary *openchoreo.ReleaseBindingSummary, st *delivery.ComponentDeploy) error {
	if pass == nil {
		return nil
	}
	if summary != nil && summary.Undeploy {
		return nil
	}
	if st == nil || !st.Ready || st.Failed {
		return nil
	}

	hold := ""
	for i := range pass.deps {
		dep := &pass.deps[i]
		if !dep.declaredBy[componentName] {
			continue
		}
		callback := dep.byComponent[componentName]
		if callback == "" {
			if hold == "" {
				hold = "waiting: this component's external URL has not resolved yet, so its " +
					"sign-in callback cannot be registered on " + strconv.Quote(dep.name)
			}
			continue
		}
		cr, gerr := s.thunder.FindByResource(ctx, ocname.ExternalResourceName(projectID, dep.name), openchoreo.DevEnvironmentName)
		if gerr != nil {
			if errors.Is(gerr, ErrThunderApplicationAPIMissing) {
				continue
			}
			return fmt.Errorf("deployment: thunder wait: find ThunderApplication %q: %w", dep.name, gerr)
		}
		if !thunderCRSatisfies(cr, callback) && hold == "" {
			hold = "waiting: dependency " + strconv.Quote(dep.name) +
				" has not registered this component's sign-in callback " + callback + " yet"
		}
	}
	if hold != "" {
		// Pending until the CR matches; forever-pending expires via deploy-budget
		// (TestDeployNeverReady_ExpiresIntoADeployFailure) — no workflow rewrite.
		st.Ready = false
		// Cleared with the verdict it described, for the reason applyEndpointWait
		// clears it: componentDeployFrom copied OpenChoreo's Ready-TRUE reason onto
		// st before this ran, and leaving it would caption a held component with
		// the reason it was up.
		st.Reason = hold
		slog.InfoContext(ctx, "deployment: holding at converging on the sign-in callback",
			"org", orgID, "project", projectID, "component", componentName, "reason", hold)
	}
	return nil
}

func thunderCRSatisfies(cr *ThunderApplicationView, callback string) bool {
	if cr == nil {
		return false
	}
	if !cr.Ready || cr.ObservedGeneration < cr.Generation {
		return false
	}
	want := canonicalWebURL(callback)
	for _, raw := range strings.Split(cr.RedirectURIs, ",") {
		if canonicalWebURL(raw) == want {
			return true
		}
	}
	return false
}

// canonicalWebURL strips default http(s) ports so OpenChoreo's
// scheme://host:443/callback matches the CR/browser form host/callback.
func canonicalWebURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return raw
	}
	port := u.Port()
	omit := (u.Scheme == "https" && (port == "" || port == "443")) ||
		(u.Scheme == "http" && (port == "" || port == "80"))
	if !omit {
		if port != "" {
			u.Host = net.JoinHostPort(u.Hostname(), port)
		}
		return u.String()
	}
	u.Host = u.Hostname()
	return u.String()
}

// componentExternalURL returns the first non-empty EndpointURL OC has
// resolved for the named component, or "" when none is materialised yet.
// Same source as runtimeconfig.componentExternalURL.
func (s *DeploymentService) componentExternalURL(ctx context.Context, orgID, projectID, componentName string) string {
	if s.components == nil {
		return ""
	}
	list, err := s.components.ListDeployments(ctx, orgID, projectID, k8sname.ToK8sName(componentName))
	if err != nil || list == nil {
		return ""
	}
	for _, d := range list.Items {
		if d.EndpointURL != "" {
			return d.EndpointURL
		}
	}
	return ""
}
