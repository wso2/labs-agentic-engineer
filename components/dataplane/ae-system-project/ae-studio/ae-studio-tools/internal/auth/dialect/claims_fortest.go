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

// MustForTest is New for claims a test knows are complete; it panics
// otherwise.
func MustForTest(c Claims) Dialect {
	d, err := New(c)
	if err != nil {
		panic(err)
	}
	return d
}
