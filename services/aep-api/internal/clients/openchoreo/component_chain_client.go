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
// 409 Conflict means the release is already there — return the same name.
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
	return "", fmt.Errorf("generate release for %q: %w", scoped,
		handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		}))
}

// EnsureReleaseBinding binds the release into an environment — the last link
// that makes OC render the Job into the project's dataplane namespace.
// 409 Conflict is success (same resumability rule as EnsureWorkload).
func (c *componentClient) EnsureReleaseBinding(ctx context.Context, orgName, projectName, componentName, environment, releaseName string) error {
	scoped := ScopedComponentName(projectName, componentName)
	bindingName := scoped + "-" + environment
	release := releaseName
	body := ocgen.ReleaseBinding{
		Metadata: ocgen.ObjectMeta{Name: bindingName},
		Spec: &ocgen.ReleaseBindingSpec{
			Environment: environment,
			Owner: struct {
				ComponentName string `json:"componentName"`
				ProjectName   string `json:"projectName"`
			}{ComponentName: scoped, ProjectName: projectName},
			ReleaseName: &release,
		},
	}

	resp, err := c.oc.CreateReleaseBindingWithResponse(ctx, orgName, ocgen.CreateReleaseBindingJSONRequestBody(body))
	if err != nil {
		return fmt.Errorf("failed to create release binding %q: %w", bindingName, err)
	}
	switch resp.StatusCode() {
	case http.StatusCreated, http.StatusOK, http.StatusConflict:
		return nil
	}
	return fmt.Errorf("create release binding %q: %w", bindingName,
		handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
}
