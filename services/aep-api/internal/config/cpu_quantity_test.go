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

package config

import "testing"

func TestParseCPUMillicores(t *testing.T) {
	ok := map[string]int{"500m": 500, "1": 1000, "0.25": 250, "1.5": 1500, "100m": 100, "3": 3000, "0.001": 1, "3.001": 3001, "3001m": 3001, "999999999": 999999999000}
	for in, want := range ok {
		got, err := ParseCPUMillicores(in)
		if err != nil || got != want {
			t.Errorf("%q = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0m", "-1", "-5m", "1.5m", "abc", "1.2.3", "1 ", " 1", "1e3", "0.0001", "1Gi", "m", ".5", "1.", "2066035336255469781", "2066035336255469781m", "9999999999", "9999999999m", "99999999999999999999.5"} {
		if _, err := ParseCPUMillicores(in); err == nil {
			t.Errorf("%q must be rejected", in)
		}
	}
}

// The ceiling comparison runs on the parsed value, so the boundary cases and
// every spelling of 500m / 1 core must agree.
func TestCanonicalCPUAndCeiling(t *testing.T) {
	canon := map[string]string{"500m": "500m", "0.5": "500m", "0500m": "500m", "1": "1", "1000m": "1", "1.0": "1", "250m": "250m", "0.25": "250m", "3": "3000m", "3000m": "3000m"}
	for in, want := range canon {
		m, err := ParseCPUMillicores(in)
		if err != nil || CanonicalCPU(m) != want {
			t.Errorf("%q -> %q, %v; want %q", in, CanonicalCPU(m), err, want)
		}
	}
	for in, ok := range map[string]bool{"3000m": true, "3": true, "3.001": false, "3001m": false, "2066035336255469781": false} {
		m, err := ParseCPUMillicores(in)
		if within := err == nil && m <= CodingAgentCPUCeilingMillicores; within != ok {
			t.Errorf("%q within ceiling = %v, want %v", in, within, ok)
		}
	}
}
