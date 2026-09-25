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
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// defaultEnv is the single environment provisioning pins in v1 — the watcher and
// the declarative-wiring comment both read the `development` binding (upstream
// parity: the two naming schemes are deliberately identical).
func defaultEnv() string { return openchoreo.DevEnvironmentName }

// Service coordinates dependency provisioning on the `provision` gate funnel: it
// mints gate issues, collects external values, provisions platform resources,
// tracks cross-project access requests, and drives each provision Execution
// through the store while closing the gate issue with a no-secrets reference.
type Service struct {
	issues            IssueClient
	execs             ExecutionStore
	design            DesignReader
	repos             RepoLocator
	rtCatalog         ExternalRTCatalog
	extProv           ExternalProvisioner
	platProv          PlatformProvisioner
	bindings          BindingReader
	workloads         WorkloadDepSource
	projects          ProjectLister
	access            AccessStore
	providers         ProviderResolver
	catalogValuePlane CatalogValuePlane
	environments      EnvironmentLister
	// pipelines resolves the org's own deployment pipeline (the "default" /
	// sole-pipeline convention — see PipelineLister) so ListOrgEnvironments can
	// order and filter by promotion order. Nil is a documented degrade: every
	// environment is served in OpenChoreo's own list order with no PromotesTo,
	// never a guessed chain.
	pipelines       PipelineLister
	orgSecrets      OrgSecretWriter
	orgResourceDocs OrgResourceDocs
	promoter        ProjectResourcePromoter
	// agents resolves the build-time Agent Manager gate (agent_gate.go). Nil is
	// a documented no-op.
	agents AgentRegistrar
	// orgPublish commits the exposesAPI.orgPublished durability marker on a
	// provider component when its access request is granted. Wired via a setter
	// (SetOrgPublishMarker) at the composition root — it points BACK at the
	// design feature, so a setter breaks the design↔provisioning wiring cycle.
	// Nil is a documented no-op (the live-catalog gate still resolves).
	orgPublish OrgPublishMarker
	// providerBuild kicks a not-yet-published org-service provider's build so it
	// deploys (and publishes org-wide) — the automated half of the cross-project
	// visibility flow (issue #164). Wired via SetProviderBuildTrigger at the
	// composition root (Task 5) so provisioning never imports build/devflow. Nil
	// is a documented best-effort no-op (logged).
	providerBuild ProviderBuildTrigger
	// valuesSaved tells the run supervisor that external values landed, so a run
	// parked on the deploy gate re-derives readiness instead of waiting out its
	// poll interval. Wired via SetValuesSavedNotifier at the composition root —
	// it points at delivery/run, so a setter keeps provisioning from importing
	// the delivery slice (ADR-0023). Nil is a documented no-op.
	valuesSaved ValuesSavedNotifier

	// roles is the build-time roles ensure. Optional: nil (or a disabled one)
	// skips the roles gate, which is what a stack with no identity provider
	// wants — the alternative is failing every build for a feature it cannot
	// use.
	roles RolesEnsurer

	// markers is the CRT marker catalog the end-user-auth overlay keys on.
	// Nil skips overlay.
	markers ResourceMarkerCatalog
	// projectNames resolves the project's display name for the overlay. Nil
	// falls back to the project id.
	projectNames ProjectNamer
	// securityJSON reads security.json at HEAD (empty tag) or a spec tag.
	// Nil skips overlay.
	securityJSON SecurityJSONReader
	// tryItCallbackURL is the platform tester's OAuth callback, published in
	// the roles gate beside the logins so a reader knows which redirect_uri to
	// ask for. Empty omits the line.
	tryItCallbackURL string
}

// OrgPublishMarker persists a provider component's deliberate publish decision.
// *design.designService satisfies it.
type OrgPublishMarker interface {
	MarkOrgPublished(ctx context.Context, orgID, projectID, component string) error
}

// SetOrgPublishMarker wires the design feature's orgPublished-marker commit so
// the grant cascade records the publish on the provider design. Nil no-op.
func (s *Service) SetOrgPublishMarker(m OrgPublishMarker) { s.orgPublish = m }

// SetProviderBuildTrigger wires the provider-build kick used by the automated
// org-service visibility flow. Nil is a documented best-effort no-op (logged).
func (s *Service) SetProviderBuildTrigger(t ProviderBuildTrigger) { s.providerBuild = t }

// SetValuesSavedNotifier wires the wake-up a run parked on the deploy gate
// listens for. Nil is a documented no-op — the run's wait-poll still re-derives.
func (s *Service) SetValuesSavedNotifier(n ValuesSavedNotifier) { s.valuesSaved = n }

// SetAgentRegistrar wires the build-time Agent Manager gate. Nil skips it, which
// is the whole pre-Agent-Manager deployment.
func (s *Service) SetAgentRegistrar(r AgentRegistrar) { s.agents = r }

// Deps is the provisioning service's collaborator set. projects / access /
// providers may be nil (a nil projects skips the cross-project consumer scan;
// nil access / providers disable the access-request surface).
type Deps struct {
	// TryItCallbackURL is config.TryItCallbackURL — see Service.tryItCallbackURL.
	TryItCallbackURL  string
	Issues            IssueClient
	Execs             ExecutionStore
	Design            DesignReader
	Repos             RepoLocator
	RTCatalog         ExternalRTCatalog
	ExtProv           ExternalProvisioner
	PlatProv          PlatformProvisioner
	Bindings          BindingReader
	Workloads         WorkloadDepSource
	Projects          ProjectLister
	Access            AccessStore
	Providers         ProviderResolver
	CatalogValuePlane CatalogValuePlane
	Environments      EnvironmentLister
	// Pipeline resolves the org's own deployment pipeline for
	// ListOrgEnvironments' promotion ordering. Nil degrades to unordered
	// list-order service with no PromotesTo (see PipelineLister).
	Pipeline        PipelineLister
	OrgSecrets      OrgSecretWriter
	OrgResourceDocs OrgResourceDocs
	// Promoter reads and rewrites a project's own resource for Promote.
	// Nil disables Promote.
	Promoter ProjectResourcePromoter
	// Roles is the build-time roles ensure. Nil skips the roles gate.
	Roles RolesEnsurer
	// Markers is the CRT marker catalog the end-user-auth overlay keys on.
	// Nil skips overlay.
	Markers ResourceMarkerCatalog
	// SecurityJSON reads security.json at HEAD (empty tag) or a spec tag.
	// Nil skips overlay.
	SecurityJSON SecurityJSONReader
	// ProjectNames resolves the project's display name for the end-user-auth
	// overlay. Nil falls back to the project id.
	ProjectNames ProjectNamer
}

// NewService wires the provisioning service from its collaborator set.
func NewService(d Deps) *Service {
	return &Service{
		issues:            d.Issues,
		execs:             d.Execs,
		design:            d.Design,
		repos:             d.Repos,
		rtCatalog:         d.RTCatalog,
		extProv:           d.ExtProv,
		platProv:          d.PlatProv,
		bindings:          d.Bindings,
		workloads:         d.Workloads,
		projects:          d.Projects,
		access:            d.Access,
		providers:         d.Providers,
		catalogValuePlane: d.CatalogValuePlane,
		environments:      d.Environments,
		pipelines:         d.Pipeline,
		roles:             d.Roles,
		orgSecrets:        d.OrgSecrets,
		orgResourceDocs:   d.OrgResourceDocs,
		promoter:          d.Promoter,
		markers:           d.Markers,
		securityJSON:      d.SecurityJSON,
		projectNames:      d.ProjectNames,
		tryItCallbackURL:  strings.TrimSpace(d.TryItCallbackURL),
	}
}

// findDepInProject locates a dependency of a given name + kind anywhere in the
// project's design at HEAD (external resources and platform resources are
// project-level — a gate issue and its values/params are keyed by dependency
// name, not by the consuming component). Returns ErrDepNotFound when no
// dependency of that name exists and ErrDepWrongKind when it exists as another
// kind.
func (s *Service) findDepInProject(ctx context.Context, orgID, projectID, depName, kind string) (*spec.Dependency, error) {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	return matchDependency(comps, depName, kind)
}

// matchDependency scans already-read design components for the first
// dependency matching (depName, kind) case-insensitively — the pure half of
// findDepInProject, split out so a caller that also needs the full comps
// slice for other purposes (SaveValues computes the UNION config schema
// across every component) can read the design ONCE and reuse it, instead of
// paying for a second design read. Returns ErrDepNotFound when no dependency
// of that name exists and ErrDepWrongKind when it exists as another kind.
func matchDependency(comps []spec.DesignComponent, depName, kind string) (*spec.Dependency, error) {
	var wrongKind bool
	for i := range comps {
		for j := range comps[i].Dependencies {
			d := &comps[i].Dependencies[j]
			if !strings.EqualFold(d.Name, depName) {
				continue
			}
			if d.Kind != kind {
				wrongKind = true
				continue
			}
			return d, nil
		}
	}
	if wrongKind {
		return nil, dependencies.ErrDepWrongKind
	}
	return nil, dependencies.ErrDepNotFound
}

// findProvisionIssue returns the open gate issue for a dependency name, or
// found=false when none exists yet.
//
// The gate's aep:dep/<slug> label is the index (gate_labels.go). The body is
// prose and is never read: a gate issue is a human-facing request, and a human
// may edit it freely without breaking the platform's ability to resolve it.
func (s *Service) findProvisionIssue(ctx context.Context, orgID, projectID, depName string) (number int, found bool, err error) {
	want := gateDepLabel(depName)
	if want == "" {
		return 0, false, nil
	}
	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{delivery.KindProvision, want})
	if err != nil {
		return 0, false, fmt.Errorf("provisioning: list issues: %w", err)
	}
	for _, issue := range issues {
		if strings.EqualFold(issue.State, "open") {
			return issue.Number, true, nil
		}
	}
	return 0, false, nil
}

// admitProvisionRow admits a provision Execution for a gate issue. The row's
// Repo MUST be the issue's repo full name so the funnel gate's
// LatestPerKind(repo, issue) resolves it. Returns (nil, false) when a provision
// run is already active for this gate (the mutex lost the race) — an idempotent
// re-provision.
func (s *Service) admitProvisionRow(ctx context.Context, orgID, projectID, repo, depName string, issueNumber int) (row *delivery.Execution, admitted bool, err error) {
	admitted, row, err = s.execs.TryAdmit(ctx, &delivery.Execution{
		OrgID:       orgID,
		ProjectID:   projectID,
		Repo:        repo,
		IssueNumber: issueNumber,
		Kind:        string(taskmeta.KindProvision),
		Status:      string(taskmeta.ExecQueued),
		Component:   depName,
	})
	return row, admitted, err
}

// completeProvisionRow finishes a provision Execution succeeded and closes the
// gate issue with a no-secrets reference. Consumer tasks gated on this
// dependency release via the gate-issue-close webhook and the eventcore sweep.
// Used by the synchronous external path and the readiness watcher.
//
// depName is the dependency this gate held, carried for the log line and (on the
// watcher path) read off the execution row's Component.
//
// It no longer posts the wiring comment. That used to happen here, and the
// audience was whatever working-set issues existed AT THIS MOMENT — which on a
// first build is none, because the run's planning phase provisions before it
// plans (run/workflow.go fillMilestone). The
// resource half the agent cannot invent now travels in design.json
// (spec/derive_wiring.go), and the endpoint half is published at cycle dispatch
// (wiring.go), where the dispatch predicate guarantees both a resolved design and
// a non-empty audience.
func (s *Service) completeProvisionRow(ctx context.Context, orgID, projectID, depName string, issueNumber int, execID, reference string) {
	exec, err := s.execs.Finish(ctx, execID, string(taskmeta.ExecSucceeded), reference)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: finish provision run failed", "execution", execID, "error", err)
		return
	}
	if exec == nil {
		return // lost the race — another replica already finished
	}
	comment := "✅ Provisioned. " + reference + "\n\nClosing — dependent tasks will dispatch automatically."
	if err := s.issues.CloseIssue(ctx, orgID, projectID, issueNumber, comment); err != nil {
		slog.WarnContext(ctx, "provisioning: close gate issue failed", "issue", issueNumber, "error", err)
	}
}

// failProvisionRow finishes a provision Execution failed and comments the gate
// issue (kept open for retry). The reason carries no secret values.
func (s *Service) failProvisionRow(ctx context.Context, orgID, projectID string, issueNumber int, execID, reason string) {
	// Detach from the request context: the most common failure reason is a
	// client disconnect (the provision blocks synchronously and the caller times
	// out), which cancels ctx. If we marked the row failed on that same canceled
	// ctx the Finish itself would fail — leaving the row 'queued', which the
	// admission partial-unique index then treats as an active run and refuses to
	// re-admit, permanently wedging re-provisioning of that dependency. Marking
	// terminal must always succeed, so it runs on a cancellation-free context
	// (values — the user JWT for the issue comment — are preserved).
	ctx = context.WithoutCancel(ctx)
	exec, err := s.execs.Finish(ctx, execID, string(taskmeta.ExecFailed), reason)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: finish provision run (failed) failed", "execution", execID, "error", err)
	}
	if exec == nil && err == nil {
		return // lost the race — another replica already finished
	}
	if issueNumber > 0 {
		if err := s.issues.CommentIssue(ctx, orgID, projectID, issueNumber, "⚠️ Provisioning failed: "+reason); err != nil {
			slog.WarnContext(ctx, "provisioning: comment gate issue failed", "issue", issueNumber, "error", err)
		}
	}
}

// titleFromName turns an OpenChoreo environment name into a readable label
// when nobody has set openchoreo.dev/display-name: "staging-local" → "Staging Local".
func titleFromName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// ListOrgEnvironments returns OpenChoreo Environments for the org namespace,
// ordered and filtered by the org's own deployment pipeline, with the
// display-name fallback and the absent/unrecognised-is-off validation default
// applied in this one place. A nil lister or empty result is an empty slice
// (never nil), never a 404.
//
// Ordering and filtering happen HERE, not in the handler: the handler's job
// is to assemble the DTO off an already-ordered, already-filtered list.
//
// When the pipeline resolves (resolvePipeline), environments the pipeline
// does not name are DROPPED — the same reasoning PipelineEnvironments'
// own doc comment gives: a converged cluster carries other platforms'
// environments, and binding a project into those would provision cell
// namespaces nothing deploys to. The survivors are ordered by the pipeline's
// promotion order, Position is their index in that order, and PromotesTo is
// the next environment's name (empty on the last one).
//
// When the pipeline does NOT resolve — no lister wired, or the org has no
// "default" pipeline and more than one candidate — nothing is dropped and
// nothing is reordered: OpenChoreo's own list order stands, Position is that
// list index, and PromotesTo is empty on every environment. Emitting a
// PromotesTo chain here would invent a promotion path the platform does not
// have.
func (s *Service) ListOrgEnvironments(ctx context.Context, orgID string) ([]EnvironmentInfo, error) {
	if s.environments == nil {
		return []EnvironmentInfo{}, nil
	}
	raw, err := s.environments.List(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: list environments: %w", err)
	}
	byName := make(map[string]EnvironmentInfo, len(raw))
	for _, e := range raw {
		if e.DisplayName == "" {
			e.DisplayName = titleFromName(e.Name)
		}
		// Absent means off. Anything we do not recognise also means off —
		// an unreadable annotation must not switch validation on.
		if e.Validation != "on" {
			e.Validation = "off"
		}
		e.PromotesTo = ""
		byName[e.Name] = e
	}

	order, resolved := s.resolvePipelineOrder(ctx, orgID)
	if !resolved {
		out := make([]EnvironmentInfo, 0, len(raw))
		for _, e := range raw {
			out = append(out, byName[e.Name])
		}
		return out, nil
	}

	out := make([]EnvironmentInfo, 0, len(order))
	for _, name := range order {
		info, ok := byName[name]
		if !ok {
			continue // the pipeline names an environment OC does not have — never invent one
		}
		out = append(out, info)
	}
	for i := range out {
		if i+1 < len(out) {
			out[i].PromotesTo = out[i+1].Name
		}
	}
	return out, nil
}

// resolvePipelineOrder resolves the org's own deployment pipeline and returns
// the environment names it promotes through, in promotion order. resolved is
// false when no lister is wired, or when the org's pipeline cannot be
// resolved unambiguously — a nil pipelines port, a listing/read failure, or
// more than one candidate pipeline with none named "default".
//
// Resolution order:
//  1. The pipeline named "default" — the documented platform convention
//     (DeploymentPipeline/default per namespace, created by setup and used
//     whenever a project does not name one).
//  2. Failing that, the sole pipeline in the namespace, if there is exactly
//     one.
//  3. Failing that, unresolved: the caller falls back to OC's own list order
//     with no promotion info, and this logs why.
func (s *Service) resolvePipelineOrder(ctx context.Context, orgID string) (order []string, resolved bool) {
	if s.pipelines == nil {
		return nil, false
	}
	names, err := s.pipelines.ListPipelineNames(ctx, orgID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: list deployment pipelines failed; serving environments in OC list order with no promotion info", "org", orgID, "error", err)
		return nil, false
	}
	pipelineName := ""
	for _, n := range names {
		if n == "default" {
			pipelineName = n
			break
		}
	}
	if pipelineName == "" && len(names) == 1 {
		pipelineName = names[0]
	}
	if pipelineName == "" {
		slog.WarnContext(ctx, "provisioning: org has no resolvable deployment pipeline (no \"default\" and not exactly one candidate); serving environments in OC list order with no promotion info", "org", orgID, "candidates", names)
		return nil, false
	}
	order, err = s.pipelines.PipelineEnvironments(ctx, orgID, pipelineName)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: read deployment pipeline environments failed; serving environments in OC list order with no promotion info", "org", orgID, "pipeline", pipelineName, "error", err)
		return nil, false
	}
	return order, true
}

// environmentNames strips EnvironmentInfo down to bare names for the callers
// that only need identifiers (env-cell synthesis, config-value bookkeeping) —
// not the full DTO, which is ListOrgEnvironments' assembly.
func environmentNames(infos []EnvironmentInfo) []string {
	out := make([]string, 0, len(infos))
	for _, e := range infos {
		out = append(out, e.Name)
	}
	return out
}

// envList returns the environments to provision, defaulting to [development].
func envList(reqEnvs []string) []string {
	out := make([]string, 0, len(reqEnvs))
	for _, e := range reqEnvs {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return []string{defaultEnv()}
	}
	return out
}
