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
	"errors"
	"strings"
	"testing"
)

const ownFxDocument = "openapi: 3.0.3\ninfo: {title: Open Exchange Rates, version: '1'}\npaths: {}\n"

// ownFxFiles is a design whose fx-rates dependency is the project's OWN
// resource in the nested shape: a provider, a key and a derived document.
func ownFxFiles() map[string]string {
	files := designFilesWithDeps(`[{"kind":"external","name":"fx-rates"}]`)
	files["dependencies/fx-rates/dependency.json"] = `{"name":"fx-rates","resource":{"name":"fx-rates","description":"Live FX rates.","provider":"Open Exchange Rates","config":[{"key":"OPENEXCHANGERATES_APP_ID","secret":true,"description":"The App ID."}],"contract":{"type":"openapi","path":"openapi.yaml","origin":"derived"}},"provenance":{"sourceUrl":"https://docs.openexchangerates.org/reference"}}`
	files["dependencies/fx-rates/openapi.yaml"] = ownFxDocument
	return files
}

func TestReadProjectResource_ReturnsTheBlockAndItsDocument(t *testing.T) {
	t.Parallel()
	files := ownFxFiles()
	svc := newService(readsFor(t, files))
	svc.fileCommitter = &contractReadingCommitter{fakeCommitter: fakeCommitter{}, files: files}

	pr, err := svc.ReadProjectResource(context.Background(), "acme", "team-expenses", "fx-rates")
	if err != nil {
		t.Fatalf("ReadProjectResource: %v", err)
	}
	res := pr.Definition.Resource
	if res.Provider != "Open Exchange Rates" || len(res.Config) != 1 || res.Contract == nil || res.Contract.Path != "openapi.yaml" {
		t.Fatalf("definition = %+v", pr.Definition)
	}
	if pr.Document != ownFxDocument {
		t.Fatalf("document = %q", pr.Document)
	}
	if pr.Definition.Provenance == nil || pr.Definition.Provenance.SourceURL == "" {
		t.Fatalf("the project's provenance rides along for the record: %+v", pr.Definition.Provenance)
	}
	if _, err := svc.ReadProjectResource(context.Background(), "acme", "team-expenses", "ghost"); !errors.Is(err, ErrDependencyNotFound) {
		t.Fatalf("unknown dependency: want ErrDependencyNotFound, got %v", err)
	}
}

// A dependency that already reuses a record has nothing of the project's own
// to promote.
func TestReadProjectResource_RefusesACopy(t *testing.T) {
	t.Parallel()
	files := designFilesWithDeps(`[{"kind":"external","name":"fx-rates"}]`)
	files["dependencies/fx-rates/dependency.json"] = `{"name":"fx-rates","resource":{"ref":"fx-rates","name":"fx-rates","provider":"Open Exchange Rates","config":[{"key":"K","secret":true}]}}`
	svc := newService(readsFor(t, files))
	svc.fileCommitter = &fakeCommitter{}
	if _, err := svc.ReadProjectResource(context.Background(), "acme", "team-expenses", "fx-rates"); !errors.Is(err, ErrDependencyIsCopy) {
		t.Fatalf("want ErrDependencyIsCopy, got %v", err)
	}
}

// After Promote the project's file is what a fresh reuse would have landed:
// ref kept, the record's block and instructions, origin registry, provenance
// naming the registry document and its hash, and the document rewritten with
// the record's bytes under its own CAS token.
func TestRewriteAsRegistryCopy_LandsTheSameFileAReuseWould(t *testing.T) {
	t.Parallel()
	files := ownFxFiles()
	fc := &contractReadingCommitter{fakeCommitter: fakeCommitter{}, files: files}
	svc := newService(readsFor(t, files))
	svc.fileCommitter = fc

	rec := RegisteredResource{
		Resource: ResourceDefinition{
			Name: "fx-rates", Description: "Live FX rates.", Provider: "Open Exchange Rates",
			Config:                  []ConfigKey{{Key: "OPENEXCHANGERATES_APP_ID", Secret: true, Description: "The App ID."}},
			Contract:                &ResourceContract{Type: DependencyContractTypeOpenAPI, Path: "fx-rates/openapi.yaml"},
			ConsumptionInstructions: "Call /latest.json once per conversion.",
			Provenance:              &ResourceProvenance{SourceURL: "https://docs.openexchangerates.org/reference", SHA256: "abc"},
		},
		Document: ownFxDocument,
	}
	if err := svc.RewriteAsRegistryCopy(context.Background(), "acme", "team-expenses", "fx-rates", rec); err != nil {
		t.Fatalf("RewriteAsRegistryCopy: %v", err)
	}
	if len(fc.writes) != 2 {
		t.Fatalf("writes = %+v, want the definition and the document", fc.writes)
	}
	byPath := map[string]DesignFileWrite{}
	for _, w := range fc.writes {
		byPath[w.Path] = w
	}
	def := byPath["specs/design/dependencies/fx-rates/dependency.json"]
	if def.BaseSHA != "sha-dependency" {
		t.Fatalf("the definition is replaced under its CAS token: %+v", def)
	}
	parsed, err := parseDependencyDefinitionJSON("fx-rates", def.Content)
	if err != nil {
		t.Fatalf("rewritten file does not parse: %v\n%s", err, def.Content)
	}
	if parsed.Resource.Ref != "fx-rates" || parsed.Resource.ConsumptionInstructions == "" || parsed.Resource.Provenance != nil {
		t.Fatalf("block = %+v", parsed.Resource)
	}
	if c := parsed.Resource.Contract; c == nil || c.Origin != DependencyContractOriginRegistry || c.Path != "openapi.yaml" {
		t.Fatalf("contract = %+v", parsed.Resource.Contract)
	}
	if parsed.Provenance == nil || parsed.Provenance.Registry != "fx-rates/openapi.yaml" || parsed.Provenance.SHA256 == "" {
		t.Fatalf("provenance = %+v", parsed.Provenance)
	}
	if strings.Contains(def.Content, "http") {
		t.Fatalf("the project file must carry no URL:\n%s", def.Content)
	}
	doc := byPath["specs/design/dependencies/fx-rates/openapi.yaml"]
	if doc.Content != ownFxDocument || doc.BaseSHA != "sha-contract" {
		t.Fatalf("document write = %+v, want the record's bytes under the existing file's token", doc)
	}
}

// The promise behind Promote: the file it leaves is the file a design turn
// naming the record would have landed. Same record, same renderer, same bytes
// (readOn aside — the two runs read the clock at different moments).
func TestRewriteAsRegistryCopy_IsByteIdenticalToAFreshReuse(t *testing.T) {
	t.Parallel()
	rec := registeredCurrency()
	files := designFilesWithDeps(`[{"kind":"external","name":"currency-service"}]`)
	files["dependencies/currency-service/dependency.json"] = `{"name":"currency-service","resource":{"name":"currency-service","provider":"Open Exchange Rates","config":[{"key":"OPENEXCHANGERATES_APP_ID","secret":true}]}}`
	fc := &contractReadingCommitter{fakeCommitter: fakeCommitter{}, files: files}
	svc := newService(readsFor(t, files))
	svc.fileCommitter = fc
	if err := svc.RewriteAsRegistryCopy(context.Background(), "acme", "web", "currency-service", *rec); err != nil {
		t.Fatalf("RewriteAsRegistryCopy: %v", err)
	}
	var promoted string
	for _, w := range fc.writes {
		if w.Path == stubPath {
			promoted = w.Content
		}
	}

	reg := fakeRegistry{records: map[string]*RegisteredResource{"currency-service": rec}}
	copies, _ := completeRegistryCopies(context.Background(), reg, "acme", []WriteOp{stub()})
	reused := copies[stubPath].Definition

	if promoted == "" || reused == "" {
		t.Fatalf("both paths must land a definition: promoted=%q reused=%q", promoted, reused)
	}
	if stripReadOn(promoted) != stripReadOn(reused) {
		t.Fatalf("a promoted dependency must be the same bytes as a reused one:\n--- promoted\n%s\n--- reused\n%s", promoted, reused)
	}
	if copies[stubPath].Files[DesignDir+"/"+dependencyDirPrefix+"currency-service/openapi.yaml"] != rec.Document {
		t.Fatalf("the document lands beside the copy on both paths")
	}
}

func stripReadOn(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.Contains(l, `"readOn"`) {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
