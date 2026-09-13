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
// It exists for the per-(org, environment) client cache at the composition root.
// One client is built per identity provider and reused, so a rotated admin
// secret would otherwise leave a cached client failing every call until the
// process restarted. A 401/403 is the signal to drop the cached client and
// re-read the binding; anything else — a 404, a 409, a timeout — says nothing
// about the credential and must not evict a working one.

import (
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
