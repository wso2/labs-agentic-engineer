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
// feature's cross-feature seam (bff-component-testing.md §6): sibling features
// that consume artifacts (project, requirements, design, task, …) fake it
// here, one pattern, instead of re-rolling ad-hoc fakes per test file.
//
// FakeArtifactService is a moq-style function-field fake: set only the methods
// the test needs; an unset method panics with its name (same convention as the
// generated clients/openchoreo/mocks). Wrap it with the REAL
// artifacts.NewArtifactStore to test store-consuming code paths — the store is
// a decorator over this interface, so the decorator logic stays real.
package artifactstest

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
)

// FakeArtifactService implements the GitHub-direct artifacts.ArtifactService via
// settable function fields.
type FakeArtifactService struct {
	ListRequirementFilesFunc     func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	ListDesignFilesFunc          func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	SaveRequirementsFunc         func(ctx context.Context, orgID, projectID string, req artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error)
	SaveDesignFunc               func(ctx context.Context, orgID, projectID string, req artifacts.SaveRequest) (*artifacts.DesignSaveResult, error)
	DiscardRequirementsFunc      func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	DiscardDesignFunc            func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	ListRequirementsVersionsFunc func(ctx context.Context, orgID, projectID string) ([]artifacts.RequirementsVersionInfo, error)
	ListDesignVersionsFunc       func(ctx context.Context, orgID, projectID string) ([]artifacts.DesignVersionInfo, error)
	ListSpecVersionTagsFunc      func(ctx context.Context, orgID, projectID string) (*artifacts.TagList, error)
	LatestDesignTagFunc          func(ctx context.Context, orgID, projectID string) string
	GetRequirementsAtTagFunc     func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTagFunc           func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtCommitFunc        func(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error)
}

var _ artifacts.ArtifactService = (*FakeArtifactService)(nil)

func (f *FakeArtifactService) ListRequirementFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.ListRequirementFilesFunc == nil {
		panic("artifactstest: ListRequirementFiles called but ListRequirementFilesFunc is not set")
	}
	return f.ListRequirementFilesFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.ListDesignFilesFunc == nil {
		panic("artifactstest: ListDesignFiles called but ListDesignFilesFunc is not set")
	}
	return f.ListDesignFilesFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) SaveRequirements(ctx context.Context, orgID, projectID string, req artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error) {
	if f.SaveRequirementsFunc == nil {
		panic("artifactstest: SaveRequirements called but SaveRequirementsFunc is not set")
	}
	return f.SaveRequirementsFunc(ctx, orgID, projectID, req)
}

func (f *FakeArtifactService) SaveDesign(ctx context.Context, orgID, projectID string, req artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
	if f.SaveDesignFunc == nil {
		panic("artifactstest: SaveDesign called but SaveDesignFunc is not set")
	}
	return f.SaveDesignFunc(ctx, orgID, projectID, req)
}

func (f *FakeArtifactService) DiscardRequirements(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.DiscardRequirementsFunc == nil {
		panic("artifactstest: DiscardRequirements called but DiscardRequirementsFunc is not set")
	}
	return f.DiscardRequirementsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) DiscardDesign(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.DiscardDesignFunc == nil {
		panic("artifactstest: DiscardDesign called but DiscardDesignFunc is not set")
	}
	return f.DiscardDesignFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]artifacts.RequirementsVersionInfo, error) {
	if f.ListRequirementsVersionsFunc == nil {
		panic("artifactstest: ListRequirementsVersions called but ListRequirementsVersionsFunc is not set")
	}
	return f.ListRequirementsVersionsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]artifacts.DesignVersionInfo, error) {
	if f.ListDesignVersionsFunc == nil {
		panic("artifactstest: ListDesignVersions called but ListDesignVersionsFunc is not set")
	}
	return f.ListDesignVersionsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) ListSpecVersionTags(ctx context.Context, orgID, projectID string) (*artifacts.TagList, error) {
	if f.ListSpecVersionTagsFunc == nil {
		panic("artifactstest: ListSpecVersionTags called but ListSpecVersionTagsFunc is not set")
	}
	return f.ListSpecVersionTagsFunc(ctx, orgID, projectID)
}

func (f *FakeArtifactService) LatestDesignTag(ctx context.Context, orgID, projectID string) string {
	if f.LatestDesignTagFunc == nil {
		panic("artifactstest: LatestDesignTag called but LatestDesignTagFunc is not set")
	}
	return f.LatestDesignTagFunc(ctx, orgID, projectID)
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
