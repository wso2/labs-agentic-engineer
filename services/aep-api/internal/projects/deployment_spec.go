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
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The desired-state PROJECTION for one component's deployment: design facts in,
// the two OpenChoreo objects the platform owns out. Pure — no I/O, no clock, no
// cluster — so the whole matrix (protected / unprotected, browser / service
// caller, auto-RCA on / off, siblings present / absent) is a table test rather
// than a cluster round trip.
//
// It replaced a reconcile SERVICE. The difference is not the code, which is
// largely the same rules; it is when they run. A sync answered "what has
// drifted, and how do I repair it" AFTER OpenChoreo had already rendered and
// served the component. This answers "what should be true" BEFORE the binding
// is written at all, which is what makes the trait config and the release pin
// land in one object write — and therefore what removes the window where a
// protected API served unauthenticated because its jwtAuth config had not been
// written yet.

// DesiredDeployment is everything the platform wants true of one component,
// split by the object it lands on and therefore by WHEN it can be written.
//
// The split is forced by OpenChoreo, not chosen: a ComponentRelease freezes the
// Component's trait list at release time, so the SHAPE has to be on the
// Component CR before the build cuts the release, while the per-environment
// CONFIG can only be written once a release exists to bind. Both halves come
// out of this one function precisely because they must agree — a trait attached
// without its config does not degrade, it fails the whole ReleaseBinding
// render.
type DesiredDeployment struct {
	// Traits is the Component CR's `spec.traits` — asserted pre-build by
	// EnsureComponent.
	Traits []openchoreo.ComponentTrait
	// Binding is the ReleaseBinding — written post-build by the deploy stage.
	Binding openchoreo.ReleaseBindingDesired
}

// DeploymentInputs is every fact the projection needs, gathered by the caller.
// Passing them in rather than fetching them is what keeps this function pure;
// DeploymentService owns the gathering.
type DeploymentInputs struct {
	// Component is the design's own record of this component.
	Component spec.DesignComponent
	// ComponentName is the k8s-shaped name (what OpenChoreo is addressed by).
	ComponentName string
	Environment   string
	// ReleaseName is the release this deployment pins. Empty composes the
	// wiring without a pin — the shape a caller wants when it is converging an
	// already-deployed component rather than promoting a new release.
	ReleaseName string
	// Issuers pins JWT validation to an org's own IDP; empty trusts any
	// cluster-configured keymanager.
	Issuers []string
	// EnvVars are the user's component config (the DB is their canonical
	// record). Nil means "not managed by this write" — see
	// openchoreo.ReleaseBindingDesired.
	EnvVars []openchoreo.WorkflowEnvVarRef
	// Files are the literal files the runtime-config projection wants mounted
	// (env-config.js for a web app). Nil means "not managed by this write".
	Files []openchoreo.WorkflowFileVar
}

// DesiredDeploymentFor projects one component's design facts onto the two
// objects the platform owns.
//
// Trait REMOVAL needs no tombstone here, unlike the emitter this replaced. The
// binding's whole `traitEnvironmentConfigs` map is authoritative when non-nil,
// so an instance the projection stops emitting is simply absent from the next
// write — where the old read-merge-write path had to carry an explicit empty
// value to mean "delete this key", and had to order that write against the
// trait-shape write to avoid leaving a trait without its config.
func DesiredDeploymentFor(in DeploymentInputs) DesiredDeployment {
	apiEnabled := spec.ResolveAPISecurityEnabled(in.Component)

	// CORS: omit allowedOrigins so the api-configuration trait schema default
	// ["*"] applies. Do not set cors.enabled false — that would deny all
	// origins, including curl-from-the-gateway clients.
	traits, configs := DesiredAPIConfigurationTraitWithIssuers(
		in.ComponentName, in.Component.EndpointName(), apiEnabled, in.Issuers)

	// Appended to the SAME slice/map: `spec.traits` is replaced wholesale on
	// write, so emitting the alert rule separately would clobber the
	// api-configuration trait rather than join it.
	if spec.ResolveAutoRCAEnabled(in.Component) {
		rcaTraits, rcaConfigs := DesiredObservabilityAlertRuleTraits(in.ComponentName)
		traits = append(traits, rcaTraits...)
		if configs == nil {
			configs = map[string]map[string]interface{}{}
		}
		for inst, cfg := range rcaConfigs {
			configs[inst] = cfg
		}
	}

	return DesiredDeployment{
		Traits: traits,
		Binding: openchoreo.ReleaseBindingDesired{
			ComponentName: in.ComponentName,
			Environment:   in.Environment,
			ReleaseName:   in.ReleaseName,
			State:         openchoreo.ReleaseBindingStateActive,
			// Authoritative, and non-nil even when empty: a component with no
			// traits wants an EMPTY config map, not an untouched one, or a trait
			// turned off in the design would keep its config forever.
			TraitEnvironmentConfigs: liveTraitConfigs(configs),
			Env:                     in.EnvVars,
			Files:                   in.Files,
		},
	}
}

// liveTraitConfigs drops the instances whose parameters are empty and
// guarantees a non-nil map.
//
// The empty-value entries are how DesiredAPIConfigurationTraitWithIssuers
// signals "this instance is off" to the read-merge-write emitter that used to
// consume it. Under authoritative-replace semantics that signal is the absence
// of the key, so the entries are dropped here rather than at their source —
// which keeps that function's contract (and its tests) untouched.
func liveTraitConfigs(configs map[string]map[string]interface{}) map[string]map[string]interface{} {
	out := make(map[string]map[string]interface{}, len(configs))
	for inst, params := range configs {
		if len(params) == 0 {
			continue
		}
		out[inst] = params
	}
	return out
}
