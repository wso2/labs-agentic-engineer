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

package build_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery/build"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The name is the user's, and it is the tag that gets cut (console ADR-0030):
// the build hands it to the tagger verbatim.
func TestBuild_CutsTheVersionTheUserNamed(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: spec.SpecSaveApproved, Tag: "payments-v2", Version: 2}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	resp := newHarness(t, svc).AsOrg("acme").
		Post("/api/v1/projects/shop/build", `{"version":"payments-v2"}`)

	if resp.Code != 200 {
		t.Fatalf("build: got %d body=%s", resp.Code, resp.Body.String())
	}
	if tagger.version != "payments-v2" {
		t.Errorf("tagger asked to cut %q, want payments-v2 — the name is not the platform's to change", tagger.version)
	}
	if out := decodeBody[gen.BuildResponse](t, resp.Body.String()); out.Tag != "payments-v2" {
		t.Errorf("tag = %q, want payments-v2", out.Tag)
	}
	// The milestone is the version's, so it carries the version's NAME.
	if got := spy.milestones(); len(got) != 1 || got[0] != "payments-v2" {
		t.Fatalf("milestones created = %v, want [payments-v2]", got)
	}
}

// A name somebody already used comes back as a conflict — never as a build on
// a quietly different tag.
func TestBuild_ANameAlreadyInUseIsAConflict(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{err: fmt.Errorf("%w: %q", spec.ErrVersionNameTaken, "v2")}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	resp := newHarness(t, svc).AsOrg("acme").
		Post("/api/v1/projects/shop/build", `{"version":"v2"}`)

	if resp.Code != 409 {
		t.Fatalf("build with a taken name: got %d, want 409 — body=%s", resp.Code, resp.Body.String())
	}
	if len(spy.milestones()) != 0 {
		t.Errorf("a refused name claimed a milestone: %v", spy.milestones())
	}
}

// A name a tag cannot hold is refused before anything is claimed.
func TestBuild_AMalformedNameIsABadRequest(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{err: fmt.Errorf("%w: %s", spec.ErrVersionNameInvalid,
		"use letters, digits, dot, dash and underscore")}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	resp := newHarness(t, svc).AsOrg("acme").
		Post("/api/v1/projects/shop/build", `{"version":"my version"}`)

	if resp.Code != 400 {
		t.Fatalf("build with a malformed name: got %d, want 400 — body=%s", resp.Code, resp.Body.String())
	}
}

// A build that names nothing takes the platform's suggestion — the ordinary
// click, and every build the platform starts for itself.
func TestBuild_NoNameLeavesTheSuggestionToTheTagger(t *testing.T) {
	spy := newPlanSpy()
	tagger := &fakeTagger{res: &spec.SpecSaveResult{Status: spec.SpecSaveApproved, Tag: "v4", Version: 4}}
	svc := withPlanPath(newSvc(fakeRepos{}, tagger), spy)

	code, body := postBuild(t, svc, "shop")

	if code != 200 {
		t.Fatalf("build: got %d body=%s", code, body)
	}
	if tagger.version != "" {
		t.Errorf("tagger asked to cut %q, want the empty suggestion", tagger.version)
	}
}

// pfVersions is the version half's port, faked for the HTTP-surface test.
type pfVersions struct{ facts spec.VersionFacts }

func (f pfVersions) BuildVersionFacts(context.Context, string, string) (spec.VersionFacts, error) {
	return f.facts, nil
}

// The version facts have to reach the WIRE, not just the service: the dialog
// reads them off this response, and a mapping that drops them leaves the field
// and the change list empty with nothing failing anywhere.
func TestGetPreflight_CarriesTheVersionFactsOnTheWire(t *testing.T) {
	pfSvc := build.NewPreflightService(build.PreflightDeps{
		Design: pfDesign{},
		Status: pfStatus{},
		Versions: pfVersions{facts: spec.VersionFacts{
			CurrentVersion:   "payments-v2",
			SuggestedVersion: "v3",
			Changes: []spec.VersionChange{
				{Name: "orders-api", Kind: spec.VersionChangeKindComponent, State: spec.VersionChangeChanged},
				{Name: "postgres-cnpg", Kind: spec.VersionChangeKindResource, State: spec.VersionChangeNew},
			},
		}},
	})
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{PreflightSvc: pfSvc})),
	}})

	resp := h.AsOrg("acme").Get("/api/v1/projects/shop/build/preflight")

	if resp.Code != 200 {
		t.Fatalf("preflight: got %d body=%s", resp.Code, resp.Body.String())
	}
	pf := decodeBody[gen.BuildPreflight](t, resp.Body.String())
	if pf.CurrentVersion != "payments-v2" || pf.SuggestedVersion != "v3" {
		t.Errorf("versions = %q/%q, want payments-v2/v3", pf.CurrentVersion, pf.SuggestedVersion)
	}
	if len(pf.Changes) != 2 {
		t.Fatalf("changes = %+v, want two rows", pf.Changes)
	}
	if pf.Changes[0].Name != "orders-api" || pf.Changes[0].Kind != "component" || pf.Changes[0].State != "changed" {
		t.Errorf("first change = %+v", pf.Changes[0])
	}
	// Name, Kind and State are mapped independently, so each is asserted: a
	// dropped Name would otherwise pass this test.
	if pf.Changes[1].Name != "postgres-cnpg" || pf.Changes[1].Kind != "platform-resource" ||
		pf.Changes[1].State != "new" {
		t.Errorf("second change = %+v", pf.Changes[1])
	}
}

// An unchanged tree says so on the wire, and carries no list.
func TestGetPreflight_UnchangedTreeOnTheWire(t *testing.T) {
	pfSvc := build.NewPreflightService(build.PreflightDeps{
		Design:   pfDesign{},
		Status:   pfStatus{},
		Versions: pfVersions{facts: spec.VersionFacts{CurrentVersion: "v2", SuggestedVersion: "v3", SpecUnchanged: true}},
	})
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{PreflightSvc: pfSvc})),
	}})

	resp := h.AsOrg("acme").Get("/api/v1/projects/shop/build/preflight")

	pf := decodeBody[gen.BuildPreflight](t, resp.Body.String())
	if !pf.SpecUnchanged || len(pf.Changes) != 0 {
		t.Errorf("preflight = %+v, want specUnchanged with no changes", pf)
	}
}
