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

package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateOrgID_Empty(t *testing.T) {
	if err := validateOrgID(""); !errors.Is(err, ErrOrgIDInvalid) {
		t.Fatalf("validateOrgID(\"\") = %v; want ErrOrgIDInvalid", err)
	}
}

func TestValidateOrgID_Reserved(t *testing.T) {
	cases := []string{"_platform", "_other", "_x"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			err := validateOrgID(c)
			if !errors.Is(err, ErrOrgIDInvalid) {
				t.Errorf("validateOrgID(%q) = %v; want ErrOrgIDInvalid", c, err)
			}
		})
	}
}

func TestValidateOrgID_Shape(t *testing.T) {
	cases := map[string]bool{
		"default":               true,
		"my-org":                true,
		"abc-123":               true,
		"a":                     true,
		"My-Org":                false, // uppercase not allowed
		"-leading-dash":         false,
		"trailing-dash-":        true, // matches DNS-label pattern (any non-leading char OK)
		"con tains space":       false,
		"slashes/in/path":       false,
		"under_score":           false,
		strings.Repeat("a", 64): false, // exceeds 63-char limit
		strings.Repeat("a", 63): true,
	}
	for input, valid := range cases {
		err := validateOrgID(input)
		if valid && err != nil {
			t.Errorf("validateOrgID(%q) = %v; want nil", input, err)
		}
		if !valid && err == nil {
			t.Errorf("validateOrgID(%q) = nil; want error", input)
		}
	}
}
