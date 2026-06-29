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
)

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
//  3. commits a normalized spec to subPath == "components/<comp>/dependencies/<dep>.openapi.yaml"
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
	if len(svc.commits) != 1 {
		t.Fatalf("want exactly one commit, got %d", len(svc.commits))
	}
	c := svc.commits[0]
	wantSub := "components/frontend/dependencies/weather-api.openapi.yaml"
	if c.sub != wantSub {
		t.Fatalf("want committed subPath %q, got %q", wantSub, c.sub)
	}
	// Committed content must be valid normalized YAML (NormalizeOpenAPIYAML output).
	if strings.TrimSpace(c.content) == "" {
		t.Fatal("committed content must not be empty")
	}
	// Must still be parseable as OpenAPI after normalization.
	if _, err := ValidateOpenAPI(c.content); err != nil {
		t.Fatalf("normalized content fails ValidateOpenAPI: %v", err)
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
	if len(svc.commits) != 0 {
		t.Fatalf("no commit must happen on invalid spec, got %d", len(svc.commits))
	}
}
