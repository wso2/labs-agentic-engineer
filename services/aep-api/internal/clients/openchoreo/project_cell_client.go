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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/internal/clients/requests"
)

//go:generate go run github.com/matryer/moq@v0.7.1 -rm -fmt goimports -pkg mocks -out mocks/project_cell_client_mock.go . ProjectCellClient

// ProjectCellClient authors the ProjectReleaseBinding that gives a project its
// cell namespace in an environment.
//
// OpenChoreo 1.2.0 split project provisioning in two. Creating a Project now
// only cuts a ProjectRelease — an immutable snapshot of the project's
// (Cluster)ProjectType. Nothing is materialized in a data plane until a
// ProjectReleaseBinding pins that release to an environment; THAT is what owns
// the cell namespace and applies the ProjectType's resources into it.
//
// Nothing creates the binding on our behalf. The OpenChoreo API's CreateProject
// does not, and the Project controller only seeds `spec.projectRelease` on
// bindings that already exist. Before 1.2.0 the project controller created the
// namespace lazily on first deployment, so AEP never had to think about it; on
// 1.2.0 a project without a binding looks completely healthy — Created=True,
// Ready=True, a ProjectRelease in place — and then every component deploy into
// it fails with "namespace ... not found".
//
// Cell bindings follow each pipeline's promotion order only — read the pipeline,
// not every Environment in the namespace.
//
// It was written while the generated `gen` client's spec pin predated all of
// this (see services/aep-api/Makefile, OC_SPEC_VERSION), so it is hand-rolled
// over the same authenticated transport, exactly like ResourceClient.
type ProjectCellClient interface {
	// ListPipelineNames returns the names of every DeploymentPipeline in the
	// namespace. Used when no project is in scope to resolve the org's own
	// pipeline by convention (the "default" pipeline setup creates, or the
	// sole pipeline present) — see provisioning.PipelineLister.
	ListPipelineNames(ctx context.Context, namespace string) ([]string, error)

	// PipelineEnvironments returns the environment names a deployment pipeline
	// promotes through, in promotion order and de-duplicated.
	//
	// Read from the pipeline rather than simply listing every Environment in
	// the namespace: on a converged cluster the namespace also carries
	// environments belonging to another platform, and binding a project into
	// those would provision cell namespaces nothing will ever deploy to.
	PipelineEnvironments(ctx context.Context, namespace, pipelineName string) ([]string, error)

	// EnsureProjectReleaseBinding get-or-creates the binding for one
	// (project, environment). Idempotent: an existing binding is left exactly
	// as it is, because `spec.projectRelease` is the promote pin — the Project
	// controller seeds it once and advancing it afterwards is a deliberate
	// promotion. Re-asserting our own release-less body over it would silently
	// undo that.
	EnsureProjectReleaseBinding(ctx context.Context, namespace, projectName, environment string) error

	// ProjectReadiness reads the Project's Ready condition: True once its
	// (Cluster)ProjectType resolved and a ProjectRelease is in place.
	ProjectReadiness(ctx context.Context, namespace, projectName string) (Readiness, error)

	// ProjectReleaseBindingReadiness reads the Ready condition of the binding
	// EnsureProjectReleaseBinding authored for (project, environment): True
	// once the cell namespace exists and is healthy on the data plane.
	ProjectReleaseBindingReadiness(ctx context.Context, namespace, projectName, environment string) (Readiness, error)
}

// ReasonProjectTypeNotFound is the Project Ready reason OpenChoreo reports
// when the referenced (Cluster)ProjectType does not exist.
const ReasonProjectTypeNotFound = "ProjectTypeNotFound"

// Readiness is a resource's Ready condition as OpenChoreo reports it. The zero
// value is a resource the controller has not reported on yet.
type Readiness struct {
	Ready   bool
	Reason  string
	Message string
}

// conditionedObject is the read-only slice of any CR needed for its Ready
// condition.
type conditionedObject struct {
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

func (o *conditionedObject) readiness() Readiness {
	for _, c := range o.Status.Conditions {
		if c.Type == "Ready" {
			return Readiness{Ready: c.Status == "True", Reason: c.Reason, Message: c.Message}
		}
	}
	return Readiness{}
}

// ProjectReleaseBinding is the slice of the CR this client authors and reads.
type ProjectReleaseBinding struct {
	APIVersion string                    `json:"apiVersion,omitempty"`
	Kind       string                    `json:"kind,omitempty"`
	Metadata   OCObjectMeta              `json:"metadata"`
	Spec       ProjectReleaseBindingSpec `json:"spec"`
}

// ProjectReleaseBindingSpec omits `projectRelease` deliberately — see
// EnsureProjectReleaseBinding. Leaving it unset is what tells the Project
// controller to seed the pin with the project's latest release.
type ProjectReleaseBindingSpec struct {
	Owner       ProjectReleaseBindingOwner `json:"owner"`
	Environment string                     `json:"environment"`
}

type ProjectReleaseBindingOwner struct {
	ProjectName string `json:"projectName"`
}

// deploymentPipeline is the read-only slice of the CR needed to enumerate
// environments.
type deploymentPipeline struct {
	Spec struct {
		PromotionPaths []struct {
			SourceEnvironmentRef struct {
				Name string `json:"name"`
			} `json:"sourceEnvironmentRef"`
			TargetEnvironmentRefs []struct {
				Name string `json:"name"`
			} `json:"targetEnvironmentRefs"`
		} `json:"promotionPaths"`
	} `json:"spec"`
}

// deploymentPipelineList is the read-only slice of the paginated list needed
// to enumerate pipeline names.
type deploymentPipelineList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"items"`
}

type projectCellClient struct {
	baseURL string
	http    resourceHTTPDoer
	editor  func(ctx context.Context, req *http.Request) error
	// labels are stamped on every write (Config.ResourceLabels).
	labels resourceLabels
}

func newProjectCellClient(cfg Config) *projectCellClient {
	if cfg.BaseURL == "" {
		panic(errors.New("init openchoreo project cell client: Config.BaseURL is required"))
	}
	inner := &http.Client{Transport: httpx.WrapTransport(nil)}
	return &projectCellClient{
		baseURL: cfg.BaseURL,
		http:    requests.NewRetryableHTTPClient(inner, buildRetryConfig(cfg)),
		editor:  authRequestEditor(cfg),
		labels:  newResourceLabels(cfg.ResourceLabels),
	}
}

func NewProjectCellClient(cfg Config) ProjectCellClient {
	return newProjectCellClient(cfg)
}

func (c *projectCellClient) getPipeline(ctx context.Context, namespace, pipelineName string) (*deploymentPipeline, error) {
	if namespace == "" || pipelineName == "" {
		return nil, fmt.Errorf("pipeline: namespace and pipeline name are required")
	}
	pipeline := &deploymentPipeline{}
	status, err := c.do(ctx, http.MethodGet,
		nsBase(namespace)+"/deploymentpipelines/"+pipelineName, nil, pipeline)
	if err != nil {
		if status == http.StatusNotFound {
			// do reports non-2xx as a plain error; classify the 404 so callers
			// can tell a missing pipeline from a transient failure.
			return nil, fmt.Errorf("get deployment pipeline %q: %w: %v", pipelineName, ErrNotFound, err)
		}
		return nil, fmt.Errorf("get deployment pipeline %q: %w", pipelineName, err)
	}
	return pipeline, nil
}

// do issues a single authenticated request and returns the HTTP status
// alongside the error, so callers can branch on 409/404. Mirrors
// resourceClient.do.
func (c *projectCellClient) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("openchoreo(project-cell): marshal body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return 0, fmt.Errorf("openchoreo(project-cell): build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.editor != nil {
		if err := c.editor(ctx, req); err != nil {
			return 0, fmt.Errorf("openchoreo(project-cell): auth: %w", err)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("openchoreo(project-cell): %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("openchoreo(project-cell): read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("openchoreo(project-cell): %s %s: %s: %s",
			method, path, resp.Status, humanErrorMessage(raw, resp.Status))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("openchoreo(project-cell): decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func (c *projectCellClient) ListPipelineNames(ctx context.Context, namespace string) ([]string, error) {
	if namespace == "" {
		return nil, fmt.Errorf("list pipeline names: namespace is required")
	}
	list := &deploymentPipelineList{}
	if _, err := c.do(ctx, http.MethodGet, nsBase(namespace)+"/deploymentpipelines", nil, list); err != nil {
		return nil, fmt.Errorf("list deployment pipelines: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Metadata.Name != "" {
			names = append(names, item.Metadata.Name)
		}
	}
	return names, nil
}

func (c *projectCellClient) PipelineEnvironments(ctx context.Context, namespace, pipelineName string) ([]string, error) {
	pipeline, err := c.getPipeline(ctx, namespace, pipelineName)
	if err != nil {
		return nil, err
	}

	// Promotion order, de-duplicated: a source in one path is a target in
	// another, and a fan-out path names the same target twice.
	var envs []string
	seen := map[string]bool{}
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			envs = append(envs, name)
		}
	}
	for _, path := range pipeline.Spec.PromotionPaths {
		add(path.SourceEnvironmentRef.Name)
		for _, target := range path.TargetEnvironmentRefs {
			add(target.Name)
		}
	}
	return envs, nil
}

func (c *projectCellClient) ProjectReadiness(ctx context.Context, namespace, projectName string) (Readiness, error) {
	if namespace == "" || projectName == "" {
		return Readiness{}, fmt.Errorf("project readiness: namespace and project are required")
	}
	return c.readiness(ctx, nsBase(namespace)+"/projects/"+projectName)
}

func (c *projectCellClient) ProjectReleaseBindingReadiness(ctx context.Context, namespace, projectName, environment string) (Readiness, error) {
	if namespace == "" || projectName == "" || environment == "" {
		return Readiness{}, fmt.Errorf("project release binding readiness: namespace, project and environment are required")
	}
	return c.readiness(ctx, nsBase(namespace)+"/projectreleasebindings/"+projectReleaseBindingName(projectName, environment))
}

func (c *projectCellClient) readiness(ctx context.Context, path string) (Readiness, error) {
	obj := &conditionedObject{}
	if status, err := c.do(ctx, http.MethodGet, path, nil, obj); err != nil {
		if status == http.StatusNotFound {
			// do reports non-2xx as a plain error; classify the 404 so callers
			// can tell a missing resource from a failed read.
			return Readiness{}, fmt.Errorf("read readiness: %w: %v", ErrNotFound, err)
		}
		return Readiness{}, fmt.Errorf("read readiness: %w", err)
	}
	return obj.readiness(), nil
}

// projectReleaseBindingName is the name EnsureProjectReleaseBinding gives the
// binding for (project, environment).
func projectReleaseBindingName(projectName, environment string) string {
	return projectName + "-" + environment
}

func (c *projectCellClient) EnsureProjectReleaseBinding(ctx context.Context, namespace, projectName, environment string) error {
	if namespace == "" || projectName == "" || environment == "" {
		return fmt.Errorf("ensure project release binding: namespace, project and environment are required")
	}
	name := projectReleaseBindingName(projectName, environment)
	binding := &ProjectReleaseBinding{
		APIVersion: ocResourceAPIVersion,
		Kind:       "ProjectReleaseBinding",
		Metadata: OCObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"openchoreo.dev/project":     projectName,
				"openchoreo.dev/environment": environment,
			},
		},
		Spec: ProjectReleaseBindingSpec{
			Owner:       ProjectReleaseBindingOwner{ProjectName: projectName},
			Environment: environment,
		},
	}
	c.labels.stampOC(&binding.Metadata)
	code, err := c.do(ctx, http.MethodPost, nsBase(namespace)+"/projectreleasebindings", binding, nil)
	if err == nil || code == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("ensure project release binding %q: %w", name, err)
}
