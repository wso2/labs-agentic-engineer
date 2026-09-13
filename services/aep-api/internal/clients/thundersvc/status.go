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

package thundersvc

// status.go — reading the HTTP status back out of a directory error.
//
// The directory surface returns errors carrying the status they came from, so a
// caller can branch without string-matching a message. Inside this package
// deleteIfPresent already does that for 404. This file exports the one question
// a caller OUTSIDE the package has to ask: "was that a rejection of my
// credential?"
//
// It also carries the ERROR-CODE half of the same idea: Thunder answers many
// distinct failures with one HTTP status (two different 409s on a resource
// server create, a 400 that means "still has children"), so the branch a caller
// needs is Thunder's own code from the response body, exposed here as a set of
// sentinels usable with errors.Is.
//
// IsAuthError exists for the per-(org, environment) client cache at the composition root.
// One client is built per identity provider and reused, so a rotated admin
// secret would otherwise leave a cached client failing every call until the
// process restarted. A 401/403 is the signal to drop the cached client and
// re-read the binding; anything else — a 404, a 409, a timeout — says nothing
// about the credential and must not evict a working one.

import (
	"encoding/json"
	"errors"
	"net/http"
)

// IsAuthError reports whether err is the identity provider REJECTING the
// caller's credential: 401 (not authenticated) or 403 (authenticated, not
// permitted).
//
// 403 counts because of how this platform's scope trap presents. ThunderID
// resolves a requested scope against a resource server; against the wrong one it
// drops `system` silently and issues a scope-less token, and every admin call
// then answers 403. That is a credential/registration problem exactly like a
// 401, and re-reading the binding is the same right response.
func IsAuthError(err error) bool {
	var status *statusError
	if !errors.As(err, &status) {
		return false
	}
	return status.code == http.StatusUnauthorized || status.code == http.StatusForbidden
}

// -- Thunder error codes ------------------------------------------------------

// The failures the resource-server / role surface has to branch on. Each is the
// sentinel for one Thunder error code, observed live against ThunderID 1.0.0
// (docs/design/draft/spikes/P1.md) and declared in the v1.0.0 `resource.yaml` /
// `role.yaml` contracts. Everything else stays an opaque error carrying the
// status and the body.
var (
	// ErrIdentifierConflict — 409 RES-1013: another resource server already
	// claims this identifier. The identifier is the access-token audience, so
	// this is "somebody else owns that audience", not "you already made this".
	ErrIdentifierConflict = errors.New("thunder: resource server identifier already in use")

	// ErrNameConflict — 409 RES-1004: another resource server in the OU already
	// has this name.
	ErrNameConflict = errors.New("thunder: resource server name already in use")

	// ErrHandleConflict — 409 RES-1014: a resource or action with this handle
	// already exists under the same PARENT. Uniqueness is per parent, not per
	// resource server: `claims:read` and `reports:read` coexist (P1 §1).
	ErrHandleConflict = errors.New("thunder: handle already in use under this parent")

	// ErrHasDependencies — 400 RES-1006: the resource server or resource still
	// has children. The delete order is actions → resources → resource server
	// (P1 §7); roles are NOT the dependency. DeleteResourceServerCascade walks it.
	ErrHasDependencies = errors.New("thunder: object still has dependencies")

	// ErrRoleNameConflict — 409 ROL-1004: a role with this name already exists
	// in the OU.
	ErrRoleNameConflict = errors.New("thunder: role name already in use")

	// ErrInvalidPermissions — 400 ROL-1012: one or more of the permission
	// strings in a role write name no action that exists. The ensure must
	// create the resource/action catalog BEFORE the roles that grant it.
	ErrInvalidPermissions = errors.New("thunder: one or more permissions do not exist")

	// ErrGrantNotPermitted — 403 SAZ-4030: the write would grant a permission
	// the caller does not itself hold. It has never been observed for the
	// platform's system client, whose `system` scope on the System resource
	// server bypasses the check (P1 §2) — it is mapped so that if it ever does
	// fire, the failure is named rather than guessed at. There is no fallback.
	//
	// It is a 403, so IsAuthError also answers true for it. That is deliberate:
	// the two are told apart by this sentinel, and the credential-rejection
	// remedy (drop the cached client, re-read the binding) is harmless applied
	// to a grant refusal.
	ErrGrantNotPermitted = errors.New("thunder: caller may not grant permissions it does not hold")
)

// ErrorCode returns Thunder's own error code for a failed call ("RES-1006"),
// or "" when the error did not come from a Thunder response or carried no code.
// A caller that has to SHOW the failure uses this; a caller that has to BRANCH
// on it uses errors.Is with a sentinel above.
func ErrorCode(err error) string {
	var status *statusError
	if !errors.As(err, &status) {
		return ""
	}
	return status.apiCode
}

// sentinelForCode maps a Thunder error code to its sentinel, nil when unmapped.
func sentinelForCode(code string) error {
	switch code {
	case "RES-1013":
		return ErrIdentifierConflict
	case "RES-1004":
		return ErrNameConflict
	case "RES-1014":
		return ErrHandleConflict
	case "RES-1006":
		return ErrHasDependencies
	case "ROL-1004":
		return ErrRoleNameConflict
	case "ROL-1012":
		return ErrInvalidPermissions
	case "SAZ-4030":
		return ErrGrantNotPermitted
	}
	return nil
}

// thunderErrorCode reads the `code` field out of a Thunder error body. Every
// error response in the 1.0.0 contracts is {code, message{key,defaultValue},
// description{…}}; a body that is not that shape yields "", which simply leaves
// the error unmapped.
func thunderErrorCode(raw []byte) string {
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	return body.Code
}
