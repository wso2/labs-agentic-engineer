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
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeRegistry struct {
	records map[string]*RegisteredResource
	err     error
}

func (f fakeRegistry) RegisteredResource(_ context.Context, _, name string) (*RegisteredResource, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records[name], nil
}

const stubPath = DesignDir + "/" + dependencyDirPrefix + "currency-service/" + DependencyDesignFile

func stub() WriteOp {
	return WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"ref":"currency-service","name":"currency-service"}}`}
}

func registeredCurrency() *RegisteredResource {
	return &RegisteredResource{
		Resource: ResourceDefinition{
			Name:                    "currency-service",
			Description:             "Live FX rates to USD.",
			Provider:                "Open Exchange Rates",
			Config:                  []ConfigKey{{Key: "OPENEXCHANGERATES_APP_ID", Secret: true, Description: "App ID"}},
			Contract:                &ResourceContract{Type: DependencyContractTypeOpenAPI, Path: "currency-service/openapi.yaml"},
			ConsumptionInstructions: "Call /latest.json once per approval.",
			Provenance:              &ResourceProvenance{SourceURL: "https://docs.openexchangerates.org/", SHA256: "orgcopy"},
		},
		Document: "openapi: 3.0.3\ninfo: {title: Open Exchange Rates, version: '1'}\npaths: {}\n",
	}
}

// A stub naming a registered resource is completed: the record's block with
// the ref kept, the document copied byte for byte beside it, and provenance
// naming the registry file and the document's hash.
func TestCompleteRegistryCopies_CompletesAStub(t *testing.T) {
	reg := fakeRegistry{records: map[string]*RegisteredResource{"currency-service": registeredCurrency()}}
	copies, warnings := completeRegistryCopies(context.Background(), reg, "org", []WriteOp{stub()})
	c, ok := copies[stubPath]
	if !ok {
		t.Fatalf("stub not completed; warnings %+v", warnings)
	}
	def, err := parseDependencyDefinitionJSON("currency-service", c.Definition)
	if err != nil {
		t.Fatalf("completed definition does not parse: %v\n%s", err, c.Definition)
	}
	if def.Resource.Ref != "currency-service" || def.Resource.Provider != "Open Exchange Rates" || len(def.Resource.Config) != 1 || def.Resource.ConsumptionInstructions == "" {
		t.Fatalf("block not copied: %+v", def.Resource)
	}
	if def.Resource.Provenance != nil {
		t.Fatalf("the org copy's own provenance must not ride into the project block: %+v", def.Resource.Provenance)
	}
	if c2 := def.Resource.Contract; c2 == nil || c2.Type != DependencyContractTypeOpenAPI || c2.Path != "openapi.yaml" || c2.Origin != DependencyContractOriginRegistry {
		t.Fatalf("contract not rewritten to the project copy: %+v", def.Resource.Contract)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(registeredCurrency().Document)))
	if def.Provenance == nil || def.Provenance.Registry != "currency-service/openapi.yaml" || def.Provenance.SHA256 != want || def.Provenance.ReadOn == "" {
		t.Fatalf("provenance must name the registry file and the document hash: %+v", def.Provenance)
	}
	docPath := DesignDir + "/" + dependencyDirPrefix + "currency-service/openapi.yaml"
	if c.Files[docPath] != registeredCurrency().Document {
		t.Fatalf("document not copied byte for byte: %q", c.Files[docPath])
	}
	if len(warnings) != 1 || warnings[0].Code != WarningRegistryCopied {
		t.Fatalf("want one copied warning, got %+v", warnings)
	}
	// No URL anywhere in what the coding agent will read.
	if strings.Contains(c.Definition, "http") {
		t.Fatalf("the project file must carry no URL:\n%s", c.Definition)
	}
}

// A record with no document lands the block and no contract; the ladder then
// reads needs-contract, which is the truth.
func TestCompleteRegistryCopies_RecordWithoutADocument(t *testing.T) {
	rec := registeredCurrency()
	rec.Resource.Contract, rec.Document = nil, ""
	reg := fakeRegistry{records: map[string]*RegisteredResource{"currency-service": rec}}
	copies, _ := completeRegistryCopies(context.Background(), reg, "org", []WriteOp{stub()})
	def, err := parseDependencyDefinitionJSON("currency-service", copies[stubPath].Definition)
	if err != nil || def.Resource.Contract != nil || len(copies[stubPath].Files) != 0 || def.Provenance != nil {
		t.Fatalf("a record without a document must land no contract and no file: %+v %v", def, err)
	}
}

// Not a stub, not a dependency file, an unreachable registry, a name nobody
// registered: each lands the write as given, the last two with a warning.
func TestCompleteRegistryCopies_LeavesEverythingElseAlone(t *testing.T) {
	reg := fakeRegistry{records: map[string]*RegisteredResource{}}
	full := WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"ref":"currency-service","name":"currency-service","provider":"Open Exchange Rates"}}`}
	other := WriteOp{Path: DesignDir + "/components/api/design.json", Content: `{"name":"api"}`}
	copies, warnings := completeRegistryCopies(context.Background(), reg, "org", []WriteOp{full, other})
	if len(copies) != 0 || len(warnings) != 0 {
		t.Fatalf("a full copy and an unrelated file must be left alone: %+v %+v", copies, warnings)
	}
	copies, warnings = completeRegistryCopies(context.Background(), reg, "org", []WriteOp{stub()})
	if len(copies) != 0 || len(warnings) != 1 || warnings[0].Code != WarningRegistryMiss || !strings.Contains(warnings[0].Message, "no registered resource") {
		t.Fatalf("a miss must warn and leave the stub: %+v %+v", copies, warnings)
	}
	copies, warnings = completeRegistryCopies(context.Background(), fakeRegistry{err: errors.New("down")}, "org", []WriteOp{stub()})
	if len(copies) != 0 || len(warnings) != 1 || warnings[0].Code != WarningRegistryUnreachable {
		t.Fatalf("an unreachable registry must warn and leave the stub: %+v %+v", copies, warnings)
	}
	copies, warnings = completeRegistryCopies(context.Background(), nil, "org", []WriteOp{stub()})
	if len(copies) != 0 || len(warnings) != 1 || warnings[0].Code != WarningRegistryUnreachable {
		t.Fatalf("no registry wired must warn and leave the stub: %+v %+v", copies, warnings)
	}
}

func TestRenderRegistryCopy_RefusesAMisnamedRecord(t *testing.T) {
	rec := registeredCurrency()
	rec.Resource.Name = "fx-rates"
	if _, _, err := renderRegistryCopy(DependencyDefinition{Name: "currency-service"}, *rec, time.Now()); err == nil {
		t.Fatalf("a record under another name must be refused")
	}
}

func TestDependencyFileDir(t *testing.T) {
	if d, ok := dependencyFileDir(stubPath); !ok || d != "currency-service" {
		t.Fatalf("dir = %q ok=%v", d, ok)
	}
	for _, p := range []string{DesignDir + "/dependencies/dependency.json", DesignDir + "/dependencies/a/b/dependency.json", DesignDir + "/dependencies/a/openapi.yaml", "specs/design/components/a/design.json"} {
		if _, ok := dependencyFileDir(p); ok {
			t.Fatalf("%s must not read as a dependency file", p)
		}
	}
}

// A contract the agent pointed at by URL (origin provider, no hash yet) is
// fetched by the platform, landed beside the definition, and the hash filled
// in; once the hash is on file, a later save fetches nothing.
func TestCompleteProviderDocuments_FetchesOnce(t *testing.T) {
	doc := "openapi: 3.0.3\ninfo: {title: Open Exchange Rates, version: '1'}\npaths:\n  /latest.json:\n    get: {responses: {'200': {description: ok}}}\n"
	calls := 0
	fetch := func(_ context.Context, url string) ([]byte, error) {
		calls++
		if url != "https://docs.openexchangerates.org/openapi.yaml" {
			return nil, errors.New("unexpected url " + url)
		}
		return []byte(doc), nil
	}
	pending := WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"name":"currency-service","provider":"Open Exchange Rates","config":[{"key":"K","secret":true}],"contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://docs.openexchangerates.org/openapi.yaml"}}`}
	out, warnings := completeProviderDocuments(context.Background(), fetch, []WriteOp{pending}, nil)
	c, ok := out[stubPath]
	if !ok || calls != 1 {
		t.Fatalf("want one fetch and a completion, got calls=%d out=%+v warnings=%+v", calls, out, warnings)
	}
	def, err := parseDependencyDefinitionJSON("currency-service", c.Definition)
	if err != nil || def.Provenance == nil || def.Provenance.SHA256 == "" || def.Provenance.ReadOn == "" || def.Provenance.SourceURL == "" {
		t.Fatalf("provenance not filled: %+v %v", def.Provenance, err)
	}
	docPath := DesignDir + "/" + dependencyDirPrefix + "currency-service/openapi.yaml"
	if !strings.Contains(c.Files[docPath], "/latest.json") {
		t.Fatalf("document not landed: %q", c.Files[docPath])
	}
	if len(warnings) != 1 || warnings[0].Code != WarningProviderDocumentFetched {
		t.Fatalf("warnings = %+v", warnings)
	}
	// Hash on file → nothing to fetch.
	settled := WriteOp{Path: stubPath, Content: c.Definition}
	out, warnings = completeProviderDocuments(context.Background(), fetch, []WriteOp{settled}, nil)
	if len(out) != 0 || len(warnings) != 0 || calls != 1 {
		t.Fatalf("a settled definition must not re-fetch: calls=%d out=%+v warnings=%+v", calls, out, warnings)
	}
	// The agent landed the document itself → nothing to fetch either.
	withDoc := WriteOp{Path: docPath, Content: doc}
	out, _ = completeProviderDocuments(context.Background(), fetch, []WriteOp{pending, withDoc}, nil)
	if len(out) != 0 || calls != 1 {
		t.Fatalf("a document in the batch must not be fetched again: calls=%d out=%+v", calls, out)
	}
	// A failed fetch lands the definition as written, with a warning.
	failing := func(context.Context, string) ([]byte, error) { return nil, errors.New("timeout") }
	out, warnings = completeProviderDocuments(context.Background(), failing, []WriteOp{pending}, nil)
	if len(out) != 0 || len(warnings) != 1 || warnings[0].Code != WarningProviderDocumentUnavailable {
		t.Fatalf("a failed fetch must warn: out=%+v warnings=%+v", out, warnings)
	}
	// Not an OpenAPI document → refused with a warning.
	junk := func(context.Context, string) ([]byte, error) { return []byte("<html>"), nil }
	out, warnings = completeProviderDocuments(context.Background(), junk, []WriteOp{pending}, nil)
	if len(out) != 0 || len(warnings) != 1 || warnings[0].Code != WarningProviderDocumentUnavailable {
		t.Fatalf("a non-OpenAPI body must warn: out=%+v warnings=%+v", out, warnings)
	}
	// A registry copy already completed for the same path is not touched.
	out, _ = completeProviderDocuments(context.Background(), fetch, []WriteOp{pending}, map[string]completedFile{stubPath: {}})
	if len(out) != 0 {
		t.Fatalf("a path the registry copy completed must be skipped")
	}
}

// A registered record whose document is not something a project can code
// against (asyncapi, protobuf, documentation) lands its block without a
// contract — the dependency then reads needs-contract — rather than a file
// the project's own gates would refuse.
func TestRenderRegistryCopy_UnsupportedDocumentTypeLandsNoContract(t *testing.T) {
	rec := registeredCurrency()
	rec.Resource.Contract = &ResourceContract{Type: DependencyContractTypeAsyncAPI, Path: "currency-service/asyncapi.yaml"}
	body, files, err := renderRegistryCopy(DependencyDefinition{Name: "currency-service"}, *rec, time.Now())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	def, err := parseDependencyDefinitionJSON("currency-service", body)
	if err != nil || def.Resource.Contract != nil || len(files) != 0 || def.Provenance != nil {
		t.Fatalf("an asyncapi record must land no project contract: %+v files=%d err=%v", def, len(files), err)
	}
	if def.Resource.Provider == "" || len(def.Resource.Config) == 0 {
		t.Fatalf("the block itself must still be copied: %+v", def.Resource)
	}
}
