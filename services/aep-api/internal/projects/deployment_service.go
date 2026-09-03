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
	idp OrgPublisher
	// envVars reads the user's component config — the canonical record for
	// `workloadOverrides.container.env`. Optional: nil leaves that field
	// unmanaged rather than writing an empty list over the user's values.
	envVars ComponentEnvVarReader
	// files computes the literal files a component needs mounted
	// (env-config.js). Optional, same unmanaged-vs-empty rule.
	files RuntimeFileProvider
	// gatewayHost is host:port of the API gateway runtime, published to a
	// consumer of a protected sibling as `<DEP>_GATEWAY_URL`. Empty leaves every
	// consumer on the direct-Service lane (see gateway_address.go).
	gatewayHost string
	// catalog, resourceClient, and thunder are the thunder-callback wait
	// ports. Any nil (including a nil store) skips the wait so existing
	// OC-only DeploymentState tests stay green without new wiring.
	catalog        resourceMarkerCatalog
	resourceClient bindingEnvironmentPatcher
	thunder        ThunderApplicationReader
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
func (s *DeploymentService) SetIDPService(idp OrgPublisher) {
	if s != nil {
		s.idp = idp
	}
}

// SetConfigSources wires the two projections whose values ride the binding's
// workload overrides.
func (s *DeploymentService) SetConfigSources(envVars ComponentEnvVarReader, files RuntimeFileProvider) {
	if s != nil {
		s.envVars, s.files = envVars, files
	}
}

// SetAPIGatewayHost wires the address a consumer reaches a protected sibling's
// managed API on. Empty (the zero value) publishes no gateway address at all,
// which leaves consumers on the unauthenticated direct-Service lane — so the
// composition root passes projects.DefaultAPIGatewayHost unless the deployment
// overrides it.
func (s *DeploymentService) SetAPIGatewayHost(host string) {
	if s != nil {
		s.gatewayHost = host
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

	// Resolved ONCE for the pass: a project-wide fact, and asking per
	// component would issue the same reads N times for the same answer.
	issuers := s.resolveIssuers(ctx, orgID, design)

	out := make([]delivery.ComponentDeploy, 0, len(targets))
	var failures []error
	for _, t := range targets {
		outcome, derr := s.deployOne(ctx, orgID, projectID, t.Component, t.CommitSHA, design, issuers)
		out = append(out, outcome)
		if derr != nil {
			failures = append(failures, fmt.Errorf("component %q: %w", t.Component, derr))
		}
	}
	return out, errors.Join(failures...)
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
	live := make([]string, 0, len(components))
	for _, name := range components {
		summary, err := s.components.GetReleaseBindingStatus(ctx, orgID, projectID, name, openchoreo.DevEnvironmentName)
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
	_, err := s.Deploy(ctx, orgID, projectID, delivery.ConvergeTargets(live))
	return err
}

// deployOne cuts the release and writes the binding for a single component.
func (s *DeploymentService) deployOne(ctx context.Context, orgID, projectID, componentName, commitSHA string,
	design *spec.DesignFile, issuers []string) (delivery.ComponentDeploy, error) {
	outcome := delivery.ComponentDeploy{Component: componentName, Environment: openchoreo.DevEnvironmentName}

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
		releaseName = delivery.ReleaseNameFor(projectID, componentName, commitSHA)
		if _, err := s.components.EnsureRelease(ctx, orgID, projectID, componentName, releaseName); err != nil {
			return outcome, fmt.Errorf("cut release: %w", permanentIfMissing(err))
		}
		outcome.Release = releaseName
	}

	desired := DesiredDeploymentFor(DeploymentInputs{
		Component:     *comp,
		ComponentName: componentName,
		Environment:   openchoreo.DevEnvironmentName,
		ReleaseName:   releaseName,
		Issuers:       issuers,
		EnvVars:       s.envVarsFor(ctx, orgID, projectID, componentName),
		Files:         s.filesFor(ctx, orgID, projectID, componentName),
		// The org IS the OC namespace components are created in, and that
		// namespace is a segment of every managed API's gateway context path.
		ComponentNamespace: orgID,
		GatewayHost:        s.gatewayHost,
		ProtectedSiblings:  ProtectedSiblingsOf(design, *comp),
	})
	if err := s.components.ApplyReleaseBinding(ctx, orgID, projectID, desired.Binding); err != nil {
		return outcome, fmt.Errorf("apply release binding: %w", permanentIfMissing(err))
	}
	slog.InfoContext(ctx, "deployment: release pinned",
		"org", orgID, "project", projectID, "component", componentName, "release", releaseName)
	return outcome, nil
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
// CR has the SPA callback (see applyThunderWait). Nil wait ports keep today's
// OC-only verdict.
func (s *DeploymentService) DeploymentState(ctx context.Context, orgID, projectID string, components []string) ([]delivery.ComponentDeploy, error) {
	if s == nil || s.components == nil {
		return nil, fmt.Errorf("deployment: not configured")
	}
	out := make([]delivery.ComponentDeploy, 0, len(components))
	for _, name := range components {
		summary, err := s.components.GetReleaseBindingStatus(ctx, orgID, projectID, name, openchoreo.DevEnvironmentName)
		if err != nil {
			return nil, fmt.Errorf("deployment: read binding for %q: %w", name, err)
		}
		st := componentDeployFrom(name, summary)
		if err := s.applyThunderWait(ctx, orgID, projectID, name, summary, &st); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// componentDeployFrom folds one binding's Ready condition into the verdict the
// run loop reasons about. The three-way answer is deliberate: "not ready yet"
// and "will never be ready" are different facts, and collapsing them would make
// the supervisor either give up on a slow rollout or wait forever on a broken
// one.
func componentDeployFrom(name string, summary *openchoreo.ReleaseBindingSummary) delivery.ComponentDeploy {
	out := delivery.ComponentDeploy{Component: name, Environment: openchoreo.DevEnvironmentName}
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

// resolveIssuers ensures the org's publisher app exists and returns the issuer
// list a protected component's JWT validation is pinned to.
//
// Best-effort by contract: the API stays reachable without a publisher
// identity, so a failure here logs and composes an unpinned trait rather than
// failing the deployment of a whole version.
func (s *DeploymentService) resolveIssuers(ctx context.Context, orgID string, design *spec.DesignFile) []string {
	if s.idp == nil || !designHasProtectedAPI(design) {
		return nil
	}
	if _, _, _, err := s.idp.EnsureOrgPublisher(ctx, orgID, "deployment"); err != nil {
		slog.WarnContext(ctx, "deployment: EnsureOrgPublisher failed; continuing", "orgID", orgID, "error", err)
	}
	profile, err := s.idp.GetProfile(ctx, orgID)
	if err != nil || profile == nil {
		return nil
	}
	if profile.Kind != "" && profile.Kind != "platform" && profile.Issuer != "" {
		return []string{profile.Issuer}
	}
	return nil
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
