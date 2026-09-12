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

package identity

// resource_server_test.go — the derivation rules, pinned literally.
//
// These are cheap tests of two one-line functions, and they are worth writing
// because the VALUE is the contract, not the code: the same strings are
// produced independently by the gateway trait, the generated SPA, the client
// overlay and the gate ticket, and a change here that looks harmless silently
// breaks every token minted afterwards. Spelling the expected strings out in
// full — rather than rebuilding them from the same constants — is the point.

import (
	"strings"
	"testing"
)

func TestResourceServerIdentifier(t *testing.T) {
	cases := []struct {
		name    string
		org     string
		project string
		want    string
	}{
		{
			name: "the design's own example", org: "acme", project: "expense-tracker",
			want: "https://aep.wso2.com/orgs/acme/projects/expense-tracker",
		},
		{
			name: "a second project of the same org is a different resource server",
			org:  "acme", project: "vendor-portal",
			want: "https://aep.wso2.com/orgs/acme/projects/vendor-portal",
		},
		{
			name: "the same project handle under another org does not collide",
			org:  "globex", project: "expense-tracker",
			want: "https://aep.wso2.com/orgs/globex/projects/expense-tracker",
		},
		{
			// A display-cased handle must not be able to mint a SECOND resource
			// server: the directory would accept the near-duplicate identifier and
			// every token issued afterwards would carry an audience the gateway
			// rejects.
			name: "display casing folds to the one identifier", org: "Acme", project: "Expense-Tracker",
			want: "https://aep.wso2.com/orgs/acme/projects/expense-tracker",
		},
		{
			name: "surrounding whitespace never reaches the identifier",
			org:  " acme ", project: "\texpense-tracker\n",
			want: "https://aep.wso2.com/orgs/acme/projects/expense-tracker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResourceServerIdentifier(tc.org, tc.project); got != tc.want {
				t.Fatalf("ResourceServerIdentifier(%q, %q) = %q, want %q", tc.org, tc.project, got, tc.want)
			}
		})
	}
}

// A handle the identifier cannot safely carry is refused LOUDLY rather than
// woven into a token audience.
//
// There is no recovering from the alternative: the identifier is written onto
// the OAuth client, into the gateway's expected audience and onto the directory
// as the resource server's permanent identity, and every one of those parties
// accepts whatever string it is handed. A slash would silently invent an extra
// path segment; a space or an empty handle would produce an audience no token
// can match. Every caller's handle has already passed the platform's boundary
// check (validate.Slug), so reaching this is a defect, not a condition.
func TestResourceServerIdentifierRefusesAHandleItCannotCarry(t *testing.T) {
	for _, tc := range []struct{ name, org, project string }{
		{"an empty org", "", "expense-tracker"},
		{"an empty project", "acme", ""},
		{"a slash in the project handle", "acme", "expense/tracker"},
		{"a space in the org handle", "ac me", "expense-tracker"},
		{"path traversal", "acme", ".."},
		{"a handle starting with a hyphen", "acme", "-tracker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatalf("ResourceServerIdentifier(%q, %q) returned an identifier for a handle that is not a slug",
						tc.org, tc.project)
				}
			}()
			_ = ResourceServerIdentifier(tc.org, tc.project)
		})
	}
}

// The identifier is a name, not an address: it must never grow a trailing slash
// (an `aud` comparison is exact) and must stay on the logical authority rather
// than whatever host the environment happens to run on.
func TestResourceServerIdentifierIsALogicalURI(t *testing.T) {
	got := ResourceServerIdentifier("acme", "expense-tracker")
	if strings.HasSuffix(got, "/") {
		t.Fatalf("identifier %q ends in a slash; an audience is compared exactly", got)
	}
	if !strings.HasPrefix(got, "https://aep.wso2.com/") {
		t.Fatalf("identifier %q left the logical authority; it would differ per environment", got)
	}
}

func TestRoleName(t *testing.T) {
	cases := []struct {
		name    string
		project string
		role    string
		want    string
	}{
		{
			name: "the design's own example", project: "expense-tracker", role: "Approver",
			want: "expense-tracker/Approver",
		},
		{
			// The role half is a PRD actor noun the console renders and
			// security.json declares; folding its case would make the directory
			// disagree with the document.
			name: "the role's case is the document's", project: "expense-tracker", role: "Finance Approver",
			want: "expense-tracker/Finance Approver",
		},
		{
			name: "the project half is a handle and folds", project: "Expense-Tracker", role: "Approver",
			want: "expense-tracker/Approver",
		},
		{
			name: "surrounding whitespace is not part of a name", project: "expense-tracker", role: "  Approver ",
			want: "expense-tracker/Approver",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RoleName(tc.project, tc.role); got != tc.want {
				t.Fatalf("RoleName(%q, %q) = %q, want %q", tc.project, tc.role, got, tc.want)
			}
		})
	}
}

// The prefix is the ensure's ownership test — "these are the roles this project
// may converge and delete" — so it has to be exactly the head of every name the
// same project derives, and it has to end at the separator.
func TestRoleNamePrefixSelectsExactlyThisProjectsRoles(t *testing.T) {
	prefix := RoleNamePrefix("expense-tracker")
	if prefix != "expense-tracker/" {
		t.Fatalf("RoleNamePrefix = %q, want %q", prefix, "expense-tracker/")
	}
	mine := RoleName("expense-tracker", "Approver")
	if !strings.HasPrefix(mine, prefix) {
		t.Fatalf("role %q does not carry its own project's prefix %q", mine, prefix)
	}
	// A project whose handle merely starts with another's must not be selected
	// by it: the separator is what makes the prefix a boundary.
	neighbour := RoleName("expense-tracker-v2", "Approver")
	if strings.HasPrefix(neighbour, prefix) {
		t.Fatalf("role %q was selected by another project's prefix %q", neighbour, prefix)
	}
}
