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

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Provision authors the OC Resource model for a platform-resource dependency
// (e.g. postgres-cnpg) and admits a running provision Execution. It is
// ASYNCHRONOUS: standing up a real backing instance takes minutes, so this
// returns once the Resource + binding are authored — the readiness watcher
// observes the binding's native Ready condition out-of-band and only then
// Finishes the run (→ deployed), closes the gate issue, and releases consumers.
//
// Parameters merge the design's authored parameters (baseline) with any
// request-supplied overrides (request wins). params carry no secrets — a
// platform resource's credentials are surfaced as binding outputs, never inputs.
func (s *Service) Provision(ctx context.Context, orgID, projectID, depName string, params map[string]any, envs []string) error {
	// HTTP-path callers (resources_huma.go) have no known gate number — resolve it
	// via the label list (these gates are from prior plans, listable, no race).
	issueNumber, _, err := s.findProvisionIssue(ctx, orgID, projectID, depName)
	if err != nil {
		return err
	}
	return s.provisionResource(ctx, orgID, projectID, depName, issueNumber, params, envs, "")
}

// provisionResource is the platform-resource provisioning core: it authors the OC
// Resource + binding and, when gateNumber > 0, admits a running provision
// Execution pinned to the development binding (the readiness watcher finishes it
// out-of-band). It takes the gate number DIRECTLY so the build path can thread the
// just-minted number past GitHub's eventually-consistent issue list (issue #164);
// the public Provision resolves it via findProvisionIssue for its HTTP callers. A
// gateNumber of 0 authors the resource with no run admitted (a safe no-op gate).
// tag is the spec tag the build is provisioning; empty means HEAD (HTTP drawer).
func (s *Service) provisionResource(ctx context.Context, orgID, projectID, depName string, gateNumber int, params map[string]any, envs []string, tag string) error {
	dep, err := s.findDepInProject(ctx, orgID, projectID, depName, spec.DependencyKindPlatformResource)
	if err != nil {
		return err
	}
	if dep.ResourceType == "" {
		return fmt.Errorf("%w: platform-resource %q has no resourceType in the design", dependencies.ErrDepNotFound, depName)
	}
	merged := make(map[string]any, len(dep.Parameters)+len(params))
	for k, v := range dep.Parameters {
		merged[k] = v
	}
	for k, v := range params {
		merged[k] = v
	}
	merged, err = s.overlayThunderParams(ctx, orgID, projectID, tag, dep.ResourceType, merged)
	if err != nil {
		return err
	}
	envs = envList(envs)

	var execID string
	if gateNumber > 0 {
		repo, rerr := s.repos.RepoFullName(ctx, orgID, projectID)
		if rerr != nil {
			return fmt.Errorf("provisioning: resolve repo: %w", rerr)
		}
		row, admitted, aerr := s.admitProvisionRow(ctx, orgID, projectID, repo, depName, gateNumber)
		if aerr != nil {
			return fmt.Errorf("provisioning: admit provision run: %w", aerr)
		}
		if !admitted {
			// A provision run is already active for this gate — idempotent 202.
			return nil
		}
		execID = row.ID
	}

	result, perr := s.platProv.Provision(ctx, orgID, projectID, depName, dep.ResourceType, merged, envs)
	if perr != nil {
		if execID != "" {
			s.failProvisionRow(ctx, orgID, projectID, gateNumber, execID, perr.Error())
		}
		return fmt.Errorf("%w: %w", dependencies.ErrProvisionFailed, perr)
	}

	// Async: mark the run RUNNING pinned to the development binding name; the
	// readiness watcher observes Ready and finishes it. Without a start the
	// watcher cannot pick it up, so a start failure is a real (logged) problem.
	if execID != "" {
		ref := result.BindingByEnv[defaultEnv]
		if ref == "" {
			ref = result.ResourceName
		}
		if _, serr := s.execs.StartWithRun(ctx, execID, ref); serr != nil {
			slog.WarnContext(ctx, "provisioning: start platform provision run failed", "execution", execID, "error", serr)
		}
	}
	return nil
}

// overlayThunderParams copies authored thunder onto CRT parameters when the
// resource type carries the end-user-auth marker. It never keys on a type name.
//
// Before overlay it deletes `scopes` so design.json / request parameters.scopes
// cannot reach the provisioner. displayName is always set from thunder.name;
// scopes is set only when thunder.scopes is non-empty. thunder.type is not a
// CRT parameter and is never copied. Nil catalog or reader, a type that is
// not end-user-auth, or an absent security.json: no overlay (and no invented
// defaults). A present-but-unparseable file fails provision.
func (s *Service) overlayThunderParams(ctx context.Context, orgID, projectID, tag, resourceType string, merged map[string]any) (map[string]any, error) {
	if s.markers == nil || s.securityJSON == nil {
		return merged, nil
	}
	byName, err := s.markers.MarkersByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("provisioning: resource markers: %w", err)
	}
	if !byName[resourceType].EndUserAuth {
		return merged, nil
	}
	delete(merged, "scopes")
	raw, err := s.securityJSON.ReadSecurityJSON(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read security.json: %w", err)
	}
	if len(raw) == 0 {
		return merged, nil
	}
	doc, err := securityspec.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("provisioning: parse security.json: %w", err)
	}
	merged["displayName"] = doc.Thunder.Name
	if doc.Thunder.Scopes != "" {
		merged["scopes"] = doc.Thunder.Scopes
	}
	return merged, nil
}
