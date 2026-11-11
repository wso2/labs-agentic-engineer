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
// names and rules one IdP kind uses to say who a caller is. A verifier
// decodes a token's IdP-specific claims into Raw and asks the Dialect for the
// caller's org and whether the token is a machine's; nothing outside this
// package reads a claim by its IdP-specific name.
//
// Two kinds exist, thunder and asgardeo. The kind is a setting
// (PLATFORM_IDP_KIND), read once at boot.
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
// Thunder install never has to set; an unknown value is an error, so an
// install naming a kind this build does not know fails at boot rather than
// running on another kind's rules.
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
// Dialect reads only its own. It is meant to be embedded in a verifier's wire
// struct beside jwt.RegisteredClaims, so it must never tag a registered claim
// name (sub, aud, iss, exp, ...): two fields with one tag at the same depth
// make encoding/json drop both.
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
// fallback (Thunder's ouName, ouId) or is empty. The fallback serves legacy
// user tokens; a machine token, minted for a client the platform registered
// itself, is expected to declare its handle outright, and its verifiers
// check this flag.
type Org struct {
	Handle         string
	ID             string
	Name           string
	HandleDeclared bool
}

var (
	// ErrNoMachineClaim is a token that says nothing about whether it is a
	// user's or a machine's. A user-only route refuses it.
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
// and no token placed as a user's or a machine's.
type Dialect struct {
	kind Kind
	// single is set by WithSingleOrg: the IdP serves one org, named
	// idpHandle in its tokens and platformHandle by the platform.
	single         bool
	idpHandle      string
	platformHandle string
}

// ForKind returns the dialect of a parsed kind.
func ForKind(k Kind) Dialect {
	return Dialect{kind: k}
}

// WithSingleOrg returns d for an IdP that serves one org under a handle that
// is not the platform's name for it: an Asgardeo tenant names its org by the
// tenant's handle, while the platform knows the org by its OpenChoreo
// namespace. Org then reads a token that declares exactly idpHandle as the
// org platformHandle, and any other token as no org at all: another org of
// the same IdP, a token without the handle claim, or one whose handle came
// from a fallback claim.
//
// Only aep-api maps: it is the reader that names namespaces. The org's pod
// is told the IdP's handle as its own (AE_ORG_HANDLE), so its dialect twins
// compare the token's handle as it is.
func (d Dialect) WithSingleOrg(idpHandle, platformHandle string) Dialect {
	d.single = true
	d.idpHandle = idpHandle
	d.platformHandle = platformHandle
	return d
}

// Kind is the kind this dialect reads.
func (d Dialect) Kind() Kind {
	return d.kind
}

// Org reads the caller's org.
//
// Thunder: ouHandle, else ouName, else ouId is the handle (the console
// mirrors this precedence; a change lands on both sides at once), ouId is
// the ID and ouName the name.
//
// Asgardeo: org_handle is the handle, with no fallback (every token carries
// it), org_id the ID and org_name the name.
//
// Under WithSingleOrg the handle read this way is the IdP's; it is replaced
// by the platform's, and a token of any other org reads as no org.
func (d Dialect) Org(r Raw) Org {
	org := d.claimedOrg(r)
	if !d.single {
		return org
	}
	if !org.HandleDeclared || d.idpHandle == "" || org.Handle != d.idpHandle {
		return Org{}
	}
	org.Handle = d.platformHandle
	return org
}

// claimedOrg is the org as the token's own claims name it, by kind.
func (d Dialect) claimedOrg(r Raw) Org {
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
