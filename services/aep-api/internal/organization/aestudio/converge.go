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

package aestudio

// converge.go — the Ensure (ADR-0040), in order: Project ae-system →
// its ProjectReleaseBinding (creates the cell namespace) → ResourceType →
// Resource → wait for its release → RRB pin. Each step reads what is there
// first and writes only what differs, so a converge with nothing to do
// writes nothing. Everything goes through the Service's clients (aep-api's
// own identity where configured) in the org's namespace.
//
// The ResourceType is PUT in place and never deleted (Cloud cannot delete
// it). The converge takes no lock and ensures no client: secrets
// are only read, by reference.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// projectDisplayName is the Project ae-system's display name.
const projectDisplayName = "AE Studio"

// converge runs one Ensure of org and returns the fingerprint of the desired
// state it worked to ("" when that could not be computed).
func (s *Service) converge(ctx context.Context, org string) (string, error) {
	oc := s.oc
	c := &run{ctx: ctx, org: org}
	var d desiredState
	if err := c.step("desired", func() (err error) { d, err = s.desired(ctx, org); return err }); err != nil {
		return "", err
	}
	var env, release string
	steps := []struct {
		name string
		fn   func() error
	}{
		{"project", func() error { return ensureProject(ctx, oc, org) }},
		{"write-target", func() (err error) { env, err = oc.Targets.Resolve(ctx, org, ProjectName); return err }},
		{"project-binding", func() error { return oc.Cells.EnsureProjectReleaseBinding(ctx, org, ProjectName, env) }},
		{"resourcetype", func() error { return ensureResourceType(ctx, oc, org) }},
		{"resource", func() (err error) { release, err = ensureResource(ctx, oc, org, d); return err }},
		{"binding", func() error { return ensureBinding(ctx, oc, org, env, release, d) }},
	}
	for _, st := range steps {
		if err := c.step(st.name, st.fn); err != nil {
			return d.fingerprint(), err
		}
	}
	return d.fingerprint(), nil
}

// run logs one converge's steps.
type run struct {
	ctx context.Context
	org string
}

// step runs fn, logging ae_studio.converge {org, step, ms}, or
// ae_studio.converge_failed {org, step, error} when it fails. Errors carry
// names and paths, never secret values (the parameters hold none).
func (r *run) step(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	var nr *notReadyError
	switch {
	case errors.As(err, &nr):
		slog.WarnContext(r.ctx, "ae_studio.converge_failed", "org", r.org, "step", name, "error", nr.reason)
	case err != nil:
		slog.ErrorContext(r.ctx, "ae_studio.converge_failed", "org", r.org, "step", name, "error", err)
	default:
		slog.InfoContext(r.ctx, "ae_studio.converge", "org", r.org, "step", name, "ms", time.Since(start).Milliseconds())
	}
	return err
}

// ensureProject creates the Project ae-system when it is missing, on the
// namespaced ProjectType/default (the project client's create body).
func ensureProject(ctx context.Context, oc OC, org string) error {
	_, err := oc.Projects.GetProject(ctx, org, ProjectName)
	if !errors.Is(err, openchoreo.ErrNotFound) {
		return err
	}
	_, err = oc.Projects.CreateProject(ctx, org, &gen.CreateProjectRequest{Name: ProjectName, DisplayName: projectDisplayName})
	if errors.Is(err, openchoreo.ErrConflict) {
		return nil // another replica created it
	}
	return err
}

// ensureResourceType installs the embedded template, or PUTs it over an
// installed one from another template.
func ensureResourceType(ctx context.Context, oc OC, org string) error {
	installed, err := oc.Resources.GetResourceType(ctx, org, ResourceName)
	switch {
	case errors.Is(err, openchoreo.ErrNotFound):
		want, terr := stampedTemplate()
		if terr != nil {
			return terr
		}
		// A 409 (another replica) returns the installed one, which the
		// hash check below then brings in line.
		if installed, err = oc.Resources.EnsureResourceType(ctx, org, want); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	if !rtDrifted(installed) {
		return nil
	}
	want, err := stampedTemplate()
	if err != nil {
		return err
	}
	_, err = oc.Resources.UpdateResourceType(ctx, org, want)
	return err
}

// stampedTemplate is the embedded ResourceType annotated with its hash.
func stampedTemplate() (*openchoreo.ResourceType, error) {
	rt, err := Template()
	if err != nil {
		return nil, err
	}
	if rt.Metadata.Annotations == nil {
		rt.Metadata.Annotations = map[string]string{}
	}
	rt.Metadata.Annotations[templateHashAnnotation] = TemplateHash()
	return rt, nil
}

// ensureResource applies the Resource when its parameters differ and returns
// the release the binding pins: the new one a change cuts, or the current
// one when nothing changed.
func ensureResource(ctx context.Context, oc OC, org string, d desiredState) (string, error) {
	installed, err := oc.Resources.GetResource(ctx, org, ResourceName)
	if err != nil && !errors.Is(err, openchoreo.ErrNotFound) {
		return "", err
	}
	if err == nil && !resourceDrifted(d, installed) {
		if rel := openchoreo.ReleaseName(installed); rel != "" {
			return rel, nil
		}
		return waitRelease(ctx, oc, org, "") // created moments ago; its first release is coming
	}
	raw, err := json.Marshal(d.Params)
	if err != nil {
		return "", fmt.Errorf("encode parameters: %w", err)
	}
	applied, err := oc.Resources.ApplyResource(ctx, org, &openchoreo.Resource{
		Metadata: openchoreo.OCObjectMeta{Name: ResourceName},
		Spec: openchoreo.ResourceSpec{
			Owner:      openchoreo.ResourceOwner{ProjectName: ProjectName},
			Type:       openchoreo.ResourceTypeRef{Kind: "ResourceType", Name: ResourceName},
			Parameters: raw,
		},
	})
	if err != nil {
		return "", err
	}
	return waitRelease(ctx, oc, org, openchoreo.ReleaseName(applied))
}

// waitRelease waits for a release other than prior.
func waitRelease(ctx context.Context, oc OC, org, prior string) (string, error) {
	return openchoreo.WaitForReleaseChange(ctx, oc.Resources, org, ResourceName, prior, releaseWaitInterval, releaseWaitTimeout)
}

// ensureBinding pins the binding in env to release with d's environment
// configs, unless it already is.
func ensureBinding(ctx context.Context, oc OC, org, env, release string, d desiredState) error {
	installed, err := oc.Resources.GetBinding(ctx, org, bindingName(env))
	if err != nil {
		return err
	}
	if installed != nil && !bindingDrifted(d, installed, release) {
		return nil
	}
	raw, err := json.Marshal(d.EnvConfigs)
	if err != nil {
		return fmt.Errorf("encode environment configs: %w", err)
	}
	_, err = oc.Resources.EnsureBinding(ctx, org, &openchoreo.ResourceReleaseBinding{
		Metadata: openchoreo.OCObjectMeta{Name: bindingName(env)},
		Spec: openchoreo.ResourceReleaseBindingSpec{
			Owner:                          openchoreo.ResourceReleaseBindingOwner{ProjectName: ProjectName, ResourceName: ResourceName},
			Environment:                    env,
			ResourceRelease:                release,
			ResourceTypeEnvironmentConfigs: raw,
		},
	})
	return err
}
