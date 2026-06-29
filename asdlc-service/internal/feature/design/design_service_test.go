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
	"strings"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
)

// stubArtifactService is a minimal ArtifactService stub for design_service
// unit tests. All methods return sensible zero values unless overridden by
// the test via the hook fields.
type stubArtifactService struct {
	listDesignFilesFunc    func(ctx context.Context, orgID, projectID string) (map[string]string, error)
	putFileFunc            func(ctx context.Context, orgID, projectID, path, content, sha string) (*artifacts.PutResult, error)
	commitDesignFileFunc   func(ctx context.Context, orgID, projectID, subPath, content, msg string) (string, error)
	commitDesignFileCalls  []commitDesignFileCall
}

type commitDesignFileCall struct {
	subPath string
	content string
	msg     string
}

func (s *stubArtifactService) GetFile(_ context.Context, _, _, _ string) (*artifacts.FileResult, error) {
	return nil, errors.New("stub: GetFile not implemented")
}
func (s *stubArtifactService) PutFile(ctx context.Context, orgID, projectID, path, content, sha string) (*artifacts.PutResult, error) {
	if s.putFileFunc != nil {
		return s.putFileFunc(ctx, orgID, projectID, path, content, sha)
	}
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
func (s *stubArtifactService) CommitDesignFile(ctx context.Context, orgID, projectID, subPath, content, msg string) (string, error) {
	s.commitDesignFileCalls = append(s.commitDesignFileCalls, commitDesignFileCall{
		subPath: subPath,
		content: content,
		msg:     msg,
	})
	if s.commitDesignFileFunc != nil {
		return s.commitDesignFileFunc(ctx, orgID, projectID, subPath, content, msg)
	}
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

// blockedDepFiles returns a design file map with one component having a
// blocked org-service dependency (status embedded in frontmatter for test
// isolation — no live resolver needed).
func blockedDepFiles() map[string]string {
	compDesignMd := `---
type: service
language: go
dependencies:
  - kind: org-service
    name: payroll-internal
    status: blocked
    reason: access-required
---
A consumer that needs access to payroll-internal.
`
	return map[string]string{
		"design.md":                          "System overview.\n",
		"components/consumer-app/design.md":  compDesignMd,
	}
}

// TestSaveAndProceedBlockedByBlockedDep (test d) asserts that SaveAndProceed
// returns ErrUnresolvedDependency when the design contains a dep with
// status="blocked" (project-only org-service, access not yet granted).
func TestSaveAndProceedBlockedByBlockedDep(t *testing.T) {
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return blockedDepFiles(), nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, saveErr := svc.SaveAndProceed(context.Background(), "org1", "proj1")
	if saveErr == nil {
		t.Fatal("want error from SaveAndProceed for blocked dep, got nil")
	}
	if !errors.Is(saveErr, ErrUnresolvedDependency) {
		t.Fatalf("want ErrUnresolvedDependency for blocked dep, got: %v", saveErr)
	}
}

// TestSaveAndProceedBlockedByAmbiguousDep (test e) asserts that SaveAndProceed
// returns ErrUnresolvedDependency when the design contains a dep with
// status="ambiguous".
func TestSaveAndProceedBlockedByAmbiguousDep(t *testing.T) {
	compDesignMd := `---
type: service
language: go
dependencies:
  - kind: org-service
    name: maybe-svc
    status: ambiguous
---
Ambiguous dependency.
`
	files := map[string]string{
		"design.md":                        "System overview.\n",
		"components/consumer-app/design.md": compDesignMd,
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
		t.Fatal("want error from SaveAndProceed for ambiguous dep, got nil")
	}
	if !errors.Is(saveErr, ErrUnresolvedDependency) {
		t.Fatalf("want ErrUnresolvedDependency for ambiguous dep, got: %v", saveErr)
	}
}

// TestUpdateDesignFileSucceedsWithBlockedDep (test f) asserts that the draft
// autosave path (UpdateDesignFile) does NOT gate on dep status — a design
// with a blocked dep can still be draft-saved.
func TestUpdateDesignFileSucceedsWithBlockedDep(t *testing.T) {
	files := blockedDepFiles()
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return files, nil
		},
		putFileFunc: func(_ context.Context, _, _, _, _, _ string) (*artifacts.PutResult, error) {
			return &artifacts.PutResult{SHA: "abc123"}, nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, err := svc.UpdateDesignFile(context.Background(), "org1", "proj1", "design.md", "Updated overview.\n")
	if err != nil {
		t.Fatalf("UpdateDesignFile must succeed for blocked dep (draft autosave not gated), got: %v", err)
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

// sampleOpenAPISpec is a minimal 3-operation OpenAPI spec used by
// TestCollectSpec tests below.
const sampleOpenAPISpec = `openapi: 3.0.3
info:
  title: OpenWeather API
  version: "1.0"
paths:
  /weather:
    get:
      summary: Current weather
      responses:
        "200":
          description: OK
  /forecast:
    get:
      summary: Forecast
      responses:
        "200":
          description: OK
    post:
      summary: Request forecast
      responses:
        "200":
          description: OK
`

// TestCollectSpec_RawSpecReturnsSpecPathAndOpCount asserts that CollectSpec
// with a rawSpec argument stores the spec (via StoreConsumedSpec), sets the
// specPath on the dependency (via SetDependencySpecPath), and returns the
// component-relative specPath + operation count. Two CommitDesignFile calls
// are expected: one from StoreConsumedSpec for the spec blob, one from
// SetDependencySpecPath for the component's design.md.
func TestCollectSpec_RawSpecReturnsSpecPathAndOpCount(t *testing.T) {
	compDesignMd := `---
type: service
language: go
dependencies:
  - kind: external
    name: openweather
    description: weather API
    needsSpec: true
---
Build a weather service.
`
	files := map[string]string{
		"design.md":                        "System overview.\n",
		"components/weather-api/design.md": compDesignMd,
	}

	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return files, nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	specPath, opCount, err := svc.CollectSpec(
		context.Background(), "org1", "proj1",
		"weather-api", "openweather",
		sampleOpenAPISpec, "",
	)
	if err != nil {
		t.Fatalf("CollectSpec returned unexpected error: %v", err)
	}
	wantSpecPath := "dependencies/openweather.openapi.yaml"
	if specPath != wantSpecPath {
		t.Errorf("specPath = %q, want %q", specPath, wantSpecPath)
	}
	if opCount != 3 {
		t.Errorf("operationCount = %d, want 3", opCount)
	}
	// Two CommitDesignFile calls: spec blob + design.md with specPath.
	if len(stub.commitDesignFileCalls) != 2 {
		t.Fatalf("expected 2 CommitDesignFile calls, got %d", len(stub.commitDesignFileCalls))
	}
	// First call: spec blob.
	firstCall := stub.commitDesignFileCalls[0]
	wantSpecSubPath := "components/weather-api/dependencies/openweather.openapi.yaml"
	if firstCall.subPath != wantSpecSubPath {
		t.Errorf("first CommitDesignFile subPath = %q, want %q", firstCall.subPath, wantSpecSubPath)
	}
	// Second call: design.md with specPath.
	secondCall := stub.commitDesignFileCalls[1]
	wantDesignSubPath := "components/weather-api/design.md"
	if secondCall.subPath != wantDesignSubPath {
		t.Errorf("second CommitDesignFile subPath = %q, want %q", secondCall.subPath, wantDesignSubPath)
	}
	if !strings.Contains(secondCall.content, "specPath:") {
		t.Errorf("updated design.md should contain specPath, got:\n%s", secondCall.content)
	}
}

// TestCollectSpec_BothFieldsReturns400 asserts that providing both rawSpec
// and specURL returns an error (validation: exactly one must be set).
func TestCollectSpec_BothFieldsReturns400(t *testing.T) {
	stub := &stubArtifactService{}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, _, err := svc.CollectSpec(
		context.Background(), "org1", "proj1",
		"comp", "dep",
		sampleOpenAPISpec, "https://example.com/openapi.yaml",
	)
	if err == nil {
		t.Fatal("expected error when both rawSpec and specURL provided, got nil")
	}
}

// TestCollectSpec_NeitherFieldReturns400 asserts that providing neither
// rawSpec nor specURL returns an error.
func TestCollectSpec_NeitherFieldReturns400(t *testing.T) {
	stub := &stubArtifactService{}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, _, err := svc.CollectSpec(
		context.Background(), "org1", "proj1",
		"comp", "dep",
		"", "",
	)
	if err == nil {
		t.Fatal("expected error when neither rawSpec nor specURL provided, got nil")
	}
}
