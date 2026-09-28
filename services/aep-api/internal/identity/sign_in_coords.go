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

import "context"

// SignInCoords is the public half of a project's sign-in, read off the
// sign-in resource's resolved binding: the OAuth client a person signs in AS,
// the resource server the token is minted for, and the issuer that mints it.
// Empty fields mean "no sign-in resource, or its binding has not resolved".
type SignInCoords struct {
	ClientID string
	Resource string
	Issuer   string
}

// SignInCoordinates reads a project's SignInCoords. Provisioning owns the
// binding, so the implementation lives there and is wired late into the panel
// (PanelService.SetSignInCoordinates), which publishes the coordinates on the
// roles view for the platform's test app.
type SignInCoordinates interface {
	SignInCoordinates(ctx context.Context, orgID, projectID string) (SignInCoords, error)
}
