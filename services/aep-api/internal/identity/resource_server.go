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
// They are pure functions, and that is the point. Both names are agreed on by
// parties that never speak to each other: the ensure creates the objects, the
// gateway trait states the audience it will accept, the generated SPA sends the
// `resource` parameter, the provisioning overlay fills the OAuth client's CR
// even when the directory row does not exist yet, and the gate ticket publishes
// them for the validation agent. Derivation is what keeps those five in
// agreement without a lookup, and it is why nothing may pass a resource-server
// identifier or a role name around as free text.

import (
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/validate"
)

// identifierBase is the logical authority every project's resource-server
// identifier is built under.
//
// It is a URI, and it NEVER RESOLVES: no DNS record, no certificate, no
// endpoint. That is deliberate. The identifier's job is to be a globally unique,
// environment-independent name — it is the access token's `aud`, the audience
// the gateway's jwt-auth policy checks, and the `resource` indicator the SPA
// asks for — and binding it to a hostname would make the same project's token
// audience differ between the local plane and a cloud one, which is exactly the
// bug an opaque identifier cannot have.
const identifierBase = "https://aep.wso2.com"

// ResourceServerIdentifier is the project's resource-server identifier:
//
//	https://aep.wso2.com/orgs/<org>/projects/<project>
//
// Uniqueness comes from the two handles, which are already unique in their own
// scopes, so the derived name is unique on any directory and stable for the life
// of the project. Both are lowercased: handles are lowercase by construction,
// and a caller that passes a display-cased one must not be able to mint a SECOND
// resource server whose identifier differs from the first only in case — the
// directory would accept it, and every token minted afterwards would carry an
// audience the gateway rejects.
//
// It PANICS on a handle that is not a DNS-label slug, and that is deliberate.
// The result is a token AUDIENCE: it is written onto the OAuth client's CR, into
// the gateway's jwt-auth policy and into the `resource` the SPA asks for, and it
// is stored on the directory as the resource server's permanent identity. A
// handle carrying a slash or a space would mint an audience nobody can correct
// afterwards and would be silently accepted by every one of those parties. There
// is no caller that could act on an error either: every one of them has a handle
// that came through the platform's own boundary check (validate.Slug, applied to
// orgHandle and projectName at the handler edge — see projects.RequireSlug), so
// a violation here is a corrupt row or a new caller that skipped the boundary,
// and both are defects rather than conditions. The edge recovers panics into a
// 500 (obs.RecovererOnPanic), so the blast radius is one request.
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
// slash and all, which the directory accepts verbatim (measured, spike P1 §2).
//
// The prefix is what makes the role OWNABLE. Roles live in one flat namespace
// per organisation unit, so without it two projects declaring `Approver` would
// converge each other's permission set on every build. With it, the ensure can
// list every role, select exactly the ones carrying its own prefix, and delete
// the ones the design dropped — safely, because no other project can have
// created them.
//
// The role half is kept verbatim: it is a PRD actor noun authored in
// security.json (`Approver`), the console shows it, and lowercasing it here
// would make the directory's name disagree with the document's.
func RoleName(projectHandle, role string) string {
	return RoleNamePrefix(projectHandle) + strings.TrimSpace(role)
}

// RoleNamePrefix is the `<project>/` a role name of this project starts with —
// the selector for "roles this project owns" when reading the whole directory.
func RoleNamePrefix(projectHandle string) string {
	return normaliseHandle(projectHandle) + "/"
}

// normaliseHandle folds a handle to the one form the derived names use. It
// trims and lowercases, and does nothing else: a handle is `[a-z][a-z0-9-]*` by
// the time it reaches the platform, so there is nothing else to fix, and
// silently rewriting anything more would hide a bad handle rather than let it
// fail visibly at the directory.
func normaliseHandle(handle string) string {
	return strings.ToLower(strings.TrimSpace(handle))
}
