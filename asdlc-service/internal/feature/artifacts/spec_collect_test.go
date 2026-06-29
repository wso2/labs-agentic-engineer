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

package artifacts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// TestValidateOpenAPI_RejectsYAMLAliasBomb locks the transitive-dependency
// guarantee that yaml.v3 (>= v3.0.4) rejects alias-expansion ("billion laughs")
// bombs via its alias budget rather than OOM/hang — a platform-design-expert
// SSRF-review item (the fetched/pasted spec is attacker-controlled). The doc
// below has 8 levels each referencing the previous 10×, i.e. ~10^8 expansions;
// ValidateOpenAPI parses via yaml.Unmarshal first, so the budget guard must fire
// there. A future dependency downgrade that drops the budget would resurface the
// OOM and trip this test (via the hang timeout).
func TestValidateOpenAPI_RejectsYAMLAliasBomb(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("a: &a \"lol\"\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		sb.WriteString(name + ": &" + name + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("*" + prev)
		}
		sb.WriteString("]\n")
		prev = name
	}
	bomb := sb.String()

	done := make(chan error, 1)
	go func() {
		_, err := ValidateOpenAPI(bomb)
		done <- err
	}()
	select {
	case err := <-done:
		// Either the alias budget rejects it, or it parses to a non-OpenAPI doc —
		// both are an error. The point is it returns quickly, not OOM/hang.
		if err == nil {
			t.Fatal("expected an error for a YAML alias bomb, got nil")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ValidateOpenAPI did not return on a YAML alias bomb — alias budget not enforced (dep downgrade?)")
	}
}

const sampleSpec = `openapi: 3.0.3
info: { title: Weather, version: "1.0" }
paths:
  /weather:
    get: { responses: { "200": { description: ok } } }
  /forecast:
    get: { responses: { "200": { description: ok } } }
    post: { responses: { "201": { description: created } } }
`

func TestValidateOpenAPICountsOperations(t *testing.T) {
	n, err := ValidateOpenAPI(sampleSpec)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 3 {
		t.Fatalf("want 3 operations, got %d", n)
	}
}

func TestValidateOpenAPIRejectsNonOpenAPI(t *testing.T) {
	if _, err := ValidateOpenAPI("foo: bar"); err == nil {
		t.Fatal("expected error for non-openapi doc")
	}
}

func TestValidateOpenAPIRejectsSwagger2(t *testing.T) {
	swagger2 := `swagger: "2.0"
info: { title: Old, version: "1.0" }
paths:
  /ping:
    get: { responses: { "200": { description: ok } } }
`
	if _, err := ValidateOpenAPI(swagger2); err == nil {
		t.Fatal("expected error for swagger 2.0 doc")
	}
}

func TestValidateOpenAPIRejectsNoPaths(t *testing.T) {
	noPaths := `openapi: "3.0.3"
info: { title: Empty, version: "1.0" }
`
	if _, err := ValidateOpenAPI(noPaths); err == nil {
		t.Fatal("expected error for OpenAPI doc with no paths")
	}
}

// TestStoreConsumedSpec_PathAndCount verifies that StoreConsumedSpec:
//  1. returns specPath == "dependencies/<dep>.openapi.yaml" (component-relative)
//  2. returns the correct operation count from ValidateOpenAPI
//  3. writes a normalized spec to the working-tree draft via PutFile at
//     "specs/design/components/<comp>/dependencies/<dep>.openapi.yaml"
//  4. does NOT call CommitDesignFile (regression: spec was previously committed
//     directly, causing the design-save diff to emit it as a DELETE tombstone)
func TestStoreConsumedSpec_PathAndCount(t *testing.T) {
	svc := &fakeArtifactSvc{files: map[string]string{}}
	store := NewArtifactStore(svc)

	specPath, opCount, err := store.StoreConsumedSpec(
		context.Background(),
		"acme", "weather-app", "frontend", "weather-api",
		sampleSpec,
	)
	if err != nil {
		t.Fatalf("StoreConsumedSpec: %v", err)
	}
	if specPath != "dependencies/weather-api.openapi.yaml" {
		t.Fatalf("want specPath %q, got %q", "dependencies/weather-api.openapi.yaml", specPath)
	}
	if opCount != 3 {
		t.Fatalf("want opCount 3, got %d", opCount)
	}
	// Must write via PutFile (working-tree draft), not CommitDesignFile.
	if len(svc.puts) != 1 {
		t.Fatalf("want exactly one PutFile call, got %d", len(svc.puts))
	}
	p := svc.puts[0]
	// PutFile receives the full repo-relative path (DesignDir + "/" + subPath).
	wantRelPath := "specs/design/components/frontend/dependencies/weather-api.openapi.yaml"
	if p.relPath != wantRelPath {
		t.Fatalf("want PutFile relPath %q, got %q", wantRelPath, p.relPath)
	}
	// Written content must be valid normalized YAML (NormalizeOpenAPIYAML output).
	if strings.TrimSpace(p.content) == "" {
		t.Fatal("written content must not be empty")
	}
	// Must still be parseable as OpenAPI after normalization.
	normalizedOpCount, err := ValidateOpenAPI(p.content)
	if err != nil {
		t.Fatalf("normalized content fails ValidateOpenAPI: %v", err)
	}
	// Normalization must not drop any operations — the normalized output must
	// contain the same number of operations as the original spec (3).
	const wantOps = 3
	if normalizedOpCount != wantOps {
		t.Fatalf("normalization changed operation count: want %d, got %d", wantOps, normalizedOpCount)
	}
	// REGRESSION: StoreConsumedSpec must NOT call CommitDesignFile.
	// Previously it committed directly, causing saveDesignViaAPI to see the
	// spec file in HEAD-but-not-working-tree and emit it as a DELETE tombstone.
	if len(svc.commits) != 0 {
		t.Fatalf("StoreConsumedSpec must not call CommitDesignFile (regression guard): got %d commits", len(svc.commits))
	}
}

func TestStoreConsumedSpec_RejectsInvalidSpec(t *testing.T) {
	svc := &fakeArtifactSvc{files: map[string]string{}}
	store := NewArtifactStore(svc)

	_, _, err := store.StoreConsumedSpec(
		context.Background(),
		"acme", "weather-app", "frontend", "not-openapi",
		"foo: bar",
	)
	if err == nil {
		t.Fatal("expected error for non-openapi spec")
	}
	if len(svc.puts) != 0 {
		t.Fatalf("no PutFile must happen on invalid spec, got %d", len(svc.puts))
	}
	if len(svc.commits) != 0 {
		t.Fatalf("no commit must happen on invalid spec, got %d", len(svc.commits))
	}
}

// TestStoreConsumedSpec_RejectsPathTraversalDepName verifies that depName
// values containing path separators or ".." are rejected before any file
// path is constructed (defense-in-depth path traversal guard).
func TestStoreConsumedSpec_RejectsPathTraversalDepName(t *testing.T) {
	dangerousNames := []string{
		"../evil",
		"../../etc/passwd",
		"sub/name",
		`sub\name`,
		"..",
	}
	for _, name := range dangerousNames {
		t.Run(name, func(t *testing.T) {
			svc := &fakeArtifactSvc{files: map[string]string{}}
			store := NewArtifactStore(svc)

			_, _, err := store.StoreConsumedSpec(
				context.Background(),
				"acme", "weather-app", "frontend", name,
				sampleSpec,
			)
			if err == nil {
				t.Fatalf("expected error for dangerous depName %q, got nil", name)
			}
			if len(svc.puts) != 0 {
				t.Fatalf("no PutFile must happen for dangerous depName %q, got %d puts", name, len(svc.puts))
			}
			if len(svc.commits) != 0 {
				t.Fatalf("no commit must happen for dangerous depName %q, got %d commits", name, len(svc.commits))
			}
		})
	}
}

// designFilesWithExternalDep builds a minimal working-tree map for a component
// that has one external dependency with needsSpec=true and no specPath yet.
func designFilesWithExternalDep(t *testing.T, compName, depName string) map[string]string {
	t.Helper()
	comp := models.DesignComponent{
		Name:          compName,
		ComponentType: "webapp",
		Language:      "TypeScript",
		AppPath:       compName,
		Dependencies: []models.Dependency{
			{
				Kind:      models.DependencyKindExternal,
				Name:      depName,
				NeedsSpec: true,
			},
		},
		ComponentAgentInstructions: "consume " + depName,
	}
	files, err := SplitDesign(&DesignFile{Overview: "root", Components: []models.DesignComponent{comp}})
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	files[DesignRootFile] = "# Design\n"
	return files
}

// TestSetDependencySpecPath_WritesDraftNotCommit verifies that SetDependencySpecPath:
//  1. writes the updated component design.md to the working-tree draft via PutFile
//  2. the written content contains the specPath field
//  3. does NOT call CommitDesignFile (regression: was previously committed
//     separately, causing saveDesignViaAPI to miss the specPath update)
func TestSetDependencySpecPath_WritesDraftNotCommit(t *testing.T) {
	svc := &fakeArtifactSvc{files: designFilesWithExternalDep(t, "frontend", "weather-api")}
	store := NewArtifactStore(svc)

	err := store.SetDependencySpecPath(
		context.Background(),
		"acme", "weather-app", "frontend", "weather-api",
		"dependencies/weather-api.openapi.yaml",
	)
	if err != nil {
		t.Fatalf("SetDependencySpecPath: %v", err)
	}
	// Must write via PutFile (working-tree draft), not CommitDesignFile.
	if len(svc.puts) != 1 {
		t.Fatalf("want exactly one PutFile call, got %d", len(svc.puts))
	}
	p := svc.puts[0]
	// PutFile receives the full repo-relative path.
	wantRelPath := "specs/design/components/frontend/design.md"
	if p.relPath != wantRelPath {
		t.Fatalf("want PutFile relPath %q, got %q", wantRelPath, p.relPath)
	}
	// Written design.md must contain the specPath.
	if !strings.Contains(p.content, "specPath:") {
		t.Fatalf("written design.md missing specPath:\n%s", p.content)
	}
	if !strings.Contains(p.content, "dependencies/weather-api.openapi.yaml") {
		t.Fatalf("written design.md missing the specPath value:\n%s", p.content)
	}
	// REGRESSION: SetDependencySpecPath must NOT call CommitDesignFile.
	// Previously it committed the design.md directly, which diverged the local
	// clone HEAD from the working tree and caused specPath to be lost on save.
	if len(svc.commits) != 0 {
		t.Fatalf("SetDependencySpecPath must not call CommitDesignFile (regression guard): got %d commits", len(svc.commits))
	}
}

// TestSetDependencySpecPath_IdempotentNoPut verifies that SetDependencySpecPath
// is a no-op when the specPath is already set and SpecUrl is cleared.
func TestSetDependencySpecPath_IdempotentNoPut(t *testing.T) {
	comp := models.DesignComponent{
		Name:          "frontend",
		ComponentType: "webapp",
		Language:      "TypeScript",
		AppPath:       "frontend",
		Dependencies: []models.Dependency{
			{
				Kind:      models.DependencyKindExternal,
				Name:      "weather-api",
				NeedsSpec: true,
				SpecPath:  "dependencies/weather-api.openapi.yaml",
				// SpecUrl already cleared (as SetDependencySpecPath would do).
				SpecUrl: "",
			},
		},
		ComponentAgentInstructions: "consume weather-api",
	}
	files, err := SplitDesign(&DesignFile{Overview: "root", Components: []models.DesignComponent{comp}})
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	files[DesignRootFile] = "# Design\n"

	svc := &fakeArtifactSvc{files: files}
	store := NewArtifactStore(svc)

	err = store.SetDependencySpecPath(
		context.Background(),
		"acme", "weather-app", "frontend", "weather-api",
		"dependencies/weather-api.openapi.yaml",
	)
	if err != nil {
		t.Fatalf("SetDependencySpecPath: %v", err)
	}
	if len(svc.puts) != 0 {
		t.Fatalf("idempotent call must not write, got %d PutFile calls", len(svc.puts))
	}
	if len(svc.commits) != 0 {
		t.Fatalf("idempotent call must not commit, got %d CommitDesignFile calls", len(svc.commits))
	}
}
