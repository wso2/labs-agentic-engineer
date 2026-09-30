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
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

func TestResolveEffectiveSRE(t *testing.T) {
	bearer := &modelconn.Connection{Format: modelconn.FormatOpenAICompatible, Host: "api.openai.com", Model: "gpt-5.4", AuthScheme: modelconn.AuthBearer}
	anthropic := &modelconn.Connection{Format: modelconn.FormatAnthropic, Host: modelconn.AnthropicHost, Model: "claude-sonnet-5", AuthScheme: modelconn.AuthXAPIKey}
	override := &OrgSreModelConnection{BaseURL: "https://api.openai.com/v1", Host: "api.openai.com", Model: "gpt-5.4-mini"}
	cases := []struct {
		name      string
		ov        *OrgSreModelConnection
		ovKey     string
		org       *modelconn.Connection
		wantSrc   SRESource
		wantKey   string
		wantModel string
	}{
		{"override wins over a capable org connection", override, "sk-override-xxxx", bearer, SRESourceOverride, "sk-override-xxxx", "gpt-5.4-mini"},
		{"capable org connection is inherited", nil, "", bearer, SRESourceOrganization, "sk-org-xxxxxxxx", "gpt-5.4"},
		{"anthropic org connection leaves it unconfigured", nil, "", anthropic, SRESourceNone, "", ""},
		{"no connection at all", nil, "", nil, SRESourceNone, "", ""},
		{"override without key bytes is ignored", override, "", anthropic, SRESourceNone, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveEffectiveSRE(tc.ov, tc.ovKey, tc.org, "sk-org-xxxxxxxx")
			if got.Source != tc.wantSrc || got.Key != tc.wantKey || got.Conn.Model != tc.wantModel {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
