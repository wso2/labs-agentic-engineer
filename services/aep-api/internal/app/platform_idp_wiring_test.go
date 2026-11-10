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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/dialect"
)

// stubThunderAdmin stands in for the Thunder admin client; no method is called.
type stubThunderAdmin struct{ thundersvc.Client }

// The platform IdP's kind picks how an org's two clients are provisioned:
// Thunder's admin client creates them, an Asgardeo install serves the
// pre-created ones, and no Thunder admin client is built for Asgardeo.
func TestOrgClientsFor(t *testing.T) {
	admin := stubThunderAdmin{}
	idp := config.PlatformIDPDefaults{OrgNamespace: "default", OrgPublisherSecret: "p", OrgStudioSecret: "s"}

	if got := orgClientsFor(dialect.KindThunder, config.PlatformIDPDefaults{}, admin); got != organization.OrgClients(admin) {
		t.Fatalf("thunder: got %T, want the admin client", got)
	}
	// No admin settings: a nil interface, so the service answers "unavailable"
	// instead of calling through a nil client.
	if got := orgClientsFor(dialect.KindThunder, config.PlatformIDPDefaults{}, nil); got != nil {
		t.Fatalf("thunder without an admin client: got %T, want nil", got)
	}

	got := orgClientsFor(dialect.KindAsgardeo, idp, nil)
	static, ok := got.(*organization.StaticOrgClients)
	if !ok {
		t.Fatalf("asgardeo: got %T, want the static registry", got)
	}
	app, err := static.EnsurePublisherApp(context.Background(), "default", "", "")
	if err != nil || app.ClientID != "aep-publisher-default" || app.Secret != "p" {
		t.Fatalf("asgardeo: the registry serves %+v, %v; want the configured org's client", app.ClientID, err)
	}

	if !usesThunderAdmin(dialect.KindThunder) || usesThunderAdmin(dialect.KindAsgardeo) {
		t.Fatal("the Thunder admin client is built for thunder only")
	}
}

// An Asgardeo install has no Thunder admin settings by design: their absence
// is not a degradation there, as it is on a Thunder install.
func TestComputeDegradations_ThunderAdminIsThunderOnly(t *testing.T) {
	has := func(d []Degradation) bool {
		for _, x := range d {
			if x.Capability == "idp-mutations" {
				return true
			}
		}
		return false
	}
	var cfg config.Config
	cfg.PlatformIDP.Kind = "thunder"
	if !has(computeDegradations(cfg, true)) {
		t.Fatal("thunder without admin settings: idp-mutations not reported")
	}
	cfg.PlatformIDP.Kind = "asgardeo"
	if has(computeDegradations(cfg, true)) {
		t.Fatal("asgardeo: idp-mutations reported, though the org's clients are pre-created")
	}
}
