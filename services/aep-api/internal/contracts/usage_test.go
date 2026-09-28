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

package contracts

import "testing"

// The host survives an Add only while every contributor that spent tokens
// agrees on it — the model's rule — so a project's card names the host it was
// billed by, and a mix names none.
func TestStampedUsageAddHostAgreement(t *testing.T) {
	spent := TokenUsage{InputTokens: 10}
	for _, tc := range []struct {
		name string
		a, b StampedUsage
		want string
	}{
		{"same host", StampedUsage{Tokens: spent, Host: "ollama.com"}, StampedUsage{Tokens: spent, Host: "ollama.com"}, "ollama.com"},
		{"mixed hosts", StampedUsage{Tokens: spent, Host: "ollama.com"}, StampedUsage{Tokens: spent, Host: "api.anthropic.com"}, ""},
		{"zero-token left keeps right", StampedUsage{Host: "api.anthropic.com"}, StampedUsage{Tokens: spent, Host: "ollama.com"}, "ollama.com"},
		{"zero-token right keeps left", StampedUsage{Tokens: spent, Host: "ollama.com"}, StampedUsage{}, "ollama.com"},
	} {
		if got := tc.a.Add(tc.b).Host; got != tc.want {
			t.Errorf("%s: host = %q, want %q", tc.name, got, tc.want)
		}
	}
}
