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

// Package artifactstest holds the exported hand fakes for the artifacts
// feature's cross-feature seam: sibling features
// that consume artifacts (project, requirements, design, task, …) fake it
// here, one pattern, instead of re-rolling ad-hoc fakes per test file.
//
// FakeArtifactService is a moq-style function-field fake: set only the methods
// the test needs; an unset method panics with its name (same convention as the
// generated clients/openchoreo/mocks). Wrap it with the REAL
// spec.NewArtifactStore to test store-consuming code paths — the store is
// a decorator over this interface, so the decorator logic stays real.
package artifactstest

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// FakeArtifactService implements the GitHub-direct spec.ArtifactService via
// settable function fields.
type FakeArtifactService struct {
	ListDesignFilesFunc           func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	SaveSpecFunc                  func(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.SpecSaveResult, error)
	ValidateSpecAtTagFunc         func(ctx context.Context, orgID, projectID, tag string) error
	LatestSpecTagFunc             func(ctx context.Context, orgID, projectID string) string
	SaveRequirementsFunc          func(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.RequirementsSaveResult, error)
	SaveDesignFunc                func(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.DesignSaveResult, error)
	ListRequirementsVersionsFunc  func(ctx context.Context, orgID, projectID string) ([]spec.RequirementsVersionInfo, error)
	ListDesignVersionsFunc        func(ctx context.Context, orgID, projectID string) ([]spec.DesignVersionInfo, error)
	ListSpecVersionTagsFunc       func(ctx context.Context, orgID, projectID string) (*spec.TagList, error)
	GetRequirementsAtTagFunc      func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTagFunc            func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtCommitFunc         func(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error)
	StatusSnapshotFunc            func(ctx context.Context, orgID, projectID string) (*spec.StatusSnapshot, error)
	RequirementsFingerprintAtFunc func(ctx context.Context, orgID, projectID, at string) (string, error)
	ComponentCountAtTagFunc       func(ctx context.Context, orgID, projectID, tag string) (int, error)
}

var _ spec.ArtifactService = (*FakeArtifactService)(nil)

func (f *FakeArtifactService) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.ListDesignFilesFunc == nil {
		panic("artifactstest: ListDesignFiles called but ListDesignFilesFunc is not set")
	}
	return f.ListDesignFilesFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) SaveSpec(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.SpecSaveResult, error) {
	if f.SaveSpecFunc == nil {
		panic("artifactstest: SaveSpec called but SaveSpecFunc is not set")
	}
	return f.SaveSpecFunc(ctx, orgID, projectID, req)
}

func (f *FakeArtifactService) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (spec.BuildScope, error) {
	return spec.BuildScope{Tag: tag}, nil
}

func (f *FakeArtifactService) ValidateSpecAtTag(ctx context.Context, orgID, projectID, tag string) error {
	if f.ValidateSpecAtTagFunc == nil {
		panic("artifactstest: ValidateSpecAtTag called but ValidateSpecAtTagFunc is not set")
	}
	return f.ValidateSpecAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *FakeArtifactService) LatestSpecTag(ctx context.Context, orgID, projectID string) string {
	if f.LatestSpecTagFunc == nil {
		panic("artifactstest: LatestSpecTag called but LatestSpecTagFunc is not set")
	}
	return f.LatestSpecTagFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) SaveRequirements(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.RequirementsSaveResult, error) {
	if f.SaveRequirementsFunc == nil {
		panic("artifactstest: SaveRequirements called but SaveRequirementsFunc is not set")
	}
	return f.SaveRequirementsFunc(ctx, orgID, projectID, req)
}

func (f *FakeArtifactService) SaveDesign(ctx context.Context, orgID, projectID string, req spec.SaveRequest) (*spec.DesignSaveResult, error) {
	if f.SaveDesignFunc == nil {
		panic("artifactstest: SaveDesign called but SaveDesignFunc is not set")
	}
	return f.SaveDesignFunc(ctx, orgID, projectID, req)
}

func (f *FakeArtifactService) ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]spec.RequirementsVersionInfo, error) {
	if f.ListRequirementsVersionsFunc == nil {
		panic("artifactstest: ListRequirementsVersions called but ListRequirementsVersionsFunc is not set")
	}
	return f.ListRequirementsVersionsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]spec.DesignVersionInfo, error) {
	if f.ListDesignVersionsFunc == nil {
		panic("artifactstest: ListDesignVersions called but ListDesignVersionsFunc is not set")
	}
	return f.ListDesignVersionsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListSpecVersionTags(ctx context.Context, orgID, projectID string) (*spec.TagList, error) {
	if f.ListSpecVersionTagsFunc == nil {
		panic("artifactstest: ListSpecVersionTags called but ListSpecVersionTagsFunc is not set")
	}
	return f.ListSpecVersionTagsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if f.GetRequirementsAtTagFunc == nil {
		panic("artifactstest: GetRequirementsAtTag called but GetRequirementsAtTagFunc is not set")
	}
	return f.GetRequirementsAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *FakeArtifactService) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if f.GetDesignAtTagFunc == nil {
		panic("artifactstest: GetDesignAtTag called but GetDesignAtTagFunc is not set")
	}
	return f.GetDesignAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *FakeArtifactService) GetDesignAtCommit(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error) {
	if f.GetDesignAtCommitFunc == nil {
		panic("artifactstest: GetDesignAtCommit called but GetDesignAtCommitFunc is not set")
	}
	return f.GetDesignAtCommitFunc(ctx, orgID, projectID, commitSHA)
}

func (f *FakeArtifactService) StatusSnapshot(ctx context.Context, orgID, projectID string) (*spec.StatusSnapshot, error) {
	if f.StatusSnapshotFunc == nil {
		panic("artifactstest: StatusSnapshot called but StatusSnapshotFunc is not set")
	}
	return f.StatusSnapshotFunc(ctx, orgID, projectID)
}

// RequirementsFingerprintAt defaults to the EMPTY fingerprint rather than
// panicking: most callers never reach the staleness check (it needs a design
// run on record), and a fake that panics on an unexercised path would fail
// tests for a question they never asked.
func (f *FakeArtifactService) RequirementsFingerprintAt(ctx context.Context, orgID, projectID, at string) (string, error) {
	if f.RequirementsFingerprintAtFunc == nil {
		return "", nil
	}
	return f.RequirementsFingerprintAtFunc(ctx, orgID, projectID, at)
}

func (f *FakeArtifactService) ComponentCountAtTag(ctx context.Context, orgID, projectID, tag string) (int, error) {
	if f.ComponentCountAtTagFunc == nil {
		panic("artifactstest: ComponentCountAtTag called but ComponentCountAtTagFunc is not set")
	}
	return f.ComponentCountAtTagFunc(ctx, orgID, projectID, tag)
}

// SetDesignBaselineResolver is wiring, not behaviour — the fakes ignore it.
func (f *FakeArtifactService) SetDesignBaselineResolver(func(ctx context.Context, orgID, projectID string) (string, error)) {
}
