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

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/gen"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// acceptanceCriteria adapts the Files API to validation's CriteriaReader port:
// it reads the `.feature` files under specs/validation/acceptance/ at HEAD,
// reporting an empty or absent directory as found=false with no error (the
// design agent has not authored the oracle yet). Keeps the files feature out of
// the validation package.
//
// Bundle rather than List-then-Read: it reads the whole directory at ONE commit,
// so two files can never come from different states of the repo.
type acceptanceCriteria struct {
	files spec.FilesService
}

func (a acceptanceCriteria) ReadAcceptanceCriteria(ctx context.Context, orgID, projectID string) ([]validation.AcceptanceCriteriaFile, bool, error) {
	bundle, rerr := a.files.Bundle(ctx, orgID, projectID, validation.AcceptanceDirPath, "")
	if rerr != nil {
		if errors.Is(rerr, spec.ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, rerr
	}
	var out []validation.AcceptanceCriteriaFile
	for _, fc := range bundle.Files {
		// The directory is the oracle's, but a stray README or an editor's
		// leftover is not a scenario file and must not count as one.
		if !strings.HasSuffix(fc.Path, ".feature") {
			continue
		}
		out = append(out, validation.AcceptanceCriteriaFile{Path: fc.Path, Content: fc.Content})
	}
	return out, len(out) > 0, nil
}

// ReportAt satisfies runread's ValidationSnapshotReader: the report the
// validation runner committed, read at one commit.
//
// A missing file is found=false rather than an error. An attempt whose verdict
// is `unreported` is precisely one that committed nothing, and the console
// renders that as its own sentence; surfacing it as a read failure would make a
// known-absent report look like a broken platform.
func (a acceptanceCriteria) ReportAt(ctx context.Context, orgID, projectID, at string) (string, bool, error) {
	fc, err := a.files.ReadAt(ctx, orgID, projectID, validation.ReportFilePath, at)
	if err != nil {
		if errors.Is(err, spec.ErrFileNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return fc.Content, true, nil
}

// CriteriaAt satisfies runread's ValidationSnapshotReader: the acceptance
// oracle as it stood at one commit.
//
// Bundle, like the HEAD read above, so every file comes from ONE state of the
// repo. An absent directory is an empty slice: a version whose oracle was never
// authored is an ordinary state and the page says so in words, not by failing.
func (a acceptanceCriteria) CriteriaAt(ctx context.Context, orgID, projectID, at string) ([]gen.AcceptanceCriteriaFile, error) {
	bundle, err := a.files.Bundle(ctx, orgID, projectID, validation.AcceptanceDirPath, at)
	if err != nil {
		if errors.Is(err, spec.ErrFileNotFound) {
			return []gen.AcceptanceCriteriaFile{}, nil
		}
		return nil, err
	}
	out := make([]gen.AcceptanceCriteriaFile, 0, len(bundle.Files))
	for _, fc := range bundle.Files {
		if !strings.HasSuffix(fc.Path, ".feature") {
			continue
		}
		out = append(out, gen.AcceptanceCriteriaFile{Path: fc.Path, Content: fc.Content})
	}
	return out, nil
}

// HasValidationCriteria satisfies the event plane's ValidationOracle: the same
// read, reduced to the yes/no the revalidate guard asks. Deliberately does not
// parse — a malformed oracle still means "there is something here to validate",
// and refusing on it at the trigger would send the caller a shape error about a
// file they may not have written. The mint parses, and skips one it cannot use.
func (a acceptanceCriteria) HasValidationCriteria(ctx context.Context, orgID, projectID string) (bool, error) {
	_, found, err := a.ReadAcceptanceCriteria(ctx, orgID, projectID)
	return found, err
}

// validationCycleLocator adapts the run-cycle repository to validation's
// CycleLocator port: it resolves a runner's cycle id to its project, org-fenced
// (GetByIDScoped returns nil for a different org — the tenant fence).
//
// The CYCLE is the runner's identity. This used to read the executions table,
// which the milestone supervisor never writes — so a dispatched validation runner
// was told its own dispatch did not exist, and the run reported `skipped` over an
// oracle it had just filed.
type validationCycleLocator struct {
	repo delivery.RunCycleRepository
}

func (l validationCycleLocator) LookupCycleProject(ctx context.Context, orgHandle, cycleID string) (string, bool, error) {
	row, err := l.repo.GetByIDScoped(ctx, orgHandle, cycleID)
	if err != nil {
		return "", false, err
	}
	if row == nil {
		return "", false, nil
	}
	return row.ProjectID, true, nil
}

// componentDeployLister is the ListDeployments slice of ComponentService the
// endpoint resolver needs (satisfied structurally by *component.componentService).
type componentDeployLister interface {
	ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*gen.DeploymentList, error)
}

// validationEndpointResolver adapts the design read + ComponentService to
// validation's EndpointResolver port: the deployed external URL (first HTTP
// external endpoint from the OpenChoreo ReleaseBinding) per design component. A
// component with no resolved URL yet is skipped; a ListDeployments ERROR is
// propagated (it is an infra failure, not "undeployed" — see ResolveEndpoints).
type validationEndpointResolver struct {
	store *spec.ArtifactStore
	comp  componentDeployLister
}

func (r validationEndpointResolver) ResolveEndpoints(ctx context.Context, orgHandle, projectID string) ([]validation.ComponentEndpoint, error) {
	// This runs inside the runner's validation-context request, whose ctx carries
	// the runner's inbound task JWT (aud git-service). Without this marker the OC
	// transport would forward that token to OpenChoreo, which rejects it (401) —
	// so every ListDeployments below would fail and we'd resolve zero endpoints.
	// Act as the BFF's own service identity (org resolved via namespace), exactly
	// like the MCP handler and the async watchers.
	ctx = authn.WithServiceIdentity(ctx)
	df, err := r.store.ReadDesign(ctx, orgHandle, projectID)
	if err != nil {
		return nil, err
	}
	var out []validation.ComponentEndpoint
	for i := range df.Components {
		name := df.Components[i].Name
		// A never-deployed component is an EMPTY 200 list (ListReleaseBindings
		// filters by component), so an error here is genuinely exceptional
		// (auth/network/OC down) — propagate it instead of silently resolving
		// fewer endpoints than the deployed system actually has.
		list, lerr := r.comp.ListDeployments(ctx, orgHandle, projectID, name)
		if lerr != nil {
			return nil, fmt.Errorf("list deployments for %s: %w", name, lerr)
		}
		// No resolved URL yet (empty list / no external endpoint) — skip.
		if url := firstDeploymentURL(list); url != "" {
			out = append(out, validation.ComponentEndpoint{Component: name, URL: url})
		}
	}
	return out, nil
}

// firstDeploymentURL returns the first non-empty deployed endpoint URL.
func firstDeploymentURL(list *gen.DeploymentList) string {
	if list == nil {
		return ""
	}
	for i := range list.Items {
		if u := list.Items[i].EndpointURL; u != "" {
			return u
		}
	}
	return ""
}
