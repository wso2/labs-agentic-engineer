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

import "testing"

// TestComputeDependencyStatus is the single table test for the precedence
// table ComputeDependencyStatus owns — every external state, precedence
// ordering (including the registry-reuse rule beating every later rule), and
// the org-service 4-state regression (pinned here directly as a pure-function
// test; artifacts.resolveOrgServices' own tests pin the same outcomes through
// the read path unchanged).
func TestComputeDependencyStatus(t *testing.T) {
	twoSuggestions := []DependencySuggestion{
		{Name: "sendgrid-rest", Style: DependencyStyleRestAPI},
		{Name: "resend-sdk", Style: DependencyStyleSDK},
	}

	cases := []struct {
		name        string
		dep         Dependency
		registryHit bool
		orgSvc      OrgServiceHit
		wantStatus  string
		wantReason  string
	}{
		// --- component / platform-resource: always resolved -----------------
		{
			name:       "component kind is always resolved",
			dep:        Dependency{Kind: DependencyKindComponent, Name: "cart"},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "platform-resource kind is always resolved regardless of resourceType",
			dep: Dependency{Kind: DependencyKindPlatformResource, Name: "orders-db",
				ResourceType: "postgres-cnpg"},
			wantStatus: DependencyStatusResolved,
		},

		// --- org-service: unchanged 4-state model ----------------------------
		{
			name:       "org-service namespace-visible resolves regardless of exists",
			dep:        Dependency{Kind: DependencyKindOrgService, Name: "employee-api"},
			orgSvc:     OrgServiceHit{Visible: true, Exists: false},
			wantStatus: DependencyStatusResolved,
		},
		{
			name:       "org-service project-only (exists, not namespace-visible) is blocked/access-required",
			dep:        Dependency{Kind: DependencyKindOrgService, Name: "payroll-internal"},
			orgSvc:     OrgServiceHit{Visible: false, Exists: true},
			wantStatus: DependencyStatusBlocked,
			wantReason: DependencyReasonAccessRequired,
		},
		{
			name:       "org-service absent from the catalog is unresolved/not-found",
			dep:        Dependency{Kind: DependencyKindOrgService, Name: "ghost-svc"},
			orgSvc:     OrgServiceHit{},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNotFound,
		},

		// --- external: precedence order (first match wins) -------------------
		{
			name: "open suggestions never resolve on their own: the user chooses",
			dep: Dependency{Kind: DependencyKindExternal, Name: "email-provider",
				Suggestions: twoSuggestions},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name: "a registry hit resolves over open suggestions",
			dep: Dependency{Kind: DependencyKindExternal, Name: "email-provider",
				Suggestions: twoSuggestions},
			registryHit: true,
			wantStatus:  DependencyStatusResolved,
		},
		{
			name:        "rule 2: registry reuse resolves with nothing else known",
			dep:         Dependency{Kind: DependencyKindExternal, Name: "stripe"},
			registryHit: true,
			wantStatus:  DependencyStatusResolved,
		},
		{
			name:       "rule 2: a platform-stamped org copy resolves with nothing else known",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "stripe", Source: DependencySourceOrg},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "rule 2: registry reuse resolves ahead of the contract rules",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleRestAPI},
			registryHit: true,
			wantStatus:  DependencyStatusResolved,
		},
		{
			name:       "rule 3: nothing identified is unresolved/needs-input",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "crm"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name:       "rule 4: a provider named but no style is unresolved/needs-input",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "crm", Provider: "HubSpot"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name: "rule 5: sdk with no manifest on disk is unresolved/needs-contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleSDK},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "rule 6: rest-api with no contract file is unresolved/needs-contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleRestAPI},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "rule 6: graphql with no contract file is unresolved/needs-contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "shopify",
				Provider: "Shopify", Style: DependencyStyleGraphQL},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "rest-api WITH its contract resolves",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleRestAPI, Contract: "openapi.yaml"},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "sdk WITH its manifest resolves, contract or not",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleSDK, SDK: "sdk.json"},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "a derived contract resolves with no permission asked",
			dep: Dependency{Kind: DependencyKindExternal, Name: "printer",
				Provider: "Star", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractDerived: true},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "no provider is unchosen even with a contract named (hydration names the provider off the document on disk)",
			dep: Dependency{Kind: DependencyKindExternal, Name: "openweather",
				Style: DependencyStyleRestAPI, Contract: "openapi.yaml"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name: "rule 7: an agent-written contract nobody accepted is unresolved/needs-acceptance",
			dep: Dependency{Kind: DependencyKindExternal, Name: "dhl",
				Provider: "DHL", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractAssumed: true},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsAcceptance,
		},
		{
			name: "an accepted assumption resolves like any contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "dhl",
				Provider: "DHL", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractAssumed: true,
				Assumed: &DependencyAssumption{By: "admin", At: "2026-09-08T10:00:00Z"}},
			wantStatus: DependencyStatusResolved,
		},

		// --- defensive default ------------------------------------------------
		{
			name:       "an unrecognized kind fails safe to resolved",
			dep:        Dependency{Kind: "bogus-kind", Name: "mystery"},
			wantStatus: DependencyStatusResolved,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotReason := ComputeDependencyStatus(tc.dep, tc.registryHit, tc.orgSvc)
			if gotStatus != tc.wantStatus || gotReason != tc.wantReason {
				t.Errorf("ComputeDependencyStatus(%+v, registryHit=%v, %+v) = (%q, %q), want (%q, %q)",
					tc.dep, tc.registryHit, tc.orgSvc, gotStatus, gotReason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

// TestComputeDependencyFlags pins the qualifiers on a RESOLVED external
// dependency — what the rail, the drawer and the deploy page show beside it —
// and that nothing else carries flags.
func TestComputeDependencyFlags(t *testing.T) {
	cases := []struct {
		name        string
		dep         Dependency
		registryHit bool
		want        []string
	}{
		{
			name: "a plain resolved rest-api has no flags",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "stripe", Provider: "Stripe", Style: DependencyStyleRestAPI, Contract: "openapi.yaml"},
		},
		{
			name:        "registry hit → registered",
			dep:         Dependency{Kind: DependencyKindExternal, Name: "github"},
			registryHit: true,
			want:        []string{DependencyFlagRegistered},
		},
		{
			name: "org copy → registered",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "github", Source: DependencySourceOrg},
			want: []string{DependencyFlagRegistered},
		},
		{
			name: "accepted assumption → assumed",
			dep: Dependency{Kind: DependencyKindExternal, Name: "dhl", Provider: "DHL", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractAssumed: true,
				Assumed: &DependencyAssumption{By: "admin", At: "now"}},
			want: []string{DependencyFlagAssumed},
		},
		{
			name: "an acceptance echoed beside a real document is no flag",
			dep: Dependency{Kind: DependencyKindExternal, Name: "dhl", Provider: "DHL", Style: DependencyStyleRestAPI, Contract: "openapi.yaml",
				Assumed: &DependencyAssumption{By: "admin", At: "now"}},
		},
		{
			name: "sdk with manifest and no API slice → sdk-only",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "twilio", Provider: "Twilio", Style: DependencyStyleSDK, SDK: "sdk.json"},
			want: []string{DependencyFlagSDKOnly},
		},
		{
			name: "sdk with manifest AND API slice → no flag",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "stripe", Provider: "Stripe", Style: DependencyStyleSDK, SDK: "sdk.json", Contract: "openapi.yaml"},
		},
		{
			name: "assumed sdk-only carries both, in a fixed order",
			dep: Dependency{Kind: DependencyKindExternal, Name: "twilio", Provider: "Twilio", Style: DependencyStyleSDK, SDK: "sdk.json", ContractAssumed: true,
				Assumed: &DependencyAssumption{By: "admin", At: "now"}},
			want: []string{DependencyFlagAssumed, DependencyFlagSDKOnly},
		},
		{
			name: "an unresolved dependency has no flags even when assumed",
			dep: Dependency{Kind: DependencyKindExternal, Name: "dhl", Provider: "DHL", Style: DependencyStyleRestAPI,
				Assumed: &DependencyAssumption{By: "admin", At: "now"}},
		},
		{
			name: "a component dependency never has flags",
			dep:  Dependency{Kind: DependencyKindComponent, Name: "cart"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeDependencyFlags(tc.dep, tc.registryHit)
			if len(got) != len(tc.want) {
				t.Fatalf("flags = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("flags = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
