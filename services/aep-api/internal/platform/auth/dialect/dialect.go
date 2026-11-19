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
// names and values one IdP uses to say who a caller is. A verifier keeps a
// token's whole payload as Raw and asks the Dialect for the caller's org and
// whether the token is a machine's; nothing outside this package reads one of
// those claims by its IdP-specific name.
//
// The names and values are settings (PLATFORM_IDP_CLAIMS_*), read once at
// boot, with the one org the install serves (PLATFORM_IDP_ORG_HANDLE,
// PLATFORM_IDP_ORG_NAMESPACE). This package knows no IdP: what a ThunderID or an Asgardeo token looks
// like is configuration. Claims a standard names (sub, aud, iss, exp,
// client_id, scope, name, email) are not the dialect's: every verifier reads
// them by their standard names.
package dialect

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Claims names what the platform IdP calls the two things no standard
// defines: the caller's org, and whether a token was issued to a machine or
// to a user. Each claim is a top-level text claim of the token.
type Claims struct {
	// OrgHandle, OrgID and OrgName are the names of the claims that carry the
	// caller's org: the handle the IdP knows it by, the IdP's own id for it,
	// and a display name. OrgName is optional.
	OrgHandle string
	OrgID     string
	OrgName   string
	// CallerType is the name of the claim that says who a token was issued
	// to. Machine is the value that means a machine. User is the value that
	// means a user: when set, only those two values are accepted; when
	// empty, any value other than Machine is a user's.
	CallerType string
	Machine    string
	User       string
}

// missing lists the required settings c leaves empty, by the name each has
// in the IdP's configuration.
func (c Claims) missing() []string {
	var out []string
	for _, f := range []struct{ name, value string }{
		{"org.handle", c.OrgHandle},
		{"org.id", c.OrgID},
		{"caller_type.claim", c.CallerType},
		{"caller_type.machine", c.Machine},
	} {
		if strings.TrimSpace(f.value) == "" {
			out = append(out, f.name)
		}
	}
	return out
}

// Raw is a token's whole payload, as decoded. A verifier's wire struct keeps
// it beside its typed standard claims (Decode), and the Dialect reads the
// IdP's own claims out of it by their configured names.
type Raw map[string]any

// text is the top-level claim name of r when it is text, else "". A claim of
// any other JSON type is not read: the dialect's claims are text claims.
func (r Raw) text(name string) string {
	s, _ := r[name].(string)
	return s
}

// Decode unmarshals a token payload twice: into typed, a wire struct's typed
// claims, and into the Raw it returns. A wire struct calls it from its
// UnmarshalJSON, on a method-less alias of itself.
func Decode(payload []byte, typed any) (Raw, error) {
	if err := json.Unmarshal(payload, typed); err != nil {
		return nil, err
	}
	var raw Raw
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Encode is Decode's inverse: typed's claims with raw's laid over them. A
// wire struct calls it from its MarshalJSON, so a token signed from the
// struct carries the IdP's own claims too.
func Encode(typed any, raw Raw) ([]byte, error) {
	b, err := json.Marshal(typed)
	if err != nil || len(raw) == 0 {
		return b, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	for k, v := range raw {
		out[k] = v
	}
	return json.Marshal(out)
}

// Org is the caller's organization as the dialect reads it. Handle is the
// name the platform knows the org by; ID is the IdP's own id for it; Name is
// a display name. Any may be empty when the token carries no org.
type Org struct {
	Handle string
	ID     string
	Name   string
}

var (
	// ErrNoMachineClaim is a token that says nothing about whether it is a
	// user's or a machine's. A user-only route refuses it.
	ErrNoMachineClaim = errors.New("token carries no machine-token claim")
	// ErrUnknownMachineClaim is a token whose machine-token claim holds a
	// value the dialect does not know. It is refused like a missing one.
	ErrUnknownMachineClaim = errors.New("token's machine-token claim has an unknown value")
)

// OrgMapping names the one org the platform IdP serves, by both of its names.
// IDPHandle is what the IdP's tokens carry (a ThunderID organization unit's
// handle, an Asgardeo organization's handle). PlatformHandle is the org's
// OpenChoreo namespace, the name every consumer of Org.Handle knows it by.
// The two may be the same word.
type OrgMapping struct {
	IDPHandle      string
	PlatformHandle string
}

// Dialect answers the dialect questions by the configured claims, for the
// one org the IdP serves. The zero value reads nothing: no org, and no token
// placed as a user's or a machine's.
type Dialect struct {
	claims Claims
	org    OrgMapping
	// anyOrgForTest makes Org read any org's token as that org, under the
	// handle the token carries. Only test support sets it (claims_fortest.go):
	// aep-api's data is keyed by org and fenced by org below the token check,
	// and the tests of those fences need a second org's caller, which a
	// dialect built by New cannot read. New never sets it.
	anyOrgForTest bool
}

// New returns the dialect of c for the org m names. A required claim name or
// value left empty, or a mapping missing either handle, is an error naming
// it: a dialect that could not tell a machine from a user, or that had no org
// to accept, must not run.
func New(c Claims, m OrgMapping) (Dialect, error) {
	missing := c.missing()
	if strings.TrimSpace(m.IDPHandle) == "" {
		missing = append(missing, "the org's handle at the IdP")
	}
	if strings.TrimSpace(m.PlatformHandle) == "" {
		missing = append(missing, "the org's namespace")
	}
	if len(missing) != 0 {
		return Dialect{}, fmt.Errorf("the platform IdP's token dialect is incomplete: %s not set", strings.Join(missing, ", "))
	}
	return Dialect{claims: c, org: m}, nil
}

// Org reads the caller's org from the token's own claims. A token declaring
// exactly the IdP's handle of the install's org reads as that org, under the
// platform's name for it, with the IdP's id and display name kept. Any other
// token (another org of the IdP, no handle claim) reads as no org. Nothing
// stands in for the handle claim.
func (d Dialect) Org(r Raw) Org {
	if d.claims.OrgHandle == "" {
		return Org{}
	}
	handle := r.text(d.claims.OrgHandle)
	switch {
	case d.anyOrgForTest:
	case handle == "" || handle != d.org.IDPHandle:
		return Org{}
	default:
		handle = d.org.PlatformHandle
	}
	org := Org{Handle: handle, ID: r.text(d.claims.OrgID)}
	if d.claims.OrgName != "" {
		org.Name = r.text(d.claims.OrgName)
	}
	return org
}

// Machine reports whether the token was issued to a machine, by its caller
// type claim. A token without the claim is ErrNoMachineClaim. The machine
// value is a machine's. Where a user value is configured, only it is a
// user's and any third value is ErrUnknownMachineClaim: with the two values
// known, another is refused rather than taken for a user's. Where none is
// configured, any other value is a user's.
func (d Dialect) Machine(r Raw) (bool, error) {
	if d.claims.CallerType == "" {
		return false, ErrNoMachineClaim
	}
	switch v := r.text(d.claims.CallerType); {
	case v == "":
		return false, ErrNoMachineClaim
	case v == d.claims.Machine:
		return true, nil
	case d.claims.User == "" || v == d.claims.User:
		return false, nil
	default:
		return false, ErrUnknownMachineClaim
	}
}
