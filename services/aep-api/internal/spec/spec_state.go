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

package spec

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
)

// GET /projects/{p}/spec/state (N5): what the spec workspace needs that its
// documents do not say.

// SpecState is the spec workspace's state beside its documents.
type SpecState struct {
	// DesignedFrom maps each designed feature to the basis its last design
	// read (reqspec.Basis), by the reading the build gate's staleness check
	// uses, so the console's "out of date" and the gate's cannot disagree.
	DesignedFrom map[string]string
	// Documents are the names of the source documents the user attached.
	Documents []string
}

// SpecState reads each designed feature's basis at the commit its last design
// read, and the attached documents' names.
func (s *artifactService) SpecState(ctx context.Context, orgID, projectID string) (SpecState, error) {
	out := SpecState{DesignedFrom: map[string]string{}}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return out, err
	}
	names, err := s.git.Workspace().ListReferences(ctx, ref)
	if err != nil {
		return out, fmt.Errorf("list references: %w", err)
	}
	out.Documents = names
	if s.designRuns == nil {
		return out, nil
	}
	runs, err := s.designRuns(ctx, orgID, projectID)
	if err != nil {
		return out, fmt.Errorf("list design runs: %w", err)
	}
	if len(runs) == 0 {
		return out, nil
	}
	reqFiles, err := s.readBundleAt(ctx, ref, "", requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return out, fmt.Errorf("read requirements: %w", err)
	}
	read := map[string]map[string]string{}
	filesAt := func(commit string) map[string]string {
		if files, ok := read[commit]; ok {
			return files
		}
		files, rerr := s.readBundleAtCommit(ctx, ref, commit, requirementsPrefix, requirementsBundleFilter)
		if rerr != nil {
			slog.WarnContext(ctx, "spec state: a design run's commit is unreadable", "project", projectID, "base", commit, "error", rerr)
			files = nil
		}
		read[commit] = files
		return files
	}
	for _, f := range reqspec.Parse(reqFiles).Features {
		if was := designedFrom(runs, f.ID, filesAt); was != nil {
			out.DesignedFrom[f.ID] = reqspec.Basis(was, f.ID)
		}
	}
	return out, nil
}
