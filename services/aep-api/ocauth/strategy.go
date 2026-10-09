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

package ocauth

import "context"

// AuthMode is the credential class the OC transport applies to one request.
// Strategies must not suggest retrying with a different class.
type AuthMode int

const (
	AuthModeNone       AuthMode = iota
	AuthModeUserJWT             // pass through inbound user JWT; no impersonation header
	AuthModeServiceM2M          // AuthProvider token; impersonation header iff resolver non-nil
)

// RequestAuthStrategy decides which credential class to use for an OC request.
// Decide is pure: no I/O. Called once per OC request from context.
type RequestAuthStrategy interface {
	Decide(ctx context.Context) AuthMode
}

// AuthProvider is the auth-token contract the OC client depends on. Lets callers
// swap a production M2M token source for a fake in tests. Method signatures
// intentionally match oauth.TokenProvider so it satisfies the interface as-is.
type AuthProvider interface {
	Token() (string, error)
	Invalidate()
}
