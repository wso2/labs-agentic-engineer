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
// hand as verify.go is with @aep/platform-idp-auth. Two kinds exist, thunder
// and asgardeo. The kind is a setting (AE_IDP_KIND), read once at boot.
package dialect

import (
	"errors"
	"fmt"
	"strings"
)

// Kind names a platform IdP's token dialect.
type Kind string

const (
	// KindThunder is ThunderID: the org is an organization unit carried as
	// ouHandle/ouName/ouId, and every token says its grant in grant_type.
	KindThunder Kind = "thunder"
	// KindAsgardeo is Asgardeo: the org is the tenant's organization carried
	// as org_handle/org_id/org_name, and every token says who it was issued
	// to in aut.
	KindAsgardeo Kind = "asgardeo"
)

// ParseKind reads the configured kind. Empty is thunder, the default a
// Thunder install never has to set; an unknown value is an error, so a pod
// naming a kind this build does not know fails at boot rather than running
// on another kind's rules.
func ParseKind(s string) (Kind, error) {
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case "", KindThunder:
		return KindThunder, nil
	case KindAsgardeo:
		return KindAsgardeo, nil
	default:
		return "", fmt.Errorf("unknown platform IdP kind %q (supported: %s, %s)", s, KindThunder, KindAsgardeo)
	}
}

// Raw is the IdP-specific claims a token may carry, as decoded: every kind's
// claims side by side, so one wire struct decodes a token of any kind and the
// Dialect reads only its own. It is meant to be embedded in the verifier's
// wire struct beside jwt.RegisteredClaims, so it must never tag a registered
// claim name (sub, aud, iss, exp, ...): two fields with one tag at the same
// depth make encoding/json drop both.
type Raw struct {
	// Thunder.
	OuID      string `json:"ouId,omitempty"`
	OuName    string `json:"ouName,omitempty"`
	OuHandle  string `json:"ouHandle,omitempty"`
	GrantType string `json:"grant_type,omitempty"`
	// Asgardeo.
	OrgID     string `json:"org_id,omitempty"`
	OrgName   string `json:"org_name,omitempty"`
	OrgHandle string `json:"org_handle,omitempty"`
	Aut       string `json:"aut,omitempty"`
}

// Org is the caller's organization as the dialect reads it. Handle is the
// name the platform knows the org by; ID is the IdP's own id for it; Name is
// a display name. Any may be empty when the token carries no org.
//
// HandleDeclared is true when Handle came from the handle claim itself
// (Thunder: ouHandle; Asgardeo: org_handle), false when it was filled from a
// fallback (Thunder's ouName, ouId) or is empty. The pod's gates require the
// declared handle: the fallback serves legacy user tokens on aep-api's edge,
// not here.
type Org struct {
	Handle         string
	ID             string
	Name           string
	HandleDeclared bool
}

var (
	// ErrNoMachineClaim is a token that says nothing about whether it is a
	// user's or a machine's. The gates refuse it.
	ErrNoMachineClaim = errors.New("token carries no machine-token claim")
	// ErrUnknownMachineClaim is a token whose machine-token claim holds a
	// value the dialect does not know. It is refused like a missing one.
	ErrUnknownMachineClaim = errors.New("token's machine-token claim has an unknown value")
)

const (
	thunderGrantClientCredentials = "client_credentials"
	asgardeoAutApplication        = "APPLICATION"
	asgardeoAutApplicationUser    = "APPLICATION_USER"
)

// Dialect answers the dialect questions for one kind. A Dialect of a kind
// this build does not know, the zero value included, reads nothing: no org,
// no token placed as a user's or a machine's, and no install-wide client
// accepted.
type Dialect struct {
	kind Kind
}

// ForKind returns the dialect of a parsed kind.
func ForKind(k Kind) Dialect {
	return Dialect{kind: k}
}

// Org reads the caller's org.
//
// Thunder: ouHandle, else ouName, else ouId is the handle, ouId is the ID
// and ouName the name.
//
// Asgardeo: org_handle is the handle, with no fallback (every token carries
// it), org_id the ID and org_name the name.
func (d Dialect) Org(r Raw) Org {
	switch d.kind {
	case KindThunder:
		handle := r.OuHandle
		if handle == "" {
			handle = r.OuName
		}
		if handle == "" {
			handle = r.OuID
		}
		return Org{Handle: handle, ID: r.OuID, Name: r.OuName, HandleDeclared: r.OuHandle != ""}
	case KindAsgardeo:
		return Org{Handle: r.OrgHandle, ID: r.OrgID, Name: r.OrgName, HandleDeclared: r.OrgHandle != ""}
	default:
		return Org{}
	}
}

// Machine reports whether the token was issued to a machine.
//
// Thunder stamps grant_type on every token; client_credentials is a
// machine's, any other grant a user's. A token without the claim is
// ErrNoMachineClaim.
//
// Asgardeo stamps aut on every token, with two values: APPLICATION is a
// machine's, APPLICATION_USER a user's. A token without the claim is
// ErrNoMachineClaim and any other value ErrUnknownMachineClaim: with only two
// values to know, a third is refused rather than taken for a user's.
func (d Dialect) Machine(r Raw) (bool, error) {
	switch d.kind {
	case KindThunder:
		if r.GrantType == "" {
			return false, ErrNoMachineClaim
		}
		return r.GrantType == thunderGrantClientCredentials, nil
	case KindAsgardeo:
		switch r.Aut {
		case "":
			return false, ErrNoMachineClaim
		case asgardeoAutApplication:
			return true, nil
		case asgardeoAutApplicationUser:
			return false, nil
		default:
			return false, ErrUnknownMachineClaim
		}
	default:
		return false, ErrNoMachineClaim
	}
}

// InstallClientOrgOK reports whether org, read from a token of the
// install-wide client (the one client every org's pod accepts aep-api's
// calls from), is what that client's token carries for a pod of podOrgID.
//
// Thunder registers the client outside any org, so its token carries no org
// id; one that does is some org's own client. Asgardeo stamps the tenant's
// org on every token, the install-wide client's included, so there the org id
// must be the pod's.
//
// aep-api's dialect has no such question: only the pod has a gate for this
// client.
func (d Dialect) InstallClientOrgOK(org Org, podOrgID string) bool {
	switch d.kind {
	case KindThunder:
		return org.ID == ""
	case KindAsgardeo:
		return podOrgID != "" && org.ID == podOrgID
	default:
		return false
	}
}
