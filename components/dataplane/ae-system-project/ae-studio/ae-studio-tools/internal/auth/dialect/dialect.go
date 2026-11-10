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

// Package dialect holds the token dialect of the platform IdP: the claim
// names and rules one IdP kind uses to say who a caller is. The verifier
// decodes a token's IdP-specific claims into Raw and asks the Dialect for the
// caller's org and whether the token is a machine's; nothing outside this
// package reads a claim by its IdP-specific name.
//
// It is the twin of aep-api's internal/platform/auth/dialect, kept in step by
// hand as verify.go is with @aep/platform-idp-auth. One kind exists,
// thunder. The kind is a setting (AE_IDP_KIND), read once at boot.
package dialect

import (
	"errors"
	"fmt"
	"strings"
)

// Kind names a platform IdP's token dialect.
type Kind string

// KindThunder is ThunderID: the org is an organization unit carried as
// ouHandle/ouName/ouId, and every token says its grant in grant_type.
const KindThunder Kind = "thunder"

// ParseKind reads the configured kind. Empty is thunder, the default a
// Thunder install never has to set; an unknown value is an error, so a pod
// naming a kind this build does not know fails at boot rather than running
// on another kind's rules.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case "", KindThunder:
		return KindThunder, nil
	default:
		return "", fmt.Errorf("unknown platform IdP kind %q (supported: %s)", s, KindThunder)
	}
}

// Raw is the IdP-specific claims a token may carry, as decoded. It is meant
// to be embedded in the verifier's wire struct beside jwt.RegisteredClaims,
// so it must never tag a registered claim name (sub, aud, iss, exp, ...):
// two fields with one tag at the same depth make encoding/json drop both.
type Raw struct {
	OuID      string `json:"ouId,omitempty"`
	OuName    string `json:"ouName,omitempty"`
	OuHandle  string `json:"ouHandle,omitempty"`
	GrantType string `json:"grant_type,omitempty"`
}

// Org is the caller's organization as the dialect reads it. Handle is the
// name the platform knows the org by; ID is the IdP's own id for it; Name is
// a display name. Any may be empty when the token carries no org.
//
// HandleDeclared is true when Handle came from the handle claim itself
// (Thunder: ouHandle), false when it was filled from a fallback (ouName,
// ouId) or is empty. The pod's gates require the declared handle: the
// fallback serves legacy user tokens on aep-api's edge, not here.
type Org struct {
	Handle         string
	ID             string
	Name           string
	HandleDeclared bool
}

// ErrNoMachineClaim is a token that says nothing about whether it is a
// user's or a machine's. The gates refuse it.
var ErrNoMachineClaim = errors.New("token carries no machine-token claim")

const thunderGrantClientCredentials = "client_credentials"

// Dialect answers the dialect questions for one kind.
type Dialect struct {
	kind Kind
}

// ForKind returns the dialect of a parsed kind.
func ForKind(k Kind) Dialect {
	return Dialect{kind: k}
}

// Org reads the caller's org. Thunder: ouHandle, else ouName, else ouId is
// the handle, ouId is the ID and ouName the name.
func (d Dialect) Org(r Raw) Org {
	handle := r.OuHandle
	if handle == "" {
		handle = r.OuName
	}
	if handle == "" {
		handle = r.OuID
	}
	return Org{Handle: handle, ID: r.OuID, Name: r.OuName, HandleDeclared: r.OuHandle != ""}
}

// Machine reports whether the token was issued to a machine. Thunder stamps
// grant_type on every token; client_credentials is a machine's, any other
// grant a user's. A token without the claim is ErrNoMachineClaim.
func (d Dialect) Machine(r Raw) (bool, error) {
	if r.GrantType == "" {
		return false, ErrNoMachineClaim
	}
	return r.GrantType == thunderGrantClientCredentials, nil
}
