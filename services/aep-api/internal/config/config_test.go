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

// A TTL that, plus OpenChoreo's re-create lag (up to 13 min on Cloud), passes
// the 30-min apply cap would fail every re-dispatch `not_applied`: boot refuses
// it, naming the key and never the value.
func TestLoad_CodingAgentJobTTLApplyCapCoupling(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("CODING_AGENT_JOB_TTL", "17m")
	if _, err := Load(); err != nil {
		t.Fatalf("17m is the largest allowed TTL: %v", err)
	}
	t.Setenv("CODING_AGENT_JOB_TTL", "17m1s")
	_, err := Load()
	if err == nil {
		t.Fatal("a TTL past the apply-cap coupling must fail Load")
	}
	if !strings.Contains(err.Error(), "CODING_AGENT_JOB_TTL") || strings.Contains(err.Error(), "17m1s") {
		t.Fatalf("error must name the key, never the value: %v", err)
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

// setRequiredLoadEnv sets the env vars Load() needs to reach cfg.Validate()
// without a "configuration errors" failure, so a test can isolate one
// optional field's wiring.
func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "https://platform-api.example")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/aep")
	t.Setenv("JWKS_URL", "https://thunder.example/oauth2/jwks")
	t.Setenv("BFF_TASK_SIGNING_KEY", "-----BEGIN KEY-----\nx\n-----END KEY-----")
}

// TestLoad_SREHandoff pins the handoff's env wiring: a key is enabled, none
// is disabled, and a short key is refused.
func TestLoad_SREHandoff(t *testing.T) {
	key := strings.Repeat("a", 64)

	t.Run("a key is enabled", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_HANDOFF_TOKEN", key)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if !cfg.SREHandoff.Enabled() || cfg.SREHandoff.Token != key {
			t.Fatalf("SREHandoff = %+v, want enabled", cfg.SREHandoff)
		}
	})

	t.Run("no key is disabled", func(t *testing.T) {
		setRequiredLoadEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v, want nil", err)
		}
		if cfg.SREHandoff.Enabled() {
			t.Fatal("SREHandoff.Enabled() = true with nothing set")
		}
	})

	t.Run("a short key is refused", func(t *testing.T) {
		setRequiredLoadEnv(t)
		t.Setenv("SRE_HANDOFF_TOKEN", "too-short")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SRE_HANDOFF_TOKEN") {
			t.Fatalf("Load() error = %v, want an SRE_HANDOFF_TOKEN error", err)
		}
	})
}

func TestLoad_CodingAgentCPURequest(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("CODING_AGENT_CPU_REQUEST", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CodingAgentCPURequest != "500m" {
		t.Fatalf("default CodingAgentCPURequest = %q, want 500m", cfg.CodingAgentCPURequest)
	}
	t.Setenv("CODING_AGENT_CPU_REQUEST", "250m")
	if cfg, err = Load(); err != nil || cfg.CodingAgentCPURequest != "250m" {
		t.Fatalf("Load = %q, %v; want 250m", cfg.CodingAgentCPURequest, err)
	}
	for in, want := range map[string]string{"0.5": "500m", "1000m": "1", "0.25": "250m", "1": "1"} {
		t.Setenv("CODING_AGENT_CPU_REQUEST", in)
		if cfg, err = Load(); err != nil || cfg.CodingAgentCPURequest != want {
			t.Fatalf("%q -> %q, %v; want %q", in, cfg.CodingAgentCPURequest, err, want)
		}
	}
	t.Setenv("CODING_AGENT_CPU_REQUEST", "2066035336255469781")
	if _, err := Load(); err == nil {
		t.Fatal("an overflowing quantity must fail Load")
	}
	const secretish = "bogus-cpu-value-xyz"
	for _, bad := range []string{secretish, "0", "-1", "4", "3001m", "1.5.2", "0m"} {
		t.Setenv("CODING_AGENT_CPU_REQUEST", bad)
		_, err := Load()
		if err == nil {
			t.Fatalf("%q must fail Load", bad)
		}
		if !strings.Contains(err.Error(), "CODING_AGENT_CPU_REQUEST") {
			t.Fatalf("%q: error must name the key: %v", bad, err)
		}
		if len(bad) > 5 && strings.Contains(err.Error(), bad) {
			t.Fatalf("%q: error must not echo the value: %v", bad, err)
		}
	}
}
