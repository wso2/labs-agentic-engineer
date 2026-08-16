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
	"context"
	"fmt"
	"net/http"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// EnsureWorkload creates the component's Workload — the per-cycle image and env.
// 409 Conflict is success: a resumed dispatch re-posts the same deterministic
// name, and the Workload it would create is byte-identical.
func (c *componentClient) EnsureWorkload(ctx context.Context, orgName, projectName string, in WorkloadInput) error {
	scoped := ScopedComponentName(projectName, in.ComponentName)
	meta := ocgen.ObjectMeta{Name: scoped}
	if len(in.Labels) > 0 {
		labels := make(map[string]string, len(in.Labels))
		for k, v := range in.Labels {
			labels[k] = v
		}
		meta.Labels = &labels
	}
	body := ocgen.Workload{
		Metadata: meta,
		Spec: &ocgen.WorkloadSpec{
			Owner: &struct {
				ComponentName string `json:"componentName"`
				ProjectName   string `json:"projectName"`
			}{ComponentName: scoped, ProjectName: projectName},
			Container: &ocgen.WorkloadContainer{
				Image: in.Image,
				Env:   workflowEnvVarRefsToGen(in.Env),
			},
		},
	}

	resp, err := c.oc.CreateWorkloadWithResponse(ctx, orgName, ocgen.CreateWorkloadJSONRequestBody(body))
	if err != nil {
		return fmt.Errorf("failed to create workload %q: %w", scoped, err)
	}
	switch resp.StatusCode() {
	case http.StatusCreated, http.StatusOK, http.StatusConflict:
		return nil
	}
	return fmt.Errorf("create workload %q: %w", scoped,
		handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
}

// EnsureRelease cuts the component's ComponentRelease under the CALLER'S name
// and returns it. The name is supplied rather than server-generated so a
// resumed dispatch rebinds the same release instead of cutting a second one.
//
// IDEMPOTENT BY READ-BACK, not by status code. openchoreo-api answers a
// generate-release for a name that already exists with a bare 500 (the same 58
// bytes of "Internal server error" any other fault gets), so there is no
// response this client could read to tell "already cut" from "genuinely broke".
// It is therefore asked the only question that has an unambiguous answer: is the
// release there?
//
// That matters more than it sounds. Every caller of this function is under
// Temporal's retry, and a deploy activity that fails on its third component
// retries all three — so the second attempt re-cuts two existing releases. Read
// as a fault, that is a stage which can never succeed again once it has half
// succeeded once, retrying until a human notices. It was observed doing exactly
// that for twenty minutes.
//
// The read happens only AFTER a refused write, not before every one: cutting a
// release that does not exist yet is the common case by a wide margin (once per
// component per cycle, against a retry that is rare), and a pre-flight GET would
// tax all of them to spare the few.
func (c *componentClient) EnsureRelease(ctx context.Context, orgName, projectName, componentName, releaseName string) (string, error) {
	scoped := ScopedComponentName(projectName, componentName)
	name := releaseName
	resp, err := c.oc.GenerateReleaseWithResponse(ctx, orgName, ocgen.ComponentNameParam(scoped),
		ocgen.GenerateReleaseJSONRequestBody{ReleaseName: &name})
	if err != nil {
		return "", fmt.Errorf("failed to generate release for %q: %w", scoped, err)
	}
	switch resp.StatusCode() {
	case http.StatusCreated, http.StatusOK, http.StatusConflict:
		return releaseName, nil
	}
	// Only a 5xx is ambiguous. A 400 or a 404 is OpenChoreo refusing the request
	// on its own terms — a malformed name, a component that is not there — and a
	// release lingering from some earlier attempt must not be allowed to answer a
	// question that was never about whether it exists.
	if resp.StatusCode() >= http.StatusInternalServerError {
		if exists, rerr := c.releaseExists(ctx, orgName, releaseName); rerr == nil && exists {
			return releaseName, nil
		}
	}
	return "", fmt.Errorf("generate release for %q: %w", scoped,
		handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		}))
}

// releaseExists reports whether a ComponentRelease of that name is already
// there. An error means "could not tell", and the caller then reports the WRITE's
// failure rather than this one — the write is the thing that was asked for, and a
// deploy must never be called successful because a read happened to fail.
func (c *componentClient) releaseExists(ctx context.Context, orgName, releaseName string) (bool, error) {
	resp, err := c.oc.GetComponentReleaseWithResponse(ctx, orgName, ocgen.ComponentReleaseNameParam(releaseName))
	if err != nil {
		return false, err
	}
	// The decoded body, not the status alone: a 200 carrying something this client
	// could not parse as a ComponentRelease is not evidence that the release is
	// there, and the caller is about to treat "there" as "the deploy succeeded".
	return resp.StatusCode() == http.StatusOK && resp.JSON200 != nil, nil
}

// ReleaseBindingName is the deterministic name of a component's binding in one
// environment. Exported because the deploy stage reads a binding back by name
// to learn whether it is Ready, and a second spelling of this rule would let
// the writer and the reader disagree about which object they mean.
func ReleaseBindingName(projectName, componentName, environment string) string {
	return ScopedComponentName(projectName, componentName) + "-" + environment
}

// EnsureReleaseBinding binds the release into an environment — the last link
// that makes OC render the Job into the project's dataplane namespace.
// 409 Conflict is success (same resumability rule as EnsureWorkload).
//
// CREATE-ONLY by contract, and that is the whole difference from
// ApplyReleaseBinding: an ephemeral component's binding is written once and
// never re-pinned, so finding one already there means a resumed dispatch and
// the right answer is to leave it exactly as it is.
func (c *componentClient) EnsureReleaseBinding(ctx context.Context, orgName, projectName, componentName, environment, releaseName string) error {
	bindingName := ReleaseBindingName(projectName, componentName, environment)
	body := releaseBindingBody(projectName, ReleaseBindingDesired{
		ComponentName: componentName,
		Environment:   environment,
		ReleaseName:   releaseName,
	})
	created, err := c.createReleaseBinding(ctx, orgName, bindingName, body)
	if err != nil {
		return err
	}
	_ = created // a conflict is success here; the existing binding stands
	return nil
}

// ApplyReleaseBinding converges a component's binding in one environment onto
// the desired state: create it when absent, otherwise read-modify-write the
// fields the caller owns.
//
// This is the DEPLOY of a user component. Writing spec.releaseName is what
// promotes a release, and doing it in the same object write as the trait
// configs and workload overrides is what closes the window a two-step wiring
// leaves open — a binding is never briefly renderable-but-wrong, because it is
// never briefly half-written.
//
// The update path re-reads inside retryStaleWrite: OC's own controllers rewrite
// these bindings continuously, and openchoreo-api reports the lost race as a
// generic 500 (see stale_write.go). The write is an idempotent
// converge-to-desired-state, which is exactly the shape that helper requires.
func (c *componentClient) ApplyReleaseBinding(ctx context.Context, orgName, projectName string, in ReleaseBindingDesired) error {
	bindingName := ReleaseBindingName(projectName, in.ComponentName, in.Environment)
	created, err := c.createReleaseBinding(ctx, orgName, bindingName,
		releaseBindingBody(projectName, in))
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	// It already existed, so converge it. Not a blind PUT of the create body:
	// the binding carries fields this caller does not own
	// (componentTypeEnvironmentConfigs, and the overrides an unmanaging caller
	// leaves nil), and a full overwrite would silently drop them.
	return retryStaleWrite(ctx, "releasebinding/"+bindingName, func(ctx context.Context) error {
		return c.putReleaseBinding(ctx, orgName, bindingName, in)
	})
}

// GetReleaseBindingStatus reads one binding's aggregate Ready condition.
//
// (nil, nil) for a binding that does not exist is a real answer, not an error:
// between the release cut and OC admitting the binding there is a window where
// the deploy stage's poll legitimately finds nothing, and a caller that had to
// distinguish 404-from-error to keep waiting would end up re-implementing this
// rule at every call site.
func (c *componentClient) GetReleaseBindingStatus(ctx context.Context, orgName, projectName, componentName, environment string) (*ReleaseBindingSummary, error) {
	bindingName := ReleaseBindingName(projectName, componentName, environment)
	resp, err := c.oc.GetReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(bindingName))
	if err != nil {
		return nil, fmt.Errorf("failed to get release binding %q: %w", bindingName, err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	summary := releaseBindingSummary(*resp.JSON200)
	return &summary, nil
}

// createReleaseBinding POSTs the binding and reports whether it was created.
// A 409 means it is already there — not an error, and the signal the caller
// uses to decide between "done" and "converge it".
func (c *componentClient) createReleaseBinding(ctx context.Context, orgName, bindingName string, body ocgen.ReleaseBinding) (bool, error) {
	resp, err := c.oc.CreateReleaseBindingWithResponse(ctx, orgName, ocgen.CreateReleaseBindingJSONRequestBody(body))
	if err != nil {
		return false, fmt.Errorf("failed to create release binding %q: %w", bindingName, err)
	}
	switch resp.StatusCode() {
	case http.StatusCreated, http.StatusOK:
		return true, nil
	case http.StatusConflict:
		return false, nil
	}
	return false, fmt.Errorf("create release binding %q: %w", bindingName,
		handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
}

// putReleaseBinding is one read-modify-write attempt: re-read the binding,
// overlay the fields the caller owns, write it back.
func (c *componentClient) putReleaseBinding(ctx context.Context, orgName, bindingName string, in ReleaseBindingDesired) error {
	getResp, err := c.oc.GetReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(bindingName))
	if err != nil {
		return fmt.Errorf("failed to get release binding %q: %w", bindingName, err)
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		return handleErrorResponse(getResp.StatusCode(), ErrorResponses{
			JSON401: getResp.JSON401,
			JSON403: getResp.JSON403,
			JSON404: getResp.JSON404,
			JSON500: getResp.JSON500,
		})
	}
	rb := *getResp.JSON200
	if rb.Spec == nil {
		rb.Spec = &ocgen.ReleaseBindingSpec{}
	}
	applyDesiredToBinding(rb.Spec, in)

	updResp, uerr := c.oc.UpdateReleaseBindingWithResponse(ctx, orgName,
		ocgen.ReleaseBindingNameParam(bindingName), ocgen.UpdateReleaseBindingJSONRequestBody(rb))
	if uerr != nil {
		return fmt.Errorf("failed to update release binding %q: %w", bindingName, uerr)
	}
	if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
		return handleErrorResponse(updResp.StatusCode(), ErrorResponses{
			JSON400: updResp.JSON400,
			JSON401: updResp.JSON401,
			JSON403: updResp.JSON403,
			JSON404: updResp.JSON404,
			JSON500: updResp.JSON500,
		})
	}
	return nil
}

// releaseBindingBody is the CREATE body for a desired binding — the same
// overlay the update path applies, over a bare spec carrying the owner.
func releaseBindingBody(projectName string, in ReleaseBindingDesired) ocgen.ReleaseBinding {
	scoped := ScopedComponentName(projectName, in.ComponentName)
	spec := &ocgen.ReleaseBindingSpec{
		Environment: in.Environment,
		Owner: struct {
			ComponentName string `json:"componentName"`
			ProjectName   string `json:"projectName"`
		}{ComponentName: scoped, ProjectName: projectName},
	}
	applyDesiredToBinding(spec, in)
	return ocgen.ReleaseBinding{
		Metadata: ocgen.ObjectMeta{Name: ReleaseBindingName(projectName, in.ComponentName, in.Environment)},
		Spec:     spec,
	}
}

// applyDesiredToBinding overlays the owned fields onto a binding spec. It is
// the ONE place the nil-means-unmanaged rule is implemented, so create and
// update cannot drift apart on it.
func applyDesiredToBinding(spec *ocgen.ReleaseBindingSpec, in ReleaseBindingDesired) {
	if in.ReleaseName != "" {
		release := in.ReleaseName
		spec.ReleaseName = &release
	}
	if in.State != "" {
		state := ocgen.ReleaseBindingSpecState(in.State)
		spec.State = &state
	}
	if in.TraitEnvironmentConfigs != nil {
		configs := make(map[string]interface{}, len(in.TraitEnvironmentConfigs))
		for inst, params := range in.TraitEnvironmentConfigs {
			configs[inst] = cloneParameterMap(params)
		}
		spec.TraitEnvironmentConfigs = &configs
	}
	if in.Env == nil && in.Files == nil {
		return
	}
	if spec.WorkloadOverrides == nil {
		spec.WorkloadOverrides = &ocgen.WorkloadOverrides{}
	}
	if spec.WorkloadOverrides.Container == nil {
		spec.WorkloadOverrides.Container = &ocgen.ContainerOverride{}
	}
	if in.Env != nil {
		env := workflowEnvVarRefsToGen(in.Env)
		spec.WorkloadOverrides.Container.Env = env
	}
	if in.Files != nil {
		files := workflowFileVarsToGen(in.Files)
		spec.WorkloadOverrides.Container.Files = files
	}
}
