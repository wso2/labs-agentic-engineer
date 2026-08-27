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

package validation

import (
	"context"
	"errors"
	"testing"
)

// The cycle the tests resolve, and an org that does not own it.
const (
	theCycle   = "cycle-1"
	theOrg     = "org"
	strangeOrg = "org-other"
)

// fakeCycleLocator answers only for the (org, cycle) pairs it holds, which is how
// the repository behaves: the org is part of the WHERE, so another org's cycle is
// indistinguishable from one that does not exist. A locator keyed on the cycle
// alone would let a broken tenant fence pass.
type fakeCycleLocator struct {
	// projects maps org handle → cycle id → project.
	projects map[string]map[string]string
	asked    []string
}

func (f *fakeCycleLocator) LookupCycleProject(_ context.Context, orgHandle, cycleID string) (string, bool, error) {
	f.asked = append(f.asked, orgHandle+"/"+cycleID)
	projectID, ok := f.projects[orgHandle][cycleID]
	return projectID, ok, nil
}

// locatorFor is a locator that resolves theCycle under theOrg and nothing else.
func locatorFor(projectID string) *fakeCycleLocator {
	return &fakeCycleLocator{projects: map[string]map[string]string{
		theOrg: {theCycle: projectID},
	}}
}

type fakeEndpoints struct {
	eps    []ComponentEndpoint
	called bool
}

func (f *fakeEndpoints) ResolveEndpoints(_ context.Context, _, _ string) ([]ComponentEndpoint, error) {
	f.called = true
	return f.eps, nil
}

func TestValidationContext_ResolvesEndpoints(t *testing.T) {
	locator := locatorFor("proj")
	svc := NewContextService(
		locator,
		&fakeEndpoints{eps: []ComponentEndpoint{
			{Component: "hello-web", URL: "https://web.example"},
			{Component: "hello-api", URL: "https://api.example"},
		}},
	)
	resp, err := svc.ValidationContext(context.Background(), theCycle, theOrg)
	if err != nil {
		t.Fatalf("ValidationContext: %v", err)
	}
	if len(resp.Endpoints) != 2 || resp.Endpoints[0].Component != "hello-web" {
		t.Errorf("endpoints = %+v", resp.Endpoints)
	}
	// Credentials are not bundled in the context, and no callback serves them:
	// the agent reads a test user's login from the roles gate ticket (ADR-0022).
	if resp.CriteriaPath != criteriaFilePath {
		t.Errorf("criteriaPath = %q; want %q", resp.CriteriaPath, criteriaFilePath)
	}
	// The id the runner presents is a CYCLE id, resolved under the verified org.
	// Looking it up anywhere else is the bug this replaces.
	if len(locator.asked) != 1 || locator.asked[0] != theOrg+"/"+theCycle {
		t.Errorf("locator asked %v; want one lookup of %q under %q", locator.asked, theCycle, theOrg)
	}
}

func TestValidationContext_UnknownCycleIs404(t *testing.T) {
	svc := NewContextService(locatorFor("proj"), &fakeEndpoints{})
	_, err := svc.ValidationContext(context.Background(), "cycle-nope", theOrg)
	if !errors.Is(err, ErrCycleNotFound) {
		t.Fatalf("want ErrCycleNotFound (→ 404), got %v", err)
	}
}

// The tenant fence: another org naming a real cycle id gets the SAME answer as
// one naming nothing, so a cross-tenant probe cannot tell them apart. And the
// endpoint resolver must never run — identity is decided first.
func TestValidationContext_AnotherOrgsCycleIs404AndResolvesNothing(t *testing.T) {
	endpoints := &fakeEndpoints{}
	svc := NewContextService(locatorFor("proj"), endpoints)

	_, err := svc.ValidationContext(context.Background(), theCycle, strangeOrg)
	if !errors.Is(err, ErrCycleNotFound) {
		t.Fatalf("want ErrCycleNotFound for another org's cycle, got %v", err)
	}
	if endpoints.called {
		t.Error("resolved endpoints for an unowned cycle — identity must gate the endpoint read")
	}
}
