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

package connections

import (
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

func TestSchemaEqual(t *testing.T) {
	a := []models.ConfigKey{{Key: "URL", Secret: false}, {Key: "TOKEN", Secret: true}}
	cases := []struct {
		name string
		b    []models.ConfigKey
		want bool
	}{
		{"same order", []models.ConfigKey{{Key: "URL"}, {Key: "TOKEN", Secret: true}}, true},
		{"reordered", []models.ConfigKey{{Key: "TOKEN", Secret: true}, {Key: "URL"}}, true},
		{"secret flag differs", []models.ConfigKey{{Key: "URL"}, {Key: "TOKEN", Secret: false}}, false},
		{"extra key", []models.ConfigKey{{Key: "URL"}, {Key: "TOKEN", Secret: true}, {Key: "X"}}, false},
		{"missing key", []models.ConfigKey{{Key: "URL"}}, false},
		{"renamed key", []models.ConfigKey{{Key: "URL"}, {Key: "TOK", Secret: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SchemaEqual(a, tc.b); got != tc.want {
				t.Errorf("SchemaEqual=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestBumpSuffix(t *testing.T) {
	cases := []struct{ current, base, want string }{
		{"salesforce", "salesforce", "salesforce-2"},
		{"salesforce-2", "salesforce", "salesforce-3"},
		{"salesforce-9", "salesforce", "salesforce-10"},
		{"openweather", "openweather", "openweather-2"},
	}
	for _, tc := range cases {
		if got := bumpSuffix(tc.current, tc.base); got != tc.want {
			t.Errorf("bumpSuffix(%q,%q)=%q, want %q", tc.current, tc.base, got, tc.want)
		}
	}
}
