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

package codingagent

import "testing"

func TestPublisherTokenURLFromJWKS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"https://platform-idp-development.gateway.dev.cloud.wso2.com/oauth2/jwks", "https://platform-idp-development.gateway.dev.cloud.wso2.com/oauth2/token"},
		{"  https://idp.example/oauth2/jwks  ", "https://idp.example/oauth2/token"},
		{"http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks", "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token"},
		{"https://idp.example/oauth2/jwks/", "https://idp.example/oauth2/token"},
		{"", ""},
		{"https://idp.example/oauth2/token", ""},
		{"https://idp.example/jwks", ""},
	}
	for _, tc := range cases {
		if got := PublisherTokenURLFromJWKS(tc.in); got != tc.want {
			t.Errorf("PublisherTokenURLFromJWKS(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
