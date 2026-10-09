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
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
)

// WriteTargets answers "which environment does AEP write this project's
// resources into": the root of the project's own deployment pipeline, the same
// environment OpenChoreo auto-deploys it to. Resolved at use and never cached,
// because pipelines are edited outside AEP so there is no event to invalidate
// on (ADR-0039).
type WriteTargets interface {
	// Resolve returns the project's write target.
	Resolve(ctx context.Context, org, project string) (string, error)
	// OrgDefaultRoot is the root of the org's default pipeline, for org-scoped
	// reads with no project in hand.
	OrgDefaultRoot(ctx context.Context, org string) (string, error)
}

// ErrNoWriteTarget is a project (or org) whose pipeline yields no usable write
// target: no pipeline ref, a missing pipeline, an empty or cyclic graph, or a
// root too long for AEP's name budgets. It is a configuration fact, not a
// transient failure; retrying does not help.
type ErrNoWriteTarget struct {
	Org, Project, Pipeline string
	Cause                  error
}

func (e *ErrNoWriteTarget) Error() string {
	scope := e.Org
	if e.Project != "" {
		scope += "/" + e.Project
	}
	if e.Pipeline == "" {
		return fmt.Sprintf("no write target for %s: %v", scope, e.Cause)
	}
	return fmt.Sprintf("no write target for %s (pipeline %q): %v", scope, e.Pipeline, e.Cause)
}

func (e *ErrNoWriteTarget) Unwrap() error { return e.Cause }

type projectReader interface {
	GetProject(ctx context.Context, orgName, projectName string) (*gen.Project, error)
}

type pipelineReader interface {
	getPipeline(ctx context.Context, namespace, pipelineName string) (*deploymentPipeline, error)
	ListPipelineNames(ctx context.Context, namespace string) ([]string, error)
}

type writeTargets struct {
	projects  projectReader
	pipelines pipelineReader
}

// NewWriteTargets builds the OpenChoreo-backed resolver.
func NewWriteTargets(cfg Config) WriteTargets {
	return &writeTargets{projects: NewProjectClient(cfg), pipelines: newProjectCellClient(cfg)}
}

func (w *writeTargets) Resolve(ctx context.Context, org, project string) (string, error) {
	if org == "" || project == "" {
		return "", fmt.Errorf("resolve write target: org and project are required (org=%q project=%q)", org, project)
	}
	p, err := w.projects.GetProject(ctx, org, project)
	if err != nil {
		return "", fmt.Errorf("resolve write target: %w", err)
	}
	if p.DeploymentPipeline == "" {
		return "", &ErrNoWriteTarget{Org: org, Project: project, Cause: ErrPipelineRefMissing}
	}
	return w.rootOf(ctx, org, project, p.DeploymentPipeline)
}

// rootOf reads one pipeline and returns its root. Only configuration facts
// become ErrNoWriteTarget; a transient read failure propagates so the caller
// (a Temporal activity) can retry.
func (w *writeTargets) rootOf(ctx context.Context, org, project, pipeline string) (string, error) {
	pl, err := w.pipelines.getPipeline(ctx, org, pipeline)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", &ErrNoWriteTarget{Org: org, Project: project, Pipeline: pipeline, Cause: err}
		}
		return "", fmt.Errorf("resolve write target: %w", err)
	}
	root, err := PipelineRoot(pipeline, pl)
	if err != nil {
		return "", &ErrNoWriteTarget{Org: org, Project: project, Pipeline: pipeline, Cause: err}
	}
	if len(root) > ocname.MaxEnvNameLen {
		return "", &ErrNoWriteTarget{Org: org, Project: project, Pipeline: pipeline,
			Cause: fmt.Errorf("%q: %w", root, ErrWriteTargetTooLong)}
	}
	return root, nil
}

func (w *writeTargets) OrgDefaultRoot(ctx context.Context, org string) (string, error) {
	if org == "" {
		return "", errors.New("resolve org default write target: org is required")
	}
	names, err := w.pipelines.ListPipelineNames(ctx, org)
	if err != nil {
		return "", fmt.Errorf("resolve org default write target: %w", err)
	}
	name, ok := ChooseOrgDefaultPipeline(names)
	if !ok {
		return "", &ErrNoWriteTarget{Org: org,
			Cause: fmt.Errorf("no pipeline named \"default\" and not exactly one candidate: %v", names)}
	}
	return w.rootOf(ctx, org, "", name)
}
