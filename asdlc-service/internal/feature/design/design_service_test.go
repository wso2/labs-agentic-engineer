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
// component-relative specPath + operation count. Both operations write to the
// working-tree draft via PutFile (NOT CommitDesignFile), so the spec and the
// updated design.md are committed atomically by the subsequent SaveDesign.
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
	// Use a mutable map so that PutFile writes are visible to subsequent
	// ListDesignFiles calls (SetDependencySpecPath reads the design after
	// StoreConsumedSpec writes the spec blob).
	files := map[string]string{
		"design.md":                        "System overview.\n",
		"components/weather-api/design.md": compDesignMd,
	}

	var putCalls []struct{ relPath, content string }
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			// Return a copy so mutations inside WriteDesignFile don't race.
			cp := make(map[string]string, len(files))
			for k, v := range files {
				cp[k] = v
			}
			return cp, nil
		},
		putFileFunc: func(_ context.Context, _, _, relPath, content, _ string) (*artifacts.PutResult, error) {
			putCalls = append(putCalls, struct{ relPath, content string }{relPath, content})
			// Reflect the write back into files so SetDependencySpecPath's
			// ReadDesign sees the updated design.md written by itself.
			const prefix = "specs/design/"
			if key := strings.TrimPrefix(relPath, prefix); key != relPath {
				files[key] = content
			}
			return &artifacts.PutResult{SHA: "abc"}, nil
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
	// Two PutFile calls: spec blob + design.md with specPath.
	if len(putCalls) != 2 {
		t.Fatalf("expected 2 PutFile calls (spec blob + design.md), got %d", len(putCalls))
	}
	// First call: spec blob.
	wantSpecRelPath := "specs/design/components/weather-api/dependencies/openweather.openapi.yaml"
	if putCalls[0].relPath != wantSpecRelPath {
		t.Errorf("first PutFile relPath = %q, want %q", putCalls[0].relPath, wantSpecRelPath)
	}
	// Second call: design.md with specPath.
	wantDesignRelPath := "specs/design/components/weather-api/design.md"
	if putCalls[1].relPath != wantDesignRelPath {
		t.Errorf("second PutFile relPath = %q, want %q", putCalls[1].relPath, wantDesignRelPath)
	}
	if !strings.Contains(putCalls[1].content, "specPath:") {
		t.Errorf("updated design.md should contain specPath, got:\n%s", putCalls[1].content)
	}
	// REGRESSION: neither StoreConsumedSpec nor SetDependencySpecPath must
	// call CommitDesignFile — both must write to the draft only.
	if len(stub.commitDesignFileCalls) != 0 {
		t.Errorf("neither CollectSpec sub-call must use CommitDesignFile (regression guard): got %d calls", len(stub.commitDesignFileCalls))
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

// TestCollectSpec_InvalidOpenAPIDocMapsToErrInvalidSpec asserts that supplying
// a rawSpec that is not a valid OpenAPI 3.x document surfaces ErrInvalidSpec
// (which the HTTP handler maps to 400). This tests the validation-class error
// path from StoreConsumedSpec → CollectSpec.
func TestCollectSpec_InvalidOpenAPIDocMapsToErrInvalidSpec(t *testing.T) {
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return map[string]string{"design.md": "overview\n"}, nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, _, err := svc.CollectSpec(
		context.Background(), "org1", "proj1",
		"comp", "dep",
		"not openapi yaml at all", "",
	)
	if err == nil {
		t.Fatal("expected error for invalid OpenAPI doc, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("want errors.Is(err, ErrInvalidSpec)=true for invalid OpenAPI doc, got: %v", err)
	}
	// Must NOT be misclassified as a fetch failure.
	if errors.Is(err, ErrSpecFetchFailed) {
		t.Errorf("invalid OpenAPI doc must not be classified as ErrSpecFetchFailed")
	}
}

// TestCollectSpec_WriteFailureMapsToInfraError asserts that a storage/write
// failure from StoreConsumedSpec (e.g. git-service PutFile error) surfaces as a
// plain error — NOT ErrInvalidSpec and NOT ErrSpecFetchFailed — so the HTTP handler
// maps it to 500 (not 400 or 502). This covers the infra-error classification
// fix (code review finding: all non-502 errors were previously mapped to 400).
// Previously this tested CommitDesignFile failure; now that StoreConsumedSpec
// uses WriteDesignFile (→ PutFile) for draft writes, the infra error comes from
// PutFile. The semantic contract is unchanged: storage failures → 500.
func TestCollectSpec_WriteFailureMapsToInfraError(t *testing.T) {
	writeErr := errors.New("git commit failed: remote unreachable")
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return map[string]string{"design.md": "overview\n"}, nil
		},
		putFileFunc: func(_ context.Context, _, _, _, _, _ string) (*artifacts.PutResult, error) {
			return nil, writeErr
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub)

	_, _, err := svc.CollectSpec(
		context.Background(), "org1", "proj1",
		"comp", "dep",
		sampleOpenAPISpec, "",
	)
	if err == nil {
		t.Fatal("expected error for write failure, got nil")
	}
	// Infra failure must NOT be misclassified as a client (400) or gateway (502) error.
	if errors.Is(err, ErrInvalidSpec) {
		t.Errorf("write failure must not be classified as ErrInvalidSpec (400)")
	}
	if errors.Is(err, ErrSpecFetchFailed) {
		t.Errorf("write failure must not be classified as ErrSpecFetchFailed (502)")
	}
	// Verify the underlying cause is preserved (unwrappable).
	if !strings.Contains(err.Error(), "git commit failed") {
		t.Errorf("expected underlying write error to be preserved, got: %v", err)
	}
}

// ---- A6: auto-fetch specUrl at SaveAndProceed ----------------------------

// sampleOpenAPISpecA6 is the minimal valid OpenAPI spec returned by the
// stub fetch function in the A6 auto-fetch tests.
const sampleOpenAPISpecA6 = `openapi: 3.0.3
info:
  title: External API
  version: "1.0"
paths:
  /items:
    get:
      summary: List items
      responses:
        "200":
          description: OK
`

// TestSaveAndProceed_AutoFetch_Success asserts that when a component has an
// external dep with needsSpec:true, no specPath, and a specUrl hint, SaveAndProceed
// auto-fetches the spec via the injectable fetchSpec func, stores it via
// CollectSpec, and then the dep is no longer unresolved — so SaveAndProceed
// proceeds to SaveDesign (and eventually succeeds). The dep's specUrl should be
// cleared from the written design.md after the auto-fetch.
//
// CollectSpec now uses PutFile (working-tree draft) for both writes:
//  1. StoreConsumedSpec → PutFile for the spec blob
//  2. SetDependencySpecPath → PutFile for the component design.md
//
// The ListDesignFiles stub reflects PutFile writes back into the files map so
// the second read (inside SetDependencySpecPath) sees the spec blob already present.
func TestSaveAndProceed_AutoFetch_Success(t *testing.T) {
	// Initial design: external dep with needsSpec + specUrl, no specPath.
	initialCompDesignMd := `---
type: service
language: go
dependencies:
  - kind: external
    name: external-api
    description: some external API
    needsSpec: true
    specUrl: "https://api.example.com/openapi.yaml"
---
Build a service that calls an external API.
`
	// Mutable files map — PutFile writes are reflected here so subsequent
	// ListDesignFiles calls see the updated content.
	files := map[string]string{
		"design.md":                       "System overview.\n",
		"components/my-service/design.md": initialCompDesignMd,
	}

	type putCall struct{ relPath, content string }
	var putCalls []putCall

	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			cp := make(map[string]string, len(files))
			for k, v := range files {
				cp[k] = v
			}
			return cp, nil
		},
		putFileFunc: func(_ context.Context, _, _, relPath, content, _ string) (*artifacts.PutResult, error) {
			putCalls = append(putCalls, putCall{relPath, content})
			// Reflect writes back into the mutable files map so that a
			// subsequent ListDesignFiles (inside SetDependencySpecPath's
			// ReadDesign) sees the updated content.
			const prefix = "specs/design/"
			if key := strings.TrimPrefix(relPath, prefix); key != relPath {
				files[key] = content
			}
			return &artifacts.PutResult{SHA: "abc"}, nil
		},
	}
	// Override SaveDesign to succeed (simulate a tagged version).
	stub2 := &saveDesignStub{stubArtifactService: stub}

	store := artifacts.NewArtifactStore(stub2)
	svc := NewDesignService(store, nil, stub2).(*designService)

	// Inject a stub fetch func that returns a valid OpenAPI spec.
	svc.fetchSpec = func(_ context.Context, url string) (string, error) {
		if url != "https://api.example.com/openapi.yaml" {
			return "", errors.New("unexpected URL: " + url)
		}
		return sampleOpenAPISpecA6, nil
	}

	design, err := svc.SaveAndProceed(context.Background(), "org1", "proj1")
	if err != nil {
		t.Fatalf("SaveAndProceed must succeed after auto-fetch, got: %v", err)
	}
	if design == nil {
		t.Fatal("expected non-nil design after SaveAndProceed")
	}

	// Verify that PutFile was called at least twice:
	// 1) StoreConsumedSpec: spec blob
	// 2) SetDependencySpecPath: design.md with specPath recorded
	if len(putCalls) < 2 {
		t.Errorf("expected at least 2 PutFile calls (spec blob + specPath), got %d", len(putCalls))
	}

	// Find the PutFile call for design.md and verify it has specPath set (no specUrl).
	var lastDesignMdPut *putCall
	for i := range putCalls {
		c := &putCalls[i]
		if strings.HasSuffix(c.relPath, "/design.md") {
			lastDesignMdPut = c
		}
	}
	if lastDesignMdPut == nil {
		t.Fatal("no PutFile call for design.md found")
	}
	if !strings.Contains(lastDesignMdPut.content, "specPath:") {
		t.Errorf("written design.md should contain specPath, got:\n%s", lastDesignMdPut.content)
	}
	if strings.Contains(lastDesignMdPut.content, "specUrl:") {
		t.Errorf("written design.md must NOT contain specUrl after auto-fetch, got:\n%s", lastDesignMdPut.content)
	}
	// REGRESSION: CommitDesignFile must NOT be called for consumer spec writes.
	if len(stub.commitDesignFileCalls) != 0 {
		t.Errorf("auto-fetch CollectSpec must not use CommitDesignFile (regression guard): got %d calls", len(stub.commitDesignFileCalls))
	}
}

// TestSaveAndProceed_AutoFetch_FetchFailure asserts that when the fetch stub
// returns an error, SaveAndProceed does NOT fail the whole save attempt due to
// the fetch error itself, but the dep remains unresolved so ErrUnresolvedDependency
// is still returned by the proceed-gate — consistent with the user needing to
// supply the spec manually.
func TestSaveAndProceed_AutoFetch_FetchFailure(t *testing.T) {
	compDesignMd := `---
type: service
language: go
dependencies:
  - kind: external
    name: external-api
    description: some external API
    needsSpec: true
    specUrl: "https://api.example.com/openapi.yaml"
---
Build a service.
`
	files := map[string]string{
		"design.md":                         "System overview.\n",
		"components/my-service/design.md":   compDesignMd,
	}
	stub := &stubArtifactService{
		listDesignFilesFunc: func(_ context.Context, _, _ string) (map[string]string, error) {
			return files, nil
		},
	}
	store := artifacts.NewArtifactStore(stub)
	svc := NewDesignService(store, nil, stub).(*designService)

	// Inject a fetch func that always fails.
	svc.fetchSpec = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("network unreachable")
	}

	_, saveErr := svc.SaveAndProceed(context.Background(), "org1", "proj1")
	if saveErr == nil {
		t.Fatal("want error from SaveAndProceed when fetch fails + dep unresolved, got nil")
	}
	// The error must be ErrUnresolvedDependency (the gate caught the still-unresolved dep),
	// NOT an infra/fetch error bubbling up.
	if !errors.Is(saveErr, ErrUnresolvedDependency) {
		t.Fatalf("want ErrUnresolvedDependency after failed fetch, got: %v", saveErr)
	}
}

// saveDesignStub wraps stubArtifactService to override SaveDesign so that
// SaveAndProceed can proceed past the git-save step in auto-fetch success tests.
// All other ArtifactService methods are promoted from the embedded stub.
type saveDesignStub struct {
	*stubArtifactService
}

func (s *saveDesignStub) SaveDesign(_ context.Context, _, _ string, _ artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
	return &artifacts.DesignSaveResult{
		Tag:                 "v1-1",
		RequirementsVersion: 1,
		DesignRevision:      1,
		Status:              "tagged",
	}, nil
}
