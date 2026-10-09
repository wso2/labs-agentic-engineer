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

// display_identity.go — who a user's work is credited to: the one name rule
// over the VERIFIED claims of the caller (auth.ClaimsFromContext), never a
// re-parse of the raw bearer.

import (
	"context"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// displayName is the name a user is shown under: the IdP's `name`, else
// given + family (a placeholder family name "User", which some IdPs fill in,
// is dropped), else the subject.
func displayName(name, given, family, subject string) string {
	if name != "" {
		return name
	}
	given, family = strings.TrimSpace(given), strings.TrimSpace(family)
	if strings.EqualFold(family, "user") {
		family = ""
	}
	if n := strings.TrimSpace(given + " " + family); n != "" {
		return n
	}
	return subject
}

// displayIdentity is the display name and email of verified claims; empty
// for no claims.
func displayIdentity(c *auth.Claims) (name, email string) {
	if c == nil {
		return "", ""
	}
	return displayName(c.Name, c.GivenName, c.FamilyName, c.Subject), c.Email
}

// creditFrom is the credit a pod turn started for the request's verified
// caller carries: the subject and its display identity. A context with no
// verified caller credits no one.
func creditFrom(ctx context.Context) aestudiotools.Credit {
	c := auth.ClaimsFromContext(ctx)
	if c == nil {
		return aestudiotools.Credit{}
	}
	name, email := displayIdentity(c)
	return aestudiotools.Credit{UserID: c.Subject, Name: name, Email: email}
}
