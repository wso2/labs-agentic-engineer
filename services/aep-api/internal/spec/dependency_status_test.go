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

func TestComputeDependencyStatus(t *testing.T) {
	twoSuggestions := []DependencySuggestion{
		{Name: "sendgrid-rest", Style: DependencyStyleRestAPI},
		{Name: "resend-sdk", Style: DependencyStyleSDK},
	}
	registered := RegistryHit{Registered: true}

	cases := []struct {
		name       string
		dep        Dependency
		registry   RegistryHit
		orgSvc     OrgServiceHit
		wantStatus string
		wantReason string
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
			name: "rule 1: a copy of a registered resource, with its contract copied, resolves",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe", ResourceRef: "stripe", Source: DependencySourceOrg,
				Provider: "Stripe", Contract: "openapi.yaml", ContractType: DependencyContractTypeOpenAPI, ContractOrigin: DependencyContractOriginRegistry},
			registry:   registered,
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "rule 1 then 4: a registered copy whose document has not landed needs its contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe", ResourceRef: "stripe", Source: DependencySourceOrg,
				Provider: "Stripe"},
			registry:   registered,
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "rule 2: a ref nobody registered is needs-input, whatever else the copy carries",
			dep: Dependency{Kind: DependencyKindExternal, Name: "currency-service", ResourceRef: "currency-service", Source: DependencySourceOrg,
				Provider: "Open Exchange Rates", Contract: "openapi.yaml", ContractType: DependencyContractTypeOpenAPI},
			registry:   RegistryHit{},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name:       "rule 2: a stub ref with the registry unreachable is needs-input, never resolved",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "currency-service", ResourceRef: "currency-service", Source: DependencySourceOrg},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name:       "rule 3: nothing identified is unresolved/needs-input",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "crm"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name:       "a registry hit on a project resource (no ref) changes nothing: the file decides",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "crm"},
			registry:   registered,
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name:       "rule 4: a provider with no contract file on disk is unresolved/needs-contract (no style rule any more)",
			dep:        Dependency{Kind: DependencyKindExternal, Name: "crm", Provider: "HubSpot"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "rule 4: an sdk contract with no manifest on disk is unresolved/needs-contract",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", ContractType: DependencyContractTypeSDK, Style: DependencyStyleSDK},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsContract,
		},
		{
			name: "openapi contract on disk resolves",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleRestAPI, ContractType: DependencyContractTypeOpenAPI, Contract: "openapi.yaml"},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "sdk manifest on disk resolves",
			dep: Dependency{Kind: DependencyKindExternal, Name: "stripe",
				Provider: "Stripe", Style: DependencyStyleSDK, ContractType: DependencyContractTypeSDK, SDK: "sdk.json"},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "a derived contract resolves with no permission asked",
			dep: Dependency{Kind: DependencyKindExternal, Name: "printer",
				Provider: "Star", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractDerived: true},
			wantStatus: DependencyStatusResolved,
		},
		{
			name: "no provider is unchosen even with a contract on disk",
			dep: Dependency{Kind: DependencyKindExternal, Name: "openweather",
				Style: DependencyStyleRestAPI, Contract: "openapi.yaml"},
			wantStatus: DependencyStatusUnresolved,
			wantReason: DependencyReasonNeedsInput,
		},
		{
			name: "rule 5: an agent-written contract nobody accepted is unresolved/needs-acceptance",
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
			gotStatus, gotReason := ComputeDependencyStatus(tc.dep, tc.registry, tc.orgSvc)
			if gotStatus != tc.wantStatus || gotReason != tc.wantReason {
				t.Errorf("ComputeDependencyStatus(%+v, registry=%+v, %+v) = (%q, %q), want (%q, %q)",
					tc.dep, tc.registry, tc.orgSvc, gotStatus, gotReason, tc.wantStatus, tc.wantReason)
			}
		})
	}
}

// TestComputeDependencyFlags pins the qualifiers on a RESOLVED external
// dependency — what the rail, the drawer and the deploy page show beside it —
// and that nothing else carries flags.
func TestComputeDependencyFlags(t *testing.T) {
	registered := RegistryHit{Registered: true, DocumentSHA256: "aaaa"}
	copied := Dependency{Kind: DependencyKindExternal, Name: "github", ResourceRef: "github", Source: DependencySourceOrg,
		Provider: "GitHub", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractType: DependencyContractTypeOpenAPI,
		Provenance: &ResourceProvenance{Registry: "github/openapi.yaml", SHA256: "aaaa"}}
	staleCopy := copied
	staleCopy.Provenance = &ResourceProvenance{Registry: "github/openapi.yaml", SHA256: "bbbb"}
	cases := []struct {
		name     string
		dep      Dependency
		registry RegistryHit
		want     []string
	}{
		{
			name: "a plain resolved rest-api has no flags",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "stripe", Provider: "Stripe", Style: DependencyStyleRestAPI, Contract: "openapi.yaml"},
		},
		{
			name:     "a copy of a registered resource → registered",
			dep:      copied,
			registry: registered,
			want:     []string{DependencyFlagRegistered},
		},
		{
			name:     "a copy whose document no longer matches the registry's → registered, stale",
			dep:      staleCopy,
			registry: registered,
			want:     []string{DependencyFlagRegistered, DependencyFlagStale},
		},
		{
			name:     "a registered record with no document never reads stale",
			dep:      staleCopy,
			registry: RegistryHit{Registered: true},
			want:     []string{DependencyFlagRegistered},
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
			name: "sdk with manifest and no API document → sdk-only",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "twilio", Provider: "Twilio", Style: DependencyStyleSDK, SDK: "sdk.json"},
			want: []string{DependencyFlagSDKOnly},
		},
		{
			name: "derived → derived",
			dep:  Dependency{Kind: DependencyKindExternal, Name: "star", Provider: "Star", Style: DependencyStyleRestAPI, Contract: "openapi.yaml", ContractDerived: true},
			want: []string{DependencyFlagDerived},
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
			name:     "a ref nobody registered has no flags: it is not resolved",
			dep:      copied,
			registry: RegistryHit{},
		},
		{
			name: "a component dependency never has flags",
			dep:  Dependency{Kind: DependencyKindComponent, Name: "cart"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeDependencyFlags(tc.dep, tc.registry)
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
