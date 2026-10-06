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
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

var validKey = base64.StdEncoding.EncodeToString(make([]byte, 32))

// validConfig returns a Config that passes Validate — every required field set.
// Each test starts from it and mutates the one field it exercises.
func validConfig() Config {
	return Config{
		CredentialEncryptionKey: validKey,
		JWKSURL:                 "https://thunder.example/oauth2/jwks",
	}
}

func TestConfigValidate_CredentialEncryptionKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid 32-byte key", validKey, false},
		{"empty", "", true},
		{"not base64", "!!!not-base64!!!", true},
		{"16 bytes (too short)", base64.StdEncoding.EncodeToString(make([]byte, 16)), true},
		{"64 bytes (too long)", base64.StdEncoding.EncodeToString(make([]byte, 64)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			c.CredentialEncryptionKey = tt.key
			if err := c.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigValidate_RequiredFields pins the fail-fast contract: an empty JWKSURL
// is a boot error, not a soft-warn that surfaces later.
func TestConfigValidate_RequiredFields(t *testing.T) {
	t.Run("all required set is valid", func(t *testing.T) {
		if err := validConfig().Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})
	t.Run("missing JWKS_URL fails", func(t *testing.T) {
		c := validConfig()
		c.JWKSURL = ""
		if err := c.Validate(); err == nil {
			t.Fatal("Validate() = nil, want error for empty JWKS_URL")
		}
	})
}

// TestValidate_NoSigningKeyRequired: BFF token minting is gone, so a config
// with every other required field and no BFF_TASK_SIGNING_KEY boots.
func TestValidate_NoSigningKeyRequired(t *testing.T) {
	t.Setenv("BFF_TASK_SIGNING_KEY", "")
	t.Setenv("BFF_TASK_SIGNING_KEY_PATH", "")
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil with no signing key", err)
	}
}

// A settled coding Component is deleted only after two no-pod reads at least
// CODING_AGENT_SETTLE_GRACE apart; 5m unless set.
func TestLoad_CodingAgentSettleGrace(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("CODING_AGENT_SETTLE_GRACE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CodingAgentSettleGrace != 5*time.Minute {
		t.Fatalf("default CodingAgentSettleGrace = %v, want 5m", cfg.CodingAgentSettleGrace)
	}
	t.Setenv("CODING_AGENT_SETTLE_GRACE", "90s")
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CodingAgentSettleGrace != 90*time.Second {
		t.Fatalf("CodingAgentSettleGrace = %v, want 90s", cfg.CodingAgentSettleGrace)
	}
}

// U1: a finished coding-agent Job is kept 600s unless CODING_AGENT_JOB_TTL says
// otherwise.
func TestLoad_CodingAgentJobTTL(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("CODING_AGENT_JOB_TTL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CodingAgentJobTTL != 600*time.Second {
		t.Fatalf("default CodingAgentJobTTL = %v, want 600s", cfg.CodingAgentJobTTL)
	}
	t.Setenv("CODING_AGENT_JOB_TTL", "15m")
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CodingAgentJobTTL != 15*time.Minute {
		t.Fatalf("CodingAgentJobTTL = %v, want 15m", cfg.CodingAgentJobTTL)
	}
	t.Setenv("CODING_AGENT_JOB_TTL", "ten minutes")
	if _, err := Load(); err == nil {
		t.Fatal("an unparseable CODING_AGENT_JOB_TTL must fail Load")
	}
}

// Aep-api holds no static OpenBao token. OPENBAO_TOKEN is read by
// nothing, and the Kubernetes-auth login defaults to role aep-api on mount
// kubernetes with the pod's projected service-account token.
func TestConfig_NoOpenBaoTokenEnv(t *testing.T) {
	setMinimalEnv(t)
	const sentinel = "static-openbao-token-sentinel"
	t.Setenv("OPENBAO_TOKEN", sentinel)
	for _, k := range []string{"OPENBAO_AUTH_ROLE", "OPENBAO_AUTH_MOUNT", "OPENBAO_AUTH_TOKEN_PATH"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", cfg), sentinel) {
		t.Fatal("OPENBAO_TOKEN reached the config; aep-api must hold no static OpenBao token")
	}
	want := OpenBaoAuthConfig{Role: "aep-api", Mount: "kubernetes", TokenPath: "/var/run/secrets/kubernetes.io/serviceaccount/token"}
	if cfg.OpenBaoAuth != want {
		t.Fatalf("OpenBaoAuth = %+v; want %+v", cfg.OpenBaoAuth, want)
	}

	t.Setenv("OPENBAO_AUTH_ROLE", "other-role")
	t.Setenv("OPENBAO_AUTH_MOUNT", "k8s")
	t.Setenv("OPENBAO_AUTH_TOKEN_PATH", "/tmp/aep-api.token")
	if cfg, err = Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want = OpenBaoAuthConfig{Role: "other-role", Mount: "k8s", TokenPath: "/tmp/aep-api.token"}
	if cfg.OpenBaoAuth != want {
		t.Fatalf("OpenBaoAuth = %+v; want the env overrides %+v", cfg.OpenBaoAuth, want)
	}
}
