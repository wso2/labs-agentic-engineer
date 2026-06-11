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

package services

import (
	"strings"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// ResolveAPISecurityEnabled is the single source of truth for "is JWT
// validation enforced on this component's HTTP endpoint?" — used by the
// trait emitter, the watcher, and any UI that surfaces the badge.
//
// Invariant: nil/empty `ExposesAPI` ⇒ false. The platform recognises
// only the documented `Auth` values; anything else also yields false.
func ResolveAPISecurityEnabled(comp models.DesignComponent) bool {
	if comp.ExposesAPI == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(comp.ExposesAPI.Auth)) {
	case "end-user-required", "service-required":
		return true
	}
	return false
}

// ResolveAPISecurityCallerKind returns the auth flavor for sibling-CORS
// gating. Only `end-user-required` APIs should advertise SPA origins in
// their CORS allowlist (service-to-service APIs have no browser caller).
// Returns "" when API security is not enabled.
func ResolveAPISecurityCallerKind(comp models.DesignComponent) string {
	if comp.ExposesAPI == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(comp.ExposesAPI.Auth)) {
	case "end-user-required":
		return "end-user"
	case "service-required":
		return "service"
	}
	return ""
}
