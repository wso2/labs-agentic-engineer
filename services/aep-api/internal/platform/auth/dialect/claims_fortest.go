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

package dialect

import "strings"

// Test support: the claim names of the two IdPs AE ships presets for, so a
// test that needs "a dialect" does not spell them out. The values are pinned
// against the shared fixtures by TestForTestClaimsMatchTheSharedFixtures.
// Production code never calls these: its claims come from settings.

// ThunderClaimsForTest is how a ThunderID token names the org and the caller
// type. It sets no user value: any grant but client_credentials is a user's.
func ThunderClaimsForTest() Claims {
	return Claims{OrgHandle: "ouHandle", OrgID: "ouId", OrgName: "ouName", CallerType: "grant_type", Machine: "client_credentials"}
}

// AsgardeoClaimsForTest is how an Asgardeo token names the org and the caller
// type, with both caller type values known.
func AsgardeoClaimsForTest() Claims {
	return Claims{OrgHandle: "org_handle", OrgID: "org_id", OrgName: "org_name", CallerType: "aut", Machine: "APPLICATION", User: "APPLICATION_USER"}
}

// SameHandleForTest is the mapping of an org whose handle at the IdP is its
// namespace, as on a local ThunderID.
func SameHandleForTest(org string) OrgMapping {
	return OrgMapping{IDPHandle: org, PlatformHandle: org}
}

// MustForTest is New for claims and a mapping a test knows are complete; it
// panics otherwise.
func MustForTest(c Claims, m OrgMapping) Dialect {
	d, err := New(c, m)
	if err != nil {
		panic(err)
	}
	return d
}

// MustAnyOrgForTest returns a dialect that reads any org's token as that
// org, under the handle the token carries, with no mapping. Production
// cannot build one: New always takes the one org the install serves. It is
// for tests of the fences below the token check (a cycle, a repository or a
// project of another org), which need a second org's valid caller.
func MustAnyOrgForTest(c Claims) Dialect {
	if m := c.missing(); len(m) != 0 {
		panic("incomplete claims: " + strings.Join(m, ", "))
	}
	return Dialect{claims: c, anyOrgForTest: true}
}
