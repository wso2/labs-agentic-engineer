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

import (
	"slices"
	"testing"
)

// setMinimalEnv sets the variables Load requires and clears AE_STUDIO_*.
func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV_FILE_PATH", "")
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "http://platform-api.invalid")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("JWKS_URL", "http://idp.invalid/jwks")
	t.Setenv("BFF_TASK_SIGNING_KEY", "test-placeholder-not-a-key")
	for _, k := range []string{
		"AE_STUDIO_IMAGE_DESIGN_AGENT", "AE_STUDIO_IMAGE_COLLAB", "AE_STUDIO_IMAGE_STUDIO_TOOLS",
		"AE_STUDIO_GATEWAY_HOST", "AE_STUDIO_PUBLIC_SCHEME", "AE_STUDIO_PUBLIC_PORT_SUFFIX",
		"AE_STUDIO_LISTENER_NAME", "AE_STUDIO_CONSOLE_ORIGINS", "AE_STUDIO_IDP_ISSUER",
		"AE_STUDIO_IDP_JWKS_URL", "AE_STUDIO_IDP_TOKEN_URL", "AE_STUDIO_IDP_USER_AUDIENCES",
		"AE_STUDIO_AEP_API_BASE_URL", "AE_STUDIO_INTERNAL_CLIENT_ID", "AE_STUDIO_INTERNAL_CLIENT_SECRET",
		"AE_STUDIO_RUNTIME_CLASS_NAME", "AE_STUDIO_CILIUM", "AE_STUDIO_EXTRA_EGRESS",
		"AE_STUDIO_STORAGE_SIZE_LIMIT", "AE_STUDIO_STORAGE_EPHEMERAL_REQUEST",
		"AE_STUDIO_STORAGE_BUDGET_BYTES", "AE_STUDIO_PULL_SECRET_KEY", "AE_STUDIO_PULL_SECRET_PROPERTY",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_WithoutAEStudio(t *testing.T) {
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("boot must not need AE_STUDIO_*: %v", err)
	}
	m := cfg.AEStudio.Missing()
	for _, k := range []string{"AE_STUDIO_IMAGE_DESIGN_AGENT", "AE_STUDIO_GATEWAY_HOST", "AE_STUDIO_INTERNAL_CLIENT_ID", "AE_STUDIO_CONSOLE_ORIGINS"} {
		if !slices.Contains(m, k) {
			t.Errorf("Missing() lacks %s: %v", k, m)
		}
	}
	if slices.Contains(m, "AE_STUDIO_INTERNAL_CLIENT_SECRET") {
		t.Error("the AE-only secret is not needed to Ensure (it is aep-api's own client, used from phase 2)")
	}
	if string(cfg.AEStudio.ExtraEgress) != "[]" || cfg.AEStudio.ListenerName != "https" || cfg.AEStudio.Storage.SizeLimit != "3Gi" || cfg.AEStudio.Storage.EphemeralRequest != "1Gi" {
		t.Errorf("defaults wrong: %+v", cfg.AEStudio)
	}
}

func TestLoad_AEStudioParsed(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("AE_STUDIO_CONSOLE_ORIGINS", "http://console.ae.localhost:8080, http://localhost:8090")
	t.Setenv("AE_STUDIO_EXTRA_EGRESS", `[{"ports":[{"port":8090}]}]`)
	t.Setenv("AE_STUDIO_CILIUM", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AEStudio.ConsoleOrigins) != 2 || !cfg.AEStudio.Cilium || cfg.AEStudio.Storage.BudgetBytes != 2147483648 || cfg.AEStudio.PublicScheme != "https" {
		t.Fatalf("%+v", cfg.AEStudio)
	}
	t.Setenv("AE_STUDIO_EXTRA_EGRESS", `{not json`)
	if _, err := Load(); err == nil {
		t.Fatal("malformed AE_STUDIO_EXTRA_EGRESS must fail boot (it is a deployment typo, not an absent feature)")
	}
}
