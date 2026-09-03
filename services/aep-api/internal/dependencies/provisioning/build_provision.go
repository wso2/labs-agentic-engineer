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
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Input kinds carried on ProvisionInput.Kind. external-config is derived from
// the design; the others still originate in request inputs.
const (
	buildKindExternalConfig = "external-config"
	buildKindPlatformResrc  = "platform-resource"
	buildKindOrgService     = "org-service"
)

// BuildProvisionInput is one dependency's resolved provisioning payload for the
// build path — field-identical to devflow.ProvisionInput but a provisioning
// package type (this feature must not import the workflow orchestrator; the
// app-root adapter maps devflow.ProvisionInput ⇄ BuildProvisionInput). A raw
// secret value is NEVER carried here — SecretRefByEnv holds the SM-API reference
// per env. Build-derived external inputs leave it empty; SaveValues owns staging.
type BuildProvisionInput struct {
	Component      string
	Dependency     string
	Kind           string
	Config         map[string]string
	SecretRefByEnv map[string]string
	Parameters     map[string]any
	Approved       bool
}

// ProvisionFailure is one dependency's provisioning failure — data the workflow
// inspects (not an activity error). It carries no secret values.
type ProvisionFailure struct {
	Component  string
	Dependency string
	Reason     string
	// Err is the underlying cause, carried so aggregation can preserve
	// ErrProvisionPermanent through errors.Is. Reason is still the string
	// form: Temporal cannot round-trip an error value on this struct.
	Err error
}

// ProvisionForBuild authors the project's dependencies from the inputs the dev
// workflow carries. It mints platform-resource `provision` gate
// issues ONCE, then authors each input BY KIND:
//   - external-config: unset/defaulted authoring through AuthorPreparedValues,
//     with no SM-API write and no gate.
//   - platform-resource: the async Provision path (the readiness watcher closes
//     the gate out-of-band).
//   - org-service: a no-op here (Task 4 fills it).
//
// A returned error means "retry the activity" (the gate mint failed). A per-input
// author failure is appended to the returned failures and the batch CONTINUES —
// the workflow decides what to do with them (it fails the run). orgID is the OC
// namespace + issues org; ocOrgID is the SM-API org id (reserved for the org
// path; the external author half needs no SM write). The external RT-authoring
// definition (name + description + config schema) is built straight off the
// project's committed design (authorExternalPrepared) — the external dep need
// not be separately registered anywhere.
func (s *Service) ProvisionForBuild(ctx context.Context, orgID, ocOrgID, projectID, tag string, milestoneNumber int, inputs []BuildProvisionInput) ([]ProvisionFailure, error) {
	// Mint gates only when the drawer carried inputs. A not-ready dependency is
	// always surfaced in the build drawer, so a build with no inputs needs no new
	// gate — and a pure re-build must not churn a fresh gate for every already-ready
	// dep. Existing gates are still reconciled by settleReadyGates below, so an
	// orphaned gate self-heals on ANY later build, drawer or not (issue #164).
	// gateByDep carries the `provision` gate issue number the mint step KNOWS for
	// each dep — captured from the CreateIssue result, not re-looked-up via GitHub's
	// eventually-consistent label list (which lags a just-created gate and strands
	// the provision run — issue #164). Empty when no inputs were carried; a missing
	// dep resolves to 0 (a safe no-op gate).
	var gateByDep map[string]int
	if len(inputs) > 0 {
		var err error
		gateByDep, err = s.EnsureProvisionIssues(ctx, orgID, projectID, tag, milestoneNumber)
		if err != nil {
			return nil, err
		}
	}

	var failures []ProvisionFailure

	// The roles gate is driven by the DESIGN at the tag, not by the drawer
	// inputs, so it runs on EVERY build that declares roles — including a
	// rebuild whose dependencies are all already Ready and therefore carry no
	// input at all. roles_gate.go carries why that matters.
	if f := s.ensureRolesGate(ctx, orgID, projectID, tag, milestoneNumber); f != nil {
		failures = append(failures, *f)
	}

	provisioned := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		provisioned[strings.ToLower(in.Dependency)] = true
	}
	skipPlatform := false
	for _, in := range inputs {
		gate := gateByDep[strings.ToLower(in.Dependency)]
		switch in.Kind {
		case buildKindExternalConfig:
			if err := s.authorExternalPrepared(ctx, orgID, ocOrgID, projectID, in, gate); err != nil {
				failures = append(failures, ProvisionFailure{Component: in.Component, Dependency: in.Dependency, Reason: err.Error()})
			}
		case buildKindPlatformResrc:
			if skipPlatform {
				continue
			}
			if err := s.provisionResource(ctx, orgID, projectID, in.Dependency, gate, in.Parameters, nil, tag); err != nil {
				failures = append(failures, ProvisionFailure{Component: in.Component, Dependency: in.Dependency, Reason: err.Error(), Err: err})
				if errors.Is(err, dependencies.ErrProvisionPermanent) {
					skipPlatform = true
				}
			}
		case buildKindOrgService:
			// Cross-project org-service visibility (issue #164, Task 4): for an
			// APPROVED-but-unresolved org-service dep, record the access request, mint
			// the consumer-side visibility gate, and kick the provider's build. An
			// unapproved dep is left alone (the user did not opt in). Per-input batch
			// semantics: a failure becomes a ProvisionFailure and the batch continues.
			if in.Approved {
				if err := s.StartOrgServiceVisibility(ctx, orgID, projectID, in.Dependency); err != nil {
					failures = append(failures, ProvisionFailure{Component: in.Component, Dependency: in.Dependency, Reason: err.Error()})
				}
			}
		default:
			slog.WarnContext(ctx, "provisioning: unknown build provision kind", "kind", in.Kind, "dependency", in.Dependency)
		}
	}

	// Reconcile every provision gate whose dep is NOT in inputs. EnsureProvisionIssues
	// re-mints an OPEN gate for each platform-resource dep in the design,
	// but the inputs loop only admits a run for the drawer subset. A dep whose OC
	// binding is already Ready is deliberately skipped by build preflight, so it
	// lands here with a fresh gate and no run — deriving `pending` forever and
	// stranding every consumer coding task (issue #164). Settle it: admit+complete a
	// provision run so its gate derives `deployed`, without re-authoring the resource.
	failures = append(failures, s.settleReadyGates(ctx, orgID, projectID, provisioned)...)
	return failures, nil
}

// settleReadyGates reconciles provision gates for deps that are already Ready in
// OpenChoreo but are NOT in the build drawer inputs (issue #164). For each such
// dep it admits+completes a provision run so the freshly-minted gate derives
// `deployed` instead of stranding consumers on a run-less `pending` gate. It
// re-authors NOTHING — the resource is already Ready. Best-effort: a design read
// hiccup must not fail the build (log + return nil). Binding reads are likewise
// best-effort; only a failure while settling a confirmed-ready gate becomes a
// ProvisionFailure the workflow can inspect.
func (s *Service) settleReadyGates(ctx context.Context, orgID, projectID string, provisioned map[string]bool) []ProvisionFailure {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "provisioning: settle read design failed", "error", err)
		return nil
	}
	var failures []ProvisionFailure
	seen := map[string]bool{}
	for _, comp := range comps {
		for _, dep := range comp.Dependencies {
			// Only platform resources mint a build-time provision gate.
			if dep.Kind != spec.DependencyKindPlatformResource {
				continue
			}
			name := strings.ToLower(dep.Name)
			// A dep in inputs is provisioned by the inputs loop; dedupe multi-consumer deps.
			if provisioned[name] || seen[name] {
				continue
			}
			seen[name] = true
			// Only an already-Ready dep is settled here. A not-Ready dep is driven by
			// its own drawer input (or is genuinely un-actionable) — leave it alone.
			st, _, serr := s.bindingStatus(ctx, orgID, projectID, dep.Name, "")
			if serr != nil {
				slog.WarnContext(ctx, "provisioning: settle read binding failed", "dependency", dep.Name, "error", serr)
				continue
			}
			if st == nil || !st.Ready {
				continue
			}
			if cerr := s.completeReadyGate(ctx, orgID, projectID, dep.Name, comp.Name); cerr != nil {
				failures = append(failures, ProvisionFailure{Component: comp.Name, Dependency: dep.Name, Reason: cerr.Error()})
			}
		}
	}
	return failures
}

// completeReadyGate admits and completes a provision run for an already-Ready
// dep's open gate so it derives `deployed` (issue #164). It mirrors the
// admit→StartWithRun→completeProvisionRow TAIL of authorExternalPrepared but
// authors NOTHING — the OC binding is already Ready, so re-authoring would only
// re-write state. No open gate (issueNumber == 0) or an already-active run
// (!admitted) is an idempotent no-op.
func (s *Service) completeReadyGate(ctx context.Context, orgID, projectID, depName, component string) error {
	slog.DebugContext(ctx, "provisioning: settling already-ready gate", "dependency", depName, "component", component)
	issueNumber, _, err := s.findProvisionIssue(ctx, orgID, projectID, depName)
	if err != nil {
		return err
	}
	if issueNumber == 0 {
		return nil
	}
	repo, err := s.repos.RepoFullName(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("provisioning: resolve repo: %w", err)
	}
	row, admitted, err := s.admitProvisionRow(ctx, orgID, projectID, repo, depName, issueNumber)
	if err != nil {
		return fmt.Errorf("provisioning: admit provision run: %w", err)
	}
	if !admitted {
		// A provision run is already active for this gate (e.g. a concurrent settle).
		return nil
	}
	ref := ocname.ExternalResourceBindingName(projectID, depName, defaultEnv)
	if _, serr := s.execs.StartWithRun(ctx, row.ID, ref); serr != nil {
		slog.WarnContext(ctx, "provisioning: start settle provision run failed", "execution", row.ID, "error", serr)
	}
	s.completeProvisionRow(ctx, orgID, projectID, depName, issueNumber, row.ID,
		fmt.Sprintf("Dependency `%s` already provisioned (OC binding Ready) — gate settled.", depName))
	return nil
}

// authorExternalPrepared runs the synchronous external-config provisioning flow
// from design-derived plain/default values. It authors via AuthorPreparedValues;
// no secret value is written to SM-API.
func (s *Service) authorExternalPrepared(ctx context.Context, orgID, ocOrgID, projectID string, in BuildProvisionInput, gateNumber int) error {
	_ = ocOrgID // the author half needs no SM-API write; kept for symmetry with SaveValues.
	// Read the design ONCE (mirrors SaveValues): validate the dep exists as an
	// external dependency, then build the RT-authoring definition straight off
	// it — name + the matched dependency's Description + the UNION config
	// schema across every declaring component — never the external_resources
	// table (that reader is gone).
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("provisioning: read design: %w", err)
	}
	dep, err := matchDependency(comps, in.Dependency, spec.DependencyKindExternal)
	if err != nil {
		return err
	}
	keys, _ := spec.UnionExternalConfigFor(comps, in.Dependency)
	er := &dependencies.ExternalResource{
		Name:        in.Dependency,
		Description: dep.Description,
		ConfigKeys:  keys,
	}
	byEnv := designPreparedValues(keys)
	registered := false
	if cells := s.registeredEnvCells(ctx, orgID, in.Dependency); len(cells) > 0 {
		byEnv = preparedValuesFromOrgCells(keys, cells)
		registered = true
	}

	// External dependencies do not mint config-collection gates. When the caller
	// supplies an existing gate, reconcile it; never discover or create one here.
	issueNumber := gateNumber
	var execID string
	if issueNumber > 0 {
		repo, rerr := s.repos.RepoFullName(ctx, orgID, projectID)
		if rerr != nil {
			return fmt.Errorf("provisioning: resolve repo: %w", rerr)
		}
		row, admitted, aerr := s.admitProvisionRow(ctx, orgID, projectID, repo, in.Dependency, issueNumber)
		if aerr != nil {
			return fmt.Errorf("provisioning: admit provision run: %w", aerr)
		}
		if admitted {
			execID = row.ID
		}
	}

	result, perr := s.extProv.AuthorPreparedValues(ctx, orgID, projectID, er, byEnv)
	if perr != nil {
		if execID != "" {
			s.failProvisionRow(ctx, orgID, projectID, issueNumber, execID, perr.Error())
		}
		return fmt.Errorf("%w: %v", dependencies.ErrProvisionFailed, perr)
	}

	if registered {
		recordResourceInstances(s.catalogValuePlane, orgID, projectID, in.Dependency, byEnv)
	}

	if execID != "" {
		ref := result.BindingByEnv[defaultEnv]
		if ref == "" {
			ref = result.ResourceName
		}
		if _, serr := s.execs.StartWithRun(ctx, execID, ref); serr != nil {
			slog.WarnContext(ctx, "provisioning: start external provision run failed", "execution", execID, "error", serr)
		}
		s.completeProvisionRow(ctx, orgID, projectID, in.Dependency, issueNumber, execID,
			fmt.Sprintf("External resource `%s` configured (OC binding `%s`).", in.Dependency, ref))
	}
	return nil
}

// preparedValuesFromOrgCells builds AuthorPreparedValues inputs from Registered
// org EnvCells: one entry per environment that appears on the plane, Plain
// filled from non-secret configured cells. SecretStorePath is the org-catalog
// vault key persisted on those cells (empty when no OrgSecretWriter was wired).
func preparedValuesFromOrgCells(keys []spec.ConfigKey, cells []EnvCell) map[string]dependencies.PreparedEnvValues {
	secret := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k.Secret {
			secret[k.Key] = true
		}
	}
	byEnv := map[string]dependencies.PreparedEnvValues{}
	for _, c := range cells {
		ev := byEnv[c.Environment]
		if ev.Plain == nil {
			ev.Plain = map[string]string{}
		}
		if c.Status == "configured" && !secret[c.Key] {
			ev.Plain[c.Key] = c.Value
		}
		if c.SecretStorePath != "" {
			ev.SecretStorePath = c.SecretStorePath
		}
		byEnv[c.Environment] = ev
	}
	return byEnv
}

// recordResourceInstances merges this project's authored environments into the
// org value plane's instance list for a Registered External resource.
func recordResourceInstances(plane CatalogValuePlane, orgID, projectID, name string, byEnv map[string]dependencies.PreparedEnvValues) {
	existing := plane.Instances(orgID, name)
	keep := make([]ResourceInstance, 0, len(existing))
	for _, inst := range existing {
		if inst.Project == projectID {
			if _, authored := byEnv[inst.Environment]; authored {
				continue
			}
		}
		keep = append(keep, inst)
	}
	for env := range byEnv {
		keep = append(keep, ResourceInstance{Project: projectID, Environment: env})
	}
	plane.PutInstances(orgID, name, keep)
}

// designPreparedValues derives the only build-time authoring values the server
// trusts: non-secret defaults (or empty strings) from the committed design. A
// build never accepts carried config values or secret-store references.
func designPreparedValues(keys []spec.ConfigKey) map[string]dependencies.PreparedEnvValues {
	plain := make(map[string]string, len(keys))
	for _, key := range keys {
		if !key.Secret {
			plain[key.Key] = key.DefaultValue
		}
	}
	return map[string]dependencies.PreparedEnvValues{
		defaultEnv: {Plain: plain},
	}
}
