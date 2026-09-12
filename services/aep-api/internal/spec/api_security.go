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

import "strings"

// ResolveAPISecurityEnabled is the single source of truth for "is JWT
// validation enforced on this component's HTTP endpoint?" — used by the
// trait emitter, the watcher, and any UI that surfaces the badge.
//
// Invariant: nil/empty `ExposesAPI` ⇒ false. The platform recognises
// only the documented `Auth` values; anything else also yields false.
func ResolveAPISecurityEnabled(comp DesignComponent) bool {
	if comp.ExposesAPI == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(comp.ExposesAPI.Auth)) {
	case authEndUserRequired, "service-required":
		return true
	}
	return false
}

// ResolveEndUserSignIn is the narrower question the gateway projection asks:
// does this component sit behind END-USER sign-in? It is the committed
// consequence derive_auth.go stamps from the sign-in dependency, and the same
// signal the openapi.yaml security gate judges the spec against — so the
// operation table the projection renders and the document the gate accepted are
// decided by one fact rather than two.
//
// `service-required` is deliberately NOT this: such a component is called by a
// sibling service with a token of its own, its spec declares no per-operation
// security, and projecting an operation table from it would replace a working
// whole-API jwt-auth with rows nothing authored.
func ResolveEndUserSignIn(comp DesignComponent) bool {
	if comp.ExposesAPI == nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(comp.ExposesAPI.Auth)) == authEndUserRequired
}
