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

func TestLoadIncludesAEPAPIInternalClientConfig(t *testing.T) {
	t.Setenv("AEP_API_INTERNAL_BASE_URL", "http://aep-api:8080")
	t.Setenv("AEP_API_INTERNAL_BEARER", "token")

	cfg := Load()

	if cfg.AEPAPIInternalBaseURL != "http://aep-api:8080" {
		t.Fatalf("AEPAPIInternalBaseURL = %q", cfg.AEPAPIInternalBaseURL)
	}
	if cfg.AEPAPIInternalBearer != "token" {
		t.Fatalf("AEPAPIInternalBearer = %q", cfg.AEPAPIInternalBearer)
	}
}
