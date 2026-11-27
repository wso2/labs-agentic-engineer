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

package app

import "testing"

// setOSSEnv sets the variables config.Load requires.
func setOSSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV_FILE_PATH", "")
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "http://platform-api.invalid")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("JWKS_URL", "http://idp.invalid/jwks")
	// The platform IdP's claims and the one org it serves: Load requires them.
	t.Setenv("PLATFORM_IDP_CLAIMS_ORG_HANDLE", "ouHandle")
	t.Setenv("PLATFORM_IDP_CLAIMS_ORG_ID", "ouId")
	t.Setenv("PLATFORM_IDP_CLAIMS_CALLER_TYPE_CLAIM", "grant_type")
	t.Setenv("PLATFORM_IDP_CLAIMS_CALLER_TYPE_MACHINE", "client_credentials")
	t.Setenv("PLATFORM_IDP_ORG_HANDLE", "default")
	t.Setenv("PLATFORM_IDP_ORG_NAMESPACE", "default")
	t.Setenv("PLATFORM_IDP_ORG_PUBLISHER_SECRET", "p")
	t.Setenv("PLATFORM_IDP_ORG_STUDIO_SECRET", "s")
}

func TestNewOSSOptions_SecretsGoThroughOpenChoreo(t *testing.T) {
	setOSSEnv(t)
	opts, err := NewOSSOptions()
	if err != nil {
		t.Fatalf("NewOSSOptions: %v", err)
	}
	if opts.SecretsProvider != nil || !opts.openChoreoSecrets {
		t.Fatalf("injected = %v, openChoreoSecrets = %v; want secrets through OpenChoreo", opts.SecretsProvider, opts.openChoreoSecrets)
	}
}
