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

package spec_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeDependencyContracts records the dependency definition view's two writes and answers
// with whatever error the test scripts — the HTTP contract under test is the
// mapping, not the service.
type fakeDependencyContracts struct {
	collectErr error
	acceptErr  error
	collected  struct {
		org, project, dep, url string
		raw                    []byte
	}
	accepted struct{ org, project, dep, by, note string }
}

func (f *fakeDependencyContracts) CollectDependencyContract(_ context.Context, orgID, projectID, depName string, rawSpec []byte, specURL string) (string, error) {
	f.collected.org, f.collected.project, f.collected.dep, f.collected.raw, f.collected.url = orgID, projectID, depName, rawSpec, specURL
	if f.collectErr != nil {
		return "", f.collectErr
	}
	return "specs/design/dependencies/" + depName + "/openapi.yaml", nil
}

func (f *fakeDependencyContracts) AcceptDependencyAssumption(_ context.Context, orgID, projectID, depName, by, note string) error {
	f.accepted.org, f.accepted.project, f.accepted.dep, f.accepted.by, f.accepted.note = orgID, projectID, depName, by, note
	return f.acceptErr
}

func newDependencyHarness(t *testing.T, fake *fakeDependencyContracts) *componenttest.Harness {
	t.Helper()
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{Design: fake}),
	}})
}

const contractPath = "/api/v1/projects/web/dependencies/stripe/contract"
const assumptionPath = "/api/v1/projects/web/dependencies/stripe/assumption"

func TestProvideDependencyContract_CommitsAndAnswersWithThePath(t *testing.T) {
	t.Parallel()
	fake := &fakeDependencyContracts{}
	h := newDependencyHarness(t, fake)

	resp := h.AsOrg("acme").Post(contractPath, `{"url":"https://example.com/openapi.yaml"}`)
	if resp.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var out struct {
		Contract string `json:"contract"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatalf("body: %v", err)
	}
	if out.Contract != "specs/design/dependencies/stripe/openapi.yaml" {
		t.Fatalf("contract = %q", out.Contract)
	}
	if fake.collected.org != "acme" || fake.collected.project != "web" || fake.collected.dep != "stripe" || fake.collected.url != "https://example.com/openapi.yaml" || len(fake.collected.raw) != 0 {
		t.Fatalf("service call drifted: %+v", fake.collected)
	}

	// Inline content rides as bytes, and no URL.
	resp = h.AsOrg("acme").Post(contractPath, `{"content":"openapi: 3.0.3"}`)
	if resp.Code != 200 {
		t.Fatalf("content: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if string(fake.collected.raw) != "openapi: 3.0.3" || fake.collected.url != "" {
		t.Fatalf("content call drifted: %+v", fake.collected)
	}
}

func TestProvideDependencyContract_MapsServiceErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"unknown dependency", spec.ErrDependencyNotFound, 404},
		{"not an external dependency", spec.ErrDependencyWrongKind, 400},
		{"invalid document", spec.ErrInvalidSpec, 400},
		{"fetch failed", spec.ErrSpecFetchFailed, 400},
		{"design moved", spec.ErrSpecCommitConflict, 409},
		{"anything else", errors.New("disk on fire"), 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, &fakeDependencyContracts{collectErr: c.err})
			resp := h.AsOrg("acme").Post(contractPath, `{"url":"https://example.com/openapi.yaml"}`)
			if resp.Code != c.code {
				t.Fatalf("want %d, got %d body=%s", c.code, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestAcceptDependencyAssumption_RecordsTheActor(t *testing.T) {
	t.Parallel()
	fake := &fakeDependencyContracts{}
	h := newDependencyHarness(t, fake)

	resp := h.AsOrg("acme").Post(assumptionPath, `{"note":"auth guessed"}`)
	if resp.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if fake.accepted.org != "acme" || fake.accepted.project != "web" || fake.accepted.dep != "stripe" || fake.accepted.note != "auth guessed" {
		t.Fatalf("service call drifted: %+v", fake.accepted)
	}
	if fake.accepted.by == "" || fake.accepted.by == "unknown" {
		t.Fatalf("the accepting user must come from the verified token, got %q", fake.accepted.by)
	}
	// An empty body is fine: the note defaults server-side.
	if resp := h.AsOrg("acme").Post(assumptionPath, ``); resp.Code != 200 {
		t.Fatalf("empty body: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAcceptDependencyAssumption_MapsServiceErrors(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		code int
	}{
		{"unknown dependency", spec.ErrDependencyNotFound, 404},
		{"nothing to accept", spec.ErrDependencyNotAssumed, 409},
		{"design moved", spec.ErrSpecCommitConflict, 409},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, &fakeDependencyContracts{acceptErr: c.err})
			resp := h.AsOrg("acme").Post(assumptionPath, `{}`)
			if resp.Code != c.code {
				t.Fatalf("want %d, got %d body=%s", c.code, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestDependencyWrites_Unconfigured503(t *testing.T) {
	t.Parallel()
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{}),
	}})
	if resp := h.AsOrg("acme").Post(contractPath, `{"url":"https://x"}`); resp.Code != 503 {
		t.Fatalf("contract: want 503, got %d", resp.Code)
	}
	if resp := h.AsOrg("acme").Post(assumptionPath, `{}`); resp.Code != 503 {
		t.Fatalf("assumption: want 503, got %d", resp.Code)
	}
}
