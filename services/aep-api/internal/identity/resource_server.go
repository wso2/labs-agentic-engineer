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

// resource_server.go — the two names a project's authorization objects are
// known by, derived from (org, project) and nothing else.
//
// They are pure functions because the parties that must agree on them never
// speak to each other: the ensure, the gateway trait, the generated SPA, the
// provisioning overlay and the gate ticket. Derivation is what keeps them in
// agreement without a lookup, which is why neither name may be passed around as
// free text.

import (
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/validate"
)

// identifierBase is the logical authority every project's resource-server
// identifier is built under.
//
// It is a URI that NEVER RESOLVES: no DNS record, no certificate, no endpoint.
// Binding it to a real hostname would make one project's token audience differ
// between the local plane and a cloud one.
const identifierBase = "https://aep.wso2.com"

// ResourceServerIdentifier is the project's resource-server identifier:
//
//	https://aep.wso2.com/orgs/<org>/projects/<project>
//
// Both handles are lowercased: a display-cased handle must not be able to mint a
// SECOND resource server differing only in case, which the directory would
// accept and whose tokens the gateway would then reject.
//
// It PANICS on a handle that is not a DNS-label slug. No caller could act on an
// error — every one of them holds a handle that already passed validate.Slug at
// the handler edge (projects.RequireSlug) — so a violation is a corrupt row or a
// caller that skipped the boundary, and a bad audience, once minted onto a CR, a
// gateway policy and the directory, is not correctable afterwards. The edge
// recovers panics into a 500, so the blast radius is one request.
func ResourceServerIdentifier(orgHandle, projectHandle string) string {
	return identifierBase +
		"/orgs/" + mustHandleSegment("organisation", orgHandle) +
		"/projects/" + mustHandleSegment("project", projectHandle)
}

// mustHandleSegment normalises one handle and refuses anything the identifier
// cannot safely carry. See ResourceServerIdentifier for why this is a panic.
func mustHandleSegment(kind, handle string) string {
	normalised := normaliseHandle(handle)
	if err := validate.Slug(normalised); err != nil {
		panic(fmt.Sprintf("identity: %s handle %q cannot appear in a resource-server identifier: %v",
			kind, handle, err))
	}
	return normalised
}

// RoleName is the project role's name on the directory: `<project>/<Role>`,
// slash and all, which the directory accepts verbatim.
//
// The prefix is what makes the role OWNABLE. Roles live in one flat namespace
// per organisation unit, so without it two projects declaring `Approver` would
// converge each other's permission set on every build. With it, the ensure
// selects exactly the roles carrying its own prefix.
//
// The role half is kept verbatim: it is the actor noun authored in
// security.json, and lowercasing it would make the directory's name disagree
// with the document's.
func RoleName(projectHandle, role string) string {
	return RoleNamePrefix(projectHandle) + strings.TrimSpace(role)
}

// RoleNamePrefix is the `<project>/` a role name of this project starts with —
// the selector for "roles this project owns" when reading the whole directory.
func RoleNamePrefix(projectHandle string) string {
	return normaliseHandle(projectHandle) + "/"
}

// normaliseHandle trims and lowercases, and does nothing else: a handle is
// `[a-z][a-z0-9-]*` by the time it reaches the platform, and rewriting anything
// more would hide a bad handle rather than let it fail visibly.
func normaliseHandle(handle string) string {
	return strings.ToLower(strings.TrimSpace(handle))
}
