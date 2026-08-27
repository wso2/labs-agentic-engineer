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

// fakeArtifactSvc is the in-package twin of artifactstest.FakeArtifactService.
// The design tests (folded into this package in P4) exercise a moq-style
// ArtifactService fake, but the spec package cannot import its own
// spec/artifactstest sub-package from an INTERNAL test file (spec_test → spec
// import cycle). The exported fake stays in artifactstest for the still-legacy
// external consumers (component / project / runtimeconfig tests); this internal
// twin serves spec's own tests. Both collapse into one when those consumers
// become domains (P9). Set only the methods the test needs; an unset method
// panics with its name.

import "context"

type fakeArtifactSvc struct {
	ListDesignFilesFunc          func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	SaveSpecFunc                 func(ctx context.Context, orgID, projectID string, req SaveRequest) (*SpecSaveResult, error)
	ValidateSpecAtTagFunc        func(ctx context.Context, orgID, projectID, tag string) error
	LatestSpecTagFunc            func(ctx context.Context, orgID, projectID string) string
	SaveRequirementsFunc         func(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error)
	SaveDesignFunc               func(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error)
	ListRequirementsVersionsFunc func(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error)
	ListDesignVersionsFunc       func(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error)
	ListSpecVersionTagsFunc      func(ctx context.Context, orgID, projectID string) (*TagList, error)
	GetRequirementsAtTagFunc     func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTagFunc           func(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtCommitFunc        func(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error)
	StatusSnapshotFunc           func(ctx context.Context, orgID, projectID string) (*StatusSnapshot, error)
	ComponentCountAtTagFunc      func(ctx context.Context, orgID, projectID, tag string) (int, error)
}

var _ ArtifactService = (*fakeArtifactSvc)(nil)

func (f *fakeArtifactSvc) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if f.ListDesignFilesFunc == nil {
		panic("spec test: ListDesignFiles called but ListDesignFilesFunc is not set")
	}
	return f.ListDesignFilesFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) SaveSpec(ctx context.Context, orgID, projectID string, req SaveRequest) (*SpecSaveResult, error) {
	if f.SaveSpecFunc == nil {
		panic("spec test: SaveSpec called but SaveSpecFunc is not set")
	}
	return f.SaveSpecFunc(ctx, orgID, projectID, req)
}

func (f *fakeArtifactSvc) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (BuildScope, error) {
	return BuildScope{Tag: tag}, nil
}

func (f *fakeArtifactSvc) ValidateSpecAtTag(ctx context.Context, orgID, projectID, tag string) error {
	if f.ValidateSpecAtTagFunc == nil {
		panic("spec test: ValidateSpecAtTag called but ValidateSpecAtTagFunc is not set")
	}
	return f.ValidateSpecAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *fakeArtifactSvc) LatestSpecTag(ctx context.Context, orgID, projectID string) string {
	if f.LatestSpecTagFunc == nil {
		panic("spec test: LatestSpecTag called but LatestSpecTagFunc is not set")
	}
	return f.LatestSpecTagFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) SaveRequirements(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error) {
	if f.SaveRequirementsFunc == nil {
		panic("spec test: SaveRequirements called but SaveRequirementsFunc is not set")
	}
	return f.SaveRequirementsFunc(ctx, orgID, projectID, req)
}

func (f *fakeArtifactSvc) SaveDesign(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error) {
	if f.SaveDesignFunc == nil {
		panic("spec test: SaveDesign called but SaveDesignFunc is not set")
	}
	return f.SaveDesignFunc(ctx, orgID, projectID, req)
}

func (f *fakeArtifactSvc) ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error) {
	if f.ListRequirementsVersionsFunc == nil {
		panic("spec test: ListRequirementsVersions called but ListRequirementsVersionsFunc is not set")
	}
	return f.ListRequirementsVersionsFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error) {
	if f.ListDesignVersionsFunc == nil {
		panic("spec test: ListDesignVersions called but ListDesignVersionsFunc is not set")
	}
	return f.ListDesignVersionsFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) ListSpecVersionTags(ctx context.Context, orgID, projectID string) (*TagList, error) {
	if f.ListSpecVersionTagsFunc == nil {
		panic("spec test: ListSpecVersionTags called but ListSpecVersionTagsFunc is not set")
	}
	return f.ListSpecVersionTagsFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if f.GetRequirementsAtTagFunc == nil {
		panic("spec test: GetRequirementsAtTag called but GetRequirementsAtTagFunc is not set")
	}
	return f.GetRequirementsAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *fakeArtifactSvc) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if f.GetDesignAtTagFunc == nil {
		panic("spec test: GetDesignAtTag called but GetDesignAtTagFunc is not set")
	}
	return f.GetDesignAtTagFunc(ctx, orgID, projectID, tag)
}

// GetDesignAtSpecTag reads the design at a `v<N>` SPEC tag; the fake shares the
// one hook, since nothing in these tests distinguishes the two tag shapes.
func (f *fakeArtifactSvc) GetDesignAtSpecTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if f.GetDesignAtTagFunc == nil {
		panic("spec test: GetDesignAtSpecTag called but GetDesignAtTagFunc is not set")
	}
	return f.GetDesignAtTagFunc(ctx, orgID, projectID, tag)
}

func (f *fakeArtifactSvc) GetDesignAtCommit(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error) {
	if f.GetDesignAtCommitFunc == nil {
		panic("spec test: GetDesignAtCommit called but GetDesignAtCommitFunc is not set")
	}
	return f.GetDesignAtCommitFunc(ctx, orgID, projectID, commitSHA)
}

func (f *fakeArtifactSvc) StatusSnapshot(ctx context.Context, orgID, projectID string) (*StatusSnapshot, error) {
	if f.StatusSnapshotFunc == nil {
		panic("spec test: StatusSnapshot called but StatusSnapshotFunc is not set")
	}
	return f.StatusSnapshotFunc(ctx, orgID, projectID)
}

func (f *fakeArtifactSvc) RequirementsFingerprintAt(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (f *fakeArtifactSvc) ComponentCountAtTag(ctx context.Context, orgID, projectID, tag string) (int, error) {
	if f.ComponentCountAtTagFunc == nil {
		panic("spec test: ComponentCountAtTag called but ComponentCountAtTagFunc is not set")
	}
	return f.ComponentCountAtTagFunc(ctx, orgID, projectID, tag)
}

// SetDesignBaselineResolver is wiring, not behaviour — the fakes ignore it.
func (f *fakeArtifactSvc) SetDesignBaselineResolver(func(ctx context.Context, orgID, projectID string) (string, error)) {
}
