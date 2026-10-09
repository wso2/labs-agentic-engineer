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
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// DeploymentService promotes a built component into an environment: cut the
// release, compose the whole desired binding, write it once, and report what
// the cluster says about it.
//
// It is the ONLY writer of a user component's ReleaseBinding. That is the point
// of the type, not an incidental property: three services used to patch
// disjoint fields of the same object on three different triggers, each soft
// no-opping when the binding did not exist yet and each relying on somebody
// else to retry. Under one writer the object is created complete, so there is
// no partial state to retry out of.
//
// Deploy is DRIVEN, never inferred. Components carry AutoDeploy=false, so
// nothing promotes a release except a call to Deploy — which is what lets the
// run supervisor place validation after the version is genuinely serving,
// rather than after a build merely asked for a deployment.
type DeploymentService struct {
	components openchoreo.ComponentClient
	store      *spec.ArtifactStore
	// idp resolves the org's JWT issuer pinning. Optional: nil composes the
	// trait with no issuer filter, which trusts any cluster-configured
	// keymanager.
	idp OrgIDPProfiles
	// envVars reads the user's component config — the canonical record for
	// `workloadOverrides.container.env`. Optional: nil leaves that field
	// unmanaged rather than writing an empty list over the user's values.
	envVars ComponentEnvVarReader
	// files computes the literal files a component needs mounted
	// (env-config.js). Optional, same unmanaged-vs-empty rule.
	files RuntimeFileProvider
	// governor registers each ai-agent with Agent Manager and leaves its model
	// credential where the composition below can read it. Optional: nil governs
	// nothing and deploys exactly as the platform did before Agent Manager.
	governor AgentGovernor
	// modelAccess grants an ai-agent the org's Anthropic key. Optional: nil
	// deploys agents without MODEL_*, which agent-building answers with a 503
	// from /healthz rather than a broken turn.
	modelAccess ModelAccessProvider

	// gatewayHostOverride pins host:port of the API gateway runtime for every
	// environment, overriding the per-(org, environment) derivation. Empty — the
	// normal case — derives it (see gateway_address.go).
	gatewayHostOverride string
	// ensurer re-asserts a Component's spec (its traits) before the deploy cuts
	// a release from it. Optional: nil cuts from whatever the Component
	// carries, which is the behaviour before this existed.
	ensurer ComponentEnsurer
	// autoRCADisabled turns the default auto-RCA alert rule off for this
	// deployment (no SRE handoff configured). Zero value = on.
	autoRCADisabled bool
	// catalog, resourceClient, and thunder are the thunder-callback wait
	// ports. Any nil (including a nil store) skips the wait so existing
	// OC-only DeploymentState tests stay green without new wiring.
	catalog        resourceMarkerCatalog
	resourceClient bindingEnvironmentPatcher
	thunder        ThunderApplicationReader
	// endpoint gates a Ready binding on its public URL actually answering. Nil
	// skips the gate, and the SAME gate is held by the status reader so the two
	// cannot answer differently — see endpoint_wait.go.
	endpoint *EndpointGate
	// environments reads the environment gateway's assertion contract. Optional:
	// nil composes every binding without one, which is the behaviour of an
	// environment whose gateway publishes no verification half.
	environments GatewayAssertionReader
	// writeTargets resolves the environment a project's deploys write into.
	// Required: Deploy, Converge and DeploymentState refuse to run without it,
	// because there is no environment they could safely assume instead.
	writeTargets writeTargetResolver
}

// writeTargetResolver names the environment a project writes into: the root of
// its own deployment pipeline. Declared consumer-side so this package depends
// on the one method it uses; openchoreo.WriteTargets satisfies it.
type writeTargetResolver interface {
	Resolve(ctx context.Context, org, project string) (string, error)
}

// GatewayAssertionReader is the narrow Environment-annotation read the
// deployment projection needs. Declared consumer-side so this feature depends on
// the one method it uses rather than on the whole EnvironmentClient, which
// openchoreo's satisfies structurally.
type GatewayAssertionReader interface {
	GetGatewayAssertion(ctx context.Context, orgID, environment string) (openchoreo.GatewayAssertion, error)
}

// envAuth is what a deploy pass resolves ONCE and every component in it shares:
// whose tokens the gateway trusts, and how a service proves a request came
// through that gateway. Both are properties of the (org, environment), so
// asking per component would issue the same reads N times for the same answer.
type envAuth struct {
	Issuers   []string
	Assertion openchoreo.GatewayAssertion
}

// ModelAccessProvider yields the MODEL_* env vars an ai-agent needs, having
// first ensured the SecretReference MODEL_API_KEY names exists. Model access is
// granted by component TYPE, not declared as a dependency (ADR-0016), so
// OpenChoreo never resolves it while rendering a release — the platform composes
// it into the binding write itself. An org with no connected key yields
// (nil, nil).
type ModelAccessProvider interface {
	ModelAccessEnvVars(ctx context.Context, orgID, component, environment string) ([]openchoreo.WorkflowEnvVarRef, error)
}

// ComponentEnvVarReader is the user's component config, consumer-side.
// *configService satisfies it.
type ComponentEnvVarReader interface {
	GetEnvVarsForDeploy(ctx context.Context, orgID, projectName, componentName string) (EnvVarSlice, error)
}

// RuntimeFileProvider computes the literal files a component's binding must
// carry. `ready` is false when the values cannot be computed yet — a SPA whose
// sibling backend has no resolved URL — and the caller must then leave the
// field unmanaged rather than write a half-populated file that the SPA would
// throw on at module load.
type RuntimeFileProvider interface {
	FilesForComponent(ctx context.Context, orgID, projectID, componentName string) (files []openchoreo.WorkflowFileVar, ready bool, err error)
}

// NewDeploymentService wires the deployer. The optional collaborators are set
// afterwards because they are built later at the composition root.
func NewDeploymentService(components openchoreo.ComponentClient, store *spec.ArtifactStore) *DeploymentService {
	return &DeploymentService{components: components, store: store}
}

// SetIDPService wires per-org JWT issuer pinning.
func (s *DeploymentService) SetIDPService(idp OrgIDPProfiles) {
	if s != nil {
		s.idp = idp
	}
}

// AgentGovernor registers one agent with Agent Manager before it is deployed.
//
// IT IS CALLED FROM Deploy, not from the delivery workflow, and that placement
// is the point: Deploy is the one path every deployment passes through —
// the workflow's promote, Converge's drift repair, and a config change's
// redeploy all land here. Governance hung off any one caller is governance the
// next caller silently skips, which is exactly how an earlier version of this
// left config-change redeploys ungoverned.
type AgentGovernor interface {
	GovernAgent(ctx context.Context, in delivery.GovernAgentInput) (delivery.GovernAgentOutcome, error)
}

// SetGovernor wires Agent Manager governance. Optional, like the others.
func (s *DeploymentService) SetGovernor(g AgentGovernor) {
	s.governor = g
}

// SetModelAccess wires an ai-agent's model access. Optional like the others,
// but its absence is invisible at deploy time and only shows up as a 500 on
// the agent's first turn — which is how it shipped once, unwired.
func (s *DeploymentService) SetModelAccess(m ModelAccessProvider) {
	if s != nil {
		s.modelAccess = m
	}
}

// SetWriteTargets wires the resolver that names each project's write target.
func (s *DeploymentService) SetWriteTargets(w writeTargetResolver) {
	if s != nil {
		s.writeTargets = w
	}
}

// writeTarget resolves the project's write target once for the operation.
// A configuration fault is permanent: no retry makes a cyclic or missing
// pipeline valid. It also carries delivery.ErrNoWriteTarget, so the run
// settles on the fault instead of filing deploy fix work. Anything else (a
// network error, an OC 5xx) propagates unchanged so Temporal retries it.
func (s *DeploymentService) writeTarget(ctx context.Context, orgID, projectID string) (string, error) {
	if s.writeTargets == nil {
		return "", fmt.Errorf("deployment: write targets not configured")
	}
	env, err := s.writeTargets.Resolve(ctx, orgID, projectID)
	var nwt *openchoreo.ErrNoWriteTarget
	if errors.As(err, &nwt) {
		return "", fmt.Errorf("%w: %w: %w", delivery.ErrDeployPermanent, delivery.ErrNoWriteTarget, err)
	}
	return env, err
}

// SetGatewayAssertions wires the read that tells a service how to verify the
// gateway's assertion. Optional: without it no component is handed a
// verification half, and each keeps trusting only what the gateway's own
// authentication already guaranteed.
func (s *DeploymentService) SetGatewayAssertions(r GatewayAssertionReader) {
	if s != nil {
		s.environments = r
	}
}

// SetConfigSources wires the two projections whose values ride the binding's
// workload overrides.
func (s *DeploymentService) SetConfigSources(envVars ComponentEnvVarReader, files RuntimeFileProvider) {
	if s != nil {
		s.envVars, s.files = envVars, files
	}
}

// SetAPIGatewayHostOverride pins the address a consumer reaches a protected
// sibling's managed API on, for every environment. Empty (the zero value) is the
// normal case: the address is then derived per (org, environment), because the
// platform runs one gateway per environment and no single literal addresses two
// of them. The composition root passes API_GATEWAY_HOST straight through.
func (s *DeploymentService) SetAPIGatewayHostOverride(host string) {
	if s != nil {
		s.gatewayHostOverride = host
	}
}

// ComponentEnsurer re-asserts a Component's spec from the design: the same
// write the build fan-out makes before a build (ComponentService.EnsureComponent).
type ComponentEnsurer interface {
	EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error
}

// SetComponentEnsurer wires the Component re-assert the deploy makes before it
// cuts a release (see deployOne).
func (s *DeploymentService) SetComponentEnsurer(c ComponentEnsurer) {
	if s != nil {
		s.ensurer = c
	}
}

// SetAutoRCAEnabled sets whether the default auto-RCA alert rule's per-
// environment config is written (on when the SRE handoff is configured). It must agree with
// ComponentService's setting, which decides whether the Component attaches the
// trait at all; the composition root passes both the same value.
func (s *DeploymentService) SetAutoRCAEnabled(enabled bool) {
	if s != nil {
		s.autoRCADisabled = !enabled
	}
}

// Deploy promotes each target at ITS OWN commit and reports what happened per
// component.
//
// The commit is per target rather than per call, and that is the difference the
// reconcile needed: one commit for a whole list is only right when the list is
// what a single merge built, and a pass that promotes each component at its own
// newest green build has a different commit for each of them (ADR-0026). An
// empty commit on a target is a CONVERGE — the wiring is re-asserted at
// whatever release is already pinned.
//
// It never returns early on one component's failure: a project's components are
// independent deployments, and stopping at the first would leave the rest of a
// version undeployed for a reason that has nothing to do with them. Failures
// ride the returned outcomes AND the joined error, so the supervisor can both
// see which component failed and know that the pass did not fully succeed.
func (s *DeploymentService) Deploy(ctx context.Context, orgID, projectID string, targets []delivery.DeployTarget) ([]delivery.ComponentDeploy, error) {
	if s == nil || s.components == nil || s.store == nil {
		return nil, fmt.Errorf("deployment: not configured")
	}
	env, err := s.writeTarget(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.deploy(ctx, orgID, projectID, env, targets)
}

// deploy is Deploy against an already resolved write target, so Converge,
// which resolves it to find the live bindings, does not resolve it twice.
func (s *DeploymentService) deploy(ctx context.Context, orgID, projectID, env string,
	targets []delivery.DeployTarget) ([]delivery.ComponentDeploy, error) {
	if s.store == nil {
		return nil, fmt.Errorf("deployment: not configured")
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if spec.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("deployment: read design: %w", err)
	}
	if design == nil {
		return nil, nil
	}

	// The issuers before anything else: a read that refuses the deploy must do
	// so before governance registers the wave's agents and before any component
	// is written, so a retried refusal repeats only the read.
	issuers, err := s.resolveIssuers(ctx, orgID, projectID, design)
	if err != nil {
		return nil, err
	}

	// Governance next, for the whole wave, and before anything is composed:
	// ai_agent_model_access.go reads the agent's stored credential while
	// building the ReleaseBinding, so a key that arrives after composition has
	// nowhere to go until the next version. A failure here fails the deploy —
	// an environment that carries an AI gateway binding has promised its agents
	// are governed, and half a governed wave is the state nobody can reason
	// about afterwards.
	if err := s.govern(ctx, orgID, projectID, env, design, targets); err != nil {
		return nil, err
	}

	// Resolved ONCE for the pass: both are (org, environment) facts, and asking
	// per component would issue the same reads N times for the same answer.
	auth := envAuth{
		Issuers:   issuers,
		Assertion: s.resolveGatewayAssertion(ctx, orgID, env, design),
	}

	out := make([]delivery.ComponentDeploy, 0, len(targets))
	var failures []error
	for _, t := range targets {
		outcome, derr := s.deployOne(ctx, orgID, projectID, t.Component, t.CommitSHA, env, design, auth)
		out = append(out, outcome)
		if derr != nil {
			failures = append(failures, fmt.Errorf("component %q: %w", t.Component, derr))
		}
	}
	return out, errors.Join(failures...)
}

// govern registers each target with Agent Manager.
//
// The governor decides what is and is not an agent — this service does not
// filter, because "which components are governed" is a governance question and
// splitting it across two packages is how the two drift.
func (s *DeploymentService) govern(ctx context.Context, orgID, projectID, env string, design *spec.DesignFile, targets []delivery.DeployTarget) error {
	if s.governor == nil || len(targets) == 0 {
		return nil
	}
	for _, t := range targets {
		in := delivery.GovernAgentInput{
			OrgID:       orgID,
			ProjectID:   projectID,
			Component:   t.Component,
			Environment: env,
		}
		declareGuardrails(&in, design, t.Component)
		outcome, err := s.governor.GovernAgent(ctx, in)
		if err != nil {
			slog.ErrorContext(ctx, "deployment: agent governance failed; the deploy is refused",
				"org", orgID, "project", projectID, "component", t.Component, "error", err)
			return fmt.Errorf("govern %q: %w", t.Component, err)
		}
		if outcome.Skipped {
			slog.InfoContext(ctx, "deployment: component is not governed by Agent Manager",
				"org", orgID, "project", projectID, "component", t.Component, "reason", outcome.Reason)
		}
	}
	return nil
}

// declareGuardrails fills in what the named ai-agent's spec declares about
// guardrails: the guardrails, its instructions, and whether the spec could be
// read at all. Any other component, or no design, declares nothing.
func declareGuardrails(in *delivery.GovernAgentInput, design *spec.DesignFile, component string) {
	if design == nil {
		return
	}
	for _, c := range design.Components {
		if c.Name != component || c.ComponentType != spec.ComponentTypeAIAgent {
			continue
		}
		decl := spec.AgentGuardrails(c.AgentAFM)
		if !decl.Readable {
			in.GuardrailsUnreadable = true
			return
		}
		in.AgentInstructions = decl.Instructions
		in.AgentUsesTools, in.AgentTakesFiles = decl.UsesTools, decl.TakesFiles
		in.AgentToolText = spec.AgentToolText(c.AgentAFM, design.Components)
		for _, g := range decl.Guardrails {
			in.Guardrails = append(in.Guardrails, delivery.GuardrailDeclaration{Policy: g.Policy, Params: g.Params, Why: g.Why})
		}
		return
	}
}

// PlanDeploymentWaves plans one reconcile pass over the version's state: what
// to promote, in what order, what to wait for and what to hold
// (see wiring_graph.go for what the plan means).
//
// It lives on the service because the plan and the writes it orders are read
// off the same artefact by the same reader — not the same READ: the plan reads
// the design once and each Deploy reads it again, so a design edit landing
// mid-stage is seen by the writes and not by the plan. That window is
// deliberately left open. Closing it would mean pinning a design revision
// through the whole stage, and a component added to the design between the plan
// and the promote is behind with no build, which the next pass classifies as
// unbuilt and leaves alone.
//
// A project with no design yet gets no ordering — every behind component in one
// wave — which is the same answer Deploy gives it: the design is the ordering
// input, not the deploy's permission.
func (s *DeploymentService) PlanDeploymentWaves(ctx context.Context, orgID, projectID string,
	state delivery.VersionState) (delivery.DeployPlan, error) {
	if s == nil || len(state.Components) == 0 {
		return delivery.DeployPlan{}, nil
	}
	if s.store == nil {
		return delivery.DeployPlan{}, fmt.Errorf("deployment: not configured")
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil && !spec.IsNotFound(err) {
		return delivery.DeployPlan{}, fmt.Errorf("deployment: read design: %w", err)
	}
	return deploymentWaves(design, state)
}

// Converge re-asserts the wiring of components that are already deployed,
// WITHOUT promoting a release.
//
// It is what a config change triggers: the user edited env vars, or a design
// edit toggled `exposesAPI.auth`, and the binding has to catch up. Reusing the
// deploy path rather than patching one field is the whole point — there is one
// function that knows what a binding should say, so a converge cannot drift
// from what the next deploy would write.
//
// A component with no binding yet is a no-op: ApplyReleaseBinding would create
// one with no release pinned, which OpenChoreo cannot render. The next deploy
// creates it properly.
func (s *DeploymentService) Converge(ctx context.Context, orgID, projectID string, components []string) error {
	if s == nil || s.components == nil {
		return nil
	}
	env, err := s.writeTarget(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	live := make([]string, 0, len(components))
	for _, name := range components {
		summary, err := s.components.GetReleaseBindingStatus(ctx, orgID, projectID, name, env)
		if err != nil {
			return fmt.Errorf("deployment: read binding for %q: %w", name, err)
		}
		if summary != nil {
			live = append(live, name)
		}
	}
	if len(live) == 0 {
		return nil
	}
	_, err = s.deploy(ctx, orgID, projectID, env, delivery.ConvergeTargets(live))
	return err
}

// deployOne cuts the release and writes the binding for a single component.
func (s *DeploymentService) deployOne(ctx context.Context, orgID, projectID, componentName, commitSHA, env string,
	design *spec.DesignFile, auth envAuth) (delivery.ComponentDeploy, error) {
	outcome := delivery.ComponentDeploy{Component: componentName, Environment: env}

	comp := findDesignComponent(design, componentName)
	if comp == nil {
		// The design lost the component between the build and here. PERMANENT:
		// no amount of retrying makes a deleted component reappear, and under
		// Temporal's default policy an unmarked error here would retry until the
		// run was cancelled by hand.
		return outcome, fmt.Errorf("%w: no such component %q in design", delivery.ErrDeployPermanent, componentName)
	}

	// Cut the release from whatever Workload the build posted. The name is
	// derived from the commit, so a re-run of this pass rebinds the SAME
	// release instead of stacking a new one per attempt — which is what makes
	// the whole deploy stage idempotent under Temporal's retries.
	//
	// An empty commit is the CONVERGE case: re-assert the wiring of an already
	// deployed component without promoting anything. A user editing env vars
	// must not be able to move which release is serving.
	var releaseName string
	if commitSHA != "" {
		// A release FREEZES the Component's traits, so re-assert the Component's
		// spec from the design first: the build's fan-out wrote those traits,
		// possibly long before. This reaches a release only when this commit's
		// release has not been cut yet — release names are per commit, and
		// EnsureRelease keeps an existing one — so a trait change since (a design
		// edit, the SRE handoff turned on or off) lands at the component's next
		// commit, not by redeploying the same one. A failed re-assert fails this
		// component's deploy (retryable) rather than releasing stale traits.
		if s.ensurer != nil {
			if err := s.ensurer.EnsureComponent(ctx, orgID, projectID, componentName); err != nil {
				return outcome, fmt.Errorf("re-assert component before release: %w", err)
			}
		}
		releaseName = delivery.ReleaseNameFor(projectID, componentName, commitSHA)
		if _, err := s.components.EnsureRelease(ctx, orgID, projectID, componentName, releaseName); err != nil {
			return outcome, fmt.Errorf("cut release: %w", permanentIfMissing(err))
		}
		outcome.Release = releaseName
	}

	desired := DesiredDeploymentFor(DeploymentInputs{
		Component:     *comp,
		ComponentName: componentName,
		Environment:   env,
		ReleaseName:   releaseName,
		Issuers:       auth.Issuers,
		// How a service proves the request reached it through the gateway. The
		// zero value — an environment gateway with no published key — leaves the
		// assertion off rather than handing over half a contract.
		GatewayAssertion: auth.Assertion,
		// The project's resource-server identifier: what a token minted for
		// THIS project carries as `aud`, and therefore what the gateway checks
		// to reject one minted for any other.
		Audience: ProjectAudience(orgID, projectID),
		EnvVars:  s.envVarsWithModelAccess(ctx, orgID, projectID, componentName, env, comp.ComponentType),
		Files:    s.filesFor(ctx, orgID, projectID, componentName),
		// The org IS the OC namespace components are created in, and that
		// namespace is a segment of every managed API's gateway context path.
		ComponentNamespace:  orgID,
		GatewayHostOverride: s.gatewayHostOverride,
		ProtectedSiblings:   ProtectedSiblingsOf(design, *comp),
		AutoRCADisabled:     s.autoRCADisabled,
	})
	if desired.APIOperationsProblem != "" {
		slog.WarnContext(ctx, "deployment: OpenAPI contract not projected onto gateway operations; "+
			"the API keeps the trait's /* default (every operation needs a token, none needs a scope)",
			"org", orgID, "project", projectID, "component", componentName,
			"problem", desired.APIOperationsProblem)
	}
	for _, note := range desired.APIOperationsNotes {
		slog.InfoContext(ctx, "deployment: operation left out of the gateway table",
			"org", orgID, "project", projectID, "component", componentName, "note", note)
	}
	if err := s.components.ApplyReleaseBinding(ctx, orgID, projectID, desired.Binding); err != nil {
		return outcome, fmt.Errorf("apply release binding: %w", permanentIfMissing(err))
	}
	slog.InfoContext(ctx, "deployment: release pinned",
		"org", orgID, "project", projectID, "component", componentName, "release", releaseName)
	return outcome, nil
}

// envVarsWithModelAccess is the user's component config plus, for an ai-agent,
// its model access. Appended rather than merged: the two never collide because
// MODEL_* is platform-owned and a user's config keys are their own.
//
// A model-access failure is logged and the deploy continues with the user's
// vars alone. Failing the deploy would be worse: the agent would not exist at
// all, where an agent without MODEL_* comes up and reports 503 from /healthz —
// a state an operator can see and fix by connecting a key.
func (s *DeploymentService) envVarsWithModelAccess(ctx context.Context, orgID, projectID, componentName, env, componentType string) []openchoreo.WorkflowEnvVarRef {
	envVars := s.envVarsFor(ctx, orgID, projectID, componentName)
	if componentType != spec.ComponentTypeAIAgent || s.modelAccess == nil {
		return envVars
	}
	modelVars, err := s.modelAccess.ModelAccessEnvVars(ctx, orgID, componentName, env)
	if err != nil {
		slog.WarnContext(ctx, "deployment: model access unavailable; ai-agent deploys without MODEL_* and will report 503 from /healthz",
			"org", orgID, "project", projectID, "component", componentName, "error", err)
		return envVars
	}
	return append(envVars, modelVars...)
}

// DeploymentState reads back what the cluster says about each component's
// binding — the deploy stage's readiness poll.
//
// A binding that does not exist yet reads as pending, not as an error: between
// the write and OpenChoreo admitting the object there is a window the poll has
// to be able to sit in.
//
// After folding OpenChoreo Ready, a web-application whose platform-resource
// CRT carries ConsumerURLEnvConfig is not Ready until the ThunderApplication
// CR has the SPA callback (see applyThunderWait). Registering those callbacks
// is a PROJECT-wide write that happens once per read, ahead of the loop, because
// web apps sharing one dependency share one callback field. Nil wait ports keep
// today's OC-only verdict.
//
// Then a component that advertises an external URL is not Ready until that URL
// ANSWERS (see applyEndpointWait). OpenChoreo reports the binding Ready when the
// control plane is done, which on a cloud plane is minutes before a first-ever
// hostname has a certificate — and `serving` is read by the validation sweep,
// the console and a person clicking the link as a claim about the edge.
func (s *DeploymentService) DeploymentState(ctx context.Context, orgID, projectID string, components []string) ([]delivery.ComponentDeploy, error) {
	if s == nil || s.components == nil {
		return nil, fmt.Errorf("deployment: not configured")
	}
	env, err := s.writeTarget(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	// Bindings first, because the consumer-URL registration below needs to know
	// which components OpenChoreo is taking down before it decides what the
	// project's callback set is.
	summaries := make([]*openchoreo.ReleaseBindingSummary, len(components))
	withdrawing := make(map[string]bool, len(components))
	for i, name := range components {
		summary, err := s.components.GetReleaseBindingStatus(ctx, orgID, projectID, name, env)
		if err != nil {
			return nil, fmt.Errorf("deployment: read binding for %q: %w", name, err)
		}
		summaries[i] = summary
		if summary != nil && summary.Undeploy {
			withdrawing[name] = true
		}
	}

	// The consumer-URL wiring is resolved and written ONCE for the project,
	// before any component's verdict is folded. A dependency several web apps
	// share holds one callback field, so a per-component write would have each
	// component replace the last (see thunderPass).
	pass, err := s.newThunderPass(ctx, orgID, projectID, env, withdrawing)
	if err != nil {
		return nil, err
	}
	if err := s.registerConsumerCallbacks(ctx, orgID, projectID, pass); err != nil {
		return nil, err
	}

	out := make([]delivery.ComponentDeploy, 0, len(components))
	for i, name := range components {
		st := componentDeployFrom(name, env, summaries[i])
		if err := s.applyThunderWait(ctx, orgID, projectID, name, pass, summaries[i], &st); err != nil {
			return nil, err
		}
		s.applyEndpointWait(ctx, orgID, projectID, name, summaries[i], &st)
		out = append(out, st)
	}
	return out, nil
}

// componentDeployFrom folds one binding's Ready condition into the verdict the
// run loop reasons about. The three-way answer is deliberate: "not ready yet"
// and "will never be ready" are different facts, and collapsing them would make
// the supervisor either give up on a slow rollout or wait forever on a broken
// one.
func componentDeployFrom(name, env string, summary *openchoreo.ReleaseBindingSummary) delivery.ComponentDeploy {
	out := delivery.ComponentDeploy{Component: name, Environment: env}
	if summary == nil {
		return out // no binding admitted yet — pending
	}
	out.Reason = summary.ReadyReason
	// The PIN, carried on every read and not only on a write. It is what lets
	// the supervisor tell a component serving its newest build from one serving
	// an older release perfectly happily — Ready says only that whatever is
	// pinned came up.
	out.Release = summary.ReleaseName
	switch {
	case summary.Undeploy:
		// Deliberately not deployed. Ready is meaningless here, and treating it
		// as pending would hang the poll on a component nobody is deploying.
		out.Ready, out.Undeploy = true, true
	case strings.EqualFold(summary.ReadyStatus, "True"):
		out.Ready = true
	case strings.EqualFold(summary.ReadyStatus, "False") && terminalDeployReason(summary.ReadyReason):
		out.Failed = true
	}
	// Everything else is PENDING, and `Ready=False` is mostly everything else.
	//
	// A binding reports Ready=False from the moment it is created, while it
	// renders and rolls out — that is its INITIAL state, not a verdict. Reading
	// it as failure declared two perfectly healthy components dead two seconds
	// after they were pinned, and filed a fix issue for each. Which is also what
	// this stage's deadline is for: a rollout that will land and one that never
	// will are indistinguishable from out here, so only running out of time may
	// turn waiting into a failure.
	return out
}

// terminalDeployReason reports whether OpenChoreo's Ready condition names a
// failure that waiting cannot fix.
//
// Deliberately a SHORT allow-list rather than "anything that isn't Ready".
// Being wrong in this direction costs the deadline's patience on a genuinely
// broken deployment; being wrong the other way condemns a healthy one, which is
// the bug this replaced. A reason not listed here is treated as "still working
// on it" and bounded by deployReadyTimeout.
func terminalDeployReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "renderingfailed", "renderfailed", "invalidrelease", "releasenotfound":
		return true
	}
	return false
}

// envVarsFor reads the user's component config. A read failure leaves the field
// UNMANAGED rather than empty: writing an empty list would delete env vars the
// user set, and a transient database error must not be able to do that.
func (s *DeploymentService) envVarsFor(ctx context.Context, orgID, projectID, componentName string) []openchoreo.WorkflowEnvVarRef {
	if s.envVars == nil {
		return nil
	}
	vars, err := s.envVars.GetEnvVarsForDeploy(ctx, orgID, projectID, componentName)
	if err != nil {
		slog.WarnContext(ctx, "deployment: component env config unreadable; leaving the binding's env untouched",
			"project", projectID, "component", componentName, "error", err)
		return nil
	}
	out := make([]openchoreo.WorkflowEnvVarRef, 0, len(vars))
	for _, ev := range vars {
		out = append(out, openchoreo.WorkflowEnvVarRef{Key: ev.Key, Value: ev.Value})
	}
	return out
}

// filesFor computes the runtime-config files. Same unmanaged-on-doubt rule, and
// here it is load-bearing: a SPA whose dependency URLs have not resolved yet
// must keep the env-config.js it already has rather than have it blanked.
func (s *DeploymentService) filesFor(ctx context.Context, orgID, projectID, componentName string) []openchoreo.WorkflowFileVar {
	if s.files == nil {
		return nil
	}
	files, ready, err := s.files.FilesForComponent(ctx, orgID, projectID, componentName)
	if err != nil {
		slog.WarnContext(ctx, "deployment: runtime config unreadable; leaving the binding's files untouched",
			"project", projectID, "component", componentName, "error", err)
		return nil
	}
	if !ready {
		return nil
	}
	return files
}

// DeleteComponentCascade deletes the OC Component CR. OC's own finalizer chain
// (Component → ComponentRelease → ReleaseBinding → RenderedRelease) GCs the
// dataplane objects, including the trait-emitted Backend and RestApi, which the
// RenderedRelease finalizer tracks even though they carry no owner reference.
func (s *DeploymentService) DeleteComponentCascade(ctx context.Context, orgID, projectID, componentName string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" || componentName == "" {
		return fmt.Errorf("deployment: empty orgID/projectID/componentName")
	}
	if err := s.components.DeleteComponent(ctx, orgID, projectID, componentName); err != nil {
		return fmt.Errorf("deployment: delete component: %w", err)
	}
	slog.InfoContext(ctx, "deployment: component deleted; OC RenderedRelease finalizer GCs trait resources",
		"orgID", orgID, "projectID", projectID, "componentName", componentName)
	return nil
}

// resolveIssuers returns the issuer list a protected component's JWT
// validation is pinned to: a BYO org's profile issuer, else none (the
// platform IDP). It only reads the profile: the publisher app is the
// gitpat submit's to create, and a deploy never creates or heals it.
//
// Fails closed: an empty list leaves the api-configuration trait accepting
// tokens from every keymanager registered on the cluster, which for a BYO org
// means any other tenant's IdP. So a failed read refuses the deploy rather
// than composing an unpinned trait. The error is a plain one, not
// ErrDeployPermanent: a read failure is transient, and the promote activity
// retries until the profile reads. A saved BYO profile with no issuer (PATCH
// /config refuses one now; older rows may hold it) is refused the same way:
// the retry picks the issuer up once the admin saves one, where a permanent
// error would fail the run with no way back. No profile at all is the
// platform-IdP org, which has nothing to pin.
func (s *DeploymentService) resolveIssuers(ctx context.Context, orgID, projectID string, design *spec.DesignFile) ([]string, error) {
	if s.idp == nil || !designHasProtectedAPI(design) {
		return nil, nil
	}
	profile, err := s.idp.GetProfile(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "deployment: org IdP profile unreadable; the deploy is refused",
			"orgID", orgID, "projectID", projectID)
		return nil, fmt.Errorf("deployment: read org IdP profile: %w", err)
	}
	if profile == nil || profile.Kind == "" || profile.Kind == "platform" {
		return nil, nil
	}
	if strings.TrimSpace(profile.Issuer) == "" {
		slog.ErrorContext(ctx, "deployment: org IdP profile has no issuer; the deploy is refused",
			"orgID", orgID, "projectID", projectID, "kind", profile.Kind)
		return nil, fmt.Errorf("deployment: the org's %s IdP profile has no issuer to pin protected APIs to",
			profile.Kind)
	}
	return []string{profile.Issuer}, nil
}

// resolveGatewayAssertion reads the environment gateway's verification half.
//
// Best-effort, unlike resolveIssuers: an environment whose gateway publishes
// no key is the NORMAL state of every environment provisioned before
// assertions existed, and refusing to deploy into one would make the feature a
// breaking change. A failure here logs
// and composes a binding with no verification half, which is exactly what such
// an environment gets anyway.
func (s *DeploymentService) resolveGatewayAssertion(ctx context.Context, orgID, env string, design *spec.DesignFile) openchoreo.GatewayAssertion {
	if s.environments == nil || !designHasProtectedAPI(design) {
		return openchoreo.GatewayAssertion{}
	}
	assertion, err := s.environments.GetGatewayAssertion(ctx, orgID, env)
	if err != nil {
		slog.WarnContext(ctx, "deployment: gateway assertion unresolved; deploying without a verification half",
			"orgID", orgID, "environment", env, "error", err)
		return openchoreo.GatewayAssertion{}
	}
	if !assertion.Configured() {
		slog.InfoContext(ctx, "deployment: environment gateway publishes no assertion key; "+
			"protected services get no verification half",
			"orgID", orgID, "environment", env)
	}
	return assertion
}

// designHasProtectedAPI reports whether any component would pin an issuer, so
// an org with no protected API never pays for the publisher provisioning.
func designHasProtectedAPI(design *spec.DesignFile) bool {
	for _, c := range design.Components {
		if spec.ResolveAPISecurityEnabled(c) {
			return true
		}
	}
	return false
}

// permanentIfMissing marks an OpenChoreo 404 as permanent. A component the
// cluster does not have cannot be deployed by trying again — it has to be
// re-provisioned, which is the fan-out's job on the next cycle, not this
// activity's to wait for.
//
// Deliberately narrow: every other OpenChoreo failure (409, 500, a dropped
// connection) IS worth repeating, and stays on the unbounded retry that is right
// for it.
func permanentIfMissing(err error) error {
	if errors.Is(err, openchoreo.ErrNotFound) || errors.Is(err, ErrComponentNotFound) {
		return fmt.Errorf("%w: %w", delivery.ErrDeployPermanent, err)
	}
	return err
}

// findDesignComponent resolves a k8s-shaped component name back to its design
// record.
func findDesignComponent(design *spec.DesignFile, componentName string) *spec.DesignComponent {
	for i := range design.Components {
		if k8sname.ToK8sName(design.Components[i].Name) == componentName {
			return &design.Components[i]
		}
	}
	return nil
}
