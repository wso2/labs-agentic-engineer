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
	ok := map[string]int{"500m": 500, "1": 1000, "0.25": 250, "1.5": 1500, "100m": 100, "3": 3000, "0.001": 1}
	for in, want := range ok {
		got, err := ParseCPUMillicores(in)
		if err != nil || got != want {
			t.Errorf("%q = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0m", "-1", "-5m", "1.5m", "abc", "1.2.3", "1 ", " 1", "1e3", "0.0001", "1Gi", "m"} {
		if _, err := ParseCPUMillicores(in); err == nil {
			t.Errorf("%q must be rejected", in)
		}
	}
}
