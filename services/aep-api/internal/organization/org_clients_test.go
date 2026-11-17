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

package organization

import (
	"strings"
	"testing"
)

// The record answers for the one org's two pre-created clients: their client
// ids, derived from the org's name, and the stored secrets.
func TestOrgClients_AnswersTheOrgsTwoClients(t *testing.T) {
	c := NewOrgClients("default", orgPublisherSecret, orgStudioSecret)

	pub, err := c.Publisher("default")
	if err != nil {
		t.Fatalf("Publisher: %v", err)
	}
	if want := (OrgClient{ClientID: "aep-publisher-default", Secret: orgPublisherSecret}); pub != want {
		t.Fatalf("publisher client id = %q, want %q with the stored secret", pub.ClientID, want.ClientID)
	}

	studio, err := c.Studio("default")
	if err != nil {
		t.Fatalf("Studio: %v", err)
	}
	if want := (OrgClient{ClientID: "ae-studio-default", Secret: orgStudioSecret}); studio != want {
		t.Fatalf("studio client id = %q, want %q with the stored secret", studio.ClientID, want.ClientID)
	}
}

// Only the configured org has clients: no other org is handed a secret, and
// a record built without an org serves none.
func TestOrgClients_RefusesAnotherOrg(t *testing.T) {
	for name, tc := range map[string]struct {
		c   *OrgClients
		org string
	}{
		"another org":       {NewOrgClients("default", orgPublisherSecret, orgStudioSecret), "globex"},
		"no org asked":      {NewOrgClients("default", orgPublisherSecret, orgStudioSecret), ""},
		"no org configured": {NewOrgClients("", orgPublisherSecret, orgStudioSecret), ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := tc.c.Publisher(tc.org); err == nil || got.Secret != "" {
				t.Fatalf("publisher of %q: err=%v, want refused with no secret", tc.org, err)
			}
			if got, err := tc.c.Studio(tc.org); err == nil || got.Secret != "" {
				t.Fatalf("studio of %q: err=%v, want refused with no secret", tc.org, err)
			}
		})
	}
}

// A record built without a secret hands that client out to nobody: the error
// names the setting, never a secret.
func TestOrgClients_MissingSecretNamesTheSetting(t *testing.T) {
	_, perr := NewOrgClients("default", "", orgStudioSecret).Publisher("default")
	_, serr := NewOrgClients("default", orgPublisherSecret, "").Studio("default")
	for setting, err := range map[string]error{
		"PLATFORM_IDP_ORG_PUBLISHER_SECRET": perr,
		"PLATFORM_IDP_ORG_STUDIO_SECRET":    serr,
	} {
		if err == nil || !strings.Contains(err.Error(), setting) {
			t.Fatalf("err = %v, want one naming %s", err, setting)
		}
		if strings.Contains(err.Error(), orgPublisherSecret) || strings.Contains(err.Error(), orgStudioSecret) {
			t.Fatalf("an error carries a secret: %v", err)
		}
	}
}
