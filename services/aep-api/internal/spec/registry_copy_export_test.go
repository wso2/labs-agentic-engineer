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
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func warningCodes(ws []Warning) map[string]bool {
	out := map[string]bool{}
	for _, w := range ws {
		out[w.Code] = true
	}
	return out
}

// CompleteDependencies is the one completion path: Apply runs it, and so does
// the dependency-completions op the AE Studio tools pod calls.
func TestCompleteDependencies_RegistryStub(t *testing.T) {
	reg := fakeRegistry{records: map[string]*RegisteredResource{"currency-service": registeredCurrency()}}
	got, warns := CompleteDependencies(context.Background(), reg, nil, "acme", []WriteOp{stub()})
	c, ok := got[stubPath]
	if !ok || len(c.Files) == 0 {
		t.Fatalf("stub not completed with its document: %+v %+v", got, warns)
	}
	if !warningCodes(warns)[WarningRegistryCopied] {
		t.Fatalf("warnings = %+v, want %s", warns, WarningRegistryCopied)
	}
}

func TestCompleteDependencies_RegistryDownLandsStubWithWarning(t *testing.T) {
	got, warns := CompleteDependencies(context.Background(), fakeRegistry{err: errors.New("down")}, nil, "acme", []WriteOp{stub()})
	if len(got) != 0 {
		t.Fatalf("an unreachable registry must complete nothing: %+v", got)
	}
	if !warningCodes(warns)[WarningRegistryUnreachable] {
		t.Fatalf("warnings = %+v, want %s", warns, WarningRegistryUnreachable)
	}
}

// A provider stub (origin provider, sourceUrl, no hash) is fetched through the
// fetch port and completed.
func TestCompleteDependencies_ProviderStub(t *testing.T) {
	fetch := func(context.Context, string) ([]byte, error) {
		return []byte("openapi: 3.0.3\ninfo: {title: t, version: '1'}\npaths:\n  /latest.json:\n    get: {responses: {'200': {description: ok}}}\n"), nil
	}
	pending := WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"name":"currency-service","provider":"Open Exchange Rates","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://docs.openexchangerates.org/openapi.yaml"}}`}
	got, warns := CompleteDependencies(context.Background(), fakeRegistry{err: errors.New("down")}, fetch, "acme", []WriteOp{pending})
	if c, ok := got[stubPath]; !ok || len(c.Files) != 1 {
		t.Fatalf("provider stub not completed: %+v %+v", got, warns)
	}
	if codes := warningCodes(warns); !codes[WarningProviderDocumentFetched] || codes[WarningRegistryUnreachable] {
		t.Fatalf("warnings = %+v: want only the fetch, never a registry warning on a provider stub", warns)
	}
}

// Completion is for stubs only. A dependency file that
// already names its provider and owes no document is completed by nobody and
// warned about by nobody, even while the registry and the fetch are down.
func TestCompleteDependencies_NonStubIsLeftAlone(t *testing.T) {
	failingFetch := func(context.Context, string) ([]byte, error) { return nil, errors.New("down") }
	settled := WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"ref":"currency-service","name":"currency-service","provider":"Open Exchange Rates","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}},"provenance":{"sourceUrl":"https://docs.openexchangerates.org/openapi.yaml","sha256":"abc"}}`}
	got, warns := CompleteDependencies(context.Background(), fakeRegistry{err: errors.New("down")}, failingFetch, "acme", []WriteOp{settled})
	if len(got) != 0 || len(warns) != 0 {
		t.Fatalf("a non-stub must be left alone: %+v %+v", got, warns)
	}
}

func TestIsDependencyFilePath(t *testing.T) {
	for p, want := range map[string]bool{
		stubPath: true,
		"specs/design/dependencies/../dependency.json":            false,
		"specs/design/dependencies/a/../b/dependency.json":        false,
		"specs/design/dependencies/currency-service/openapi.yaml": false,
		"specs/design/components/api/design.json":                 false,
		"/specs/design/dependencies/x/dependency.json":            false,
	} {
		if got := IsDependencyFilePath(p); got != want {
			t.Errorf("IsDependencyFilePath(%q) = %v, want %v", p, got, want)
		}
	}
}

// contract.path is model output. A document path that leaves the dependency's
// own directory (traversal, a subdir, the definition itself) is refused with a
// warning before anything is fetched, and no file is returned for it.
func TestCompleteDependencies_ProviderDocumentPathStaysInItsDirectory(t *testing.T) {
	for _, contractPath := range []string{
		"../../../../.github/workflows/evil.yml",
		"../other-dep/openapi.yaml",
		"nested/openapi.yaml",
		"./openapi.yaml",
		"/etc/openapi.yaml",
		"dependency.json",
	} {
		t.Run(contractPath, func(t *testing.T) {
			fetched := 0
			fetch := func(context.Context, string) ([]byte, error) {
				fetched++
				return []byte("openapi: 3.0.3\n"), nil
			}
			cp, _ := json.Marshal(contractPath)
			pending := WriteOp{Path: stubPath, Content: `{"name":"currency-service","resource":{"name":"currency-service","provider":"P","contract":{"type":"openapi","path":` + string(cp) + `,"origin":"provider"}},"provenance":{"sourceUrl":"https://docs.example.com/openapi.yaml"}}`}
			if _, err := parseDependencyDefinitionJSON("currency-service", pending.Content); err != nil {
				t.Skipf("the definition parser already refuses this path: %v", err)
			}
			got, warns := CompleteDependencies(context.Background(), nil, fetch, "acme", []WriteOp{pending})
			if fetched != 0 || len(got) != 0 {
				t.Fatalf("fetched=%d completed=%+v: an escaping document path must not be fetched or returned", fetched, got)
			}
			if len(warns) != 1 || warns[0].Code != WarningProviderDocumentUnavailable || warns[0].Path != stubPath {
				t.Fatalf("warnings = %+v, want one %s on the stub", warns, WarningProviderDocumentUnavailable)
			}
		})
	}
}

// The registry side gets the same guard: a record whose contract path's file
// name is a traversal is not rendered.
func TestRenderRegistryCopy_RefusesAnEscapingDocumentPath(t *testing.T) {
	rec := registeredCurrency()
	rec.Resource.Contract.Path = "currency-service/.."
	if _, _, err := renderRegistryCopy(DependencyDefinition{Name: "currency-service"}, *rec, time.Now()); err == nil {
		t.Fatal("want an error for a document path that is not a file in the dependency directory")
	}
}
