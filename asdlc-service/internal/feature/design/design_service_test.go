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

package design

import "testing"

// TestComponentNameFromDesignPath — only `components/<name>/design.md`
// triggers trait_sync; root design.md and openapi.yaml are ignored. Gate
// for the design-edit write site (componentNameFromDesignPath lives in
// design_service.go).
func TestComponentNameFromDesignPath(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantOK   bool
	}{
		{"components/svc/design.md", "svc", true},
		{"components/user-api/design.md", "user-api", true},
		{"design.md", "", false},
		{"components/svc/openapi.yaml", "", false},
		{"components//design.md", "", false},
		{"components/a/b/design.md", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		name, ok := componentNameFromDesignPath(c.in)
		if name != c.wantName || ok != c.wantOK {
			t.Errorf("componentNameFromDesignPath(%q) = (%q,%v), want (%q,%v)",
				c.in, name, ok, c.wantName, c.wantOK)
		}
	}
}
