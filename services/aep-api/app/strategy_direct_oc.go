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

package app

import (
	"context"

	"github.com/wso2/aep/aep-api/ocauth"
)

// DirectOCStrategy is the OSS / direct-OC default RequestAuthStrategy: it
// forwards the inbound console user's JWT to OpenChoreo as-is so OC applies
// that user's own RBAC, and falls back to the BFF's M2M service identity only
// when there is no user JWT to forward (webhook/dispatch-triggered calls that
// never carried one) or the call was explicitly marked ocauth.WithServiceIdentity
// (background watchers, MCP tool calls, validation's runner-context lookups —
// see those call sites for why each one cannot use the caller's token).
type DirectOCStrategy struct{}

// Decide implements ocauth.RequestAuthStrategy.
func (DirectOCStrategy) Decide(ctx context.Context) ocauth.AuthMode {
	if ocauth.IsServiceIdentity(ctx) || ocauth.GetAuthToken(ctx) == "" {
		return ocauth.AuthModeServiceM2M
	}
	return ocauth.AuthModeUserJWT
}

var _ ocauth.RequestAuthStrategy = DirectOCStrategy{}
