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

package design

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
)

// stubArtifactService is a minimal ArtifactService stub for design_service
// unit tests. All methods return sensible zero values unless overridden by
// the test via the listDesignFilesFunc field.
type stubArtifactService struct {
	listDesignFilesFunc func(ctx context.Context, orgID, projectID string) (map[string]string, error)
}

func (s *stubArtifactService) GetFile(_ context.Context, _, _, _ string) (*artifacts.FileResult, error) {
	return nil, errors.New("stub: GetFile not implemented")
}
func (s *stubArtifactService) PutFile(_ context.Context, _, _, _, _, _ string) (*artifacts.PutResult, error) {
	return nil, errors.New("stub: PutFile not implemented")
}
func (s *stubArtifactService) ListRequirementFiles(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) DeleteRequirementFile(_ context.Context, _, _, _ string) error {
	return nil
}
func (s *stubArtifactService) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	if s.listDesignFilesFunc != nil {
		return s.listDesignFilesFunc(ctx, orgID, projectID)
	}
	return nil, nil
}
func (s *stubArtifactService) DeleteDesignFile(_ context.Context, _, _, _ string) error { return nil }
func (s *stubArtifactService) DeleteDesignDirectory(_ context.Context, _, _, _ string) error {
	return nil
}
func (s *stubArtifactService) CommitDesignFile(_ context.Context, _, _, _, _, _ string) (string, error) {
	return "", nil
}
func (s *stubArtifactService) SaveRequirements(_ context.Context, _, _ string, _ artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error) {
	return nil, errors.New("stub: SaveRequirements not implemented")
}
func (s *stubArtifactService) SaveDesign(_ context.Context, _, _ string, _ artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
	return nil, errors.New("stub: SaveDesign not implemented")
}
func (s *stubArtifactService) DiscardRequirements(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) DiscardDesign(_ context.Context, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) CaptureRequirementsSnapshot(_ context.Context, _, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) RestoreRequirementsSnapshot(_ context.Context, _, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) DeleteRequirementsSnapshot(_ context.Context, _, _, _ string) error {
	return nil
}
func (s *stubArtifactService) ReadFileFromRequirementsSnapshot(_ context.Context, _, _, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (s *stubArtifactService) ListRequirementsVersions(_ context.Context, _, _ string) ([]artifacts.RequirementsVersionInfo, error) {
	return nil, nil
}
func (s *stubArtifactService) ListDesignVersions(_ context.Context, _, _ string) ([]artifacts.DesignVersionInfo, error) {
	return nil, nil
}
func (s *stubArtifactService) GetRequirementsAtTag(_ context.Context, _, _, _ string) (map[string]string, error) {
	return nil, nil
}
func (s *stubArtifactService) GetDesignAtTag(_ context.Context, _, _, _ string) (map[string]string, error) {
	return nil, nil
}

// TestSaveAndProceedBlockedByUnresolvedExternalDep asserts that SaveAndProceed
// returns ErrUnresolvedDependency when the design contains an external
// dependency with needsSpec=true and no specPath (computed as unresolved at
// read time by assembleDependencies). This covers the broadened save-gate:
// ANY unresolved dep blocks save, not just org-service.
func TestSaveAndProceedBlockedByUnresolvedExternalDep(t *testing.T) {
	// Build the file map directly so we control the frontmatter precisely.
	compDesignMd := `---
type: service
language: go
dependencies:
  - kind: external
    name: openweather
    description: weather api
    needsSpec: true
---
Build a weather service.
`
	files := map[string]string{
		"design.md":                    "System overview.\n",
		"components/weather-api/design.md": compDesignMd,
	}

	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return files, nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, saveErr := svc.SaveAndProceed(context.Background(), "org1", "proj1")
	if saveErr == nil {
		t.Fatal("want error from SaveAndProceed, got nil")
	}
	if !errors.Is(saveErr, ErrUnresolvedDependency) {
		t.Fatalf("want ErrUnresolvedDependency, got: %v", saveErr)
	}
}

// TestComponentNameFromDesignPath — only `components/<name>/design.md`
// triggers trait_sync; root design.md and openapi.yaml are ignored. Gate
// for the design-edit write site (componentNameFromDesignPath lives in
// design_service.go).
func TestComponentNameFromDesignPath(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantOK   bool
	}{
		{"components/svc/design.md", "svc", true},
		{"components/user-api/design.md", "user-api", true},
		{"design.md", "", false},
		{"components/svc/openapi.yaml", "", false},
		{"components//design.md", "", false},
		{"components/a/b/design.md", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		name, ok := componentNameFromDesignPath(c.in)
		if name != c.wantName || ok != c.wantOK {
			t.Errorf("componentNameFromDesignPath(%q) = (%q,%v), want (%q,%v)",
				c.in, name, ok, c.wantName, c.wantOK)
		}
	}
}
