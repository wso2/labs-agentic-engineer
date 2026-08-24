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

package spec

import "testing"

func TestResolveAPISecurityEnabled(t *testing.T) {
	cases := []struct {
		name    string
		exposes *ExposesAPI
		want    bool
	}{
		{"nil block", nil, false},
		{"empty auth", &ExposesAPI{Auth: ""}, false},
		{"none", &ExposesAPI{Auth: "none"}, false},
		{"end-user-required", &ExposesAPI{Auth: "end-user-required"}, true},
		{"service-required", &ExposesAPI{Auth: "service-required"}, true},
		{"whitespace tolerant", &ExposesAPI{Auth: "  end-user-required  "}, true},
		{"unrecognised value defensive false", &ExposesAPI{Auth: "yes"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveAPISecurityEnabled(DesignComponent{ExposesAPI: c.exposes})
			if got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
