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

package provisioning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakePromoter is the project side of Promote: it hands back the project's own
// resource and records the copy the platform asked it to write.
type fakePromoter struct {
	resource *spec.ProjectResource
	readErr  error
	rewrote  []spec.RegisteredResource
}

func (f *fakePromoter) ReadProjectResource(context.Context, string, string, string) (*spec.ProjectResource, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.resource, nil
}

func (f *fakePromoter) RewriteAsRegistryCopy(_ context.Context, _, _, _ string, rec spec.RegisteredResource) error {
	f.rewrote = append(f.rewrote, rec)
	return nil
}

// promoteDocs is the org docs repo: it records what Promote commits and hands
// the path back the way the real store does.
type promoteDocs struct {
	committed map[string]string // "<logicalName>/<fileName>" → content
}

func (d *promoteDocs) CommitUTF8(_ context.Context, _, logicalName, fileName, content string) (string, error) {
	if d.committed == nil {
		d.committed = map[string]string{}
	}
	p := logicalName + "/" + fileName
	d.committed[p] = content
	return p, nil
}

func (d *promoteDocs) ReadUTF8(_ context.Context, _, path string) (string, error) {
	c, ok := d.committed[path]
	if !ok {
		return "", fmt.Errorf("no file %q", path)
	}
	return c, nil
}

const fxDocument = "openapi: 3.0.3\ninfo: {title: Open Exchange Rates, version: '1'}\npaths: {}\n"

// fxOwnResource is a project's own currency resource: an inline block with a
// provider, two keys and a derived contract on disk.
func fxOwnResource() *spec.ProjectResource {
	return &spec.ProjectResource{
		Definition: spec.DependencyDefinition{
			Name: "fx-rates",
			Resource: spec.ResourceDefinition{
				Name:        "fx-rates",
				Description: "Live foreign-exchange rates.",
				Provider:    "Open Exchange Rates",
				Config: []spec.ConfigKey{
					{Key: "OPENEXCHANGERATES_APP_ID", Secret: true, Description: "The App ID."},
					{Key: "FX_BASE", Description: "Base currency."},
				},
				Contract: &spec.ResourceContract{Type: spec.DependencyContractTypeOpenAPI, Path: "openapi.yaml", Origin: spec.DependencyContractOriginDerived},
			},
			Provenance: &spec.ResourceProvenance{SourceURL: "https://docs.openexchangerates.org/reference"},
		},
		Document: fxDocument,
	}
}

func projectBinding(values map[string]string) *openchoreo.ResourceReleaseBinding {
	raw, _ := json.Marshal(values)
	return &openchoreo.ResourceReleaseBinding{Spec: openchoreo.ResourceReleaseBindingSpec{ResourceTypeEnvironmentConfigs: raw}}
}

type promoteFixture struct {
	svc      *Service
	rt       *fakeRTCatalog
	docs     *promoteDocs
	secrets  *fakeOrgSecrets
	promoter *fakePromoter
	plane    *MemoryValuePlane
}

func newPromoteFixture(resource *spec.ProjectResource, bindings map[string]*openchoreo.ResourceReleaseBinding) promoteFixture {
	f := promoteFixture{
		rt:       &fakeRTCatalog{},
		docs:     &promoteDocs{},
		secrets:  &fakeOrgSecrets{key: "vault/org-catalog/fx-rates"},
		promoter: &fakePromoter{resource: resource},
		plane:    NewMemoryValuePlane(),
	}
	f.svc = NewService(Deps{
		Design:            fakeDesign{comps: []spec.DesignComponent{{Name: "expenses-api", Dependencies: []spec.Dependency{{Kind: spec.DependencyKindExternal, Name: "fx-rates"}}}}},
		Projects:          fakeProjects{refs: []ProjectRef{{OrgID: "acme", ProjectID: "team-expenses"}}},
		RTCatalog:         f.rt,
		Bindings:          &fakeBindings{byName: bindings},
		Environments:      fakeEnvs{names: []string{"development", "production"}},
		OrgSecrets:        f.secrets,
		OrgResourceDocs:   f.docs,
		CatalogValuePlane: f.plane,
		Promoter:          f.promoter,
	})
	return f
}

func promoteRequest(rows ...gen.EnvValueWriteDTO) gen.PromoteExternalResourceRequest {
	return gen.PromoteExternalResourceRequest{
		ConsumptionInstructions: "Call /latest.json once per conversion; cache for an hour.",
		EnvValues:               rows,
	}
}

func apiStatus(t *testing.T, err error) int {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("want an API error, got %v", err)
	}
	return ae.Status
}

// The organization takes the project's block as it stands and adds what only
// it can: instructions and a value in every environment. The development
// value is carried over from the project's binding (plain from the binding,
// the secret by a vault copy); production is typed. The record lands with the
// project's document committed under the record's name, and the project's
// file is rewritten as a copy of that record.
func TestPromoteExternalResource_MakesTheRecordAndRewritesTheProject(t *testing.T) {
	f := newPromoteFixture(fxOwnResource(), map[string]*openchoreo.ResourceReleaseBinding{
		"team-expenses-fx-rates-development": projectBinding(map[string]string{"FX_BASE": "USD", openchoreo.SecretStorePathField: "vault/team-expenses/fx-rates-development"}),
	})
	view, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", promoteRequest(
		gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "prod-app-id"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
	))
	if err != nil {
		t.Fatalf("PromoteExternalResource: %v", err)
	}

	// The record: scope org, the project's provider and keys, the organization's instructions.
	if len(f.rt.defs) != 1 {
		t.Fatalf("types = %+v, want the one record", f.rt.defs)
	}
	rec := f.rt.defs[0]
	if !rec.Registered() || rec.Provider != "Open Exchange Rates" || len(rec.Config) != 2 || rec.ConsumptionInstructions == "" || rec.Description != "Live foreign-exchange rates." {
		t.Fatalf("record = %+v", rec)
	}
	wantSum := fmt.Sprintf("%x", sha256.Sum256([]byte(fxDocument)))
	if rec.Contract == nil || rec.Contract.Type != "openapi" || rec.Contract.Path != "fx-rates/openapi.yaml" {
		t.Fatalf("contract pointer = %+v, want the document under the record's name", rec.Contract)
	}
	if rec.Provenance == nil || rec.Provenance.SHA256 != wantSum || rec.Provenance.SourceURL != "https://docs.openexchangerates.org/reference" {
		t.Fatalf("provenance = %+v, want the document hash and the project's source URL", rec.Provenance)
	}
	if f.docs.committed["fx-rates/openapi.yaml"] != fxDocument {
		t.Fatalf("document not committed to the org docs repo: %+v", f.docs.committed)
	}

	// Values: development carried over, production typed; the secret moved vault to vault.
	if len(f.secrets.copied) != 1 || f.secrets.copied[0] != "vault/team-expenses/fx-rates-development -> fx-rates-development" {
		t.Fatalf("secret copies = %+v, want the development value carried over once", f.secrets.copied)
	}
	cells := map[string]EnvCell{}
	for _, c := range view.EnvCells {
		cells[c.Environment+"/"+c.Key] = c
	}
	if c := cells["development/FX_BASE"]; c.Status != "configured" || c.Value != "USD" || c.SecretStorePath != "vault/org-catalog/fx-rates" {
		t.Fatalf("development FX_BASE = %+v, want the binding's value and the org vault path", c)
	}
	if c := cells["production/FX_BASE"]; c.Status != "configured" || c.Value != "EUR" {
		t.Fatalf("production FX_BASE = %+v", c)
	}
	if view.Scope != openchoreo.ExternalResourceScopeOrg || len(view.Consumers) != 1 || view.Consumers[0].ComponentName != "expenses-api" {
		t.Fatalf("view = %+v", view)
	}

	// The project's file is rewritten from the SAME record the registry holds.
	if len(f.promoter.rewrote) != 1 {
		t.Fatalf("rewrites = %d, want one", len(f.promoter.rewrote))
	}
	copyRec := f.promoter.rewrote[0]
	if copyRec.Resource.Name != "fx-rates" || copyRec.Resource.ConsumptionInstructions == "" || copyRec.Resource.Contract == nil || copyRec.Resource.Contract.Path != "fx-rates/openapi.yaml" || copyRec.Document != fxDocument {
		t.Fatalf("copy source = %+v", copyRec)
	}
}

// Without a value to carry over and none typed, an environment is refused by
// name — nothing is committed, written or ensured.
func TestPromoteExternalResource_RefusesAnEnvironmentWithNoValue(t *testing.T) {
	f := newPromoteFixture(fxOwnResource(), nil)
	_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", promoteRequest(
		gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "prod"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
	))
	if apiStatus(t, err) != 400 || !strings.Contains(err.Error(), `"development"`) {
		t.Fatalf("want 400 naming development, got %v", err)
	}
	if len(f.rt.defs) != 0 || len(f.docs.committed) != 0 || len(f.promoter.rewrote) != 0 {
		t.Fatalf("a refused promote must leave nothing behind: types=%d docs=%d rewrites=%d", len(f.rt.defs), len(f.docs.committed), len(f.promoter.rewrote))
	}
}

// A typed secret beside a carried-over environment would be silently dropped
// by the vault copy, so the mix is refused.
func TestPromoteExternalResource_RefusesAPartialSecretBesideACarryOver(t *testing.T) {
	f := newPromoteFixture(fxOwnResource(), map[string]*openchoreo.ResourceReleaseBinding{
		"team-expenses-fx-rates-development": projectBinding(map[string]string{"FX_BASE": "USD", openchoreo.SecretStorePathField: "vault/x"}),
	})
	res := fxOwnResource()
	res.Definition.Resource.Config = append(res.Definition.Resource.Config, spec.ConfigKey{Key: "SECOND_SECRET", Secret: true})
	f.promoter.resource = res
	_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", promoteRequest(
		gen.EnvValueWriteDTO{Environment: "development", Key: "OPENEXCHANGERATES_APP_ID", Value: "typed"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "p"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "SECOND_SECRET", Value: "p2"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
	))
	if apiStatus(t, err) != 400 || !strings.Contains(err.Error(), "every secret value") {
		t.Fatalf("want 400 about every secret value, got %v", err)
	}
}

func TestPromoteExternalResource_Refusals(t *testing.T) {
	full := func() gen.PromoteExternalResourceRequest {
		return promoteRequest(
			gen.EnvValueWriteDTO{Environment: "development", Key: "OPENEXCHANGERATES_APP_ID", Value: "d"},
			gen.EnvValueWriteDTO{Environment: "development", Key: "FX_BASE", Value: "USD"},
			gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "p"},
			gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
		)
	}
	t.Run("already a copy of a record", func(t *testing.T) {
		f := newPromoteFixture(nil, nil)
		f.promoter.readErr = fmt.Errorf("%w: fx-rates", spec.ErrDependencyIsCopy)
		_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", full())
		if apiStatus(t, err) != 409 {
			t.Fatalf("want 409, got %v", err)
		}
	})
	t.Run("no such dependency", func(t *testing.T) {
		f := newPromoteFixture(nil, nil)
		f.promoter.readErr = fmt.Errorf("%w: ghost", spec.ErrDependencyNotFound)
		_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "ghost", full())
		if apiStatus(t, err) != 404 {
			t.Fatalf("want 404, got %v", err)
		}
	})
	t.Run("the organization already registered the name", func(t *testing.T) {
		f := newPromoteFixture(fxOwnResource(), nil)
		f.rt.defs = []openchoreo.ExternalResourceDefinition{{Name: "fx-rates", Scope: openchoreo.ExternalResourceScopeOrg}}
		_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", full())
		if apiStatus(t, err) != 409 || !strings.Contains(err.Error(), "reuse it instead") {
			t.Fatalf("want 409 pointing at reuse, got %v", err)
		}
	})
	t.Run("no provider chosen yet", func(t *testing.T) {
		res := fxOwnResource()
		res.Definition.Resource.Provider = ""
		f := newPromoteFixture(res, nil)
		_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", full())
		if apiStatus(t, err) != 400 || !strings.Contains(err.Error(), "provider") {
			t.Fatalf("want 400 about the provider, got %v", err)
		}
	})
	t.Run("instructions required", func(t *testing.T) {
		f := newPromoteFixture(fxOwnResource(), nil)
		req := full()
		req.ConsumptionInstructions = "  "
		_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", req)
		if apiStatus(t, err) != 400 {
			t.Fatalf("want 400, got %v", err)
		}
	})
}

// A project's own resource without a document still becomes a record — one
// with no contract pointer; the copy then reads needs-contract in the project,
// which is the truth.
func TestPromoteExternalResource_WithoutADocument(t *testing.T) {
	res := fxOwnResource()
	res.Definition.Resource.Contract = nil
	res.Document = ""
	f := newPromoteFixture(res, nil)
	_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", promoteRequest(
		gen.EnvValueWriteDTO{Environment: "development", Key: "OPENEXCHANGERATES_APP_ID", Value: "d"},
		gen.EnvValueWriteDTO{Environment: "development", Key: "FX_BASE", Value: "USD"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "p"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
	))
	if err != nil {
		t.Fatalf("PromoteExternalResource: %v", err)
	}
	if len(f.docs.committed) != 0 || f.rt.defs[0].Contract != nil {
		t.Fatalf("no document → no commit and no pointer: docs=%+v contract=%+v", f.docs.committed, f.rt.defs[0].Contract)
	}
}

// The Resources page lists a project's own resources after the records: one
// row per (project, name) with the project's block, its consumers, and per
// environment whether the project's binding holds a value — never the value.
func TestListExternalResources_ListsAProjectsOwnResources(t *testing.T) {
	own := spec.Dependency{
		Kind: spec.DependencyKindExternal, Name: "fx-rates", Description: "Live FX rates.", Provider: "Open Exchange Rates",
		Config:       []spec.ConfigKey{{Key: "OPENEXCHANGERATES_APP_ID", Secret: true}, {Key: "FX_BASE"}},
		Contract:     "openapi.yaml",
		ContractType: spec.DependencyContractTypeOpenAPI,
	}
	reused := spec.Dependency{Kind: spec.DependencyKindExternal, Name: "stripe", ResourceRef: "stripe", Provider: "Stripe"}
	svc := NewService(Deps{
		Design: fakeDesign{comps: []spec.DesignComponent{
			{Name: "expenses-api", Dependencies: []spec.Dependency{own, reused}},
			{Name: "expenses-web", Dependencies: []spec.Dependency{own}},
		}},
		Projects:     fakeProjects{refs: []ProjectRef{{OrgID: "acme", ProjectID: "team-expenses"}}},
		RTCatalog:    &fakeRTCatalog{defs: []openchoreo.ExternalResourceDefinition{{Name: "stripe", Scope: openchoreo.ExternalResourceScopeOrg, Provider: "Stripe"}}},
		Environments: fakeEnvs{names: []string{"development", "production"}},
		Bindings: &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
			"team-expenses-fx-rates-development": projectBinding(map[string]string{"FX_BASE": "USD", openchoreo.SecretStorePathField: "vault/x"}),
		}},
	})
	views, err := svc.ListExternalResources(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListExternalResources: %v", err)
	}
	if len(views) != 2 || views[0].Name != "stripe" || views[0].Scope != openchoreo.ExternalResourceScopeOrg {
		t.Fatalf("views = %+v, want the stripe record first and one project row", views)
	}
	row := views[1]
	if row.Name != "fx-rates" || row.Scope != openchoreo.ExternalResourceScopeProject || row.Project != "team-expenses" || row.Provider != "Open Exchange Rates" || len(row.Config) != 2 {
		t.Fatalf("project row = %+v", row)
	}
	if row.Contract == nil || row.Contract.Type != "openapi" || row.Contract.Path != "openapi.yaml" {
		t.Fatalf("project row contract = %+v", row.Contract)
	}
	if len(row.Consumers) != 2 {
		t.Fatalf("both components hold the one resource: consumers = %+v", row.Consumers)
	}
	status := map[string]string{}
	for _, c := range row.EnvCells {
		if c.Value != "" {
			t.Fatalf("a project row never carries a value: %+v", c)
		}
		status[c.Environment+"/"+c.Key] = c.Status
	}
	if status["development/FX_BASE"] != "configured" || status["development/OPENEXCHANGERATES_APP_ID"] != "configured" || status["production/FX_BASE"] != "unset" {
		t.Fatalf("cell status = %+v", status)
	}
	// The reused stripe is the record's consumer, not a row of its own.
	if len(views[0].Consumers) != 1 || views[0].Consumers[0].ComponentName != "expenses-api" {
		t.Fatalf("record consumers = %+v", views[0].Consumers)
	}
}

// A block that names a document the project does not hold would make a record
// with no contract behind the project's back; it is refused until the project
// provides the document.
func TestPromoteExternalResource_RefusesAMissingDocument(t *testing.T) {
	res := fxOwnResource()
	res.Document = ""
	f := newPromoteFixture(res, nil)
	_, err := f.svc.PromoteExternalResource(context.Background(), "acme", "team-expenses", "fx-rates", promoteRequest(
		gen.EnvValueWriteDTO{Environment: "development", Key: "OPENEXCHANGERATES_APP_ID", Value: "d"},
		gen.EnvValueWriteDTO{Environment: "development", Key: "FX_BASE", Value: "USD"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "OPENEXCHANGERATES_APP_ID", Value: "p"},
		gen.EnvValueWriteDTO{Environment: "production", Key: "FX_BASE", Value: "EUR"},
	))
	if apiStatus(t, err) != 400 || !strings.Contains(err.Error(), "openapi.yaml") {
		t.Fatalf("want 400 naming the missing document, got %v", err)
	}
	if len(f.rt.defs) != 0 || len(f.promoter.rewrote) != 0 {
		t.Fatalf("a refused promote must leave nothing behind")
	}
}
