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

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// crtTypeCatalog adapts the dependencies resource-type catalog onto spec's
// own CRTType vocabulary: design-save's resourceTypeCatalog port returns
// spec.CRTType, so the spec domain names the dependencies feature nowhere
// (the "a domain names no other domain's entity, even in a port" rule). It is
// the projection point — dependencies becomes a domain in P8; this stays a port.
type crtTypeCatalog struct {
	cat *dependencies.ResourceTypeCatalog
}

func (c crtTypeCatalog) ResourceTypesByName(ctx context.Context) (map[string]spec.CRTType, error) {
	types, err := c.cat.TypesByName(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]spec.CRTType, len(types))
	for k, v := range types {
		out[k] = spec.CRTType{
			EndUserAuth:          v.Markers.EndUserAuth,
			ConsumerURLEnvConfig: v.Markers.ConsumerURLEnvConfig,
			ConsumerURLPath:      v.Markers.ConsumerURLPath,
			Skill:                v.Markers.Skill,
			Description:          v.Description,
			Outputs:              v.Outputs,
		}
	}
	return out, nil
}

// designFilesCommitter adapts the project repository to design's narrow
// designFileCommitter port — the committed-truth single-commit write surface
// the design service uses to persist a dependency's contract + its
// dependency.json (and the user's acceptance of an assumed contract) atomically
// to main, through the org's AE Studio pod. The design service's writes are
// already complete files, so they commit raw: no scaffolding, completion or
// soft validation runs on them (the pod runs those for the Room's
// edits). It lives at the composition root so the design feature names no
// repository port (arch boundary).
type designFilesCommitter struct {
	git   sourcecontrol.Git
	repos sourcecontrol.ProjectRepoRows
}

// workloadReader is the eventcore wiring-conformance check's file read: the
// shipped workload.yaml at HEAD, projected onto the narrower shape eventcore
// holds (no CAS token — the check never writes).
type workloadReader struct {
	projectFiles
}

func (a workloadReader) ReadFile(ctx context.Context, orgID, projectID, path string) (string, bool, error) {
	content, _, found, err := a.readFile(ctx, orgID, projectID, "", path)
	return content, found, err
}

// ReadFile returns a file's current content + blob sha (the CAS token). A file
// absent at HEAD is reported as ok=false with no error (a fresh spec create).
func (a designFilesCommitter) ReadFile(ctx context.Context, orgID, projectID, path string) (content, sha string, ok bool, err error) {
	return projectFiles{git: a.git, repos: a.repos}.readFile(ctx, orgID, projectID, "", path)
}

// Commit writes every file in one commit on main, each under its own baseSha
// (the sha the design service read; "" = the file must not exist yet). A
// baseSha that no longer holds (a concurrent design edit) is
// spec.ErrSpecCommitConflict so the route can 409; it is not retried, since
// the caller's baseSha is the point.
func (a designFilesCommitter) Commit(ctx context.Context, orgID, projectID string, writes []spec.DesignFileWrite, message string) error {
	ref, _, err := sourcecontrol.RepoRefFor(ctx, a.repos, orgID, projectID)
	if err != nil {
		return err
	}
	req := sourcecontrol.CommitRequest{Message: message}
	for _, w := range writes {
		req.Writes = append(req.Writes, sourcecontrol.FileWrite{Path: w.Path, Content: w.Content, BaseSHA: w.BaseSHA})
	}
	if _, err := a.git.Commit(ctx, ref, req); err != nil {
		if errors.Is(err, sourcecontrol.ErrCommitConflict) {
			return fmt.Errorf("%w: %w", spec.ErrSpecCommitConflict, err)
		}
		return err
	}
	return nil
}
